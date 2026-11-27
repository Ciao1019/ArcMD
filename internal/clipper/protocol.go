// Package clipper implements the Chrome Native Messaging wire protocol,
// cross-platform browser host registration, URL normalization, and safe image
// validation for the ArcMD Web Clipper extension. It has no UI dependencies.
package clipper

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	// HostName is the reverse-DNS Native Messaging host identifier registered
	// with Chromium-based browsers (lowercase letters, digits, underscores, dots only).
	HostName = "com.cizai.arcmd.clipper"
	// HostDescription is written to the browser Native Messaging manifest.
	HostDescription = "ArcMD Web Clipper Native Messaging Host"
	// ProtocolVersion is the wire protocol version returned by clip.ping.
	ProtocolVersion = 1
	// DefaultExtensionID is derived from the pinned RSA public key in
	// Ciao1019/arcmd-clipper so unpacked installs work out of the box.
	DefaultExtensionID = "njbinggncmencnnnhcccpamjiolhglen"
	// ExtensionRepoURL links users to the companion browser extension repository.
	ExtensionRepoURL = "https://github.com/Ciao1019/arcmd-clipper"

	// MaxIncomingFrameBytes caps a single extension -> host frame (16 MiB:
	// enough for one 10 MiB image encoded as base64 plus JSON envelope).
	MaxIncomingFrameBytes uint32 = 16 << 20
	// MaxOutgoingFrameBytes caps a single host -> extension frame safely below
	// Chrome's hard 1 MiB Native Messaging response limit.
	MaxOutgoingFrameBytes uint32 = 900 << 10
)

// Request is a single Native Messaging call from the browser extension.
type Request struct {
	ID     string          `json:"id,omitempty"`
	Action string          `json:"action"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is the reply sent back over stdout for one Request.
type Response struct {
	ID     string `json:"id,omitempty"`
	OK     bool   `json:"ok"`
	Live   bool   `json:"live,omitempty"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Handler executes a single clipper action and reports whether it ran against
// the live desktop window (live=true) or fell back to headless disk mode.
type Handler func(ctx context.Context, action string, params json.RawMessage) (result any, live bool, err error)

// IsNativeMessagingInvocation reports whether argv indicates a Native Messaging
// host launch — either via explicit `arcmd native-host` or Chromium's automatic
// invocation passing `chrome-extension://<id>/` (or `moz-extension://...`) as argv[1].
func IsNativeMessagingInvocation(args []string) bool {
	if len(args) == 0 {
		return false
	}
	first := strings.TrimSpace(args[0])
	return first == "native-host" ||
		strings.HasPrefix(first, "chrome-extension://") ||
		strings.HasPrefix(first, "moz-extension://")
}

// ReadFrame reads one length-prefixed Native Messaging JSON payload.
func ReadFrame(r io.Reader, maxBytes uint32) ([]byte, error) {
	if maxBytes == 0 {
		maxBytes = MaxIncomingFrameBytes
	}
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(lenBuf[:])
	if n == 0 {
		return nil, errors.New("clipper: empty native messaging frame")
	}
	if n > maxBytes {
		return nil, fmt.Errorf("clipper: incoming frame too large (%d > %d bytes)", n, maxBytes)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// WriteFrame writes one length-prefixed Native Messaging JSON payload.
func WriteFrame(w io.Writer, payload []byte) error {
	if uint64(len(payload)) > uint64(MaxOutgoingFrameBytes) {
		return fmt.Errorf("clipper: outgoing frame too large (%d > %d bytes)", len(payload), MaxOutgoingFrameBytes)
	}
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(payload)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// Serve runs the Chrome Native Messaging request/response loop until EOF or ctx cancellation.
func Serve(ctx context.Context, r io.Reader, w io.Writer, handler Handler) error {
	if handler == nil {
		return errors.New("clipper: handler is nil")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := ReadFrame(r, MaxIncomingFrameBytes)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			// Oversized or malformed frame length cannot be recovered cleanly on a byte stream.
			_ = writeResponse(w, Response{OK: false, Error: err.Error()})
			return err
		}
		var req Request
		if err := json.Unmarshal(raw, &req); err != nil {
			if wErr := writeResponse(w, Response{OK: false, Error: fmt.Sprintf("invalid json: %v", err)}); wErr != nil {
				return wErr
			}
			continue
		}
		action := strings.TrimSpace(req.Action)
		if action == "" {
			if wErr := writeResponse(w, Response{ID: req.ID, OK: false, Error: "missing action"}); wErr != nil {
				return wErr
			}
			continue
		}
		res, live, callErr := handler(ctx, action, req.Params)
		resp := Response{ID: req.ID, Live: live}
		if callErr != nil {
			resp.OK = false
			resp.Error = callErr.Error()
		} else {
			resp.OK = true
			resp.Result = res
		}
		if wErr := writeResponse(w, resp); wErr != nil {
			return wErr
		}
	}
}

func writeResponse(w io.Writer, resp Response) error {
	raw, err := json.Marshal(resp)
	if err != nil {
		fallback, _ := json.Marshal(Response{
			ID:    resp.ID,
			OK:    false,
			Error: fmt.Sprintf("marshal response failed: %v", err),
		})
		return WriteFrame(w, fallback)
	}
	if uint64(len(raw)) > uint64(MaxOutgoingFrameBytes) {
		fallback, _ := json.Marshal(Response{
			ID:    resp.ID,
			OK:    false,
			Live:  resp.Live,
			Error: fmt.Sprintf("response exceeds native messaging limit (%d bytes)", len(raw)),
		})
		return WriteFrame(w, fallback)
	}
	return WriteFrame(w, raw)
}

// ParseExtensionID normalizes a Chrome extension ID or full chrome-extension://<id>/
// origin string, returning the lowercase 32-char a..p ID and true when valid.
func ParseExtensionID(raw string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.TrimPrefix(s, "chrome-extension://")
	s = strings.TrimSuffix(s, "/")
	if len(s) != 32 {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 'a' || s[i] > 'p' {
			return "", false
		}
	}
	return s, true
}

// NormalizeExtensionIDs returns a deduplicated list starting with DefaultExtensionID
// followed by any valid additional extension IDs in order.
func NormalizeExtensionIDs(extra []string) []string {
	seen := map[string]bool{DefaultExtensionID: true}
	out := []string{DefaultExtensionID}
	for _, raw := range extra {
		id, ok := ParseExtensionID(raw)
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// AllowedOrigins formats NormalizeExtensionIDs(extra) as Chrome Native Messaging
// `allowed_origins` entries (`chrome-extension://<id>/`).
func AllowedOrigins(extra []string) []string {
	ids := NormalizeExtensionIDs(extra)
	origins := make([]string, len(ids))
	for i, id := range ids {
		origins[i] = "chrome-extension://" + id + "/"
	}
	return origins
}
