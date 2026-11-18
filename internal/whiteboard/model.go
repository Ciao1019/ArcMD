// Package whiteboard defines ArcMD's pure Go Excalidraw-compatible
// hand-drawn whiteboard model, Rough.js-style vector synthesis, geometry,
// undo history, and SVG export without any UI toolkit dependency.
package whiteboard

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/wiki"
)

// CurrentFormatVersion matches Excalidraw's v2 scene format.
const CurrentFormatVersion = 2

// ElementKind identifies a whiteboard element type.
type ElementKind string

const (
	KindRectangle     ElementKind = "rectangle"
	KindDiamond       ElementKind = "diamond"
	KindEllipse       ElementKind = "ellipse"
	KindArrow         ElementKind = "arrow"
	KindLine          ElementKind = "line"
	KindFreedraw      ElementKind = "freedraw"
	KindText          ElementKind = "text"
	KindNote          ElementKind = "note"
	KindFrame         ElementKind = "frame"
	KindTriangle      ElementKind = "triangle"
	KindParallelogram ElementKind = "parallelogram"
	KindHexagon       ElementKind = "hexagon"
	KindCylinder      ElementKind = "cylinder"
	KindCloud         ElementKind = "cloud"
	KindStar          ElementKind = "star"
	KindCallout       ElementKind = "callout"
	KindDocument      ElementKind = "document"
	KindStadium       ElementKind = "stadium"
	KindActor         ElementKind = "actor"
)

// FillStyle specifies how closed shapes are filled.
type FillStyle string

const (
	FillNone       FillStyle = ""
	FillHachure    FillStyle = "hachure"
	FillCrossHatch FillStyle = "cross-hatch"
	FillSolid      FillStyle = "solid"
)

// StrokeStyle specifies stroke dash style.
type StrokeStyle string

const (
	StrokeSolid  StrokeStyle = "solid"
	StrokeDashed StrokeStyle = "dashed"
	StrokeDotted StrokeStyle = "dotted"
)

// Arrowhead specifies line/arrow endpoint decorations.
type Arrowhead string

const (
	ArrowNone     Arrowhead = ""
	ArrowOpen     Arrowhead = "arrow"
	ArrowTriangle Arrowhead = "triangle"
	ArrowBar      Arrowhead = "bar"
	ArrowDot      Arrowhead = "dot"
)

// Binding connects an arrow/line endpoint to a target shape element.
type Binding struct {
	ElementID string  `json:"elementId"`
	Focus     float32 `json:"focus,omitempty"`
	Gap       float32 `json:"gap,omitempty"`
}

// SemanticColors are the theme-adaptive color names shared by light and dark paper.
var SemanticColors = []string{"ink", "gray", "red", "orange", "green", "blue", "purple"}

// Element is one whiteboard object in scene coordinates (DIPs).
type Element struct {
	ID             string            `json:"id"`
	Kind           ElementKind       `json:"type"`
	X              float32           `json:"x"`
	Y              float32           `json:"y"`
	W              float32           `json:"width"`
	H              float32           `json:"height"`
	Angle          float32           `json:"angle,omitempty"`
	StrokeColor    string            `json:"strokeColor"`
	FillColor      string            `json:"backgroundColor,omitempty"`
	FillStyle      FillStyle         `json:"fillStyle,omitempty"`
	StrokeWidth    float32           `json:"strokeWidth"`
	StrokeStyle    StrokeStyle       `json:"strokeStyle,omitempty"`
	Roughness      int               `json:"roughness"`
	Opacity        int               `json:"opacity,omitempty"` // 0..100 (0 treated as 100 when unset via normalization, use -1 or field below)
	Seed           uint32            `json:"seed"`
	Version        int               `json:"version"`
	Roundness      bool              `json:"roundness,omitempty"`
	Points         [][2]float32      `json:"points,omitempty"`
	StartBinding   *Binding          `json:"startBinding,omitempty"`
	EndBinding     *Binding          `json:"endBinding,omitempty"`
	StartArrowhead Arrowhead         `json:"startArrowhead,omitempty"`
	EndArrowhead   Arrowhead         `json:"endArrowhead,omitempty"`
	Smooth         bool              `json:"smooth,omitempty"`  // curved arrow/line
	Elbowed        bool              `json:"elbowed,omitempty"` // orthogonal 90° connector
	GroupIDs       []string          `json:"groupIds,omitempty"`
	CustomData     map[string]string `json:"customData,omitempty"`
	Text           string            `json:"text,omitempty"`
	FontSize       float32           `json:"fontSize,omitempty"`
	FontFamily     string            `json:"fontFamily,omitempty"` // "hand", "sans", "mono"
	TextAlign      string            `json:"textAlign,omitempty"`  // "left", "center", "right"
	Link           string            `json:"link,omitempty"`       // e.g. "[[DocName]]" or URL
	FrameID        string            `json:"frameId,omitempty"`
	Name           string            `json:"name,omitempty"` // frame title or element label
	Locked         bool              `json:"locked,omitempty"`
}

// AppState stores lightweight persisted canvas state compatible with Excalidraw.
type AppState struct {
	ViewBackgroundColor string  `json:"viewBackgroundColor,omitempty"`
	GridSize            int     `json:"gridSize,omitempty"`
	Zoom                float32 `json:"zoom,omitempty"`
	ScrollX             float32 `json:"scrollX,omitempty"`
	ScrollY             float32 `json:"scrollY,omitempty"`
}

// Scene is the complete persisted whiteboard document.
type Scene struct {
	Type     string     `json:"type"`
	Version  int        `json:"version"`
	Source   string     `json:"source,omitempty"`
	Elements []*Element `json:"elements"`
	AppState AppState   `json:"appState,omitempty"`
}

// SummaryLine represents one semantic line in MarkdownSummary for QA/spacefs.
type SummaryLine struct {
	ElementID string
	Text      string
}

// NewID generates a random 12-byte hex element ID.
func NewID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// NewSeed generates a deterministic non-zero PRNG seed for a new element.
func NewSeed() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	s := binary.LittleEndian.Uint32(b[:])
	if s == 0 {
		return 1
	}
	return s
}

// NewScene creates an empty v2 whiteboard scene.
func NewScene() *Scene {
	return &Scene{
		Type:     "excalidraw",
		Version:  CurrentFormatVersion,
		Source:   "arcmd",
		Elements: nil,
		AppState: AppState{GridSize: 20, Zoom: 1},
	}
}

// NewElement creates an initialized element with sensible Excalidraw defaults.
func NewElement(kind ElementKind, x, y, w, h float32) *Element {
	el := &Element{
		ID:          NewID(),
		Kind:        kind,
		X:           x,
		Y:           y,
		W:           w,
		H:           h,
		StrokeColor: "ink",
		StrokeWidth: 2,
		StrokeStyle: StrokeSolid,
		Roughness:   1,
		Opacity:     100,
		Seed:        NewSeed(),
		Version:     1,
		FontSize:    18,
		FontFamily:  "hand",
		TextAlign:   "center",
	}
	switch kind {
	case KindRectangle, KindCallout, KindStadium, KindCloud:
		el.Roundness = true
		el.FillStyle = FillHachure
	case KindDiamond, KindEllipse, KindTriangle, KindParallelogram, KindHexagon, KindCylinder, KindStar, KindDocument:
		el.FillStyle = FillHachure
	case KindActor:
		el.FillStyle = FillNone
	case KindArrow:
		el.EndArrowhead = ArrowOpen
		el.Points = [][2]float32{{0, 0}, {w, h}}
		el.Smooth = true
	case KindLine:
		el.Points = [][2]float32{{0, 0}, {w, h}}
	case KindFreedraw:
		el.Points = [][2]float32{{0, 0}}
		el.Roughness = 0
		el.StrokeWidth = 2.5
	case KindText:
		el.TextAlign = "left"
		el.Roughness = 0
		el.FontSize = 20
	case KindNote:
		el.StrokeColor = "orange"
		el.FillColor = "orange"
		el.FillStyle = FillHachure
		el.Roundness = true
		el.FontSize = 15
		el.TextAlign = "left"
	case KindFrame:
		el.StrokeColor = "gray"
		el.Roughness = 0
		el.StrokeWidth = 1.5
		el.StrokeStyle = StrokeDashed
		el.Name = "Frame"
	}
	return el
}

// StarterScene returns a clean starter whiteboard showcasing hand-drawn shapes,
// hachure fills, connected arrows, and notes for newly inserted inline boards.
func StarterScene() *Scene {
	s := NewScene()
	a := NewElement(KindRectangle, 40, 60, 170, 76)
	a.ID = "starter-a"
	a.Seed = 104729
	a.StrokeColor = "blue"
	a.FillColor = "blue"
	a.FillStyle = FillHachure
	a.Text = "核心想法"

	b := NewElement(KindDiamond, 290, 44, 150, 108)
	b.ID = "starter-b"
	b.Seed = 224737
	b.StrokeColor = "orange"
	b.FillColor = "orange"
	b.FillStyle = FillCrossHatch
	b.Text = "可行性?"

	c := NewElement(KindEllipse, 520, 60, 160, 76)
	c.ID = "starter-c"
	c.Seed = 350377
	c.StrokeColor = "green"
	c.FillColor = "green"
	c.FillStyle = FillHachure
	c.Text = "落地实现"

	arr1 := NewElement(KindArrow, 210, 98, 80, 0)
	arr1.ID = "starter-arr1"
	arr1.Seed = 479909
	arr1.StrokeColor = "ink"
	arr1.StartBinding = &Binding{ElementID: a.ID, Gap: 6}
	arr1.EndBinding = &Binding{ElementID: b.ID, Gap: 6}
	arr1.Points = [][2]float32{{0, 0}, {80, 0}}

	arr2 := NewElement(KindArrow, 440, 98, 80, 0)
	arr2.ID = "starter-arr2"
	arr2.Seed = 611953
	arr2.StrokeColor = "green"
	arr2.Text = "通过"
	arr2.StartBinding = &Binding{ElementID: b.ID, Gap: 6}
	arr2.EndBinding = &Binding{ElementID: c.ID, Gap: 6}
	arr2.Points = [][2]float32{{0, 0}, {80, 0}}

	s.Elements = []*Element{a, b, c, arr1, arr2}
	UpdateBindings(s)
	return s
}

// Clone deep-copies the scene and all elements.
func (s *Scene) Clone() *Scene {
	if s == nil {
		return NewScene()
	}
	out := &Scene{
		Type:     s.Type,
		Version:  s.Version,
		Source:   s.Source,
		AppState: s.AppState,
		Elements: make([]*Element, len(s.Elements)),
	}
	for i, el := range s.Elements {
		out.Elements[i] = el.Clone()
	}
	return out
}

// Clone deep-copies an element.
func (el *Element) Clone() *Element {
	if el == nil {
		return nil
	}
	cp := *el
	if el.Points != nil {
		cp.Points = slices.Clone(el.Points)
	}
	if el.GroupIDs != nil {
		cp.GroupIDs = slices.Clone(el.GroupIDs)
	}
	if el.CustomData != nil {
		cp.CustomData = maps.Clone(el.CustomData)
	}
	if el.StartBinding != nil {
		b := *el.StartBinding
		cp.StartBinding = &b
	}
	if el.EndBinding != nil {
		b := *el.EndBinding
		cp.EndBinding = &b
	}
	return &cp
}

// ElementByID returns the element with the given ID or nil.
func (s *Scene) ElementByID(id string) *Element {
	if s == nil || id == "" {
		return nil
	}
	for _, el := range s.Elements {
		if el.ID == id {
			return el
		}
	}
	return nil
}

// Digest returns a deterministic SHA-256 digest of the scene state.
func (s *Scene) Digest() [32]byte {
	enc, _ := s.Encode()
	return sha256.Sum256([]byte(enc))
}

// Encode serializes the scene into Excalidraw v2 compatible JSON.
func (s *Scene) Encode() (string, error) {
	if s == nil {
		s = NewScene()
	}
	out := rawScene{
		Type:     "excalidraw",
		Version:  CurrentFormatVersion,
		Source:   "arcmd",
		Elements: make([]rawElement, 0, len(s.Elements)),
		AppState: s.AppState,
	}
	if s.Source != "" {
		out.Source = s.Source
	}
	if out.AppState.Zoom <= 0 {
		out.AppState.Zoom = 1
	}
	for _, el := range s.Elements {
		if el == nil {
			continue
		}
		out.Elements = append(out.Elements, toRawElement(el))
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(append(data, '\n')), nil
}

// Decode parses Excalidraw v2 JSON or amphibious *.excalidraw.svg documents.
// Empty input returns a fresh empty scene; invalid JSON or unsupported future
// formats return an error so callers never overwrite broken files.
func Decode(src string) (*Scene, error) {
	trimmed := strings.TrimSpace(src)
	if trimmed == "" {
		return NewScene(), nil
	}
	if strings.Contains(trimmed, svgPayloadStart) || strings.HasPrefix(trimmed, "<svg") || strings.HasPrefix(trimmed, "<?xml") {
		payload, err := extractSVGPayload(trimmed)
		if err != nil {
			return nil, err
		}
		trimmed = payload
	}
	var raw rawScene
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return nil, fmt.Errorf("whiteboard: invalid JSON: %w", err)
	}
	if raw.Type != "" && raw.Type != "excalidraw" {
		return nil, fmt.Errorf("whiteboard: unexpected type %q", raw.Type)
	}
	if raw.Version > CurrentFormatVersion {
		return nil, fmt.Errorf("whiteboard: unsupported version %d", raw.Version)
	}
	scene := &Scene{
		Type:     "excalidraw",
		Version:  CurrentFormatVersion,
		Source:   raw.Source,
		AppState: raw.AppState,
	}
	if scene.Source == "" {
		scene.Source = "arcmd"
	}
	if scene.AppState.Zoom <= 0 {
		scene.AppState.Zoom = 1
	}

	// External Excalidraw files store shape labels as separate text elements
	// with containerId. Keep a first pass so we can fold them into their host.
	byID := map[string]*Element{}
	var boundTexts []rawElement

	for _, re := range raw.Elements {
		if re.IsDeleted {
			continue
		}
		if re.Type == "text" && re.ContainerID != "" {
			boundTexts = append(boundTexts, re)
			continue
		}
		el := fromRawElement(re)
		scene.Elements = append(scene.Elements, el)
		byID[el.ID] = el
	}
	for _, bt := range boundTexts {
		if host, ok := byID[bt.ContainerID]; ok && host.Text == "" {
			host.Text = bt.Text
			if bt.FontSize > 0 {
				host.FontSize = bt.FontSize
			}
			if ff := decodeFontFamily(bt.FontFamily); ff != "" {
				host.FontFamily = ff
			}
			if bt.TextAlign != "" {
				host.TextAlign = bt.TextAlign
			}
		} else {
			el := fromRawElement(bt)
			scene.Elements = append(scene.Elements, el)
		}
	}
	return scene, nil
}

func extractSVGPayload(svg string) (string, error) {
	_, rest, ok := strings.Cut(svg, svgPayloadStart)
	if !ok {
		return "", errors.New("whiteboard: svg does not contain embedded excalidraw payload")
	}
	encoded, _, ok := strings.Cut(rest, svgPayloadEnd)
	if !ok {
		return "", errors.New("whiteboard: unterminated excalidraw SVG payload")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", fmt.Errorf("whiteboard: invalid SVG payload encoding: %w", err)
	}
	return string(raw), nil
}

type rawScene struct {
	Type     string       `json:"type"`
	Version  int          `json:"version"`
	Source   string       `json:"source,omitempty"`
	Elements []rawElement `json:"elements"`
	AppState AppState     `json:"appState,omitempty"`
}

type rawElement struct {
	ID              string            `json:"id"`
	Type            string            `json:"type"`
	X               float32           `json:"x"`
	Y               float32           `json:"y"`
	Width           float32           `json:"width"`
	Height          float32           `json:"height"`
	Angle           float32           `json:"angle,omitempty"`
	StrokeColor     string            `json:"strokeColor"`
	BackgroundColor string            `json:"backgroundColor,omitempty"`
	FillStyle       string            `json:"fillStyle,omitempty"`
	StrokeWidth     float32           `json:"strokeWidth"`
	StrokeStyle     string            `json:"strokeStyle,omitempty"`
	Roughness       *int              `json:"roughness,omitempty"`
	Opacity         *int              `json:"opacity,omitempty"`
	Seed            uint32            `json:"seed"`
	Version         int               `json:"version"`
	IsDeleted       bool              `json:"isDeleted,omitempty"`
	Roundness       json.RawMessage   `json:"roundness,omitempty"`
	Points          [][2]float32      `json:"points,omitempty"`
	StartBinding    *Binding          `json:"startBinding,omitempty"`
	EndBinding      *Binding          `json:"endBinding,omitempty"`
	StartArrowhead  string            `json:"startArrowhead,omitempty"`
	EndArrowhead    string            `json:"endArrowhead,omitempty"`
	Smooth          bool              `json:"smooth,omitempty"`
	Elbowed         bool              `json:"elbowed,omitempty"`
	GroupIDs        []string          `json:"groupIds,omitempty"`
	CustomData      map[string]string `json:"customData,omitempty"`
	Text            string            `json:"text,omitempty"`
	FontSize        float32           `json:"fontSize,omitempty"`
	FontFamily      json.RawMessage   `json:"fontFamily,omitempty"`
	TextAlign       string            `json:"textAlign,omitempty"`
	ContainerID     string            `json:"containerId,omitempty"`
	Link            string            `json:"link,omitempty"`
	FrameID         string            `json:"frameId,omitempty"`
	Name            string            `json:"name,omitempty"`
	Locked          bool              `json:"locked,omitempty"`
}

func toRawElement(el *Element) rawElement {
	rough := el.Roughness
	op := el.Opacity
	if op <= 0 {
		op = 100
	}
	rawType := excalidrawBaseType(el.Kind)
	var custom map[string]string
	if len(el.CustomData) > 0 {
		custom = maps.Clone(el.CustomData)
	}
	if isExtendedShapeKind(el.Kind) {
		if custom == nil {
			custom = make(map[string]string, 1)
		}
		custom["arcmdShape"] = string(el.Kind)
	}
	re := rawElement{
		ID:              el.ID,
		Type:            rawType,
		X:               el.X,
		Y:               el.Y,
		Width:           el.W,
		Height:          el.H,
		Angle:           el.Angle,
		StrokeColor:     el.StrokeColor,
		BackgroundColor: el.FillColor,
		FillStyle:       string(el.FillStyle),
		StrokeWidth:     el.StrokeWidth,
		StrokeStyle:     string(el.StrokeStyle),
		Roughness:       &rough,
		Opacity:         &op,
		Seed:            el.Seed,
		Version:         max(1, el.Version),
		Points:          el.Points,
		StartBinding:    el.StartBinding,
		EndBinding:      el.EndBinding,
		StartArrowhead:  string(el.StartArrowhead),
		EndArrowhead:    string(el.EndArrowhead),
		Smooth:          el.Smooth,
		Elbowed:         el.Elbowed,
		GroupIDs:        slices.Clone(el.GroupIDs),
		CustomData:      custom,
		Text:            el.Text,
		FontSize:        el.FontSize,
		TextAlign:       el.TextAlign,
		Link:            el.Link,
		FrameID:         el.FrameID,
		Name:            el.Name,
		Locked:          el.Locked,
	}
	if el.Roundness {
		re.Roundness = json.RawMessage(`{"type":3}`)
	}
	if el.FontFamily != "" {
		b, _ := json.Marshal(el.FontFamily)
		re.FontFamily = b
	}
	return re
}

func fromRawElement(re rawElement) *Element {
	kind := ElementKind(re.Type)
	if re.CustomData != nil {
		if customShape := ElementKind(re.CustomData["arcmdShape"]); isValidKind(customShape) {
			kind = customShape
		}
	}
	if !isValidKind(kind) {
		kind = KindRectangle
	}
	id := re.ID
	if id == "" {
		id = NewID()
	}
	seed := re.Seed
	if seed == 0 {
		seed = NewSeed()
	}
	sw := re.StrokeWidth
	if sw <= 0 {
		sw = 2
	}
	rough := 1
	if re.Roughness != nil {
		rough = max(0, min(2, *re.Roughness))
	} else if kind == KindFreedraw || kind == KindText || kind == KindFrame {
		rough = 0
	}
	op := 100
	if re.Opacity != nil {
		op = max(5, min(100, *re.Opacity))
	}
	fs := FillStyle(re.FillStyle)
	switch fs {
	case FillNone, FillHachure, FillCrossHatch, FillSolid:
	default:
		fs = FillHachure
	}
	ss := StrokeStyle(re.StrokeStyle)
	switch ss {
	case StrokeSolid, StrokeDashed, StrokeDotted:
	default:
		ss = StrokeSolid
	}
	ff := decodeFontFamily(re.FontFamily)
	if ff == "" {
		ff = "hand"
	}
	fontSize := re.FontSize
	if fontSize <= 0 {
		fontSize = 18
	}
	ta := re.TextAlign
	if ta == "" {
		if kind == KindText || kind == KindNote {
			ta = "left"
		} else {
			ta = "center"
		}
	}
	var startB, endB *Binding
	if re.StartBinding != nil && re.StartBinding.ElementID != "" {
		b := *re.StartBinding
		startB = &b
	}
	if re.EndBinding != nil && re.EndBinding.ElementID != "" {
		b := *re.EndBinding
		endB = &b
	}
	var pts [][2]float32
	if len(re.Points) > 0 {
		pts = slices.Clone(re.Points)
	} else if kind == KindArrow || kind == KindLine {
		pts = [][2]float32{{0, 0}, {re.Width, re.Height}}
	}
	endHead := Arrowhead(re.EndArrowhead)
	if kind == KindArrow && endHead == "" && re.StartArrowhead == "" {
		endHead = ArrowOpen
	}
	var custom map[string]string
	if len(re.CustomData) > 0 {
		custom = maps.Clone(re.CustomData)
		delete(custom, "arcmdShape")
		if len(custom) == 0 {
			custom = nil
		}
	}
	return &Element{
		ID:             id,
		Kind:           kind,
		X:              re.X,
		Y:              re.Y,
		W:              re.Width,
		H:              re.Height,
		Angle:          re.Angle,
		StrokeColor:    normalizeColorName(re.StrokeColor, "ink"),
		FillColor:      normalizeColorName(re.BackgroundColor, ""),
		FillStyle:      fs,
		StrokeWidth:    sw,
		StrokeStyle:    ss,
		Roughness:      rough,
		Opacity:        op,
		Seed:           seed,
		Version:        max(1, re.Version),
		Roundness:      decodeRoundness(re.Roundness),
		Points:         pts,
		StartBinding:   startB,
		EndBinding:     endB,
		StartArrowhead: Arrowhead(re.StartArrowhead),
		EndArrowhead:   endHead,
		Smooth:         re.Smooth,
		Elbowed:        re.Elbowed,
		GroupIDs:       slices.Clone(re.GroupIDs),
		CustomData:     custom,
		Text:           re.Text,
		FontSize:       fontSize,
		FontFamily:     ff,
		TextAlign:      ta,
		Link:           re.Link,
		FrameID:        re.FrameID,
		Name:           re.Name,
		Locked:         re.Locked,
	}
}

func decodeRoundness(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b
	}
	var obj struct {
		Type int `json:"type"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.Type > 0
	}
	return false
}

func decodeFontFamily(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		switch strings.ToLower(s) {
		case "hand", "virgil", "excalifont":
			return "hand"
		case "sans", "helvetica", "nunito", "system":
			return "sans"
		case "mono", "cascadia", "monospace":
			return "mono"
		}
		if n, err := strconv.Atoi(s); err == nil {
			return fontFamilyByInt(n)
		}
		return ""
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return fontFamilyByInt(n)
	}
	return ""
}

func fontFamilyByInt(n int) string {
	switch n {
	case 2:
		return "sans"
	case 3:
		return "mono"
	default:
		return "hand"
	}
}

// normalizeColorName maps Excalidraw's standard hex palette to ArcMD's semantic
// light/dark adaptive color keys while preserving custom hex colors if unknown.
func normalizeColorName(raw, fallback string) string {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" || s == "transparent" || s == "none" {
		return fallback
	}
	switch s {
	case "ink", "gray", "red", "orange", "green", "blue", "purple":
		return s
	case "#1e1e1e", "#000000", "#1f2329", "#ffffff", "#e3e4e7":
		return "ink"
	case "#868e96", "#495057", "#adb5bd", "#ced4da", "#e9ecef":
		return "gray"
	case "#e03131", "#c92a2a", "#ffc9c9", "#ffe3e3", "#f03e3e":
		return "red"
	case "#f08c00", "#e8590c", "#ffec99", "#fff3bf", "#ffd8a8":
		return "orange"
	case "#2f9e44", "#099268", "#b2f2bb", "#d3f9d8", "#40c057":
		return "green"
	case "#1971c2", "#1864ab", "#a5d8ff", "#d0ebff", "#339af0":
		return "blue"
	case "#6741d9", "#9c36b5", "#d0bfff", "#eebefa", "#845ef7":
		return "purple"
	}
	return raw
}

// PlainText extracts searchable plain text across all elements in order.
func (s *Scene) PlainText() string {
	if s == nil {
		return ""
	}
	var lines []string
	for _, el := range s.Elements {
		if el == nil {
			continue
		}
		if name := strings.TrimSpace(el.Name); name != "" && el.Kind == KindFrame {
			lines = append(lines, name)
		}
		if text := strings.TrimSpace(el.Text); text != "" {
			lines = append(lines, text)
		}
		if link := strings.TrimSpace(el.Link); link != "" && !strings.Contains(el.Text, link) {
			lines = append(lines, link)
		}
	}
	return strings.Join(lines, "\n")
}

// MarkdownSummary projects the whiteboard into readable Markdown-like lines
// (including node labels, connections "A --[label]--> B", and [[wiki]] links)
// mapped back to element IDs for spacefs and Q&A citation reveal.
func (s *Scene) MarkdownSummary() []SummaryLine {
	if s == nil || len(s.Elements) == 0 {
		return nil
	}
	var out []SummaryLine
	labelOf := func(id string) string {
		el := s.ElementByID(id)
		if el == nil {
			return ""
		}
		t := strings.TrimSpace(strings.ReplaceAll(el.Text, "\n", " "))
		if t != "" {
			return t
		}
		if el.Name != "" {
			return el.Name
		}
		if el.Link != "" {
			return el.Link
		}
		return string(el.Kind)
	}
	for _, el := range s.Elements {
		if el == nil {
			continue
		}
		switch el.Kind {
		case KindArrow, KindLine:
			from := ""
			to := ""
			if el.StartBinding != nil {
				from = labelOf(el.StartBinding.ElementID)
			}
			if el.EndBinding != nil {
				to = labelOf(el.EndBinding.ElementID)
			}
			edge := strings.TrimSpace(strings.ReplaceAll(el.Text, "\n", " "))
			var desc string
			switch {
			case from != "" && to != "" && edge != "":
				desc = fmt.Sprintf("- %s --[%s]--> %s", from, edge, to)
			case from != "" && to != "":
				desc = fmt.Sprintf("- %s --> %s", from, to)
			case edge != "":
				desc = fmt.Sprintf("- %s", edge)
			}
			if desc != "" {
				out = append(out, SummaryLine{ElementID: el.ID, Text: desc})
			}
		case KindFrame:
			title := strings.TrimSpace(el.Name)
			if title == "" {
				title = strings.TrimSpace(el.Text)
			}
			if title != "" {
				out = append(out, SummaryLine{ElementID: el.ID, Text: "## " + title})
			}
		default:
			t := strings.TrimSpace(strings.ReplaceAll(el.Text, "\n", " "))
			if t == "" && el.Link == "" {
				continue
			}
			var b strings.Builder
			if el.Kind == KindNote {
				b.WriteString("> ")
			} else if el.Kind != KindText {
				b.WriteString("- [" + string(el.Kind) + "] ")
			}
			b.WriteString(t)
			if el.Link != "" && !strings.Contains(t, el.Link) {
				if t != "" {
					b.WriteString(" ")
				}
				b.WriteString(el.Link)
			}
			out = append(out, SummaryLine{ElementID: el.ID, Text: b.String()})
		}
	}
	return out
}

// WikiLinks returns all [[Document]] references found in element text or Link fields.
func (s *Scene) WikiLinks() []wiki.Link {
	if s == nil {
		return nil
	}
	var out []wiki.Link
	for _, el := range s.Elements {
		if el == nil {
			continue
		}
		if el.Text != "" {
			out = append(out, wiki.Links(el.Text)...)
		}
		if el.Link != "" {
			links := wiki.Links(el.Link)
			if len(links) > 0 {
				out = append(out, links...)
			} else if target, label := parseDirectDocRef(el.Link); target != "" {
				out = append(out, wiki.Link{Target: target, Label: label})
			}
		}
	}
	return out
}

// LinkTarget resolves an element's primary linked document name (from el.Link
// or the first [[link]] in el.Text).
func (el *Element) LinkTarget() (target, label string, ok bool) {
	if el == nil {
		return "", "", false
	}
	if el.Link != "" {
		if ls := wiki.Links(el.Link); len(ls) > 0 {
			return ls[0].Target, ls[0].Label, true
		}
		if t, l := parseDirectDocRef(el.Link); t != "" {
			return t, l, true
		}
	}
	if el.Text != "" {
		if ls := wiki.Links(el.Text); len(ls) > 0 {
			return ls[0].Target, ls[0].Label, true
		}
	}
	return "", "", false
}

func parseDirectDocRef(s string) (string, string) {
	s = strings.TrimSpace(s)
	if s == "" || strings.Contains(s, "://") || strings.HasPrefix(s, "mailto:") {
		return "", ""
	}
	if strings.HasPrefix(s, "[[") && strings.HasSuffix(s, "]]") {
		return wiki.Parse(s[2 : len(s)-2])
	}
	return "", ""
}

// Tags returns all #tags inside element text.
func (s *Scene) Tags() []string {
	if s == nil {
		return nil
	}
	var out []string
	for _, el := range s.Elements {
		if el == nil || el.Text == "" {
			continue
		}
		for _, t := range wiki.Tags(el.Text) {
			out = append(out, t.Name)
		}
	}
	return out
}

// RewriteWikiLinks renames [[from]] links across all elements' Text and Link fields.
func (s *Scene) RewriteWikiLinks(from, to string) bool {
	if s == nil || wiki.Same(from, to) || from == "" || to == "" {
		return false
	}
	changed := false
	for _, el := range s.Elements {
		if el == nil {
			continue
		}
		if next, ok := wiki.Rename(el.Text, from, to); ok {
			el.Text = next
			el.Version++
			changed = true
		}
		if next, ok := wiki.Rename(el.Link, from, to); ok {
			el.Link = next
			el.Version++
			changed = true
		}
	}
	return changed
}

// RewriteTags renames #from tags (including nested #from/sub) across element text.
func (s *Scene) RewriteTags(from, to string) bool {
	if s == nil || from == "" || to == "" || strings.EqualFold(from, to) {
		return false
	}
	changed := false
	for _, el := range s.Elements {
		if el == nil || el.Text == "" {
			continue
		}
		if next, ok := wiki.RenameTag(el.Text, from, to); ok {
			el.Text = next
			el.Version++
			changed = true
		}
	}
	return changed
}

// Validate checks basic invariants of the scene.
func (s *Scene) Validate() error {
	if s == nil {
		return errors.New("whiteboard: nil scene")
	}
	seen := make(map[string]bool, len(s.Elements))
	for _, el := range s.Elements {
		if el == nil {
			return errors.New("whiteboard: nil element")
		}
		if el.ID == "" {
			return errors.New("whiteboard: empty element id")
		}
		if seen[el.ID] {
			return fmt.Errorf("whiteboard: duplicate element id %q", el.ID)
		}
		seen[el.ID] = true
		if math.IsNaN(float64(el.X)) || math.IsNaN(float64(el.Y)) || math.IsNaN(float64(el.W)) || math.IsNaN(float64(el.H)) {
			return fmt.Errorf("whiteboard: NaN coordinates on %q", el.ID)
		}
	}
	return nil
}
