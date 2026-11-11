package plugin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestDownloadRejectsInsecureURLsBeforeSending(t *testing.T) {
	client := &http.Client{Transport: rejectTransport{t}}
	for _, address := range []string{
		"http://example.com/index.json",
		"http://127.0.0.1/index.json",
		"http://127.0.0.1.example.com/index.json",
		"http://127.0.0.1@example.com/index.json",
		"http://[::1]/index.json",
		"file:///tmp/index.json",
		"https:///index.json",
	} {
		t.Run(address, func(t *testing.T) {
			if _, err := FetchIndex(context.Background(), client, address); err == nil {
				t.Fatal("insecure URL accepted")
			}
		})
	}
}

type rejectTransport struct{ t *testing.T }

func (r rejectTransport) RoundTrip(*http.Request) (*http.Response, error) {
	r.t.Error("an insecure request reached the transport")
	return nil, errors.New("unexpected request")
}

func TestDownloadRejectsHTTPSDowngrade(t *testing.T) {
	var sent atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Add(1)
		fmt.Fprint(w, `{"version":1,"plugins":[]}`)
	}))
	defer plain.Close()
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL, http.StatusFound)
	}))
	defer tls.Close()
	client := tls.Client()
	if _, err := FetchIndex(context.Background(), client, tls.URL); err == nil {
		t.Fatal("HTTPS-to-HTTP redirect accepted")
	}
	if sent.Load() != 0 {
		t.Fatal("downgrade was rejected only after sending plaintext")
	}
	if client.CheckRedirect != nil {
		t.Fatal("the caller's shared client was modified")
	}
}

func TestDownloadPreservesHTTPSRedirectsAndCallerPolicy(t *testing.T) {
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/index.json", http.StatusFound)
			return
		}
		fmt.Fprint(w, `{"version":1,"plugins":[]}`)
	}))
	defer tls.Close()
	client := tls.Client()
	idx, err := FetchIndex(context.Background(), client, tls.URL+"/start")
	if err != nil || idx.Version != 1 {
		t.Fatal("valid HTTPS redirect failed", err)
	}
	denied := errors.New("caller refused redirect")
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return denied }
	if _, err := FetchIndex(context.Background(), client, tls.URL+"/start"); !errors.Is(err, denied) {
		t.Fatal("caller policy was ignored", err)
	}
}
