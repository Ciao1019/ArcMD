package plugin

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

type memSecrets map[string][]byte

func (m memSecrets) Get(key string) ([]byte, error) {
	v, ok := m[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return slices.Clone(v), nil
}

func TestManifestSettingsValidation(t *testing.T) {
	valid := `{
	  "id": "dev.arcmd.cloud-sync",
	  "name": "Cloud Sync",
	  "version": "0.1.0",
	  "runtime": "wasm",
	  "main": "plugin.wasm",
	  "permissions": ["secrets", "network", "space:read", "space:write"],
	  "contributes": {
	    "commands": [{"id": "sync", "title": "Sync Now", "primary": true, "inSettings": true}],
	    "settings": [
	      {"id": "provider", "title": "Provider", "type": "segmented", "options": [{"value": "s3", "label": "S3"}]},
	      {"id": "token", "title": "Token", "type": "secret", "when": {"field": "provider", "equals": "s3"}}
	    ]
	  }
	}`
	if _, err := ParseManifest([]byte(valid)); err != nil {
		t.Fatalf("valid manifest failed: %v", err)
	}

	noSecretPerm := strings.Replace(valid, `"secrets", `, "", 1)
	if _, err := ParseManifest([]byte(noSecretPerm)); err == nil || !strings.Contains(err.Error(), "secrets") {
		t.Fatalf("expected secret permission error, got %v", err)
	}

	noOptions := `{
	  "id": "dev.arcmd.bad",
	  "name": "Bad",
	  "version": "0.1.0",
	  "runtime": "wasm",
	  "main": "plugin.wasm",
	  "contributes": {"settings": [{"id": "mode", "title": "Mode", "type": "select"}]}
	}`
	if _, err := ParseManifest([]byte(noOptions)); err == nil || !strings.Contains(err.Error(), "options") {
		t.Fatalf("expected options error, got %v", err)
	}
}

func TestConfigStoreAndSecretIsolation(t *testing.T) {
	dir := t.TempDir()
	store := &ConfigStore{Dir: dir}
	m := &Manifest{
		ID:          "dev.arcmd.sync",
		Name:        "Sync",
		Version:     "0.1.0",
		Runtime:     RuntimeWASM,
		Main:        "plugin.wasm",
		Permissions: []Permission{PermSecrets},
		Contributes: Contributes{
			Settings: []SettingField{
				{ID: "endpoint", Title: "Endpoint", Type: "string", Default: "https://s3.example.com"},
				{ID: "secretKey", Title: "Secret", Type: "secret"},
			},
		},
	}

	cfg, err := store.Load(m)
	if err != nil {
		t.Fatal(err)
	}
	if cfg["endpoint"] != "https://s3.example.com" {
		t.Fatalf("default not applied: %+v", cfg)
	}

	if err := store.Save(m, map[string]any{
		"endpoint":  "https://r2.example.com",
		"secretKey": "must-not-be-saved-here",
	}); err != nil {
		t.Fatal(err)
	}
	p, _ := store.SettingsPath(m.ID)
	raw, _ := os.ReadFile(p)
	if strings.Contains(string(raw), "must-not-be-saved-here") {
		t.Fatal("secret leaked into settings.json")
	}

	sk, err := SecretKey(m.ID, "secretKey")
	if err != nil {
		t.Fatal(err)
	}
	if sk == "ai-api-key" || !strings.HasPrefix(sk, "plugin-dev.arcmd.sync-") {
		t.Fatalf("unexpected secret key %q", sk)
	}
	if _, err := SecretKey(m.ID, "../ai-api-key"); err == nil {
		t.Fatal("unsafe field ID accepted")
	}
}

func TestWASMHostDispatchPermissionsAndNetwork(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	spaceDir := t.TempDir()
	os.WriteFile(filepath.Join(spaceDir, "Note.md"), []byte("hello"), 0o644)

	var plainHits atomic.Int32
	plainSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plainHits.Add(1)
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "plain")
	}))
	defer plainSrv.Close()

	var authOnRedirect string
	var tlsSrv *httptest.Server
	otherTLS := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authOnRedirect = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "redirected")
	}))
	defer otherTLS.Close()

	tlsSrv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redir" {
			http.Redirect(w, r, otherTLS.URL+"/target", http.StatusFound)
			return
		}
		if r.URL.Path == "/downgrade" {
			http.Redirect(w, r, "http://example.com/evil", http.StatusFound)
			return
		}
		w.Header().Set("X-Method", r.Method)
		io.WriteString(w, "secure")
	}))
	defer tlsSrv.Close()

	// Trust both TLS test servers on the injected client.
	client := tlsSrv.Client()
	origTransport := client.Transport.(*http.Transport).Clone()
	origTransport.TLSClientConfig.InsecureSkipVerify = true
	client.Transport = origTransport

	secrets := memSecrets{
		"ai-api-key":                    []byte("core-ai-secret"),
		"plugin-dev.arcmd.test-token":   []byte("plugin-secret"),
		"plugin-dev.arcmd.other-secret": []byte("other-plugin-secret"),
	}
	host, err := NewWASMHost(ctx, HostConfig{
		DataDir:    dataDir,
		Secrets:    secrets,
		HTTPClient: func() *http.Client { return client },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()

	spaces := NewFolderSpaces([]SpaceSnapshot{{ID: "sp1", Name: "Work", Root: spaceDir}}, nil, nil)

	// Plugin with NO permissions.
	unpriv := &Installed{
		Manifest: &Manifest{
			ID:      "dev.arcmd.test",
			Name:    "Test",
			Version: "0.1.0",
			Runtime: RuntimeWASM,
			Main:    "plugin.wasm",
		},
	}
	invUnpriv := &invocationState{installed: unpriv, spaces: spaces}
	for _, op := range []HostRequest{
		{Op: "secrets.get", Key: "token"},
		{Op: "spaces.list"},
		{Op: "space.list_files", SpaceID: "sp1"},
		{Op: "space.read_file", SpaceID: "sp1", Path: "Note.md"},
		{Op: "space.write_file", SpaceID: "sp1", Path: "Note.md"},
		{Op: "space.delete_file", SpaceID: "sp1", Path: "Note.md"},
		{Op: "http.request", Method: "GET", URL: tlsSrv.URL},
	} {
		rep, _ := host.dispatch(ctx, invUnpriv, op, nil)
		if rep.OK || rep.Code != CodePermissionDenied {
			t.Errorf("op %s succeeded without permission: %+v", op.Op, rep)
		}
	}

	// Plugin WITH secrets + network + space:read.
	priv := &Installed{
		Manifest: &Manifest{
			ID:          "dev.arcmd.test",
			Name:        "Test",
			Version:     "0.1.0",
			Runtime:     RuntimeWASM,
			Main:        "plugin.wasm",
			Permissions: []Permission{PermSecrets, PermNetwork, PermSpaceRead},
		},
	}
	invPriv := &invocationState{installed: priv, spaces: spaces}

	// Secret isolation: plugin only reads its own prefixed key, never ai-api-key or other plugin.
	rep, _ := host.dispatch(ctx, invPriv, HostRequest{Op: "secrets.get", Key: "token"}, nil)
	if !rep.OK || rep.Value != "plugin-secret" {
		t.Fatalf("secrets.get(token) = %+v", rep)
	}
	rep, _ = host.dispatch(ctx, invPriv, HostRequest{Op: "secrets.get", Key: "../ai-api-key"}, nil)
	if rep.OK {
		t.Fatal("secrets.get accepted traversal key")
	}

	// State storage isolates plugin and rejects path traversal.
	rep, _ = host.dispatch(ctx, invPriv, HostRequest{Op: "state.set", Key: "../escape"}, []byte("x"))
	if rep.OK {
		t.Fatal("state.set accepted traversal key")
	}
	rep, _ = host.dispatch(ctx, invPriv, HostRequest{Op: "state.set", Key: "cursor.json"}, []byte(`{"ok":true}`))
	if !rep.OK {
		t.Fatalf("state.set failed: %+v", rep)
	}
	rep, body := host.dispatch(ctx, invPriv, HostRequest{Op: "state.get", Key: "cursor.json"}, nil)
	if !rep.OK || string(body) != `{"ok":true}` {
		t.Fatalf("state.get = %+v, %q", rep, body)
	}

	// Network guard: non-loopback plain HTTP rejected before sending.
	rep, _ = host.dispatch(ctx, invPriv, HostRequest{Op: "http.request", Method: "GET", URL: "http://example.com/dav"}, nil)
	if rep.OK {
		t.Fatal("plain remote http accepted without AllowInsecureHTTP")
	}
	// Loopback plain HTTP allowed.
	rep, body = host.dispatch(ctx, invPriv, HostRequest{Op: "http.request", Method: "PROPFIND", URL: plainSrv.URL}, nil)
	if !rep.OK || string(body) != "plain" || plainHits.Load() != 1 {
		t.Fatalf("loopback http failed: %+v, %q", rep, body)
	}
	// HTTPS works, redirect downgrade rejected, cross-origin redirect strips Authorization.
	rep, body = host.dispatch(ctx, invPriv, HostRequest{Op: "http.request", Method: "MKCOL", URL: tlsSrv.URL + "/ok"}, nil)
	if !rep.OK || rep.Headers["X-Method"] != "MKCOL" || string(body) != "secure" {
		t.Fatalf("https custom method failed: %+v, %q", rep, body)
	}
	rep, _ = host.dispatch(ctx, invPriv, HostRequest{Op: "http.request", Method: "GET", URL: tlsSrv.URL + "/downgrade"}, nil)
	if rep.OK {
		t.Fatal("https->http downgrade redirect succeeded")
	}
	rep, _ = host.dispatch(ctx, invPriv, HostRequest{
		Op:      "http.request",
		Method:  "GET",
		URL:     tlsSrv.URL + "/redir",
		Headers: map[string]string{"Authorization": "Bearer top-secret"},
	}, nil)
	if !rep.OK || authOnRedirect != "" {
		t.Fatalf("cross-origin redirect leaked Authorization %q: %+v", authOnRedirect, rep)
	}
}

func TestWASMHostRealModuleExecution(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	pluginDir := t.TempDir()
	wasmBytes := buildEchoSecretsWASM()
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.wasm"), wasmBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	host, err := NewWASMHost(ctx, HostConfig{
		DataDir: dataDir,
		Secrets: memSecrets{"plugin-dev.arcmd.echo-apiKey": []byte("sk-123")},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()

	// Without document:read/write or ui:panel, Request.Document and Response.Edits are stripped.
	inst, err := host.Start(ctx, &Installed{
		Dir:     pluginDir,
		Enabled: true,
		Manifest: &Manifest{
			ID:          "dev.arcmd.echo",
			Name:        "Echo",
			Version:     "0.1.0",
			Runtime:     RuntimeWASM,
			Main:        "plugin.wasm",
			Permissions: []Permission{PermSecrets},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close()

	resp, err := inst.Invoke(ctx, Request{
		Command:  "ping",
		Document: &DocumentContext{Path: "a.md", Text: "secret doc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Toast != "ping" {
		t.Fatalf("resp = %+v, want Toast=ping", resp)
	}
	if len(resp.Edits) != 0 {
		t.Fatalf("edits should be stripped without document:write: %+v", resp.Edits)
	}

	// Panel invocation requires ui:panel.
	if _, err := inst.Invoke(ctx, Request{Panel: "status"}); err == nil {
		t.Fatal("panel invocation succeeded without ui:panel")
	}

	// Verify InspectArchive + InstallArchive flow.
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	w, _ := zw.Create(ManifestFile)
	w.Write([]byte(`{"id":"dev.arcmd.echo","name":"Echo","version":"0.1.0","runtime":"wasm","main":"plugin.wasm"}`))
	w2, _ := zw.Create("plugin.wasm")
	w2.Write(wasmBytes)
	zw.Close()
	man, digest, err := InspectArchive(zipBuf.Bytes())
	if err != nil || man.ID != "dev.arcmd.echo" || len(digest) != 64 {
		t.Fatalf("InspectArchive = %+v, %q, %v", man, digest, err)
	}
	mgr := NewManager(t.TempDir())
	installed, err := mgr.InstallArchive(zipBuf.Bytes(), Listing{ID: man.ID, Version: man.Version, SHA256: digest})
	if err != nil || !installed.Enabled {
		t.Fatalf("InstallArchive = %+v, %v", installed, err)
	}
}

// buildEchoSecretsWASM builds a valid WASI WebAssembly module that:
// 1. Calls arcmd.call with a HostRequest{Op:"secrets.get",Key:"apiKey"} at memory 0..N
// 2. Calls arcmd.read_result to read the HostReply frame
// 3. Writes `{"toast":"ping","edits":[{"range":{"start":0,"end":0},"text":"x"}]}` to stdout via fd_write
// 4. Calls proc_exit(0)
func buildEchoSecretsWASM() []byte {
	frame, _ := EncodeFrame(HostRequest{Op: "secrets.get", Key: "apiKey"}, nil)
	outJSON := []byte(`{"toast":"ping","edits":[{"range":{"start":0,"end":0},"text":"x"}]}`)

	const (
		reqOff  = 64
		iovOff  = 16
		nwrOff  = 32
		outOff  = 256
		recvOff = 512
	)

	var mod bytes.Buffer
	// WASM header
	mod.Write([]byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00})

	lebU := func(v uint32) []byte {
		var out []byte
		for {
			b := byte(v & 0x7f)
			v >>= 7
			if v != 0 {
				b |= 0x80
			}
			out = append(out, b)
			if v == 0 {
				break
			}
		}
		return out
	}
	sec := func(id byte, body []byte) {
		mod.WriteByte(id)
		mod.Write(lebU(uint32(len(body))))
		mod.Write(body)
	}
	str := func(s string) []byte {
		b := lebU(uint32(len(s)))
		return append(b, s...)
	}

	// Type section (1):
	// 0: (i32, i32) -> i32   [arcmd.call, arcmd.read_result]
	// 1: (i32, i32, i32, i32) -> i32 [wasi.fd_write]
	// 2: (i32) -> ()         [wasi.proc_exit]
	// 3: () -> ()            [_start]
	var tsec bytes.Buffer
	tsec.WriteByte(4)
	tsec.Write([]byte{0x60, 2, 0x7f, 0x7f, 1, 0x7f})
	tsec.Write([]byte{0x60, 4, 0x7f, 0x7f, 0x7f, 0x7f, 1, 0x7f})
	tsec.Write([]byte{0x60, 1, 0x7f, 0})
	tsec.Write([]byte{0x60, 0, 0})
	sec(1, tsec.Bytes())

	// Import section (2):
	// 0: arcmd.call (type 0)
	// 1: arcmd.read_result (type 0)
	// 2: wasi_snapshot_preview1.fd_write (type 1)
	// 3: wasi_snapshot_preview1.proc_exit (type 2)
	var isec bytes.Buffer
	isec.WriteByte(4)
	isec.Write(str("arcmd"))
	isec.Write(str("call"))
	isec.Write([]byte{0x00, 0})
	isec.Write(str("arcmd"))
	isec.Write(str("read_result"))
	isec.Write([]byte{0x00, 0})
	isec.Write(str("wasi_snapshot_preview1"))
	isec.Write(str("fd_write"))
	isec.Write([]byte{0x00, 1})
	isec.Write(str("wasi_snapshot_preview1"))
	isec.Write(str("proc_exit"))
	isec.Write([]byte{0x00, 2})
	sec(2, isec.Bytes())

	// Function section (3): func 4 has type 3 (_start)
	sec(3, []byte{1, 3})

	// Memory section (5): 1 page
	sec(5, []byte{1, 0x00, 1})

	// Export section (7): memory + _start
	var esec bytes.Buffer
	esec.WriteByte(2)
	esec.Write(str("memory"))
	esec.Write([]byte{0x02, 0})
	esec.Write(str("_start"))
	esec.Write([]byte{0x00, 4})
	sec(7, esec.Bytes())

	// Code section (10): body for _start
	var fn bytes.Buffer
	fn.WriteByte(1)           // 1 local group
	fn.Write([]byte{1, 0x7f}) // 1 local i32 (len)
	i32const := func(v int32) {
		fn.WriteByte(0x41)
		// signed LEB128
		for {
			b := byte(v & 0x7f)
			v >>= 7
			if (v == 0 && b&0x40 == 0) || (v == -1 && b&0x40 != 0) {
				fn.WriteByte(b)
				break
			}
			fn.WriteByte(b | 0x80)
		}
	}
	// len = arcmd.call(reqOff, len(frame))
	i32const(reqOff)
	i32const(int32(len(frame)))
	fn.Write([]byte{0x10, 0}) // call 0
	fn.Write([]byte{0x21, 0}) // local.set 0
	// _ = arcmd.read_result(recvOff, local 0)
	i32const(recvOff)
	fn.Write([]byte{0x20, 0}) // local.get 0
	fn.Write([]byte{0x10, 1}) // call 1
	fn.WriteByte(0x1a)        // drop
	// fd_write(1, iovOff, 1, nwrOff)
	i32const(1)
	i32const(iovOff)
	i32const(1)
	i32const(nwrOff)
	fn.Write([]byte{0x10, 2}) // call 2
	fn.WriteByte(0x1a)        // drop
	// proc_exit(0)
	i32const(0)
	fn.Write([]byte{0x10, 3}) // call 3
	fn.WriteByte(0x0b)        // end

	var csec bytes.Buffer
	csec.WriteByte(1)
	csec.Write(lebU(uint32(fn.Len())))
	csec.Write(fn.Bytes())
	sec(10, csec.Bytes())

	// Data section (11):
	// seg 0: frame at reqOff
	// seg 1: iov {outOff, len(outJSON)} at iovOff
	// seg 2: outJSON at outOff
	var dsec bytes.Buffer
	dsec.WriteByte(3)
	addSeg := func(off int32, data []byte) {
		dsec.WriteByte(0)    // active mem 0
		dsec.WriteByte(0x41) // i32.const
		u := off
		for {
			b := byte(u & 0x7f)
			u >>= 7
			if (u == 0 && b&0x40 == 0) || (u == -1 && b&0x40 != 0) {
				dsec.WriteByte(b)
				break
			}
			dsec.WriteByte(b | 0x80)
		}
		dsec.WriteByte(0x0b)
		dsec.Write(lebU(uint32(len(data))))
		dsec.Write(data)
	}
	addSeg(reqOff, frame)
	iov := make([]byte, 8)
	binary.LittleEndian.PutUint32(iov[0:4], outOff)
	binary.LittleEndian.PutUint32(iov[4:8], uint32(len(outJSON)))
	addSeg(iovOff, iov)
	addSeg(outOff, outJSON)
	sec(11, dsec.Bytes())

	return mod.Bytes()
}
