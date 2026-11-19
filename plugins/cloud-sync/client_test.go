package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/plugin"
)

func TestS3VirtualHostedSigV4AndErrors(t *testing.T) {
	var gotMethod, gotURL string
	var gotHeaders map[string]string

	setNativeHostBridge(func(reqFrame []byte) ([]byte, error) {
		var req plugin.HostRequest
		if _, err := plugin.DecodeFrame(reqFrame, &req); err != nil {
			t.Fatal(err)
		}
		gotMethod = req.Method
		gotURL = req.URL
		gotHeaders = req.Headers
		body := `<Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`
		return plugin.EncodeFrame(plugin.HostReply{OK: true, Status: 403}, []byte(body))
	})
	defer setNativeHostBridge(nil)

	c, err := newS3Client("https://s3.us-east-1.amazonaws.com", "us-east-1", "my-notes", "AKIA", "SECRET", "ArcMD", false, false)
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC) }

	err = c.Test()
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("expected AccessDenied error, got %v", err)
	}
	if gotMethod != http.MethodGet || !strings.HasPrefix(gotURL, "https://my-notes.s3.us-east-1.amazonaws.com/?") {
		t.Fatalf("virtual-hosted URL = %s %s", gotMethod, gotURL)
	}
	if gotHeaders["Host"] != "my-notes.s3.us-east-1.amazonaws.com" {
		t.Fatalf("Host header = %q", gotHeaders["Host"])
	}
	if !strings.Contains(gotHeaders["Authorization"], "Credential=AKIA/20261119/us-east-1/s3/aws4_request") {
		t.Fatalf("bad Authorization = %q", gotHeaders["Authorization"])
	}
}

func TestWebDAVStripHrefAndAuthError(t *testing.T) {
	rel, ok := stripDAVHref("https://dav.jianguoyun.com/dav/ArcMD/Work/%E7%AC%94%E8%AE%B0/2026%20%281%29.md", "/dav/ArcMD/Work", "/dav/ArcMD/Work")
	if !ok || rel != "笔记/2026 (1).md" {
		t.Fatalf("stripDAVHref(full) = %q, %v", rel, ok)
	}
	rel, ok = stripDAVHref("/dav/ArcMD/Work/", "/dav/ArcMD/Work", "/dav/ArcMD/Work")
	if ok || rel != "" {
		t.Fatalf("self directory should be skipped: %q, %v", rel, ok)
	}

	setNativeHostBridge(func(reqFrame []byte) ([]byte, error) {
		return plugin.EncodeFrame(plugin.HostReply{OK: true, Status: 401}, nil)
	})
	defer setNativeHostBridge(nil)

	c, err := newWebDAVClient("https://dav.jianguoyun.com/dav/", "u", "p", "ArcMD", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Test(); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected 401 auth error, got %v", err)
	}
}

func TestSpacePrefixesDisambiguation(t *testing.T) {
	got := spacePrefixes([]spaceInfo{
		{ID: "abcdef1234", Name: "Work"},
		{ID: "9999998888", Name: "Work"},
		{ID: "personal1", Name: "Personal"},
	})
	if got["abcdef1234"] == got["9999998888"] {
		t.Fatalf("duplicate names collided: %+v", got)
	}
	if got["personal1"] != "Personal" {
		t.Fatalf("unique name altered: %+v", got)
	}
	var _ = bytes.Compare
}
