// Package plugin is ArcMD's extension system: the manifest a plugin ships,
// the marketplace's registry index, installing and enabling plugins, and the
// contract between ArcMD and a plugin's runtime (Host).
//
// Capabilities beyond editing Markdown — AI writing tools above all — are
// meant to come from plugins, downloaded from the marketplace, not from the
// core. Plugins never draw native UI themselves: they contribute commands
// and panels, which ArcMD draws, and answer with edits, Markdown and
// notifications. See docs/plugins.md.
package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

// ManifestFile is the name of the manifest at the root of a plugin.
const ManifestFile = "arcmd-plugin.json"

// Runtime is how a plugin's code runs.
type Runtime string

const (
	// RuntimeWASM runs a WebAssembly module (WASI) inside ArcMD's process,
	// sandboxed: the planned default, portable and safe.
	RuntimeWASM Runtime = "wasm"
	// RuntimeProcess runs an executable that speaks JSON-RPC over its
	// standard input and output, as language servers do: for plugins
	// that need a native toolchain or a local model.
	RuntimeProcess Runtime = "process"
)

// Permission is a capability a plugin asks for; the user grants them as
// they install it, and the host refuses calls outside them.
type Permission string

const (
	PermDocumentRead  Permission = "document:read"  // the open document and selection
	PermDocumentWrite Permission = "document:write" // edit the open document
	PermSpaceRead     Permission = "space:read"     // list and read the active space's files
	PermSpaceWrite    Permission = "space:write"    // create and change files in the active space
	PermNetwork       Permission = "network"        // reach the network (AI providers)
	PermSecrets       Permission = "secrets"        // keep API keys in the system keychain
	PermUIPanel       Permission = "ui:panel"       // show a panel of Markdown in the sidebar
)

// Permissions lists every known permission with what to tell the user.
var Permissions = map[Permission]string{
	PermDocumentRead:  "Read the open document",
	PermDocumentWrite: "Edit the open document",
	PermSpaceRead:     "Read the files of the active space",
	PermSpaceWrite:    "Create and change files in the active space",
	PermNetwork:       "Connect to the internet",
	PermSecrets:       "Store secrets, such as API keys, in the system keychain",
	PermUIPanel:       "Show a panel in the sidebar",
}

// Manifest describes a plugin: arcmd-plugin.json.
type Manifest struct {
	// ID is unique in the marketplace, reverse-DNS style, as
	// "dev.arcmd.ai-writer".
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"` // semantic version, as "1.2.0"
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	License     string `json:"license,omitempty"` // SPDX identifier
	Homepage    string `json:"homepage,omitempty"`
	// Engine is the lowest ArcMD version the plugin works with, as
	// "0.1.0"; "" is any.
	Engine string `json:"engine,omitempty"`
	// Runtime and Main say how to run the plugin: a .wasm module, or an
	// executable, relative to the plugin's folder.
	Runtime     Runtime      `json:"runtime"`
	Main        string       `json:"main"`
	Permissions []Permission `json:"permissions,omitempty"`
	Contributes Contributes  `json:"contributes,omitzero"`
}

// Contributes is what a plugin adds to ArcMD's interface.
type Contributes struct {
	Commands []Command `json:"commands,omitempty"`
	Panels   []Panel   `json:"panels,omitempty"`
}

// Command is an action in the command bar.
type Command struct {
	ID    string `json:"id"` // unique within the plugin
	Title string `json:"title"`
	// When limits where the command shows: "document" while a document
	// is open, "selection" while text is selected, "" always.
	When string `json:"when,omitempty"`
}

// Panel is a sidebar panel whose content the plugin returns as Markdown.
type Panel struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Icon  string `json:"icon,omitempty"` // a short glyph
}

var (
	idPattern      = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*\.[a-z0-9-]+$`)
	versionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$`)
	localID        = regexp.MustCompile(`^[a-z0-9][a-z0-9-_.]*$`)
)

// ParseManifest decodes and validates a manifest.
func ParseManifest(b []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("plugin: manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate reports what is wrong with the manifest, all at once.
func (m *Manifest) Validate() error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	if !idPattern.MatchString(m.ID) {
		bad("id %q is not reverse-DNS style, as dev.example.plugin", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		bad("name is missing")
	}
	if !versionPattern.MatchString(m.Version) {
		bad("version %q is not a semantic version", m.Version)
	}
	if m.Engine != "" && !versionPattern.MatchString(m.Engine) {
		bad("engine %q is not a semantic version", m.Engine)
	}
	switch m.Runtime {
	case RuntimeWASM:
		if path.Ext(m.Main) != ".wasm" {
			bad("main %q of a wasm plugin must be a .wasm file", m.Main)
		}
	case RuntimeProcess:
	default:
		bad("runtime %q is not %q or %q", m.Runtime, RuntimeWASM, RuntimeProcess)
	}
	if m.Main == "" || path.IsAbs(m.Main) || slices.Contains(strings.Split(m.Main, "/"), "..") {
		bad("main %q must be a path inside the plugin", m.Main)
	}
	for _, p := range m.Permissions {
		if _, ok := Permissions[p]; !ok {
			bad("unknown permission %q", p)
		}
	}
	seen := map[string]bool{}
	for _, c := range m.Contributes.Commands {
		if !localID.MatchString(c.ID) || seen["c:"+c.ID] {
			bad("command id %q is invalid or repeated", c.ID)
		}
		seen["c:"+c.ID] = true
		switch c.When {
		case "", "document", "selection":
		default:
			bad("command %q: when %q is not \"document\" or \"selection\"", c.ID, c.When)
		}
	}
	for _, p := range m.Contributes.Panels {
		if !localID.MatchString(p.ID) || seen["p:"+p.ID] {
			bad("panel id %q is invalid or repeated", p.ID)
		}
		seen["p:"+p.ID] = true
	}
	if len(m.Contributes.Panels) > 0 && !slices.Contains(m.Permissions, PermUIPanel) {
		bad("panels need the %q permission", PermUIPanel)
	}
	if len(errs) > 0 {
		return fmt.Errorf("plugin: invalid manifest: %w", errors.Join(errs...))
	}
	return nil
}
