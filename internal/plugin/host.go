package plugin

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// MaxFrameBytes bounds a framed request or response exchanged across the WASM boundary.
const MaxFrameBytes = 64<<20 + 1<<20

// Host runs plugins of one Runtime.
type Host interface {
	Runtime() Runtime
	// Start loads an installed plugin. The host enforces the manifest's
	// permissions on every call the plugin makes back into ArcMD.
	Start(ctx context.Context, p *Installed) (Instance, error)
}

// Instance is a running plugin.
type Instance interface {
	// Invoke runs one of the plugin's commands, or renders one of its
	// panels (Request.Panel), and returns what ArcMD should do.
	Invoke(ctx context.Context, req Request) (*Response, error)
	Close() error
}

// Request is what ArcMD tells a plugin when invoking it. Fields the
// plugin's permissions do not cover are left empty.
type Request struct {
	Command     string         `json:"command,omitempty"`
	Panel       string         `json:"panel,omitempty"`
	Args        map[string]any `json:"args,omitempty"`
	ActiveSpace string         `json:"activeSpace,omitempty"`
	// Document is the open document: its path in the space and its text
	// (document:read).
	Document *DocumentContext `json:"document,omitempty"`
}

// DocumentContext is the open document as a plugin sees it.
type DocumentContext struct {
	Path string `json:"path"`
	Text string `json:"text"`
	// Selection is in bytes of Text; Start == End is the caret.
	Selection Range `json:"selection"`
}

// Range is a range of bytes of a text.
type Range struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Response is what a plugin asks ArcMD to do.
type Response struct {
	// Edits change the open document (document:write), as one undo step.
	Edits []Edit `json:"edits,omitempty"`
	// Markdown is shown in the panel invoked, or in a sheet for a command.
	Markdown string `json:"markdown,omitempty"`
	// Toast is a short message to show.
	Toast string `json:"toast,omitempty"`
	// Status is an optional short status line surfaced on the plugin card.
	Status string `json:"status,omitempty"`
	// Data carries optional structured payload returned to command callers.
	Data map[string]any `json:"data,omitempty"`
}

// Edit replaces Range of the open document with Text.
type Edit struct {
	Range Range  `json:"range"`
	Text  string `json:"text"`
}

// SpaceInfo describes a space exposed to plugins with space:read.
type SpaceInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SpaceFileEntry describes a syncable file in a space.
type SpaceFileEntry struct {
	Path            string `json:"path"`
	Size            int64  `json:"size"`
	ModTimeUnixNano int64  `json:"modTimeUnixNano"`
	Dirty           bool   `json:"dirty,omitempty"`
}

// SpaceFileMeta describes a space file read or written through the host.
type SpaceFileMeta struct {
	Path            string `json:"path"`
	Size            int64  `json:"size"`
	ModTimeUnixNano int64  `json:"modTimeUnixNano"`
	SHA256          string `json:"sha256,omitempty"`
	Dirty           bool   `json:"dirty,omitempty"`
}

// SpaceFileKey identifies a space and a slash-separated relative path inside it.
type SpaceFileKey struct {
	SpaceID string
	Path    string
}

// SpaceService mediates sandboxed space file access on behalf of a plugin invocation.
type SpaceService interface {
	ListSpaces() []SpaceInfo
	ListFiles(spaceID string) ([]SpaceFileEntry, error)
	ReadFile(spaceID, rel string) ([]byte, SpaceFileMeta, error)
	WriteFile(spaceID, rel string, data []byte, expectedSHA256 string, modTimeUnixNano int64) (SpaceFileMeta, error)
	WriteConflict(spaceID, rel string, data []byte) (SpaceFileMeta, error)
	DeleteFile(spaceID, rel, expectedSHA256 string) error
	Touched() []SpaceFileKey
}

// SecretReader reads plugin-scoped credentials without exposing other secrets.
type SecretReader interface {
	Get(key string) ([]byte, error)
}

// HostConfig provides sandboxed host capabilities to WASMHost.
type HostConfig struct {
	DataDir    string
	Config     *ConfigStore
	Secrets    SecretReader
	HTTPClient func() *http.Client
	Spaces     func(context.Context) SpaceService
}

// HostRequest is the JSON header of a guest-to-host ABI call.
type HostRequest struct {
	Op                string            `json:"op"`
	Key               string            `json:"key,omitempty"`
	SpaceID           string            `json:"spaceId,omitempty"`
	Path              string            `json:"path,omitempty"`
	ExpectedSHA256    string            `json:"expectedSha256,omitempty"`
	ModTimeUnixNano   int64             `json:"modTimeUnixNano,omitempty"`
	Method            string            `json:"method,omitempty"`
	URL               string            `json:"url,omitempty"`
	Headers           map[string]string `json:"headers,omitempty"`
	AllowInsecureHTTP bool              `json:"allowInsecureHttp,omitempty"`
}

// HostReply is the JSON header of a host-to-guest ABI response.
type HostReply struct {
	OK      bool              `json:"ok"`
	Error   string            `json:"error,omitempty"`
	Code    string            `json:"code,omitempty"`
	Status  int               `json:"status,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Value   string            `json:"value,omitempty"`
	Config  map[string]any    `json:"config,omitempty"`
	Spaces  []SpaceInfo       `json:"spaces,omitempty"`
	Files   []SpaceFileEntry  `json:"files,omitempty"`
	File    *SpaceFileMeta    `json:"file,omitempty"`
	Deleted bool              `json:"deleted,omitempty"`
}

// Common HostReply error codes returned to plugins.
const (
	CodePermissionDenied = "permission_denied"
	CodeNotFound         = "not_found"
	CodeConflict         = "conflict"
	CodeDirty            = "dirty"
	CodeInvalidArgument  = "invalid_argument"
	CodeNetworkError     = "network_error"
	CodeInternal         = "internal"
)

type spaceCtxKey struct{}

// WithSpaceService attaches a per-invocation SpaceService to ctx.
func WithSpaceService(ctx context.Context, svc SpaceService) context.Context {
	return context.WithValue(ctx, spaceCtxKey{}, svc)
}

// SpaceServiceFromContext returns the SpaceService attached to ctx, or nil.
func SpaceServiceFromContext(ctx context.Context) SpaceService {
	if svc, ok := ctx.Value(spaceCtxKey{}).(SpaceService); ok {
		return svc
	}
	return nil
}

// EncodeFrame packs a JSON header and optional binary payload into a length-prefixed frame.
func EncodeFrame(header any, body []byte) ([]byte, error) {
	hdr, err := json.Marshal(header)
	if err != nil {
		return nil, err
	}
	total := 4 + len(hdr) + len(body)
	if total > MaxFrameBytes {
		return nil, fmt.Errorf("plugin: frame size %d exceeds %d", total, MaxFrameBytes)
	}
	out := make([]byte, total)
	binary.BigEndian.PutUint32(out[:4], uint32(len(hdr)))
	copy(out[4:], hdr)
	copy(out[4+len(hdr):], body)
	return out, nil
}

// DecodeFrame unpacks a length-prefixed frame into headerOut and binary body slice.
func DecodeFrame(frame []byte, headerOut any) ([]byte, error) {
	if len(frame) < 4 {
		return nil, errors.New("plugin: truncated frame header")
	}
	hdrLen := int(binary.BigEndian.Uint32(frame[:4]))
	if hdrLen < 0 || 4+hdrLen > len(frame) {
		return nil, errors.New("plugin: invalid frame header length")
	}
	if err := json.Unmarshal(frame[4:4+hdrLen], headerOut); err != nil {
		return nil, err
	}
	return frame[4+hdrLen:], nil
}
