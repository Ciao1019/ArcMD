package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Ciao1019/ArcMD/internal/fsutil"
)

const agentIPCFilename = "agent-ipc.json"

type agentIPCInfo struct {
	Addr    string `json:"addr"`
	Token   string `json:"token"`
	PID     int    `json:"pid"`
	Version string `json:"version"`
}

type agentIPCServer struct {
	ln         net.Listener
	srv        *http.Server
	path       string
	token      string
	baseCancel context.CancelFunc
}

type ipcConnectedKey struct{}

var (
	liveIPCDialer  = &net.Dialer{Timeout: 300 * time.Millisecond}
	liveIPCTimeout = 30 * time.Second
	liveIPCClient  = &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
			IdleConnTimeout:   30 * time.Second,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				conn, err := liveIPCDialer.DialContext(ctx, network, addr)
				if err == nil {
					if flag, ok := ctx.Value(ipcConnectedKey{}).(*atomic.Bool); ok && flag != nil {
						flag.Store(true)
					}
				}
				return conn, err
			},
		},
	}
)

// ControlRequest describes a single local-agent operation against ArcMD.
type ControlRequest struct {
	Action string          `json:"action"`
	Params json.RawMessage `json:"params,omitempty"`
}

// ControlResponse is the wire envelope returned by the local IPC endpoint.
type ControlResponse struct {
	OK     bool   `json:"ok"`
	Live   bool   `json:"live"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

func agentIPCPath(cfg Config) string {
	if cfg.ConfigDir == "" {
		return ""
	}
	return filepath.Join(cfg.ConfigDir, agentIPCFilename)
}

func randomHexToken(bytesLen int) (string, error) {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// startAgentIPC binds a loopback-only HTTP server protected by a 0600 bearer
// token in ConfigDir so CLI and MCP calls synchronize live with the open window.
func (a *App) startAgentIPC() error {
	if a.agentIPC != nil {
		return nil
	}
	ipcPath := agentIPCPath(a.cfg)
	if ipcPath == "" {
		return nil
	}
	tok, err := randomHexToken(32)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	baseCtx, baseCancel := context.WithCancel(context.Background())
	mux := http.NewServeMux()
	checkAuth := func(w http.ResponseWriter, r *http.Request) bool {
		got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
		if subtle.ConstantTimeCompare([]byte(got), []byte(tok)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.HandleFunc("/v1/ping", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "version": Version, "pid": os.Getpid()})
	})
	mux.HandleFunc("/v1/control", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			writeControlJSON(w, http.StatusBadRequest, ControlResponse{Error: readErr.Error()})
			return
		}
		var req ControlRequest
		if decErr := json.Unmarshal(body, &req); decErr != nil {
			writeControlJSON(w, http.StatusBadRequest, ControlResponse{Error: decErr.Error()})
			return
		}
		var res any
		var execErr error
		if strings.TrimSpace(req.Action) == "clip.ai" {
			res, execErr = a.handleClipAIAsync(r.Context(), req.Params)
		} else {
			res, execErr = a.invokeOnMain(r.Context(), func() (any, error) {
				return a.handleControl(req)
			})
		}
		if execErr != nil {
			writeControlJSON(w, http.StatusOK, ControlResponse{OK: false, Live: true, Error: execErr.Error()})
			return
		}
		writeControlJSON(w, http.StatusOK, ControlResponse{OK: true, Live: true, Result: res})
	})

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
	}
	info := agentIPCInfo{
		Addr:    ln.Addr().String(),
		Token:   tok,
		PID:     os.Getpid(),
		Version: Version,
	}
	rawInfo, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		baseCancel()
		_ = ln.Close()
		return err
	}
	if err := fsutil.WriteFileAtomic(ipcPath, append(rawInfo, '\n'), 0o600); err != nil {
		baseCancel()
		_ = ln.Close()
		return err
	}
	a.agentIPC = &agentIPCServer{
		ln:         ln,
		srv:        srv,
		path:       ipcPath,
		token:      tok,
		baseCancel: baseCancel,
	}
	go func() {
		_ = srv.Serve(ln)
	}()
	return nil
}

// stopAgentIPC shuts down the loopback IPC listener and removes agent-ipc.json
// if it still belongs to this server instance.
func (a *App) stopAgentIPC() {
	s := a.agentIPC
	if s == nil {
		return
	}
	a.agentIPC = nil
	if s.baseCancel != nil {
		s.baseCancel()
	}
	_ = s.ln.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	_ = s.srv.Shutdown(ctx)
	cancel()
	if s.path != "" {
		if raw, err := os.ReadFile(s.path); err == nil {
			var cur agentIPCInfo
			if json.Unmarshal(raw, &cur) == nil && cur.Token == s.token {
				_ = os.Remove(s.path)
			}
		}
	}
}

func (a *App) invokeOnMain(ctx context.Context, fn func() (any, error)) (any, error) {
	type outcome struct {
		val any
		err error
	}
	if a.controlExec != nil {
		done := make(chan outcome, 1)
		go func() {
			v, err := a.controlExec(func() (any, error) {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return fn()
			})
			done <- outcome{val: v, err: err}
		}()
		select {
		case out := <-done:
			return out.val, out.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if a.win != nil {
		done := make(chan outcome, 1)
		a.win.Update(func() {
			if ctx.Err() != nil {
				return
			}
			v, err := fn()
			done <- outcome{val: v, err: err}
		})
		select {
		case out := <-done:
			return out.val, out.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return fn()
}

func writeControlJSON(w http.ResponseWriter, code int, resp ControlResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(resp)
}

var errIPCUnavailable = errors.New("agent ipc unavailable")

// ExecuteControl runs req against the running ArcMD desktop window over local
// IPC when available, or transparently falls back to a headless state controller
// operating directly on disk.
func ExecuteControl(ctx context.Context, cfg Config, req ControlRequest) (result any, live bool, err error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	res, ipcErr := callLiveIPC(ctx, cfg, req)
	if ipcErr == nil {
		return res, true, nil
	}
	if !errors.Is(ipcErr, errIPCUnavailable) {
		return nil, true, ipcErr
	}
	h, err := newHeadlessApp(cfg)
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(req.Action) == "clip.ai" {
		out, err := h.handleClipAIAsync(ctx, req.Params)
		return out, false, err
	}
	out, err := h.handleControl(req)
	return out, false, err
}

func callLiveIPC(ctx context.Context, cfg Config, req ControlRequest) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ipcPath := agentIPCPath(cfg)
	if ipcPath == "" {
		return nil, errIPCUnavailable
	}
	raw, err := os.ReadFile(ipcPath)
	if err != nil {
		return nil, errIPCUnavailable
	}
	var info agentIPCInfo
	if err := json.Unmarshal(raw, &info); err != nil || info.Addr == "" || info.Token == "" {
		return nil, errIPCUnavailable
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var connected atomic.Bool
	reqCtx := context.WithValue(ctx, ipcConnectedKey{}, &connected)
	timeout := liveIPCTimeout
	if strings.TrimSpace(req.Action) == "clip.ai" && timeout < 60*time.Second {
		timeout = 60 * time.Second
	}
	callCtx, cancel := context.WithTimeout(reqCtx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, "http://"+info.Addr+"/v1/control", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+info.Token)

	resp, err := liveIPCClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !connected.Load() {
			_ = os.Remove(ipcPath)
			return nil, errIPCUnavailable
		}
		return nil, fmt.Errorf("agent ipc request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errors.New("agent ipc unauthorized")
	}
	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("agent ipc read failed: %w", err)
	}
	var parsed ControlResponse
	if resp.StatusCode != http.StatusOK {
		if json.Unmarshal(respBytes, &parsed) == nil && parsed.Error != "" {
			return nil, errors.New(parsed.Error)
		}
		return nil, fmt.Errorf("agent ipc status %d", resp.StatusCode)
	}
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return nil, fmt.Errorf("agent ipc invalid response: %w", err)
	}
	if !parsed.OK {
		if parsed.Error == "" {
			parsed.Error = "operation failed"
		}
		return nil, errors.New(parsed.Error)
	}
	return parsed.Result, nil
}
