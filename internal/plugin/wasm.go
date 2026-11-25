package plugin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	wsys "github.com/tetratelabs/wazero/sys"
)

const (
	maxStateBytes       = 16 << 20
	maxHTTPBodyBytes    = 64 << 20
	maxPluginStdioBytes = 1 << 20
	wasmMemoryLimitPage = 4096 // 256 MiB (4096 * 64 KiB)
)

type limitedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	origLen := len(p)
	rem := b.limit - b.buf.Len()
	if rem <= 0 {
		b.truncated = true
		return origLen, nil
	}
	if len(p) > rem {
		b.truncated = true
		p = p[:rem]
	}
	_, _ = b.buf.Write(p)
	return origLen, nil
}

type invCtxKey struct{}

type invocationState struct {
	installed *Installed
	spaces    SpaceService
	pending   []byte
}

// WASMHost runs sandboxed WASI WebAssembly plugins using wazero (pure Go, no cgo).
type WASMHost struct {
	cfg    HostConfig
	rt     wazero.Runtime
	mu     sync.Mutex
	closed bool
}

// NewWASMHost initializes a wazero runtime with WASI (no filesystem or sockets)
// and the permission-enforcing "arcmd" host module.
func NewWASMHost(ctx context.Context, cfg HostConfig) (*WASMHost, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg.HTTPClient == nil {
		defaultClient := &http.Client{Timeout: 60 * time.Second}
		cfg.HTTPClient = func() *http.Client { return defaultClient }
	}
	rCfg := wazero.NewRuntimeConfig().WithMemoryLimitPages(wasmMemoryLimitPage).WithCloseOnContextDone(true)
	rt := wazero.NewRuntimeWithConfig(ctx, rCfg)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("plugin: wasm wasi init: %w", err)
	}
	h := &WASMHost{cfg: cfg, rt: rt}
	_, err := rt.NewHostModuleBuilder("arcmd").
		NewFunctionBuilder().
		WithFunc(h.hostCall).
		Export("call").
		NewFunctionBuilder().
		WithFunc(h.hostReadResult).
		Export("read_result").
		Instantiate(ctx)
	if err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("plugin: wasm host init: %w", err)
	}
	return h, nil
}

// Runtime implements Host.
func (h *WASMHost) Runtime() Runtime { return RuntimeWASM }

// Close shuts down the underlying wazero runtime and all compiled modules.
func (h *WASMHost) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	h.mu.Unlock()
	return h.rt.Close(context.Background())
}

// Start validates and compiles p's .wasm binary into a reusable Instance.
func (h *WASMHost) Start(ctx context.Context, p *Installed) (Instance, error) {
	if p == nil || p.Manifest == nil {
		return nil, errors.New("plugin: missing manifest")
	}
	if p.Manifest.Runtime != RuntimeWASM {
		return nil, fmt.Errorf("plugin: %s uses runtime %q, not wasm", p.Manifest.ID, p.Manifest.Runtime)
	}
	if err := p.Manifest.Validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	relMain := filepath.FromSlash(p.Manifest.Main)
	if !filepath.IsLocal(relMain) {
		return nil, fmt.Errorf("plugin: unsafe main path %q", p.Manifest.Main)
	}
	wasmPath := filepath.Join(p.Dir, relMain)
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, fmt.Errorf("plugin: read %s: %w", p.Manifest.Main, err)
	}
	if int64(len(wasmBytes)) > MaxPackageSize {
		return nil, fmt.Errorf("plugin: %s exceeds %d bytes", p.Manifest.Main, MaxPackageSize)
	}
	h.mu.Lock()
	closed := h.closed
	h.mu.Unlock()
	if closed {
		return nil, errors.New("plugin: wasm host is closed")
	}
	compiled, err := h.rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("plugin: compile %s: %w", p.Manifest.ID, err)
	}
	return &wasmInstance{host: h, installed: p, compiled: compiled}, nil
}

type wasmInstance struct {
	host      *WASMHost
	installed *Installed
	compiled  wazero.CompiledModule
	mu        sync.Mutex
	closed    bool
}

func (w *wasmInstance) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	w.mu.Unlock()
	return w.compiled.Close(context.Background())
}

func (w *wasmInstance) hasPerm(p Permission) bool {
	return slices.Contains(w.installed.Manifest.Permissions, p)
}

func (w *wasmInstance) Invoke(ctx context.Context, req Request) (*Response, error) {
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return nil, errors.New("plugin: instance is closed")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if req.Panel != "" && !w.hasPerm(PermUIPanel) {
		return nil, fmt.Errorf("plugin: %s lacks %q permission", w.installed.Manifest.ID, PermUIPanel)
	}

	filtered := req
	if !w.hasPerm(PermDocumentRead) {
		filtered.Document = nil
	}
	if !w.hasPerm(PermSpaceRead) {
		filtered.ActiveSpace = ""
	}
	reqBytes, err := json.Marshal(filtered)
	if err != nil {
		return nil, err
	}

	spaces := SpaceServiceFromContext(ctx)
	if spaces == nil && w.host.cfg.Spaces != nil {
		spaces = w.host.cfg.Spaces(ctx)
	}
	inv := &invocationState{
		installed: w.installed,
		spaces:    spaces,
	}
	invCtx := context.WithValue(ctx, invCtxKey{}, inv)

	stdout := &limitedBuffer{limit: maxPluginStdioBytes}
	stderr := &limitedBuffer{limit: maxPluginStdioBytes}
	modCfg := wazero.NewModuleConfig().
		WithName("").
		WithStdin(bytes.NewReader(reqBytes)).
		WithStdout(stdout).
		WithStderr(stderr).
		WithRandSource(rand.Reader).
		WithSysWalltime().
		WithSysNanotime().
		WithSysNanosleep()

	mod, err := w.host.rt.InstantiateModule(invCtx, w.compiled, modCfg)
	if mod != nil {
		_ = mod.Close(context.Background())
	}
	if err != nil {
		var exitErr *wsys.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 0 {
			err = nil
		}
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.buf.String())
		if msg != "" {
			return nil, fmt.Errorf("plugin: %s: %w (%s)", w.installed.Manifest.ID, err, msg)
		}
		return nil, fmt.Errorf("plugin: %s: %w", w.installed.Manifest.ID, err)
	}
	if stdout.truncated {
		return nil, fmt.Errorf("plugin: %s output exceeds %d bytes", w.installed.Manifest.ID, maxPluginStdioBytes)
	}
	rawOut := bytes.TrimSpace(stdout.buf.Bytes())
	if len(rawOut) == 0 {
		return &Response{}, nil
	}
	var resp Response
	if err := json.Unmarshal(rawOut, &resp); err != nil {
		return nil, fmt.Errorf("plugin: %s returned invalid JSON response: %w", w.installed.Manifest.ID, err)
	}
	if !w.hasPerm(PermDocumentWrite) {
		resp.Edits = nil
	}
	return &resp, nil
}

func (h *WASMHost) hostCall(ctx context.Context, m api.Module, reqPtr, reqLen uint32) uint32 {
	inv, ok := ctx.Value(invCtxKey{}).(*invocationState)
	if !ok || m == nil || m.Memory() == nil {
		return 0
	}
	if reqLen > MaxFrameBytes {
		return inv.setError(CodeInvalidArgument, fmt.Errorf("request frame %d exceeds limit", reqLen))
	}
	raw, ok := m.Memory().Read(reqPtr, reqLen)
	if !ok {
		return 0
	}
	var hreq HostRequest
	body, err := DecodeFrame(raw, &hreq)
	if err != nil {
		return inv.setError(CodeInvalidArgument, err)
	}
	reply, replyBody := h.dispatch(ctx, inv, hreq, body)
	frame, err := EncodeFrame(reply, replyBody)
	if err != nil {
		return inv.setError(CodeInternal, err)
	}
	inv.pending = frame
	return uint32(len(frame))
}

func (h *WASMHost) hostReadResult(ctx context.Context, m api.Module, dstPtr, dstLen uint32) uint32 {
	inv, ok := ctx.Value(invCtxKey{}).(*invocationState)
	if !ok || m == nil || m.Memory() == nil {
		return 0
	}
	data := inv.pending
	inv.pending = nil
	if len(data) == 0 || dstLen < uint32(len(data)) {
		return 0
	}
	if !m.Memory().Write(dstPtr, data) {
		return 0
	}
	return uint32(len(data))
}

func (inv *invocationState) setError(code string, err error) uint32 {
	frame, fErr := EncodeFrame(HostReply{OK: false, Code: code, Error: err.Error()}, nil)
	if fErr != nil {
		inv.pending = nil
		return 0
	}
	inv.pending = frame
	return uint32(len(frame))
}

func (inv *invocationState) hasPerm(p Permission) bool {
	return slices.Contains(inv.installed.Manifest.Permissions, p)
}

func (h *WASMHost) configStore() *ConfigStore {
	if h.cfg.Config != nil {
		return h.cfg.Config
	}
	if h.cfg.DataDir != "" {
		return &ConfigStore{Dir: h.cfg.DataDir}
	}
	return nil
}

func (h *WASMHost) dispatch(ctx context.Context, inv *invocationState, req HostRequest, body []byte) (HostReply, []byte) {
	fail := func(code string, err error) (HostReply, []byte) {
		return HostReply{OK: false, Code: code, Error: err.Error()}, nil
	}
	failSpace := func(err error) (HostReply, []byte) {
		var se *SpaceError
		if errors.As(err, &se) {
			return fail(se.Code, se.Err)
		}
		return fail(CodeInternal, err)
	}

	switch req.Op {
	case "config.get":
		cfg, err := h.configStore().Load(inv.installed.Manifest)
		if err != nil {
			return fail(CodeInternal, err)
		}
		return HostReply{OK: true, Config: cfg}, nil

	case "secrets.get":
		if !inv.hasPerm(PermSecrets) {
			return fail(CodePermissionDenied, fmt.Errorf("missing %q permission", PermSecrets))
		}
		if h.cfg.Secrets == nil {
			return fail(CodeInternal, errors.New("secret store is not configured"))
		}
		sk, err := SecretKey(inv.installed.Manifest.ID, req.Key)
		if err != nil {
			return fail(CodeInvalidArgument, err)
		}
		val, err := h.cfg.Secrets.Get(sk)
		if errors.Is(err, fs.ErrNotExist) {
			return HostReply{OK: true, Value: ""}, nil
		}
		if err != nil {
			return fail(CodeInternal, err)
		}
		return HostReply{OK: true, Value: string(val)}, nil

	case "state.get":
		store := h.configStore()
		if store == nil {
			return fail(CodeInternal, errors.New("plugin state store is not configured"))
		}
		p, err := store.StatePath(inv.installed.Manifest.ID, req.Key)
		if err != nil {
			return fail(CodeInvalidArgument, err)
		}
		b, err := os.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			return HostReply{OK: true}, nil
		}
		if err != nil {
			return fail(CodeInternal, err)
		}
		return HostReply{OK: true}, b

	case "state.set":
		store := h.configStore()
		if store == nil {
			return fail(CodeInternal, errors.New("plugin state store is not configured"))
		}
		if len(body) > maxStateBytes {
			return fail(CodeInvalidArgument, fmt.Errorf("state value exceeds %d bytes", maxStateBytes))
		}
		p, err := store.StatePath(inv.installed.Manifest.ID, req.Key)
		if err != nil {
			return fail(CodeInvalidArgument, err)
		}
		dir := filepath.Dir(p)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fail(CodeInternal, err)
		}
		_ = os.Chmod(dir, 0o700)
		if err := fsutil.WriteFileAtomic(p, body, 0o600); err != nil {
			return fail(CodeInternal, err)
		}
		return HostReply{OK: true}, nil

	case "state.delete":
		store := h.configStore()
		if store == nil {
			return fail(CodeInternal, errors.New("plugin state store is not configured"))
		}
		p, err := store.StatePath(inv.installed.Manifest.ID, req.Key)
		if err != nil {
			return fail(CodeInvalidArgument, err)
		}
		if err := os.Remove(p); err != nil && !fsutil.IsMissingFile(p, err) {
			return fail(CodeInternal, err)
		}
		return HostReply{OK: true, Deleted: true}, nil

	case "spaces.list":
		if !inv.hasPerm(PermSpaceRead) {
			return fail(CodePermissionDenied, fmt.Errorf("missing %q permission", PermSpaceRead))
		}
		if inv.spaces == nil {
			return HostReply{OK: true}, nil
		}
		return HostReply{OK: true, Spaces: inv.spaces.ListSpaces()}, nil

	case "space.list_files":
		if !inv.hasPerm(PermSpaceRead) {
			return fail(CodePermissionDenied, fmt.Errorf("missing %q permission", PermSpaceRead))
		}
		if inv.spaces == nil {
			return fail(CodeInternal, errors.New("space service is unavailable"))
		}
		files, err := inv.spaces.ListFiles(req.SpaceID)
		if err != nil {
			return failSpace(err)
		}
		return HostReply{OK: true, Files: files}, nil

	case "space.read_file":
		if !inv.hasPerm(PermSpaceRead) {
			return fail(CodePermissionDenied, fmt.Errorf("missing %q permission", PermSpaceRead))
		}
		if inv.spaces == nil {
			return fail(CodeInternal, errors.New("space service is unavailable"))
		}
		data, meta, err := inv.spaces.ReadFile(req.SpaceID, req.Path)
		if err != nil {
			return failSpace(err)
		}
		return HostReply{OK: true, File: &meta}, data

	case "space.write_file":
		if !inv.hasPerm(PermSpaceWrite) {
			return fail(CodePermissionDenied, fmt.Errorf("missing %q permission", PermSpaceWrite))
		}
		if inv.spaces == nil {
			return fail(CodeInternal, errors.New("space service is unavailable"))
		}
		meta, err := inv.spaces.WriteFile(req.SpaceID, req.Path, body, req.ExpectedSHA256, req.ModTimeUnixNano)
		if err != nil {
			return failSpace(err)
		}
		return HostReply{OK: true, File: &meta}, nil

	case "space.write_conflict":
		if !inv.hasPerm(PermSpaceWrite) {
			return fail(CodePermissionDenied, fmt.Errorf("missing %q permission", PermSpaceWrite))
		}
		if inv.spaces == nil {
			return fail(CodeInternal, errors.New("space service is unavailable"))
		}
		meta, err := inv.spaces.WriteConflict(req.SpaceID, req.Path, body)
		if err != nil {
			return failSpace(err)
		}
		return HostReply{OK: true, File: &meta}, nil

	case "space.delete_file":
		if !inv.hasPerm(PermSpaceWrite) {
			return fail(CodePermissionDenied, fmt.Errorf("missing %q permission", PermSpaceWrite))
		}
		if inv.spaces == nil {
			return fail(CodeInternal, errors.New("space service is unavailable"))
		}
		if err := inv.spaces.DeleteFile(req.SpaceID, req.Path, req.ExpectedSHA256); err != nil {
			return failSpace(err)
		}
		return HostReply{OK: true, Deleted: true}, nil

	case "http.request":
		if !inv.hasPerm(PermNetwork) {
			return fail(CodePermissionDenied, fmt.Errorf("missing %q permission", PermNetwork))
		}
		return h.doHTTP(ctx, req, body)

	default:
		return fail(CodeInvalidArgument, fmt.Errorf("unknown host operation %q", req.Op))
	}
}

func (h *WASMHost) doHTTP(ctx context.Context, req HostRequest, body []byte) (HostReply, []byte) {
	fail := func(code string, err error) (HostReply, []byte) {
		return HostReply{OK: false, Code: code, Error: err.Error()}, nil
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	if !validHTTPMethod(method) {
		return fail(CodeInvalidArgument, fmt.Errorf("invalid HTTP method %q", req.Method))
	}
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, req.URL, bodyReader)
	if err != nil {
		return fail(CodeInvalidArgument, err)
	}
	if len(body) > 0 {
		httpReq.ContentLength = int64(len(body))
		httpReq.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
	}
	if err := checkPluginHTTPURL(httpReq.URL, req.AllowInsecureHTTP); err != nil {
		return fail(CodeInvalidArgument, err)
	}
	for k, v := range req.Headers {
		if strings.EqualFold(k, "Host") {
			httpReq.Host = v
			continue
		}
		httpReq.Header.Set(k, v)
	}

	var client *http.Client
	if h.cfg.HTTPClient != nil {
		client = h.cfg.HTTPClient()
	}
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	// Shallow copy so CheckRedirect guards every hop without mutating a shared client.
	guarded := *client
	origOrigin := originKey(httpReq.URL)
	guarded.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if err := checkPluginHTTPURL(next.URL, req.AllowInsecureHTTP); err != nil {
			return err
		}
		if originKey(next.URL) != origOrigin {
			next.Header.Del("Authorization")
			next.Header.Del("Cookie")
		}
		if client.CheckRedirect != nil {
			return client.CheckRedirect(next, via)
		}
		if len(via) >= 10 {
			return errors.New("plugin: stopped after 10 redirects")
		}
		return nil
	}

	resp, err := guarded.Do(httpReq)
	if err != nil {
		return fail(CodeNetworkError, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBodyBytes+1))
	if err != nil {
		return fail(CodeNetworkError, err)
	}
	if int64(len(respBody)) > maxHTTPBodyBytes {
		return fail(CodeNetworkError, fmt.Errorf("response body exceeds %d bytes", maxHTTPBodyBytes))
	}
	headers := make(map[string]string, len(resp.Header))
	for k, vals := range resp.Header {
		headers[k] = strings.Join(vals, ", ")
	}
	return HostReply{
		OK:      true,
		Status:  resp.StatusCode,
		Headers: headers,
	}, respBody
}

func validHTTPMethod(m string) bool {
	if m == "" {
		return false
	}
	for i := 0; i < len(m); i++ {
		c := m[i]
		if !((c >= 'A' && c <= 'Z') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func checkPluginHTTPURL(u *url.URL, allowInsecure bool) error {
	if u == nil || u.Hostname() == "" {
		return fmt.Errorf("plugin: URL %q has no host", u)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if allowInsecure || isLoopbackHost(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("plugin: %q requires https (or enable Allow Insecure HTTP)", u.Redacted())
	default:
		return fmt.Errorf("plugin: unsupported URL scheme %q", u.Scheme)
	}
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func originKey(u *url.URL) string {
	if u == nil {
		return ""
	}
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Hostname()) + ":" + port
}
