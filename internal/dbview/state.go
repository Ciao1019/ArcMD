package dbview

import (
	"slices"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/egoist/mygo/ui"
)

// Record open modes matching ViewConfig.OpenMode.
const (
	OpenSidePeek   = "side"
	OpenCenterPeek = "center"
	OpenFullPage   = "full"
)

// Options wires the database view to host app persistence, record opening,
// trash handling, and notifications without coupling dbview to app/editor.
type Options struct {
	ReadOnly             bool
	Embedded             bool
	HideViewTabs         bool
	Now                  time.Time
	LoadImage            func(recPath, imgURL string) *ui.Bitmap
	OnChanged            func(affectedPaths []string)
	OnOpenRecord         func(rec *database.Record, mode string)
	OnOpenRelationTarget func(targetDB, ref string)
	ListSpaceDBs         func() []database.SpaceDBInfo
	OnTrashRecords       func(backingPaths []string)
	OnOpenURL            func(rawURL string)
	OnNotify             func(msg string)
	OnAfterCreate        func(rec *database.Record)
}

// State holds all transient UI state for one open database view or page
// properties panel.
type State struct {
	Store *database.Store

	LocalView   bool
	LocalViewID string

	SearchOpen         bool
	SearchQuery        string
	searchFocusPending bool

	FilterOpen  bool
	SortOpen    bool
	GroupOpen   bool
	PropsOpen   bool
	LayoutOpen  bool
	NewViewOpen bool
	AddPropOpen bool

	NewPropName       string
	NewPropType       database.PropertyType
	NewViewName       string
	NewViewType       database.ViewType
	RenameViewID      string
	RenameViewText    string
	RenameViewPending bool

	EditingTitle      bool
	TitleDraft        string
	TitleFocusPending bool

	FocusRow int
	FocusCol int

	Editing          bool
	EditRecordID     string
	EditPropID       string
	EditText         string
	EditFocusPending bool
	EditWasFocused   bool

	EditPopoverOpen       bool
	ActivePopoverRecordID string
	ActivePopoverPropID   string
	EditOptionQuery       string
	EditDateCursor        time.Time
	EditDateStart         string
	EditDateEnd           string
	EditDateHasEnd        bool
	EditDateIncludeTime   bool
	EditDateEditingEnd    bool
	EditListItemDraft     string

	SelectedIDs map[string]bool

	RowScroll            ui.ListState
	TableScrollX         ui.ScrollState
	TableColMenuPropID   string
	TableColMenuOpen     bool
	TableRowMenuRecordID string
	TableRowMenuOpen     bool
	TabScroll            ui.ScrollState
	BoardScrollX         ui.ScrollState
	BoardScrollY         ui.ScrollState
	ListScroll           ui.ListState
	GalleryScroll        ui.ScrollState

	AddingGroup          bool
	NewGroupName         string
	NewGroupFocusPending bool

	BoardAssigneeFilter     string
	BoardFocusedCardID      string
	BoardFocusPending       bool
	BoardRenamingColKey     string
	BoardRenamingColDraft   string
	BoardRenamingColPending bool

	CalendarCursor            time.Time
	CalendarMiniMonth         time.Time
	CalendarHiddenCats        map[string]bool
	CalendarAutoUnschedViewID string
	calZeroWidthRetriedKey    string
	CalendarUnscheduledOpen   bool
	CalendarOverflowDay       string
	CalendarOverflowOpen      bool
	CalendarScroll            ui.ScrollState
	CalendarTimeScroll        ui.ScrollState
	CalendarTimeScrolledKey   string
	CalendarYearScroll        ui.ScrollState
	CalendarAgendaScroll      ui.ScrollState
	CalendarUnschedScroll     ui.ScrollState
	CalendarDetailOpen        bool
	CalendarDetailRecordID    string
	CalendarDetailAnchorKey   string
	CalendarDetailTitleDraft  string
	CalendarDetailPickerOpen  bool
	CalendarDetailEditingEnd  bool
	CalendarDetailPickerMonth time.Time
	CalendarDetailHourScroll  ui.ScrollState
	CalendarDetailMinScroll   ui.ScrollState
	calDetailMatchedAnchor    bool
	calDetailFallbackKey      string
	calDetailFallbackAnchor   ui.Element
	calDetailFallbackEv       calEvent
	CalendarDragActive        bool
	CalendarDragRecordID      string
	CalendarDragMode          uint8
	CalendarDragDX            float32
	CalendarDragDY            float32
	CalendarDragMoved         bool
	CalendarDragOrigStart     time.Time
	CalendarDragOrigEnd       time.Time
	CalendarDragOrigVal       database.DateValue

	TimelineScrollX         ui.ScrollState
	TimelineRowScroll       ui.ListState
	TimelineTableWidth      float32
	TimelineCenteredKey     string
	TimelineUnscheduledOpen bool
	TimelineDragActive      bool
	TimelineDragRecordID    string
	TimelineDragMode        uint8
	TimelineDragDX          float32
	TimelineDragMoved       bool
	TimelineDragOrigStart   time.Time
	TimelineDragOrigEnd     time.Time
	TimelineDragOrigVal     database.DateValue
	TimelineDragOrigEndVal  database.DateValue

	FeedScroll     ui.ScrollState
	FeedQuickDraft string

	FormScroll          ui.ScrollState
	FormEditMode        bool
	FormViewID          string
	FormDraft           map[string]database.Value
	FormTextDrafts      map[string]*string
	FormDateEndDrafts   map[string]*string
	FormErrors          map[string]string
	FormSubmittedTitle  string
	FormLastRecordID    string
	FormTitleDraft      string
	FormDescDraft       string
	FormSubmitTextDraft string

	RenamingPropID      string
	RenamingPropText    string
	RenamingPropPending bool

	FilterDrafts map[string]string

	ColorRulesOpen  bool
	ColorRuleDrafts map[string]*string

	TemplatesOpen     bool
	EditingTemplateID string
	TplNameDraft      string
	TplIconDraft      string
	TplBodyDraft      string
	TplStatusDraft    string
	TplPropDrafts     map[string]*string

	EditPropOpen         bool
	ConfigPropID         string
	EditFormulaExpr      string
	EditRelationBackName string
	RelationQuery        string

	RootHandle   ui.Handle
	RequestFocus bool
	Motion       dbMotionState
	BoardDrag    boardDragState
}

// NewState initializes UI state backed by store.
func NewState(store *database.Store) *State {
	return &State{
		Store:              store,
		FocusRow:           -1,
		FocusCol:           -1,
		NewPropType:        database.PropText,
		NewViewType:        database.ViewTable,
		SelectedIDs:        make(map[string]bool),
		CalendarHiddenCats: make(map[string]bool),
		FilterDrafts:       make(map[string]string),
		FormDraft:          make(map[string]database.Value),
		FormTextDrafts:     make(map[string]*string),
		FormDateEndDrafts:  make(map[string]*string),
		FormErrors:         make(map[string]string),
		ColorRuleDrafts:    make(map[string]*string),
		TplPropDrafts:      make(map[string]*string),
	}
}

// Version returns the store's revision so autosave can track database changes.
func (s *State) Version() uint64 {
	if s == nil || s.Store == nil {
		return 0
	}
	return s.Store.Revision()
}

// Revision returns the store's revision.
func (s *State) Revision() uint64 { return s.Version() }

// ActiveView returns a copy of the currently active ViewConfig, or nil.
func (s *State) ActiveView() *database.ViewConfig {
	if s == nil || s.Store == nil {
		return nil
	}
	return s.activeViewFrom(s.Store.SchemaSnapshot())
}

func (s *State) activeViewFrom(db *database.Database) *database.ViewConfig {
	if s == nil || db == nil {
		return nil
	}
	if s.LocalView && s.LocalViewID != "" {
		if v, idx := db.ViewByID(s.LocalViewID); idx >= 0 {
			cp := v.Clone()
			return &cp
		}
	}
	if db.ActiveView != "" {
		if v, idx := db.ViewByID(db.ActiveView); idx >= 0 {
			cp := v.Clone()
			return &cp
		}
	}
	if len(db.Views) > 0 {
		cp := db.Views[0].Clone()
		return &cp
	}
	return nil
}

// ActiveViewID returns the ID of the currently active view, or "".
func (s *State) ActiveViewID() string {
	if v := s.ActiveView(); v != nil {
		return v.ID
	}
	return ""
}

// SetActiveView switches the active view tab and cancels active inline editing.
func (s *State) SetActiveView(viewID string) {
	if s == nil || s.Store == nil {
		return
	}
	s.CancelEdit()
	s.CloseToolbarPopovers()
	s.FocusRow, s.FocusCol = -1, -1
	s.FormSubmittedTitle = ""
	s.calZeroWidthRetriedKey = ""
	s.Motion.drag = dbDragPreview{}
	s.Motion.settle = dbDragSettle{}
	s.Motion.suppressOverlay = false
	s.BoardDrag.reset()
	if s.LocalView {
		s.LocalViewID = viewID
		return
	}
	_ = s.Store.SetActiveView(viewID)
}

// CloseToolbarPopovers dismisses all toolbar popovers.
func (s *State) CloseToolbarPopovers() {
	s.FilterOpen = false
	s.SortOpen = false
	s.GroupOpen = false
	s.PropsOpen = false
	s.LayoutOpen = false
	s.NewViewOpen = false
	s.AddPropOpen = false
	s.ColorRulesOpen = false
	s.TemplatesOpen = false
	s.EditPropOpen = false
	s.AddingGroup = false
	s.BoardRenamingColKey = ""
	s.TableColMenuOpen = false
	s.TableColMenuPropID = ""
	s.TableRowMenuOpen = false
	s.TableRowMenuRecordID = ""
	s.CalendarOverflowOpen = false
	s.CalendarDetailOpen = false
	s.CalendarDetailPickerOpen = false
	s.CalendarDragActive = false
	s.TimelineDragActive = false
}

// OpenPropertyConfig opens the configuration popover for prop.
func (s *State) OpenPropertyConfig(prop database.Property) {
	s.CloseToolbarPopovers()
	s.EditPopoverOpen = false
	s.EditPropOpen = true
	s.ConfigPropID = prop.ID
	if prop.Formula != nil {
		s.EditFormulaExpr = prop.Formula.Expression
	} else {
		s.EditFormulaExpr = ""
	}
	s.EditRelationBackName = ""
}

// StartEditCell opens inline text editing for (recordID, propID).
func (s *State) StartEditCell(recordID, propID, initial string) {
	s.CloseToolbarPopovers()
	s.EditPopoverOpen = false
	s.ActivePopoverRecordID = ""
	s.ActivePopoverPropID = ""
	s.Editing = true
	s.EditRecordID = recordID
	s.EditPropID = propID
	s.EditText = initial
	s.EditFocusPending = true
	s.EditWasFocused = false
}

// OpenCellPopover opens the anchored property popover for (recordID, propID).
func (s *State) OpenCellPopover(rec *database.Record, prop database.Property, now time.Time) {
	if rec == nil {
		return
	}
	s.CloseToolbarPopovers()
	s.Editing = false
	s.EditRecordID = ""
	s.EditPropID = ""
	s.EditWasFocused = false
	s.EditPopoverOpen = true
	s.ActivePopoverRecordID = rec.ID
	s.ActivePopoverPropID = prop.ID
	s.EditOptionQuery = ""
	s.EditListItemDraft = ""
	s.EditDateEditingEnd = false

	if prop.Type == database.PropDate {
		if now.IsZero() {
			now = time.Now()
		}
		v := rec.ValueFor(prop)
		s.EditDateStart = v.Date.Start
		s.EditDateEnd = v.Date.End
		s.EditDateHasEnd = v.Date.End != ""
		s.EditDateIncludeTime = v.Date.IncludeTime
		if t, _, ok := database.ParseDateString(v.Date.Start, now.Location()); ok {
			s.EditDateCursor = t
		} else {
			s.EditDateCursor = now
		}
	}
}

// CancelEdit aborts any active inline text edit or cell popover without saving.
func (s *State) CancelEdit() {
	s.Editing = false
	s.EditRecordID = ""
	s.EditPropID = ""
	s.EditText = ""
	s.EditFocusPending = false
	s.EditWasFocused = false
	s.EditPopoverOpen = false
	s.ActivePopoverRecordID = ""
	s.ActivePopoverPropID = ""
	s.EditOptionQuery = ""
	s.EditListItemDraft = ""
}

// CommitTextEdit saves the active inline text edit into Store and notifies Options.
func (s *State) CommitTextEdit(opts Options) {
	if !s.Editing || s.Store == nil {
		s.CancelEdit()
		return
	}
	recID := s.EditRecordID
	propID := s.EditPropID
	raw := s.EditText
	s.Editing = false
	s.EditRecordID = ""
	s.EditPropID = ""
	s.EditFocusPending = false
	s.EditWasFocused = false

	db := s.Store.SchemaSnapshot()
	if db == nil {
		return
	}
	prop, idx := db.PropertyByID(propID)
	if idx < 0 {
		return
	}
	val := database.CoerceValue(*prop, database.TextValue(raw))
	if prop.Type == database.PropTitle {
		val = database.TextValue(strings.TrimSpace(raw))
	}
	affected, err := s.Store.SetCell(recID, propID, val)
	if err != nil {
		if opts.OnNotify != nil {
			opts.OnNotify(err.Error())
		}
		return
	}
	if opts.OnChanged != nil {
		opts.OnChanged(affected)
	}
}

// ClearSelection clears multi-row selection.
func (s *State) ClearSelection() {
	if len(s.SelectedIDs) > 0 {
		clear(s.SelectedIDs)
	}
}

// SelectedRecords returns the currently checked records in display order.
func (s *State) SelectedRecords(rows []*database.Record) []*database.Record {
	if len(s.SelectedIDs) == 0 {
		return nil
	}
	var out []*database.Record
	for _, r := range rows {
		if r != nil && s.SelectedIDs[r.ID] {
			out = append(out, r)
		}
	}
	return out
}

func (s *State) setCellAndNotify(recID, propID string, val database.Value, opts Options) bool {
	if s == nil || s.Store == nil || opts.ReadOnly {
		return false
	}
	affected, err := s.Store.SetCell(recID, propID, val)
	if err != nil {
		if opts.OnNotify != nil {
			opts.OnNotify(err.Error())
		}
		return false
	}
	if opts.OnChanged != nil {
		opts.OnChanged(affected)
	}
	return true
}

func ensureOptionColor(idx int) string {
	colors := database.OptionColors[1:] // skip "default" for new colorful tags
	return colors[idx%len(colors)]
}

func toggleStringInSlice(xs []string, item string) []string {
	if i := slices.Index(xs, item); i >= 0 {
		return slices.Delete(xs, i, i+1)
	}
	return append(xs, item)
}
