package clipper

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

func TestFrameRoundTripAndServe(t *testing.T) {
	var in bytes.Buffer
	write := func(v any) {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err := WriteFrame(&in, raw); err != nil {
			t.Fatal(err)
		}
	}
	write(Request{ID: "1", Action: "clip.ping"})
	write(Request{ID: "2", Action: "clip.fail"})
	write(Request{ID: "3", Action: ""})

	var out bytes.Buffer
	err := Serve(context.Background(), &in, &out, func(ctx context.Context, action string, params json.RawMessage) (any, bool, error) {
		switch action {
		case "clip.ping":
			return map[string]any{"version": "0.5.0"}, true, nil
		case "clip.fail":
			return nil, false, errSentinel("boom")
		default:
			t.Fatalf("unexpected action %q", action)
			return nil, false, nil
		}
	})
	if err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	var resps []Response
	for out.Len() > 0 {
		raw, err := ReadFrame(&out, MaxOutgoingFrameBytes)
		if err != nil {
			t.Fatalf("ReadFrame failed: %v", err)
		}
		var r Response
		if err := json.Unmarshal(raw, &r); err != nil {
			t.Fatalf("Unmarshal response failed: %v", err)
		}
		resps = append(resps, r)
	}
	if len(resps) != 3 {
		t.Fatalf("got %d responses, want 3: %+v", len(resps), resps)
	}
	if resps[0].ID != "1" || !resps[0].OK || !resps[0].Live {
		t.Fatalf("resp[0] = %+v", resps[0])
	}
	if resps[1].ID != "2" || resps[1].OK || resps[1].Error != "boom" {
		t.Fatalf("resp[1] = %+v", resps[1])
	}
	if resps[2].ID != "3" || resps[2].OK || !strings.Contains(resps[2].Error, "missing action") {
		t.Fatalf("resp[2] = %+v", resps[2])
	}
}

type errSentinel string

func (e errSentinel) Error() string { return string(e) }

func TestServeCapsOversizedOutgoingResponse(t *testing.T) {
	var in bytes.Buffer
	raw, _ := json.Marshal(Request{ID: "big", Action: "clip.big"})
	_ = WriteFrame(&in, raw)
	var out bytes.Buffer
	err := Serve(context.Background(), &in, &out, func(ctx context.Context, action string, params json.RawMessage) (any, bool, error) {
		return map[string]string{"blob": strings.Repeat("A", int(MaxOutgoingFrameBytes)+1024)}, true, nil
	})
	if err != nil {
		t.Fatalf("Serve error: %v", err)
	}
	respRaw, err := ReadFrame(&out, MaxOutgoingFrameBytes)
	if err != nil {
		t.Fatalf("ReadFrame error: %v", err)
	}
	var r Response
	if err := json.Unmarshal(respRaw, &r); err != nil {
		t.Fatal(err)
	}
	if r.OK || r.ID != "big" || !strings.Contains(r.Error, "exceeds native messaging limit") {
		t.Fatalf("unexpected oversized fallback: %+v", r)
	}
}

func TestReadFrameRejectsOversizedIncoming(t *testing.T) {
	var buf bytes.Buffer
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], MaxIncomingFrameBytes+1)
	buf.Write(hdr[:])
	if _, err := ReadFrame(&buf, MaxIncomingFrameBytes); err == nil {
		t.Fatal("expected oversized frame error")
	}
}

func TestIsNativeMessagingInvocation(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"README.md"}, false},
		{[]string{"native-host"}, true},
		{[]string{"chrome-extension://" + DefaultExtensionID + "/", "--parent-window=12345"}, true},
		{[]string{"moz-extension://12345678-1234-1234-1234-123456789abc/"}, true},
	}
	for _, tc := range cases {
		if got := IsNativeMessagingInvocation(tc.args); got != tc.want {
			t.Fatalf("IsNativeMessagingInvocation(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestExtensionIDsAndAllowedOrigins(t *testing.T) {
	custom := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	origins := AllowedOrigins([]string{
		"CHROME-EXTENSION://" + custom + "/",
		DefaultExtensionID,
		"invalid-id",
		"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", // > 'p'
		custom,
	})
	if len(origins) != 2 {
		t.Fatalf("got origins %v, want 2", origins)
	}
	if origins[0] != "chrome-extension://"+DefaultExtensionID+"/" {
		t.Fatalf("origins[0] = %q", origins[0])
	}
	if origins[1] != "chrome-extension://"+custom+"/" {
		t.Fatalf("origins[1] = %q", origins[1])
	}
}

func TestNormalizeURL(t *testing.T) {
	a := NormalizeURL("HTTPS://Example.COM:443/post/123/?b=2&utm_source=twitter&a=1&fbclid=xyz#section")
	b := NormalizeURL("https://example.com/post/123?a=1&b=2")
	if a != b {
		t.Fatalf("normalized mismatch:\n a=%q\n b=%q", a, b)
	}
	wx1 := NormalizeURL("https://mp.weixin.qq.com/s?__biz=MzA=&mid=100&idx=1&sn=abc&chksm=deadbeef&scene=21#wechat_redirect")
	wx2 := NormalizeURL("https://mp.weixin.qq.com/s?__biz=MzA=&idx=1&mid=100&sn=abc")
	if wx1 != wx2 {
		t.Fatalf("wechat normalized mismatch:\n 1=%q\n 2=%q", wx1, wx2)
	}
}

func TestSafeDocTitle(t *testing.T) {
	got := SafeDocTitle(`  Hello: A/B*C? "Test" | <Tag> \ End...  `)
	want := "Hello A B C Test Tag End"
	if got != want {
		t.Fatalf("SafeDocTitle = %q, want %q", got, want)
	}
	if SafeDocTitle("   ///...   ") != "Untitled Clip" {
		t.Fatal("expected fallback title")
	}
}

func TestValidateImage(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	ext, name, err := ValidateImage("../evil/photo.jpeg", "image/jpeg", png)
	if err != nil {
		t.Fatalf("ValidateImage PNG failed: %v", err)
	}
	if ext != ".png" || name != "photo.png" {
		t.Fatalf("got (%q, %q), want (.png, photo.png)", ext, name)
	}

	webp := append([]byte("RIFF\x24\x00\x00\x00WEBP"), make([]byte, 16)...)
	ext, name, err = ValidateImage("hero", "", webp)
	if err != nil || ext != ".webp" || name != "hero.webp" {
		t.Fatalf("ValidateImage WebP = (%q, %q, %v)", ext, name, err)
	}

	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	if _, _, err := ValidateImage("icon.svg", "image/svg+xml", svg); err == nil {
		t.Fatal("expected SVG to be rejected")
	}
	if _, _, err := ValidateImage("empty.png", "image/png", nil); err == nil {
		t.Fatal("expected empty image to be rejected")
	}
}
