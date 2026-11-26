package mdz

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// SpecInfo identifies the target MDZip specification in manifest.json.
type SpecInfo struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version"`
}

// ToolInfo identifies a producer application or core library.
type ToolInfo struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	URL     string `json:"url,omitempty"`
}

// ProducerInfo holds producer provenance metadata.
type ProducerInfo struct {
	Application *ToolInfo `json:"application,omitempty"`
	Core        *ToolInfo `json:"core,omitempty"`
}

// Person identifies an author, creator, or modifier.
type Person struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

// Timestamp represents manifest created/modified metadata.
// Consumers accept both object form ({"when": "...", "by": {...}}) and legacy
// draft 1.0.x RFC 3339 string form (Spec §6.1).
type Timestamp struct {
	When string  `json:"when"`
	By   *Person `json:"by,omitempty"`
}

// Manifest models the optional manifest.json file at the root of an .mdz archive (Spec §6).
type Manifest struct {
	Spec        *SpecInfo     `json:"spec,omitempty"`
	Producer    *ProducerInfo `json:"producer,omitempty"`
	Author      *Person       `json:"author,omitempty"`
	Title       string        `json:"title,omitempty"`
	Mode        string        `json:"mode,omitempty"`
	EntryPoint  string        `json:"entryPoint,omitempty"`
	Language    string        `json:"language,omitempty"`
	Description string        `json:"description,omitempty"`
	Version     string        `json:"version,omitempty"`
	Created     *Timestamp    `json:"created,omitempty"`
	Modified    *Timestamp    `json:"modified,omitempty"`
	License     string        `json:"license,omitempty"`
	Keywords    []string      `json:"keywords,omitempty"`
	Cover       string        `json:"cover,omitempty"`
}

// semver parses a strict Semantic Versioning 2.0.0 string and returns (major, minor, patch, ok).
func parseSemver(s string) (int, int, int, bool) {
	if s == "" {
		return 0, 0, 0, false
	}
	core := s
	if idx := strings.IndexByte(core, '+'); idx >= 0 {
		build := core[idx+1:]
		if !validSemverBuild(build) {
			return 0, 0, 0, false
		}
		core = core[:idx]
	}
	if idx := strings.IndexByte(core, '-'); idx >= 0 {
		pre := core[idx+1:]
		if !validSemverPrerelease(pre) {
			return 0, 0, 0, false
		}
		core = core[:idx]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	nums := make([]int, 3)
	for i, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return 0, 0, 0, false
		}
		for j := 0; j < len(part); j++ {
			if part[j] < '0' || part[j] > '9' {
				return 0, 0, 0, false
			}
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return 0, 0, 0, false
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], true
}

func validSemverPrerelease(s string) bool {
	if s == "" {
		return false
	}
	for _, id := range strings.Split(s, ".") {
		if id == "" {
			return false
		}
		allDigits := true
		for i := 0; i < len(id); i++ {
			c := id[i]
			if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '-') {
				return false
			}
			if c < '0' || c > '9' {
				allDigits = false
			}
		}
		if allDigits && len(id) > 1 && id[0] == '0' {
			return false
		}
	}
	return true
}

func validSemverBuild(s string) bool {
	if s == "" {
		return false
	}
	for _, id := range strings.Split(s, ".") {
		if id == "" {
			return false
		}
		for i := 0; i < len(id); i++ {
			c := id[i]
			if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '-') {
				return false
			}
		}
	}
	return true
}

// validRFC3339 reports whether s is a valid RFC 3339 date-time string.
func validRFC3339(s string) bool {
	if len(s) < 20 {
		return false
	}
	// RFC 3339 permits lowercase 't' and 'z'; Go's time.RFC3339Nano expects uppercase.
	normStr := strings.Replace(strings.Replace(s, "t", "T", 1), "z", "Z", 1)
	if _, err := time.Parse(time.RFC3339Nano, normStr); err != nil {
		return false
	}
	return true
}

// checkDuplicateKeys inspects JSON syntax and reports duplicate object keys as warnings
// (Spec §6.4: consumers SHOULD report duplicate member names and accept the last occurrence).
func checkDuplicateKeys(raw []byte) ([]string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var warnings []string
	var walk func(prefix string) error
	walk = func(prefix string) error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := make(map[string]struct{})
			for dec.More() {
				ktok, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := ktok.(string)
				if !ok {
					return fmt.Errorf("expected string key")
				}
				fullKey := key
				if prefix != "" {
					fullKey = prefix + "." + key
				}
				if _, dup := seen[key]; dup {
					warnings = append(warnings, fmt.Sprintf("duplicate manifest key %q", fullKey))
				}
				seen[key] = struct{}{}
				if err := walk(fullKey); err != nil {
					return err
				}
			}
			// Consume closing '}'.
			if _, err := dec.Token(); err != nil {
				return err
			}
		case '[':
			for dec.More() {
				if err := walk(prefix); err != nil {
					return err
				}
			}
			// Consume closing ']'.
			if _, err := dec.Token(); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(""); err != nil {
		return nil, err
	}
	// Ensure there are no trailing tokens after the top-level JSON value.
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing data after JSON object")
	}
	return warnings, nil
}

// parseManifest parses and validates manifest.json according to Spec §6.4 and §11.2.
// Critical field errors return an error; invalid descriptive fields are ignored and
// recorded in warnings. Unknown fields are silently ignored.
func parseManifest(raw []byte) (*Manifest, []string, error) {
	if !utf8.Valid(raw) {
		return nil, nil, fmt.Errorf("%w: manifest.json is not valid UTF-8", ErrManifestInvalid)
	}
	dupWarnings, err := checkDuplicateKeys(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil || top == nil {
		return nil, nil, fmt.Errorf("%w: top-level value must be a JSON object", ErrManifestInvalid)
	}

	m := &Manifest{}
	warnings := append([]string(nil), dupWarnings...)
	warn := func(format string, args ...any) {
		warnings = append(warnings, fmt.Sprintf(format, args...))
	}

	// Critical field: spec & spec.version
	if specRaw, ok := top["spec"]; ok {
		var specObj map[string]json.RawMessage
		if err := json.Unmarshal(specRaw, &specObj); err != nil || specObj == nil {
			return nil, nil, fmt.Errorf("%w: spec must be an object", ErrManifestInvalid)
		}
		si := &SpecInfo{}
		if nameRaw, has := specObj["name"]; has {
			var s string
			if err := json.Unmarshal(nameRaw, &s); err == nil && !isJSONNull(nameRaw) {
				si.Name = s
			} else {
				warn("ignored invalid manifest field spec.name")
			}
		}
		if verRaw, has := specObj["version"]; has {
			var v string
			if isJSONNull(verRaw) || json.Unmarshal(verRaw, &v) != nil {
				return nil, nil, fmt.Errorf("%w: spec.version must be a string", ErrManifestInvalid)
			}
			major, _, _, ok := parseSemver(v)
			if !ok {
				return nil, nil, fmt.Errorf("%w: spec.version %q is not valid SemVer 2.0.0", ErrManifestInvalid, v)
			}
			if major > SupportedMajor {
				return nil, nil, fmt.Errorf("%w: spec.version %q (major %d > %d)", ErrVersionUnsupported, v, major, SupportedMajor)
			}
			if major < SupportedMajor {
				warn("manifest spec.version %q targets older major version %d", v, major)
			}
			si.Version = v
		}
		m.Spec = si
	}

	// Critical field: mode
	if modeRaw, ok := top["mode"]; ok {
		if isJSONNull(modeRaw) {
			return nil, nil, fmt.Errorf("%w: mode must be a string", ErrManifestInvalid)
		}
		var mode string
		if err := json.Unmarshal(modeRaw, &mode); err != nil {
			return nil, nil, fmt.Errorf("%w: mode must be a string", ErrManifestInvalid)
		}
		switch mode {
		case ModeDocument, ModeProject:
			m.Mode = mode
		default:
			return nil, nil, fmt.Errorf("%w: %q", ErrModeUnsupported, mode)
		}
	}

	// Critical field: entryPoint (type and syntax checked here; existence checked by reader)
	if epRaw, ok := top["entryPoint"]; ok {
		if isJSONNull(epRaw) {
			return nil, nil, fmt.Errorf("%w: entryPoint must be a string", ErrEntryPointInvalid)
		}
		var ep string
		if err := json.Unmarshal(epRaw, &ep); err != nil {
			return nil, nil, fmt.Errorf("%w: entryPoint must be a string", ErrEntryPointInvalid)
		}
		cleaned := cleanSingleLeadingDotSlash(ep)
		if cleaned == "" || ValidateEntryPath(cleaned, false) != nil || !IsMarkdownPath(cleaned) {
			return nil, nil, fmt.Errorf("%w: %q", ErrEntryPointInvalid, ep)
		}
		m.EntryPoint = cleaned
	}

	// Descriptive fields (Spec §6.4: invalid values ignored with optional warning)
	if rawVal, ok := top["title"]; ok {
		if s, ok := decodeNonEmptyString(rawVal); ok {
			m.Title = s
		} else {
			warn("ignored invalid manifest field title")
		}
	}
	if rawVal, ok := top["language"]; ok {
		if s, ok := decodeString(rawVal); ok {
			m.Language = s
		} else {
			warn("ignored invalid manifest field language")
		}
	}
	if rawVal, ok := top["description"]; ok {
		if s, ok := decodeString(rawVal); ok {
			m.Description = s
		} else {
			warn("ignored invalid manifest field description")
		}
	}
	if rawVal, ok := top["version"]; ok {
		if s, ok := decodeString(rawVal); ok {
			m.Version = s
		} else {
			warn("ignored invalid manifest field version")
		}
	}
	if rawVal, ok := top["license"]; ok {
		if s, ok := decodeString(rawVal); ok {
			m.License = s
		} else {
			warn("ignored invalid manifest field license")
		}
	}
	if rawVal, ok := top["keywords"]; ok {
		if list, ok := decodeStringSlice(rawVal); ok {
			m.Keywords = list
		} else {
			warn("ignored invalid manifest field keywords")
		}
	}
	if rawVal, ok := top["cover"]; ok {
		if s, ok := decodeNonEmptyString(rawVal); ok {
			cleaned := cleanSingleLeadingDotSlash(s)
			if ValidateEntryPath(cleaned, false) == nil {
				m.Cover = cleaned
			} else {
				warn("ignored invalid manifest cover path %q", s)
			}
		} else {
			warn("ignored invalid manifest field cover")
		}
	}
	if rawVal, ok := top["author"]; ok {
		if p, ok := decodePerson(rawVal); ok {
			m.Author = p
		} else {
			warn("ignored invalid manifest field author")
		}
	}
	if rawVal, ok := top["producer"]; ok {
		if p, ok := decodeProducer(rawVal); ok {
			m.Producer = p
		} else {
			warn("ignored invalid manifest field producer")
		}
	}
	if rawVal, ok := top["created"]; ok {
		if ts, ok := decodeTimestamp(rawVal); ok {
			m.Created = ts
		} else {
			warn("ignored invalid manifest field created")
		}
	}
	if rawVal, ok := top["modified"]; ok {
		if ts, ok := decodeTimestamp(rawVal); ok {
			m.Modified = ts
		} else {
			warn("ignored invalid manifest field modified")
		}
	}

	return m, warnings, nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func decodeString(raw json.RawMessage) (string, bool) {
	if isJSONNull(raw) {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func decodeNonEmptyString(raw json.RawMessage) (string, bool) {
	s, ok := decodeString(raw)
	if !ok || strings.TrimSpace(s) == "" {
		return "", false
	}
	return s, true
}

func decodeStringSlice(raw json.RawMessage) ([]string, bool) {
	if isJSONNull(raw) {
		return nil, false
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s, ok := decodeString(item)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func decodePerson(raw json.RawMessage) (*Person, bool) {
	if isJSONNull(raw) {
		return nil, false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, false
	}
	p := &Person{}
	if v, ok := obj["name"]; ok {
		if s, ok := decodeString(v); ok {
			p.Name = s
		}
	}
	if v, ok := obj["email"]; ok {
		if s, ok := decodeString(v); ok {
			p.Email = s
		}
	}
	if v, ok := obj["url"]; ok {
		if s, ok := decodeString(v); ok {
			p.URL = s
		}
	}
	return p, true
}

func decodeToolInfo(raw json.RawMessage) (*ToolInfo, bool) {
	if isJSONNull(raw) {
		return nil, false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, false
	}
	ti := &ToolInfo{}
	if v, ok := obj["name"]; ok {
		if s, ok := decodeString(v); ok {
			ti.Name = s
		}
	}
	if v, ok := obj["version"]; ok {
		if s, ok := decodeString(v); ok {
			ti.Version = s
		}
	}
	if v, ok := obj["url"]; ok {
		if s, ok := decodeString(v); ok {
			ti.URL = s
		}
	}
	return ti, true
}

func decodeProducer(raw json.RawMessage) (*ProducerInfo, bool) {
	if isJSONNull(raw) {
		return nil, false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, false
	}
	p := &ProducerInfo{}
	if v, ok := obj["application"]; ok {
		if ti, ok := decodeToolInfo(v); ok {
			p.Application = ti
		}
	}
	if v, ok := obj["core"]; ok {
		if ti, ok := decodeToolInfo(v); ok {
			p.Core = ti
		}
	}
	return p, true
}

func decodeTimestamp(raw json.RawMessage) (*Timestamp, bool) {
	if isJSONNull(raw) {
		return nil, false
	}
	// Legacy draft 1.0.x plain string form (Spec §6.1).
	if s, ok := decodeString(raw); ok {
		if !validRFC3339(s) {
			return nil, false
		}
		return &Timestamp{When: s}, true
	}
	// Current object form: required "when" RFC 3339 string, optional "by" Person.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, false
	}
	whenRaw, hasWhen := obj["when"]
	if !hasWhen {
		return nil, false
	}
	whenStr, ok := decodeString(whenRaw)
	if !ok || !validRFC3339(whenStr) {
		return nil, false
	}
	ts := &Timestamp{When: whenStr}
	if byRaw, hasBy := obj["by"]; hasBy {
		if p, ok := decodePerson(byRaw); ok {
			ts.By = p
		}
	}
	return ts, true
}
