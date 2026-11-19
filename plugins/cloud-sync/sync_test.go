package main_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/plugin"
	"github.com/Ciao1019/ArcMD/plugins/cloud-sync/bundle"
)

type memS3Server struct {
	mu          sync.Mutex
	bucket      string
	accessKey   string
	secretKey   string
	region      string
	objects     map[string]memObj
	maxPageSize int
	getCount    atomic.Int32
	putCount    atomic.Int32
	delCount    atomic.Int32
}

type memObj struct {
	data    []byte
	etag    string
	modTime time.Time
}

func newMemS3Server(t *testing.T, bucket, accessKey, secretKey, region string) (*memS3Server, *httptest.Server) {
	t.Helper()
	m := &memS3Server{
		bucket:      bucket,
		accessKey:   accessKey,
		secretKey:   secretKey,
		region:      region,
		objects:     map[string]memObj{},
		maxPageSize: 1000,
	}
	srv := httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(srv.Close)
	return m, srv
}

func (m *memS3Server) serve(w http.ResponseWriter, r *http.Request) {
	if !m.verifySigV4(r) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `<Error><Code>SignatureDoesNotMatch</Code><Message>invalid signature</Message></Error>`)
		return
	}
	prefix := "/" + m.bucket
	if r.URL.Path != prefix && !strings.HasPrefix(r.URL.Path, prefix+"/") {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `<Error><Code>NoSuchBucket</Code><Message>bucket not found</Message></Error>`)
		return
	}

	if r.Method == http.MethodGet && (r.URL.Path == prefix || r.URL.Path == prefix+"/") && r.URL.Query().Get("list-type") == "2" {
		m.handleListV2(w, r)
		return
	}

	key := strings.TrimPrefix(r.URL.Path, prefix+"/")
	m.mu.Lock()
	defer m.mu.Unlock()

	switch r.Method {
	case http.MethodGet:
		m.getCount.Add(1)
		obj, ok := m.objects[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `<Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`)
			return
		}
		w.Header().Set("ETag", `"`+obj.etag+`"`)
		w.Header().Set("Last-Modified", obj.modTime.UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
		w.Write(obj.data)

	case http.MethodHead:
		obj, ok := m.objects[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("ETag", `"`+obj.etag+`"`)
		w.Header().Set("Last-Modified", obj.modTime.UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)

	case http.MethodPut:
		m.putCount.Add(1)
		body, _ := io.ReadAll(r.Body)
		sum := md5.Sum(body)
		etag := hex.EncodeToString(sum[:])
		m.objects[key] = memObj{
			data:    body,
			etag:    etag,
			modTime: time.Now().UTC(),
		}
		w.Header().Set("ETag", `"`+etag+`"`)
		w.WriteHeader(http.StatusOK)

	case http.MethodDelete:
		m.delCount.Add(1)
		delete(m.objects, key)
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (m *memS3Server) handleListV2(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	prefix := q.Get("prefix")
	token := q.Get("continuation-token")
	maxKeys := m.maxPageSize
	if mk := q.Get("max-keys"); mk != "" {
		var parsed int
		if _, err := fmt.Sscanf(mk, "%d", &parsed); err == nil && parsed > 0 && parsed < maxKeys {
			maxKeys = parsed
		}
	}

	m.mu.Lock()
	var keys []string
	for k := range m.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	start := 0
	if token != "" {
		fmt.Sscanf(token, "%d", &start)
	}
	end := min(len(keys), start+maxKeys)
	isTruncated := end < len(keys)
	nextToken := ""
	if isTruncated {
		nextToken = fmt.Sprintf("%d", end)
	}

	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult>`)
	fmt.Fprintf(&buf, "<IsTruncated>%v</IsTruncated>", isTruncated)
	if nextToken != "" {
		fmt.Fprintf(&buf, "<NextContinuationToken>%s</NextContinuationToken>", nextToken)
	}
	for _, k := range keys[start:end] {
		obj := m.objects[k]
		buf.WriteString("<Contents>")
		buf.WriteString("<Key>")
		xml.EscapeText(&buf, []byte(k))
		buf.WriteString("</Key>")
		fmt.Fprintf(&buf, "<LastModified>%s</LastModified>", obj.modTime.UTC().Format(time.RFC3339))
		fmt.Fprintf(&buf, "<ETag>&quot;%s&quot;</ETag>", obj.etag)
		fmt.Fprintf(&buf, "<Size>%d</Size>", len(obj.data))
		buf.WriteString("</Contents>")
	}
	m.mu.Unlock()
	buf.WriteString("</ListBucketResult>")
	w.Header().Set("Content-Type", "application/xml")
	w.Write(buf.Bytes())
}

func (m *memS3Server) verifySigV4(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(auth, "AWS4-HMAC-SHA256 "), ",")
	kv := map[string]string{}
	for _, p := range parts {
		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if ok {
			kv[k] = v
		}
	}
	credParts := strings.Split(kv["Credential"], "/")
	if len(credParts) != 5 || credParts[0] != m.accessKey || credParts[2] != m.region || credParts[3] != "s3" || credParts[4] != "aws4_request" {
		return false
	}
	signedHeaders := strings.Split(kv["SignedHeaders"], ";")
	var canHdrs strings.Builder
	for _, h := range signedHeaders {
		val := r.Header.Get(h)
		if h == "host" {
			val = r.Host
		}
		canHdrs.WriteString(h + ":" + strings.TrimSpace(val) + "\n")
	}
	rawBody, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(rawBody))
	sumBody := sha256.Sum256(rawBody)
	payloadHash := hex.EncodeToString(sumBody[:])
	if r.Header.Get("x-amz-content-sha256") != payloadHash {
		return false
	}
	canQuery := canonicalQueryFromRaw(r.URL.RawQuery)
	canURI := r.URL.EscapedPath()
	canReq := strings.Join([]string{r.Method, canURI, canQuery, canHdrs.String(), kv["SignedHeaders"], payloadHash}, "\n")
	sumReq := sha256.Sum256([]byte(canReq))
	amzDate := r.Header.Get("x-amz-date")
	scope := strings.Join(credParts[1:], "/")
	sts := strings.Join([]string{"AWS4-HMAC-SHA256", amzDate, scope, hex.EncodeToString(sumReq[:])}, "\n")

	hmacSum := func(key []byte, data string) []byte {
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte(data))
		return mac.Sum(nil)
	}
	kDate := hmacSum([]byte("AWS4"+m.secretKey), credParts[1])
	kReg := hmacSum(kDate, m.region)
	kSvc := hmacSum(kReg, "s3")
	kSign := hmacSum(kSvc, "aws4_request")
	wantSig := hex.EncodeToString(hmacSum(kSign, sts))
	return kv["Signature"] == wantSig
}

func canonicalQueryFromRaw(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "&")
	sort.Strings(parts)
	return strings.Join(parts, "&")
}

type memDAVServer struct {
	mu           sync.Mutex
	user         string
	pass         string
	dirs         map[string]bool
	files        map[string]memObj
	omitPutETag  bool
	getCount     atomic.Int32
	putCount     atomic.Int32
	propfindInf  atomic.Int32
	mkcolParents []string
}

func newMemDAVServer(t *testing.T, user, pass string) (*memDAVServer, *httptest.Server) {
	t.Helper()
	m := &memDAVServer{
		user:  user,
		pass:  pass,
		dirs:  map[string]bool{"/dav": true},
		files: map[string]memObj{},
	}
	srv := httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(srv.Close)
	return m, srv
}

func (m *memDAVServer) serve(w http.ResponseWriter, r *http.Request) {
	u, p, ok := r.BasicAuth()
	if !ok || u != m.user || p != m.pass {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	reqPath := path.Clean("/" + strings.TrimLeft(r.URL.Path, "/"))
	m.mu.Lock()
	defer m.mu.Unlock()

	switch r.Method {
	case "PROPFIND":
		depth := strings.ToLower(r.Header.Get("Depth"))
		if depth == "infinity" {
			m.propfindInf.Add(1)
			w.WriteHeader(http.StatusForbidden) // Jianguoyun rejects Depth: infinity
			return
		}
		isDir := m.dirs[reqPath]
		f, isFile := m.files[reqPath]
		if !isDir && !isFile {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		var buf bytes.Buffer
		buf.WriteString(`<?xml version="1.0" encoding="utf-8"?><d:multistatus xmlns:d="DAV:">`)
		writeDir := func(p string) {
			buf.WriteString("<d:response><d:href>")
			buf.WriteString(escapeDAVPath(p) + "/")
			buf.WriteString("</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>")
		}
		writeFile := func(p string, obj memObj) {
			buf.WriteString("<d:response><d:href>")
			buf.WriteString(escapeDAVPath(p))
			buf.WriteString("</d:href><d:propstat><d:prop><d:resourcetype/>")
			fmt.Fprintf(&buf, "<d:getcontentlength>%d</d:getcontentlength>", len(obj.data))
			fmt.Fprintf(&buf, "<d:getlastmodified>%s</d:getlastmodified>", obj.modTime.UTC().Format(http.TimeFormat))
			fmt.Fprintf(&buf, `<d:getetag>"%s"</d:getetag>`, obj.etag)
			buf.WriteString("</d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>")
		}
		if isFile {
			writeFile(reqPath, f)
		} else {
			writeDir(reqPath)
			if depth == "1" {
				for d := range m.dirs {
					if d != reqPath && path.Dir(d) == reqPath {
						writeDir(d)
					}
				}
				for fp, obj := range m.files {
					if path.Dir(fp) == reqPath {
						writeFile(fp, obj)
					}
				}
			}
		}
		buf.WriteString("</d:multistatus>")
		w.Write(buf.Bytes())

	case "MKCOL":
		parent := path.Dir(reqPath)
		if !m.dirs[parent] {
			w.WriteHeader(http.StatusConflict)
			return
		}
		m.dirs[reqPath] = true
		m.mkcolParents = append(m.mkcolParents, reqPath)
		w.WriteHeader(http.StatusCreated)

	case http.MethodGet:
		m.getCount.Add(1)
		obj, ok := m.files[reqPath]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("ETag", `"`+obj.etag+`"`)
		w.Header().Set("Last-Modified", obj.modTime.UTC().Format(http.TimeFormat))
		w.Write(obj.data)

	case http.MethodPut:
		m.putCount.Add(1)
		parent := path.Dir(reqPath)
		if !m.dirs[parent] {
			w.WriteHeader(http.StatusConflict)
			return
		}
		body, _ := io.ReadAll(r.Body)
		sum := md5.Sum(body)
		etag := hex.EncodeToString(sum[:])
		now := time.Now().UTC()
		m.files[reqPath] = memObj{data: body, etag: etag, modTime: now}
		if !m.omitPutETag {
			w.Header().Set("ETag", `"`+etag+`"`)
			w.Header().Set("Last-Modified", now.Format(http.TimeFormat))
		}
		w.WriteHeader(http.StatusCreated)

	case http.MethodDelete:
		delete(m.files, reqPath)
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func escapeDAVPath(p string) string {
	segs := strings.Split(strings.TrimLeft(p, "/"), "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return "/" + strings.Join(segs, "/")
}

type testMemSecrets map[string][]byte

func (m testMemSecrets) Get(key string) ([]byte, error) {
	v, ok := m[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return slices.Clone(v), nil
}

type syncDevice struct {
	name    string
	spRoot  string
	spID    string
	spName  string
	dataDir string
	cfg     *plugin.ConfigStore
	host    *plugin.WASMHost
	inst    plugin.Instance
	trashed []string
	dirty   map[plugin.SpaceFileKey]bool
}

func setupSyncDevice(t *testing.T, name, spID, spName string, settings map[string]any, secrets testMemSecrets) *syncDevice {
	t.Helper()
	ctx := context.Background()
	pluginsDir := filepath.Join(t.TempDir(), "plugins")
	dataDir := filepath.Join(t.TempDir(), "plugins-data")
	spRoot := t.TempDir()

	mgr := plugin.NewManager(pluginsDir)
	cat := bundle.OfficialCatalog()
	if len(cat) == 0 {
		t.Fatal("empty official catalog")
	}
	installed, err := mgr.InstallArchive(cat[0].Archive, cat[0].Listing)
	if err != nil {
		t.Fatal(err)
	}
	cfgStore := &plugin.ConfigStore{Dir: dataDir}
	if err := cfgStore.Save(installed.Manifest, settings); err != nil {
		t.Fatal(err)
	}
	host, err := plugin.NewWASMHost(ctx, plugin.HostConfig{
		DataDir: dataDir,
		Config:  cfgStore,
		Secrets: secrets,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	inst, err := host.Start(ctx, installed)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = inst.Close() })

	return &syncDevice{
		name:    name,
		spRoot:  spRoot,
		spID:    spID,
		spName:  spName,
		dataDir: dataDir,
		cfg:     cfgStore,
		host:    host,
		inst:    inst,
		dirty:   map[plugin.SpaceFileKey]bool{},
	}
}

func (d *syncDevice) invoke(t *testing.T, req plugin.Request) (*plugin.Response, []plugin.SpaceFileKey) {
	t.Helper()
	spaces := plugin.NewFolderSpaces(
		[]plugin.SpaceSnapshot{{ID: d.spID, Name: d.spName, Root: d.spRoot}},
		d.dirty,
		func(spaceID, spaceName, root, rel string) error {
			d.trashed = append(d.trashed, rel)
			return os.Remove(filepath.Join(root, filepath.FromSlash(rel)))
		},
	)
	ctx := plugin.WithSpaceService(context.Background(), spaces)
	resp, err := d.inst.Invoke(ctx, req)
	if err != nil {
		t.Fatalf("%s invoke(%+v): %v", d.name, req, err)
	}
	return resp, spaces.Touched()
}

func (d *syncDevice) writeLocal(t *testing.T, rel string, data []byte) {
	t.Helper()
	full := filepath.Join(d.spRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (d *syncDevice) readLocal(t *testing.T, rel string) ([]byte, bool) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(d.spRoot, filepath.FromSlash(rel)))
	if os.IsNotExist(err) {
		return nil, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return b, true
}

func (d *syncDevice) listLocalFiles(t *testing.T) []string {
	t.Helper()
	spaces := plugin.NewFolderSpaces([]plugin.SpaceSnapshot{{ID: d.spID, Name: d.spName, Root: d.spRoot}}, nil, nil)
	entries, err := spaces.ListFiles(d.spID)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Path
	}
	return out
}

func TestCloudSyncS3EndToEndWASM(t *testing.T) {
	s3, srv := newMemS3Server(t, "notes-bucket", "ak-test", "sk-secret", "auto")
	s3.maxPageSize = 2 // force ListObjectsV2 pagination

	secrets := testMemSecrets{
		"plugin-dev.arcmd.cloud-sync-s3SecretKey": []byte("sk-secret"),
	}
	settings := map[string]any{
		"provider":    "s3",
		"s3Endpoint":  srv.URL,
		"s3Region":    "auto",
		"s3Bucket":    "notes-bucket",
		"s3AccessKey": "ak-test",
		"s3Prefix":    "ArcMD",
		"spaces":      []string{"sp-work"},
	}

	dev1 := setupSyncDevice(t, "dev1", "sp-work", "Work 空间", settings, secrets)
	dev2 := setupSyncDevice(t, "dev2", "sp-work", "Work 空间", settings, secrets)

	// 1. Test connection via compiled WASM binary.
	resp, _ := dev1.invoke(t, plugin.Request{Command: "test-connection"})
	if !strings.Contains(resp.Toast, "S3 连接成功") {
		t.Fatalf("test-connection = %+v", resp)
	}

	// 2. Populate device 1 with markdown, CJK filename, binary image, template, and history (history must be skipped).
	dev1.writeLocal(t, "Welcome.md", []byte("# Welcome\n"))
	dev1.writeLocal(t, "笔记/周报 (2026)[1].md", []byte("# 周报\n内容\n"))
	dev1.writeLocal(t, "assets/pic.png", []byte{0x89, 'P', 'N', 'G', 1, 2, 3})
	dev1.writeLocal(t, ".arcmd/templates/Daily.md", []byte("# {{date}}\n"))
	dev1.writeLocal(t, ".arcmd/history/Welcome.md/20261119-000000.000.md", []byte("local history"))

	resp, _ = dev1.invoke(t, plugin.Request{Command: "sync-now"})
	if !strings.Contains(resp.Toast, "上传 4") {
		t.Fatalf("initial sync dev1 = %+v", resp)
	}
	if _, leaked := s3.objects["ArcMD/Work 空间/.arcmd/history/Welcome.md/20261119-000000.000.md"]; leaked {
		t.Fatal("history snapshot leaked to S3")
	}

	// 3. Device 2 syncs -> downloads all 4 across paginated ListObjectsV2.
	resp, touched := dev2.invoke(t, plugin.Request{Command: "sync-now"})
	if !strings.Contains(resp.Toast, "下载 4") || len(touched) != 4 {
		t.Fatalf("initial sync dev2 = %+v, touched=%v", resp, touched)
	}
	if got, ok := dev2.readLocal(t, "笔记/周报 (2026)[1].md"); !ok || string(got) != "# 周报\n内容\n" {
		t.Fatalf("CJK file = %q, %v", got, ok)
	}
	if got, ok := dev2.readLocal(t, "assets/pic.png"); !ok || !bytes.Equal(got, []byte{0x89, 'P', 'N', 'G', 1, 2, 3}) {
		t.Fatalf("PNG mismatch: %v", got)
	}

	// 4. Unchanged fast skip: no GETs or PUTs on repeat sync.
	getsBefore, putsBefore := s3.getCount.Load(), s3.putCount.Load()
	resp, _ = dev2.invoke(t, plugin.Request{Command: "sync-now"})
	if !strings.Contains(resp.Toast, "已是最新") {
		t.Fatalf("idle sync = %+v", resp)
	}
	if s3.getCount.Load() != getsBefore || s3.putCount.Load() != putsBefore {
		t.Fatalf("unchanged sync transferred data: gets %d->%d puts %d->%d", getsBefore, s3.getCount.Load(), putsBefore, s3.putCount.Load())
	}

	// 5a. Non-overlapping 2-device text edits -> automatic 3-way merge without conflict!
	dev1.writeLocal(t, "笔记/周报 (2026)[1].md", []byte("# 周报 (已审阅)\n内容\n"))
	dev2.writeLocal(t, "笔记/周报 (2026)[1].md", []byte("# 周报\n内容\n\n## 下周计划\n"))
	dev1.invoke(t, plugin.Request{Command: "sync-now"})
	resp, _ = dev2.invoke(t, plugin.Request{Command: "sync-now"})
	if !strings.Contains(resp.Toast, "自动合并 1") {
		t.Fatalf("expected clean 3-way auto-merge on dev2: %+v", resp)
	}
	dev1.invoke(t, plugin.Request{Command: "sync-now"})
	wantMerged := "# 周报 (已审阅)\n内容\n\n## 下周计划\n"
	if g1, _ := dev1.readLocal(t, "笔记/周报 (2026)[1].md"); string(g1) != wantMerged {
		t.Fatalf("dev1 merged = %q, want %q", g1, wantMerged)
	}
	if g2, _ := dev2.readLocal(t, "笔记/周报 (2026)[1].md"); string(g2) != wantMerged {
		t.Fatalf("dev2 merged = %q, want %q", g2, wantMerged)
	}

	// 5b. Overlapping 2-device text conflict -> stages into conflicts.json for 3-way merge WITHOUT creating "(冲突副本)" files!
	dev1.writeLocal(t, "Welcome.md", []byte("# Welcome from Dev1\n"))
	dev2.writeLocal(t, "Welcome.md", []byte("# Welcome from Dev2\n"))
	dev1.invoke(t, plugin.Request{Command: "sync-now"})
	resp, _ = dev2.invoke(t, plugin.Request{Command: "sync-now"})
	if !strings.Contains(resp.Toast, "待合并冲突 1") {
		t.Fatalf("expected staged conflict on dev2: %+v", resp)
	}
	for _, f := range dev2.listLocalFiles(t) {
		if strings.Contains(f, "冲突副本") {
			t.Fatalf("unexpected conflict copy file created for text doc: %v", f)
		}
	}
	cfPath, err := dev2.cfg.StatePath("dev.arcmd.cloud-sync", "conflicts.json")
	if err != nil {
		t.Fatal(err)
	}
	cfRaw, err := os.ReadFile(cfPath)
	if err != nil || !strings.Contains(string(cfRaw), "Welcome from Dev1") || !strings.Contains(string(cfRaw), "Welcome from Dev2") {
		t.Fatalf("conflicts.json missing staged versions: err=%v raw=%s", err, cfRaw)
	}

	// 6. Local delete on dev1 propagates to remote and moves dev2's copy to Trash (never raw os.Remove).
	_ = os.Remove(filepath.Join(dev1.spRoot, "笔记/周报 (2026)[1].md"))
	resp, _ = dev1.invoke(t, plugin.Request{Command: "sync-now"})
	if !strings.Contains(resp.Toast, "删除 1") {
		t.Fatalf("delete sync dev1 = %+v", resp)
	}
	resp, _ = dev2.invoke(t, plugin.Request{Command: "sync-now"})
	if !strings.Contains(resp.Toast, "删除 1") || !slices.Equal(dev2.trashed, []string{"笔记/周报 (2026)[1].md"}) {
		t.Fatalf("dev2 trash delete = %+v, trashed=%v", resp, dev2.trashed)
	}

	// 7. Dirty open editor protection: remote changes must NOT clobber dirty buffer.
	dev1.writeLocal(t, "Welcome.md", []byte("# Remote edit while dev2 dirty\n"))
	dev1.invoke(t, plugin.Request{Command: "sync-now"})
	dev2.dirty[plugin.SpaceFileKey{SpaceID: "sp-work", Path: "Welcome.md"}] = true
	resp, _ = dev2.invoke(t, plugin.Request{Command: "sync-now"})
	if !strings.Contains(resp.Toast, "待合并冲突 1") {
		t.Fatalf("dirty file conflict = %+v", resp)
	}
	if cur, _ := dev2.readLocal(t, "Welcome.md"); string(cur) != "# Welcome from Dev2\n" {
		t.Fatalf("dirty local file was overwritten: %q", cur)
	}

	// 8. Sync history markdown command/panel.
	statusResp, _ := dev1.invoke(t, plugin.Request{Panel: "status"})
	if !strings.Contains(statusResp.Markdown, "# 云端同步记录") || !strings.Contains(statusResp.Markdown, "S3") {
		t.Fatalf("unexpected status panel: %+v", statusResp)
	}
}

func TestCloudSyncWebDAVEndToEndWASM(t *testing.T) {
	dav, srv := newMemDAVServer(t, "alice", "app-pass")
	dav.omitPutETag = true // test PROPFIND Depth:0 fallback after PUT

	secrets := testMemSecrets{
		"plugin-dev.arcmd.cloud-sync-webdavPassword": []byte("app-pass"),
	}
	settings := map[string]any{
		"provider":       "webdav",
		"webdavURL":      srv.URL + "/dav/",
		"webdavUsername": "alice",
		"webdavRoot":     "ArcMD",
		"spaces":         []string{"sp-personal"},
	}

	dev1 := setupSyncDevice(t, "dav1", "sp-personal", "Personal", settings, secrets)
	dev2 := setupSyncDevice(t, "dav2", "sp-personal", "Personal", settings, secrets)

	resp, _ := dev1.invoke(t, plugin.Request{Command: "test-connection"})
	if !strings.Contains(resp.Toast, "WebDAV 连接成功") {
		t.Fatalf("test-connection = %+v", resp)
	}

	dev1.writeLocal(t, "Welcome.md", []byte("# Personal\n"))
	dev1.writeLocal(t, "日记/2026-11-19.md", []byte("# 今日\n"))
	dev1.writeLocal(t, "assets/封面.png", []byte{1, 2, 3, 4})

	resp, _ = dev1.invoke(t, plugin.Request{Command: "sync-now"})
	if !strings.Contains(resp.Toast, "上传 3") {
		t.Fatalf("dev1 upload = %+v", resp)
	}
	if dav.propfindInf.Load() != 0 {
		t.Fatal("WebDAV client sent Depth: infinity")
	}

	resp, _ = dev2.invoke(t, plugin.Request{Command: "sync-now"})
	if !strings.Contains(resp.Toast, "下载 3") {
		t.Fatalf("dev2 download = %+v", resp)
	}
	if b, ok := dev2.readLocal(t, "日记/2026-11-19.md"); !ok || string(b) != "# 今日\n" {
		t.Fatalf("WebDAV CJK download mismatch: %q, %v", b, ok)
	}

	// Fast skip on WebDAV even though PUT omitted ETag (via Depth:0 stat fallback).
	getsBefore := dav.getCount.Load()
	resp, _ = dev2.invoke(t, plugin.Request{Command: "sync-now"})
	if !strings.Contains(resp.Toast, "已是最新") || dav.getCount.Load() != getsBefore {
		t.Fatalf("WebDAV idle sync failed: %+v, gets %d->%d", resp, getsBefore, dav.getCount.Load())
	}

	// Verify remote-delete + local-modified keeps local edit instead of deleting.
	_ = os.Remove(filepath.Join(dev1.spRoot, "日记/2026-11-19.md"))
	dev1.invoke(t, plugin.Request{Command: "sync-now"})
	dev2.writeLocal(t, "日记/2026-11-19.md", []byte("# 本地补充编辑\n"))
	resp, _ = dev2.invoke(t, plugin.Request{Command: "sync-now"})
	if !strings.Contains(resp.Toast, "上传 1") || len(dev2.trashed) != 0 {
		t.Fatalf("expected modified local to re-upload instead of trashing: %+v, trashed=%v", resp, dev2.trashed)
	}
	dev1.invoke(t, plugin.Request{Command: "sync-now"}) // dev1 pulls dev2's re-uploaded file

	// Verify local-delete + remote-modified keeps remote edit instead of deleting remote.
	_ = os.Remove(filepath.Join(dev1.spRoot, "Welcome.md"))
	dev2.writeLocal(t, "Welcome.md", []byte("# 远端修改保留\n"))
	dev2.invoke(t, plugin.Request{Command: "sync-now"})
	resp, _ = dev1.invoke(t, plugin.Request{Command: "sync-now"})
	if !strings.Contains(resp.Toast, "下载 1") {
		t.Fatalf("expected remote update to restore over local delete: %+v", resp)
	}
	if b, ok := dev1.readLocal(t, "Welcome.md"); !ok || string(b) != "# 远端修改保留\n" {
		t.Fatalf("restored content = %q, %v", b, ok)
	}

	var _ = json.Marshal // keep clean
}

func TestBundleChecksumAndManifest(t *testing.T) {
	cat := bundle.OfficialCatalog()
	if len(cat) != 1 {
		t.Fatalf("catalog len = %d", len(cat))
	}
	entry := cat[0]
	if entry.Manifest.ID != "dev.arcmd.cloud-sync" || entry.Listing.SHA256 == "" {
		t.Fatalf("unexpected catalog entry: %+v", entry.Listing)
	}
	sum := sha256.Sum256(bundle.CloudSyncZip)
	if hex.EncodeToString(sum[:]) != entry.Listing.SHA256 {
		t.Fatalf("SHA256 mismatch")
	}
}
