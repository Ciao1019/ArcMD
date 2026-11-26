package mdz

import (
	"archive/zip"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/unicode/norm"
)

type rawEntry struct {
	name   string
	data   []byte
	method uint16
	flags  uint16
	mode   fs.FileMode
}

func makeRawZIP(t *testing.T, entries []rawEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		hdr := &zip.FileHeader{
			Name:   e.name,
			Method: e.method,
			Flags:  e.flags,
		}
		if hdr.Method == 0 && e.method == 0 {
			hdr.Method = zip.Store
		}
		if e.mode != 0 {
			hdr.SetMode(e.mode)
		} else if !strings.HasSuffix(e.name, "/") {
			hdr.SetMode(0o644)
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("CreateHeader(%q): %v", e.name, err)
		}
		if len(e.data) > 0 {
			if _, err := w.Write(e.data); err != nil {
				t.Fatalf("Write(%q): %v", e.name, err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close zip: %v", err)
	}
	return buf.Bytes()
}

func TestBuildAndOpenRoundTrip(t *testing.T) {
	now := time.Date(2026, 4, 1, 10, 30, 0, 0, time.UTC)
	pngBytes := []byte("\x89PNG\r\n\x1a\nfake-png-payload")
	data, err := Build(WriteOptions{
		Manifest: Manifest{
			Title:    "Architecture Notes",
			Mode:     ModeDocument,
			Language: "zh-CN",
			Cover:    "assets/Cover.png",
			Producer: &ProducerInfo{
				Application: &ToolInfo{
					Name:    "ArcMD",
					Version: "0.4.0",
					URL:     "https://github.com/Ciao1019/ArcMD",
				},
			},
		},
		Files: []File{
			{Path: "index.md", Data: []byte("# Title\r\n\r\n![img](assets/Cover.png)\r\n")},
			{Path: "assets/Cover.png", Data: pngBytes},
			{Path: "notes/detail.md", Data: []byte("## Detail\nBody\n")},
		},
		ModTime: now,
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Open from bytes.
	arc, err := OpenBytes(data)
	if err != nil {
		t.Fatalf("OpenBytes failed: %v", err)
	}
	defer func() { _ = arc.Close() }()

	if arc.EntryPoint() != "index.md" {
		t.Fatalf("EntryPoint = %q, want index.md", arc.EntryPoint())
	}
	if arc.Mode() != ModeDocument {
		t.Fatalf("Mode = %q, want document", arc.Mode())
	}
	if arc.Title() != "Architecture Notes" {
		t.Fatalf("Title = %q, want Architecture Notes", arc.Title())
	}
	if arc.CoverPath() != "assets/Cover.png" {
		t.Fatalf("CoverPath = %q, want assets/Cover.png", arc.CoverPath())
	}
	m := arc.Manifest()
	if m == nil || m.Spec == nil || m.Spec.Version != SpecVersion {
		t.Fatalf("manifest spec mismatch: %+v", m)
	}
	if m.Created == nil || m.Created.When != "2026-04-01T10:30:00Z" {
		t.Fatalf("manifest created mismatch: %+v", m.Created)
	}

	// Verify Markdown was normalized to LF while binary asset stayed exact and stored uncompressed.
	rawIndex, err := arc.ReadFile("index.md")
	if err != nil {
		t.Fatalf("ReadFile(index.md): %v", err)
	}
	if bytes.Contains(rawIndex, []byte("\r")) {
		t.Fatalf("index.md should have LF endings, got %q", string(rawIndex))
	}
	gotPNG, err := arc.ReadFile("assets/Cover.png")
	if err != nil || !bytes.Equal(gotPNG, pngBytes) {
		t.Fatalf("ReadFile(assets/Cover.png) mismatch: err=%v", err)
	}
	for _, e := range arc.Entries() {
		if e.Path == "assets/Cover.png" && e.Method != zip.Store {
			t.Fatalf("PNG entry should use Store (0), got method %d", e.Method)
		}
		if e.Path == "index.md" && e.Method != zip.Deflate {
			t.Fatalf("Markdown entry should use Deflate (8), got method %d", e.Method)
		}
	}

	// Verify disk Open releases file handle immediately so Windows rename/remove succeeds.
	dir := t.TempDir()
	p1 := filepath.Join(dir, "doc.mdz")
	if err := os.WriteFile(p1, data, 0o644); err != nil {
		t.Fatal(err)
	}
	diskArc, err := Open(p1)
	if err != nil {
		t.Fatalf("Open(%s): %v", p1, err)
	}
	p2 := filepath.Join(dir, "renamed.mdz")
	if err := os.Rename(p1, p2); err != nil {
		t.Fatalf("Rename while open failed (file handle leaked?): %v", err)
	}
	diskArc.SetPath(p2)
	mdText, err := diskArc.ReadMarkdown("notes/detail.md")
	if err != nil || !strings.Contains(mdText, "Body") {
		t.Fatalf("ReadMarkdown after rename failed: %q, %v", mdText, err)
	}
	_ = diskArc.Close()
}

func TestEntryPointDiscoverySpec55(t *testing.T) {
	t.Run("rule1_manifest_entrypoint_with_leading_dotslash", func(t *testing.T) {
		z := makeRawZIP(t, []rawEntry{
			{name: "manifest.json", data: []byte(`{"spec":{"version":"1.2.0"},"entryPoint":"./chapters/start.md"}`)},
			{name: "index.md", data: []byte("# Not Entry")},
			{name: "chapters/start.md", data: []byte("# Start")},
		})
		arc, err := OpenBytes(z)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if arc.EntryPoint() != "chapters/start.md" {
			t.Fatalf("EntryPoint = %q, want chapters/start.md", arc.EntryPoint())
		}
	})

	t.Run("rule1_double_dotslash_is_invalid", func(t *testing.T) {
		z := makeRawZIP(t, []rawEntry{
			{name: "manifest.json", data: []byte(`{"entryPoint":"././index.md"}`)},
			{name: "index.md", data: []byte("# Hi")},
		})
		_, err := OpenBytes(z)
		if !errors.Is(err, ErrEntryPointInvalid) {
			t.Fatalf("err = %v, want ErrEntryPointInvalid", err)
		}
	})

	t.Run("rule1_missing_reports_case_near_match", func(t *testing.T) {
		z := makeRawZIP(t, []rawEntry{
			{name: "manifest.json", data: []byte(`{"entryPoint":"Index.md"}`)},
			{name: "index.md", data: []byte("# Lowercase")},
		})
		_, err := OpenBytes(z)
		if !errors.Is(err, ErrEntryPointMissing) {
			t.Fatalf("err = %v, want ErrEntryPointMissing", err)
		}
		if !strings.Contains(err.Error(), "index.md") {
			t.Fatalf("expected case near-match hint in error, got %v", err)
		}
	})

	t.Run("rule2_no_manifest_picks_index_md", func(t *testing.T) {
		z := makeRawZIP(t, []rawEntry{
			{name: "index.md", data: []byte("# Home")},
			{name: "other.md", data: []byte("# Other")},
		})
		arc, err := OpenBytes(z)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if arc.EntryPoint() != "index.md" {
			t.Fatalf("EntryPoint = %q, want index.md", arc.EntryPoint())
		}
	})

	t.Run("rule3_ignores_closed_support_files", func(t *testing.T) {
		z := makeRawZIP(t, []rawEntry{
			{name: "README.md", data: []byte("readme")},
			{name: "AGENTS.MD", data: []byte("agents")},
			{name: "License.markdown", data: []byte("mit")},
			{name: "changelog.md", data: []byte("log")},
			{name: "CONTRIBUTING.md", data: []byte("contrib")},
			{name: "notes.markdown", data: []byte("# Actual Content")},
			{name: "sub/extra.md", data: []byte("# Nested")},
		})
		arc, err := OpenBytes(z)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if arc.EntryPoint() != "notes.markdown" {
			t.Fatalf("EntryPoint = %q, want notes.markdown", arc.EntryPoint())
		}
	})

	t.Run("rule3_sole_support_file_is_selected", func(t *testing.T) {
		z := makeRawZIP(t, []rawEntry{
			{name: "README.md", data: []byte("# Only File")},
			{name: "sub/doc.md", data: []byte("# Sub")},
		})
		arc, err := OpenBytes(z)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if arc.EntryPoint() != "README.md" {
			t.Fatalf("EntryPoint = %q, want README.md", arc.EntryPoint())
		}
	})

	t.Run("rule4_two_support_files_ambiguous", func(t *testing.T) {
		z := makeRawZIP(t, []rawEntry{
			{name: "README.md", data: []byte("r")},
			{name: "AGENTS.md", data: []byte("a")},
		})
		arc, err := OpenBytes(z)
		if !errors.Is(err, ErrEntryPointUnresolved) {
			t.Fatalf("err = %v, want ErrEntryPointUnresolved", err)
		}
		if arc == nil || len(arc.MarkdownPages()) != 2 {
			t.Fatalf("expected non-nil archive with 2 pages for user choice, got %+v", arc)
		}
	})

	t.Run("rule4_nested_only_is_ambiguous", func(t *testing.T) {
		z := makeRawZIP(t, []rawEntry{
			{name: "docs/only.md", data: []byte("# Nested")},
		})
		arc, err := OpenBytes(z)
		if !errors.Is(err, ErrEntryPointUnresolved) {
			t.Fatalf("err = %v, want ErrEntryPointUnresolved", err)
		}
		if arc == nil || len(arc.MarkdownPages()) != 1 || arc.MarkdownPages()[0] != "docs/only.md" {
			t.Fatalf("expected nested page listed, got %v", arc.MarkdownPages())
		}
	})
}

func TestManifestValidationSpec6And11(t *testing.T) {
	t.Run("descriptive_errors_ignored_and_unknown_fields_preserved", func(t *testing.T) {
		manifest := `{
			"spec": {"name": 123, "version": "1.99.0-beta.1+build.4"},
			"mode": "project",
			"entryPoint": "index.md",
			"title": "",
			"keywords": "not-an-array",
			"created": "2026-03-08T14:30:00Z",
			"modified": {"when": "2026-03-09T08:00:00+09:00", "by": {"name": "Alice"}},
			"cover": "missing.png",
			"futureCustomObject": {"nested": [1, true, null]}
		}`
		z := makeRawZIP(t, []rawEntry{
			{name: "manifest.json", data: []byte(manifest)},
			{name: "index.md", data: []byte("# Project")},
		})
		arc, err := OpenBytes(z)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if arc.Mode() != ModeProject {
			t.Fatalf("Mode = %q, want project", arc.Mode())
		}
		if arc.CoverPath() != "" {
			t.Fatalf("missing cover should be ignored, got %q", arc.CoverPath())
		}
		m := arc.Manifest()
		if m.Created == nil || m.Created.When != "2026-03-08T14:30:00Z" {
			t.Fatalf("legacy string created failed: %+v", m.Created)
		}
		if m.Modified == nil || m.Modified.By == nil || m.Modified.By.Name != "Alice" {
			t.Fatalf("object modified failed: %+v", m.Modified)
		}
		if len(arc.Warnings()) == 0 {
			t.Fatalf("expected non-fatal warnings for invalid descriptive fields")
		}
	})

	cases := []struct {
		name     string
		manifest string
		wantErr  error
	}{
		{"invalid_json", `{bad`, ErrManifestInvalid},
		{"top_level_array", `[]`, ErrManifestInvalid},
		{"null_spec", `{"spec": null}`, ErrManifestInvalid},
		{"bad_semver", `{"spec": {"version": "01.2.0"}}`, ErrManifestInvalid},
		{"unsupported_major", `{"spec": {"version": "2.0.0"}}`, ErrVersionUnsupported},
		{"null_mode", `{"mode": null}`, ErrManifestInvalid},
		{"uppercase_mode", `{"mode": "Document"}`, ErrModeUnsupported},
		{"unknown_mode", `{"mode": "slides"}`, ErrModeUnsupported},
		{"null_entrypoint", `{"entryPoint": null}`, ErrEntryPointInvalid},
		{"non_md_entrypoint", `{"entryPoint": "image.png"}`, ErrEntryPointInvalid},
		{"dir_entrypoint", `{"entryPoint": "docs/"}`, ErrEntryPointInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			z := makeRawZIP(t, []rawEntry{
				{name: "manifest.json", data: []byte(tc.manifest)},
				{name: "index.md", data: []byte("# Doc")},
			})
			_, err := OpenBytes(z)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestNFCPathMatchingAndSecuritySpec54And13(t *testing.T) {
	t.Run("nfc_matches_nfd_stored_entry", func(t *testing.T) {
		nfdName := norm.NFD.String("café.md")
		nfcName := norm.NFC.String("café.md")
		if nfdName == nfcName {
			t.Fatalf("expected NFD != NFC")
		}
		z := makeRawZIP(t, []rawEntry{
			{name: "manifest.json", data: []byte(`{"entryPoint":"` + nfcName + `"}`)},
			{name: nfdName, data: []byte("# Café")},
		})
		arc, err := OpenBytes(z)
		if err != nil {
			t.Fatalf("NFC lookup against NFD stored entry failed: %v", err)
		}
		if !arc.HasFile(nfcName) {
			t.Fatalf("HasFile(NFC) = false")
		}
		md, err := arc.ReadMarkdown(nfcName)
		if err != nil || md != "# Café" {
			t.Fatalf("ReadMarkdown(NFC) = %q, %v", md, err)
		}
	})

	t.Run("nfc_duplicate_rejected", func(t *testing.T) {
		z := makeRawZIP(t, []rawEntry{
			{name: norm.NFC.String("café.md"), data: []byte("a")},
			{name: norm.NFD.String("café.md"), data: []byte("b")},
		})
		_, err := OpenBytes(z)
		if !errors.Is(err, ErrDuplicatePath) {
			t.Fatalf("err = %v, want ErrDuplicatePath", err)
		}
	})

	t.Run("file_and_dir_conflict_rejected", func(t *testing.T) {
		z := makeRawZIP(t, []rawEntry{
			{name: "docs", data: []byte("file")},
			{name: "docs/index.md", data: []byte("# doc")},
		})
		_, err := OpenBytes(z)
		if !errors.Is(err, ErrPathConflict) {
			t.Fatalf("err = %v, want ErrPathConflict", err)
		}
	})

	badPaths := []string{
		"/abs.md",
		"../escape.md",
		"docs/../escape.md",
		"./index.md",
		"docs//index.md",
		"bad\\slash.md",
		"bad:colon.md",
		"ctrl\x07.md",
	}
	for _, bp := range badPaths {
		t.Run("reject_path_"+bp, func(t *testing.T) {
			z := makeRawZIP(t, []rawEntry{
				{name: bp, data: []byte("x")},
			})
			_, err := OpenBytes(z)
			if !errors.Is(err, ErrPathInvalid) {
				t.Fatalf("path %q: err = %v, want ErrPathInvalid", bp, err)
			}
		})
	}

	t.Run("reject_encrypted", func(t *testing.T) {
		z := makeRawZIP(t, []rawEntry{
			{name: "index.md", data: []byte("secret"), flags: 0x1},
		})
		_, err := OpenBytes(z)
		if !errors.Is(err, ErrZipEncrypted) {
			t.Fatalf("err = %v, want ErrZipEncrypted", err)
		}
	})

	t.Run("reject_symlink", func(t *testing.T) {
		z := makeRawZIP(t, []rawEntry{
			{name: "index.md", data: []byte("/etc/passwd"), mode: fs.ModeSymlink | 0o777},
		})
		_, err := OpenBytes(z)
		if !errors.Is(err, ErrPathInvalid) {
			t.Fatalf("err = %v, want ErrPathInvalid", err)
		}
	})
}

func TestResolveLinkSpec93(t *testing.T) {
	z := makeRawZIP(t, []rawEntry{
		{name: "index.md", data: []byte("# Home")},
		{name: "chapters/Mary Shelley/intro.md", data: []byte("# Intro")},
		{name: "chapters/file#1.md", data: []byte("# Hash")},
		{name: "images/logo.png", data: []byte("png")},
	})
	arc, err := OpenBytes(z)
	if err != nil {
		t.Fatal(err)
	}

	// Self-fragment & query preservation inside fragment.
	r := arc.ResolveLink("index.md", "#heading?raw=1")
	if r.Kind != LinkInternal || !r.Found || r.Path != "index.md" || r.Fragment != "heading?raw=1" {
		t.Fatalf("self fragment mismatch: %+v", r)
	}

	// Percent-decoding with spaces and ?query stripping before #.
	r = arc.ResolveLink("index.md", "chapters/Mary%20Shelley/intro.md?view=print#top?keep")
	if r.Kind != LinkInternal || !r.Found || r.Path != "chapters/Mary Shelley/intro.md" || r.Fragment != "top?keep" {
		t.Fatalf("percent decode mismatch: %+v", r)
	}

	// Encoded '#' (%23) is part of filename, not fragment delimiter.
	r = arc.ResolveLink("index.md", "chapters/file%231.md#sec")
	if r.Kind != LinkInternal || !r.Found || r.Path != "chapters/file#1.md" || r.Fragment != "sec" {
		t.Fatalf("encoded hash mismatch: %+v", r)
	}

	// Relative '..' staying inside archive.
	r = arc.ResolveLink("chapters/Mary Shelley/intro.md", "../../images/logo.png")
	if r.Kind != LinkInternal || !r.Found || r.Path != "images/logo.png" {
		t.Fatalf("relative parent mismatch: %+v", r)
	}

	// Relative '..' leaving archive must NOT clamp to root.
	r = arc.ResolveLink("chapters/Mary Shelley/intro.md", "../../../outside.md")
	if r.Kind != LinkOutsideArchive {
		t.Fatalf("expected LinkOutsideArchive, got %+v", r)
	}
	r = arc.ResolveLink("index.md", "../index.md")
	if r.Kind != LinkOutsideArchive {
		t.Fatalf("expected LinkOutsideArchive for ../index.md from root, got %+v", r)
	}

	// Case-only mismatch does not silently resolve, reports near match.
	r = arc.ResolveLink("index.md", "images/Logo.png")
	if r.Kind != LinkUnresolved || r.Found || r.NearMatch != "images/logo.png" {
		t.Fatalf("expected unresolved with near match, got %+v", r)
	}

	// Encoded slash (%2F), leading slash, backslash, double-encoded '..' (%252e%252e).
	for _, bad := range []string{"/index.md", `images\logo.png`, "images%2Flogo.png", "images/", "%ZZ.md", "%252e%252e/index.md"} {
		r = arc.ResolveLink("index.md", bad)
		if r.Kind != LinkUnresolved || r.Found {
			t.Fatalf("expected unresolved for %q, got %+v", bad, r)
		}
	}

	// External URLs.
	for _, ext := range []string{"https://example.com/a", "mailto:user@example.com"} {
		r = arc.ResolveLink("index.md", ext)
		if r.Kind != LinkExternal {
			t.Fatalf("expected LinkExternal for %q, got %+v", ext, r)
		}
	}
}
