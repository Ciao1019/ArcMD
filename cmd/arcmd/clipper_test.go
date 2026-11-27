package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/app"
	"github.com/Ciao1019/ArcMD/internal/clipper"
)

func TestDispatchCLINativeHostAutoDetectAndPing(t *testing.T) {
	home := t.TempDir()
	cfg := app.Config{
		ConfigDir:    filepath.Join(home, "config"),
		DocumentsDir: filepath.Join(home, "spaces"),
	}

	var stdin, stdout, stderr bytes.Buffer
	reqRaw, _ := json.Marshal(clipper.Request{ID: "ping-1", Action: "clip.ping"})
	if err := clipper.WriteFrame(&stdin, reqRaw); err != nil {
		t.Fatal(err)
	}

	// Simulate Chrome launching ArcMD.exe with chrome-extension://<id>/ as argv[1].
	code, handled := dispatchCLI(
		[]string{"chrome-extension://" + clipper.DefaultExtensionID + "/", "--parent-window=0"},
		cfg, &stdin, &stdout, &stderr,
	)
	if !handled || code != 0 {
		t.Fatalf("dispatchCLI native-host = (code=%d, handled=%v), stderr=%q", code, handled, stderr.String())
	}

	respRaw, err := clipper.ReadFrame(&stdout, clipper.MaxOutgoingFrameBytes)
	if err != nil {
		t.Fatalf("ReadFrame stdout: %v", err)
	}
	var resp clipper.Response
	if err := json.Unmarshal(respRaw, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.ID != "ping-1" {
		t.Fatalf("unexpected native-host response: %+v", resp)
	}
}
