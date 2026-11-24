package database

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"
)

// CurrentVersion is the schema format version written by this ArcMD build.
const CurrentVersion = 1

// PropertyType identifies the schema kind of a database column/property.
type PropertyType string

const (
	PropTitle       PropertyType = "title"
	PropText        PropertyType = "text"
	PropNumber      PropertyType = "number"
	PropSelect      PropertyType = "select"
	PropMultiSelect PropertyType = "multi_select"
	PropStatus      PropertyType = "status"
	PropDate        PropertyType = "date"
	PropCheckbox    PropertyType = "checkbox"
	PropURL         PropertyType = "url"
	PropEmail       PropertyType = "email"
	PropPhone       PropertyType = "phone"
	PropFiles       PropertyType = "files"
	PropPerson      PropertyType = "person"
	PropCreatedTime PropertyType = "created_time"
	PropEditedTime  PropertyType = "edited_time"
	PropUniqueID    PropertyType = "unique_id"
	PropRelation    PropertyType = "relation"
	PropRollup      PropertyType = "rollup"
	PropFormula     PropertyType = "formula"
)

var validPropertyTypes = map[PropertyType]bool{
	PropTitle:       true,
	PropText:        true,
	PropNumber:      true,
	PropSelect:      true,
	PropMultiSelect: true,
	PropStatus:      true,
	PropDate:        true,
	PropCheckbox:    true,
	PropURL:         true,
	PropEmail:       true,
	PropPhone:       true,
	PropFiles:       true,
	PropPerson:      true,
	PropCreatedTime: true,
	PropEditedTime:  true,
	PropUniqueID:    true,
	PropRelation:    true,
	PropRollup:      true,
	PropFormula:     true,
}

// ValidPropertyType reports whether t is a supported property type.
func ValidPropertyType(t PropertyType) bool { return validPropertyTypes[t] }

// StatusGroup buckets status options into Notion's three workflow categories.
type StatusGroup string

const (
	StatusTodo       StatusGroup = "todo"
	StatusInProgress StatusGroup = "in_progress"
	StatusComplete   StatusGroup = "complete"
)

// OptionColors lists the standard Notion-compatible tag/status color tokens.
var OptionColors = []string{"default", "gray", "brown", "orange", "yellow", "green", "blue", "purple", "pink", "red"}

// ViewType identifies one of the 9 supported Notion-style database views.
type ViewType string

const (
	ViewTable    ViewType = "table"
	ViewBoard    ViewType = "board"
	ViewTimeline ViewType = "timeline"
	ViewCalendar ViewType = "calendar"
	ViewList     ViewType = "list"
	ViewGallery  ViewType = "gallery"
	ViewChart    ViewType = "chart"
	ViewFeed     ViewType = "feed"
	ViewForm     ViewType = "form"
)

var validViewTypes = map[ViewType]bool{
	ViewTable:    true,
	ViewBoard:    true,
	ViewTimeline: true,
	ViewCalendar: true,
	ViewList:     true,
	ViewGallery:  true,
	ViewChart:    true,
	ViewFeed:     true,
	ViewForm:     true,
}

// ValidViewType reports whether v is a supported view type.
func ValidViewType(v ViewType) bool { return validViewTypes[v] }

// SourceType defines where database records come from.
type SourceType string

const (
	SourceFolder SourceType = "folder"
	SourceSpace  SourceType = "space"
	SourceInline SourceType = "inline"
)

// ValidSourceType reports whether s is a supported record source type.
func ValidSourceType(s SourceType) bool {
	return s == SourceFolder || s == SourceSpace || s == SourceInline
}

// AggregationType identifies a column or group calculation mode.
type AggregationType string

const (
	AggNone             AggregationType = "none"
	AggCountAll         AggregationType = "count_all"
	AggCountValues      AggregationType = "count_values"
	AggCountUnique      AggregationType = "count_unique"
	AggCountEmpty       AggregationType = "count_empty"
	AggCountNotEmpty    AggregationType = "count_not_empty"
	AggPercentEmpty     AggregationType = "percent_empty"
	AggPercentNotEmpty  AggregationType = "percent_not_empty"
	AggSum              AggregationType = "sum"
	AggAverage          AggregationType = "average"
	AggMedian           AggregationType = "median"
	AggMin              AggregationType = "min"
	AggMax              AggregationType = "max"
	AggRange            AggregationType = "range"
	AggChecked          AggregationType = "checked"
	AggUnchecked        AggregationType = "unchecked"
	AggPercentChecked   AggregationType = "percent_checked"
	AggPercentUnchecked AggregationType = "percent_unchecked"
	AggEarliestDate     AggregationType = "earliest_date"
	AggLatestDate       AggregationType = "latest_date"
	AggDateRange        AggregationType = "date_range"
	AggShowOriginal     AggregationType = "show_original"
	AggShowUnique       AggregationType = "show_unique"
)

// SelectOption defines one choice in a Select, MultiSelect, or Status property.
type SelectOption struct {
	ID    string      `json:"id"`
	Name  string      `json:"name"`
	Color string      `json:"color,omitempty"`
	Group StatusGroup `json:"group,omitempty"`
}

// NumberConfig configures display formatting and progress visualization.
type NumberConfig struct {
	Format     string  `json:"format,omitempty"` // number, number_with_commas, percent, dollar, yuan, euro, pound, yen, currency
	Currency   string  `json:"currency,omitempty"`
	Decimals   int     `json:"decimals,omitempty"`
	ShowAs     string  `json:"showAs,omitempty"` // number, bar, ring
	BarColor   string  `json:"barColor,omitempty"`
	DivideBy   float64 `json:"divideBy,omitempty"`
	ShowNumber bool    `json:"showNumber,omitempty"`
}

// DateConfig configures date formatting and frontmatter end-date mapping.
type DateConfig struct {
	DateFormat  string `json:"dateFormat,omitempty"`
	TimeFormat  string `json:"timeFormat,omitempty"`
	IncludeTime bool   `json:"includeTime,omitempty"`
	EndKey      string `json:"endKey,omitempty"`
}

// RelationConfig links records to another database (or self).
type RelationConfig struct {
	TargetDB     string `json:"targetDB,omitempty"`
	SyncedPropID string `json:"syncedPropID,omitempty"`
	LimitOne     bool   `json:"limitOne,omitempty"`
}

// RollupConfig aggregates a property across a Relation property.
type RollupConfig struct {
	RelationPropID string          `json:"relationPropID"`
	TargetPropID   string          `json:"targetPropID"`
	Calculate      AggregationType `json:"calculate"`
}

// FormulaConfig holds a Formula 2.0 expression.
type FormulaConfig struct {
	Expression string `json:"expression"`
}

// UniqueIDConfig configures auto-incrementing record identifiers (e.g. TASK-12).
type UniqueIDConfig struct {
	Prefix string `json:"prefix,omitempty"`
	Next   int    `json:"next,omitempty"`
}

// Property defines one column/field in a database schema.
type Property struct {
	ID          string          `json:"id"`
	Key         string          `json:"key"`
	Name        string          `json:"name"`
	Type        PropertyType    `json:"type"`
	Options     []SelectOption  `json:"options,omitempty"`
	Number      *NumberConfig   `json:"number,omitempty"`
	Date        *DateConfig     `json:"date,omitempty"`
	Relation    *RelationConfig `json:"relation,omitempty"`
	Rollup      *RollupConfig   `json:"rollup,omitempty"`
	Formula     *FormulaConfig  `json:"formula,omitempty"`
	UniqueID    *UniqueIDConfig `json:"uniqueID,omitempty"`
	Description string          `json:"description,omitempty"`
	Wrap        bool            `json:"wrap,omitempty"`
	Width       float32         `json:"width,omitempty"`
}

// Clone returns a deep copy of p.
func (p Property) Clone() Property {
	out := p
	if p.Options != nil {
		out.Options = append([]SelectOption(nil), p.Options...)
	}
	if p.Number != nil {
		v := *p.Number
		out.Number = &v
	}
	if p.Date != nil {
		v := *p.Date
		out.Date = &v
	}
	if p.Relation != nil {
		v := *p.Relation
		out.Relation = &v
	}
	if p.Rollup != nil {
		v := *p.Rollup
		out.Rollup = &v
	}
	if p.Formula != nil {
		v := *p.Formula
		out.Formula = &v
	}
	if p.UniqueID != nil {
		v := *p.UniqueID
		out.UniqueID = &v
	}
	return out
}

// SourceConfig configures where records are stored and scanned.
type SourceConfig struct {
	Type      SourceType `json:"type"`
	Dir       string     `json:"dir,omitempty"`
	Recursive bool       `json:"recursive,omitempty"`
	TagFilter []string   `json:"tagFilter,omitempty"`
}

// Clone returns a deep copy of s.
func (s SourceConfig) Clone() SourceConfig {
	out := s
	if s.TagFilter != nil {
		out.TagFilter = append([]string(nil), s.TagFilter...)
	}
	return out
}

// ViewProperty controls per-view visibility, order, width, and wrap behavior.
type ViewProperty struct {
	PropertyID string  `json:"propertyID"`
	Visible    bool    `json:"visible"`
	Width      float32 `json:"width,omitempty"`
	Wrap       bool    `json:"wrap,omitempty"`
}

// FilterRule matches a single property against an operator and target Value.
type FilterRule struct {
	ID         string   `json:"id"`
	PropertyID string   `json:"propertyID"`
	Op         FilterOp `json:"op"`
	Value      Value    `json:"value,omitempty"`
}

// Clone returns a deep copy of r.
func (r FilterRule) Clone() FilterRule {
	out := r
	out.Value = r.Value.Clone()
	return out
}

// FilterGroup combines rules and nested filter groups with "and" or "or".
type FilterGroup struct {
	Conjunction string        `json:"conjunction"` // "and" or "or"
	Rules       []FilterRule  `json:"rules,omitempty"`
	Groups      []FilterGroup `json:"groups,omitempty"`
}

// Clone returns a deep copy of g.
func (g *FilterGroup) Clone() *FilterGroup {
	if g == nil {
		return nil
	}
	out := &FilterGroup{Conjunction: g.Conjunction}
	if g.Rules != nil {
		out.Rules = make([]FilterRule, len(g.Rules))
		for i, r := range g.Rules {
			out.Rules[i] = r.Clone()
		}
	}
	if g.Groups != nil {
		out.Groups = make([]FilterGroup, len(g.Groups))
		for i := range g.Groups {
			out.Groups[i] = *g.Groups[i].Clone()
		}
	}
	return out
}

// SortRule sorts records by one property in ascending or descending order.
type SortRule struct {
	PropertyID string `json:"propertyID"`
	Desc       bool   `json:"desc,omitempty"`
}

// GroupConfig defines how records are bucketed into groups.
type GroupConfig struct {
	PropertyID string   `json:"propertyID"`
	DateBy     string   `json:"dateBy,omitempty"`   // day, week, month, year, relative
	StatusBy   string   `json:"statusBy,omitempty"` // option, group
	NumberStep float64  `json:"numberStep,omitempty"`
	Sort       string   `json:"sort,omitempty"` // manual, asc, desc
	HideEmpty  bool     `json:"hideEmpty,omitempty"`
	Order      []string `json:"order,omitempty"`
	Collapsed  []string `json:"collapsed,omitempty"`
}

// Clone returns a deep copy of g.
func (g *GroupConfig) Clone() *GroupConfig {
	if g == nil {
		return nil
	}
	out := *g
	if g.Order != nil {
		out.Order = append([]string(nil), g.Order...)
	}
	if g.Collapsed != nil {
		out.Collapsed = append([]string(nil), g.Collapsed...)
	}
	return &out
}

// ColorRule applies a tint to matching rows or cards.
type ColorRule struct {
	ID     string      `json:"id"`
	Filter FilterGroup `json:"filter"`
	Color  string      `json:"color"`
}

// Clone returns a deep copy of c.
func (c ColorRule) Clone() ColorRule {
	out := c
	if cp := c.Filter.Clone(); cp != nil {
		out.Filter = *cp
	}
	return out
}

// TableViewConfig configures table-specific presentation and column footer sums.
type TableViewConfig struct {
	FreezeFirst  bool                       `json:"freezeFirst,omitempty"`
	ShowIcon     bool                       `json:"showIcon,omitempty"`
	WrapAll      bool                       `json:"wrapAll,omitempty"`
	RowHeight    string                     `json:"rowHeight,omitempty"`
	Aggregations map[string]AggregationType `json:"aggregations,omitempty"`
}

// BoardViewConfig configures Kanban board cards, columns, and swimlanes.
type BoardViewConfig struct {
	CardSize      string       `json:"cardSize,omitempty"`    // small, medium, large
	CoverSource   string       `json:"coverSource,omitempty"` // none, cover, content, prop:<id>
	FitImage      bool         `json:"fitImage,omitempty"`
	SubGroup      *GroupConfig `json:"subGroup,omitempty"`
	HideEmptyCols bool         `json:"hideEmptyCols,omitempty"`
	ColorColumn   bool         `json:"colorColumn,omitempty"`
	ShowCount     bool         `json:"showCount,omitempty"`
}

// TimelineViewConfig configures Gantt/timeline scale, side table, and dependencies.
type TimelineViewConfig struct {
	StartPropID string   `json:"startPropID,omitempty"`
	EndPropID   string   `json:"endPropID,omitempty"`
	Zoom        string   `json:"zoom,omitempty"` // hour, day, week, biweek, month, quarter, year
	ShowTable   bool     `json:"showTable,omitempty"`
	TableProps  []string `json:"tableProps,omitempty"`
	ShowArrows  bool     `json:"showArrows,omitempty"`
	CascadeMode string   `json:"cascadeMode,omitempty"` // none, maintain, avoid_overlap
}

// CalendarViewConfig configures monthly/weekly calendar layout.
type CalendarViewConfig struct {
	DatePropID   string `json:"datePropID,omitempty"`
	Mode         string `json:"mode,omitempty"` // month, week
	ShowWeekends bool   `json:"showWeekends,omitempty"`
	WeekStart    int    `json:"weekStart,omitempty"` // 0=Sunday, 1=Monday
}

// GalleryViewConfig configures gallery card grid sizing and covers.
type GalleryViewConfig struct {
	CardSize    string `json:"cardSize,omitempty"`    // small, medium, large
	CoverSource string `json:"coverSource,omitempty"` // none, cover, content, prop:<id>
	FitImage    bool   `json:"fitImage,omitempty"`
	ShowIcon    bool   `json:"showIcon,omitempty"`
}

// ChartViewConfig configures bar, line, and donut/pie charts.
type ChartViewConfig struct {
	Kind        string          `json:"kind,omitempty"` // bar, hbar, line, donut
	XPropID     string          `json:"xPropID,omitempty"`
	XDateBy     string          `json:"xDateBy,omitempty"`
	XSort       string          `json:"xSort,omitempty"`
	YMode       string          `json:"yMode,omitempty"` // count, prop
	YPropID     string          `json:"yPropID,omitempty"`
	YAgg        AggregationType `json:"yAgg,omitempty"`
	StackPropID string          `json:"stackPropID,omitempty"`
	ShowValues  bool            `json:"showValues,omitempty"`
	ShowLegend  bool            `json:"showLegend,omitempty"`
	Smooth      bool            `json:"smooth,omitempty"`
	Donut       bool            `json:"donut,omitempty"`
}

// FeedViewConfig configures feed stream cards.
type FeedViewConfig struct {
	ShowExcerpt bool `json:"showExcerpt,omitempty"`
	ShowImage   bool `json:"showImage,omitempty"`
	Compact     bool `json:"compact,omitempty"`
}

// FormFieldConfig configures a single field in a Form view.
type FormFieldConfig struct {
	PropertyID string `json:"propertyID"`
	Label      string `json:"label,omitempty"`
	Help       string `json:"help,omitempty"`
	Required   bool   `json:"required,omitempty"`
	Hidden     bool   `json:"hidden,omitempty"`
	Default    *Value `json:"default,omitempty"`
}

// FormViewConfig configures form submission views.
type FormViewConfig struct {
	Title            string            `json:"title,omitempty"`
	Description      string            `json:"description,omitempty"`
	Fields           []FormFieldConfig `json:"fields,omitempty"`
	SubmitText       string            `json:"submitText,omitempty"`
	ContinueOnSubmit bool              `json:"continueOnSubmit,omitempty"`
	TemplateID       string            `json:"templateID,omitempty"`
}

// ViewConfig stores the configuration of one named view tab.
type ViewConfig struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Type        ViewType            `json:"type"`
	Icon        string              `json:"icon,omitempty"`
	Filter      *FilterGroup        `json:"filter,omitempty"`
	Sorts       []SortRule          `json:"sorts,omitempty"`
	Group       *GroupConfig        `json:"group,omitempty"`
	Properties  []ViewProperty      `json:"properties,omitempty"`
	ManualOrder []string            `json:"manualOrder,omitempty"`
	Colors      []ColorRule         `json:"colors,omitempty"`
	OpenMode    string              `json:"openMode,omitempty"` // side, center, full
	Table       *TableViewConfig    `json:"table,omitempty"`
	Board       *BoardViewConfig    `json:"board,omitempty"`
	Timeline    *TimelineViewConfig `json:"timeline,omitempty"`
	Calendar    *CalendarViewConfig `json:"calendar,omitempty"`
	Gallery     *GalleryViewConfig  `json:"gallery,omitempty"`
	Chart       *ChartViewConfig    `json:"chart,omitempty"`
	Feed        *FeedViewConfig     `json:"feed,omitempty"`
	Form        *FormViewConfig     `json:"form,omitempty"`
}

// Clone returns a deep copy of v.
func (v ViewConfig) Clone() ViewConfig {
	out := v
	out.Filter = v.Filter.Clone()
	if v.Sorts != nil {
		out.Sorts = append([]SortRule(nil), v.Sorts...)
	}
	out.Group = v.Group.Clone()
	if v.Properties != nil {
		out.Properties = append([]ViewProperty(nil), v.Properties...)
	}
	if v.ManualOrder != nil {
		out.ManualOrder = append([]string(nil), v.ManualOrder...)
	}
	if v.Colors != nil {
		out.Colors = make([]ColorRule, len(v.Colors))
		for i, c := range v.Colors {
			out.Colors[i] = c.Clone()
		}
	}
	if v.Table != nil {
		t := *v.Table
		if v.Table.Aggregations != nil {
			t.Aggregations = make(map[string]AggregationType, len(v.Table.Aggregations))
			for k, agg := range v.Table.Aggregations {
				t.Aggregations[k] = agg
			}
		}
		out.Table = &t
	}
	if v.Board != nil {
		b := *v.Board
		b.SubGroup = v.Board.SubGroup.Clone()
		out.Board = &b
	}
	if v.Timeline != nil {
		tl := *v.Timeline
		if v.Timeline.TableProps != nil {
			tl.TableProps = append([]string(nil), v.Timeline.TableProps...)
		}
		out.Timeline = &tl
	}
	if v.Calendar != nil {
		c := *v.Calendar
		out.Calendar = &c
	}
	if v.Gallery != nil {
		g := *v.Gallery
		out.Gallery = &g
	}
	if v.Chart != nil {
		ch := *v.Chart
		out.Chart = &ch
	}
	if v.Feed != nil {
		fd := *v.Feed
		out.Feed = &fd
	}
	if v.Form != nil {
		fm := *v.Form
		if v.Form.Fields != nil {
			fm.Fields = make([]FormFieldConfig, len(v.Form.Fields))
			for i, f := range v.Form.Fields {
				fc := f
				if f.Default != nil {
					dv := f.Default.Clone()
					fc.Default = &dv
				}
				fm.Fields[i] = fc
			}
		}
		out.Form = &fm
	}
	return out
}

// Template defines a reusable record skeleton with preset properties and body.
type Template struct {
	ID    string           `json:"id"`
	Name  string           `json:"name"`
	Icon  string           `json:"icon,omitempty"`
	Props map[string]Value `json:"props,omitempty"`
	Body  string           `json:"body,omitempty"`
}

// Clone returns a deep copy of t.
func (t Template) Clone() Template {
	out := t
	if t.Props != nil {
		out.Props = make(map[string]Value, len(t.Props))
		for k, v := range t.Props {
			out.Props[k] = v.Clone()
		}
	}
	return out
}

// Database holds the complete schema, view definitions, templates, and
// optional inline records of a .db.md database.
type Database struct {
	Version     int          `json:"version"`
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Description string       `json:"description,omitempty"`
	Icon        string       `json:"icon,omitempty"`
	Cover       string       `json:"cover,omitempty"`
	CoverY      float64      `json:"coverY,omitempty"`
	Source      SourceConfig `json:"source"`
	Properties  []Property   `json:"properties"`
	Views       []ViewConfig `json:"views"`
	ActiveView  string       `json:"activeView,omitempty"`
	Templates   []Template   `json:"templates,omitempty"`
	InlineRows  []*Record    `json:"inlineRows,omitempty"`
}

var (
	idMu   sync.Mutex
	idPool [4080]byte
	idUsed = len(idPool)
)

// NewID generates a random 24-character lowercase hex identifier.
func NewID() string {
	var raw [12]byte
	idMu.Lock()
	if idUsed+len(raw) > len(idPool) {
		if _, err := rand.Read(idPool[:]); err != nil {
			idMu.Unlock()
			panic(err)
		}
		idUsed = 0
	}
	copy(raw[:], idPool[idUsed:idUsed+len(raw)])
	idUsed += len(raw)
	idMu.Unlock()
	var encoded [24]byte
	hex.Encode(encoded[:], raw[:])
	return string(encoded[:])
}

// NewDefault creates a starter Database with Title, Status, MultiSelect Tags,
// and Date properties, plus default Table and Board views.
func NewDefault(title string, source SourceConfig) *Database {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "未命名数据库"
	}
	if !ValidSourceType(source.Type) {
		source.Type = SourceFolder
	}
	pTitle := Property{ID: "prop_title", Key: "title", Name: "名称", Type: PropTitle, Width: 260}
	pStatus := Property{
		ID:   "prop_status",
		Key:  "status",
		Name: "状态",
		Type: PropStatus,
		Options: []SelectOption{
			{ID: "st_todo", Name: "未开始", Color: "gray", Group: StatusTodo},
			{ID: "st_doing", Name: "进行中", Color: "blue", Group: StatusInProgress},
			{ID: "st_done", Name: "已完成", Color: "green", Group: StatusComplete},
		},
		Width: 120,
	}
	pTags := Property{
		ID:   "prop_tags",
		Key:  "tags",
		Name: "标签",
		Type: PropMultiSelect,
		Options: []SelectOption{
			{ID: "tag_important", Name: "重要", Color: "red"},
			{ID: "tag_idea", Name: "灵感", Color: "purple"},
		},
		Width: 160,
	}
	pDate := Property{
		ID:    "prop_date",
		Key:   "date",
		Name:  "日期",
		Type:  PropDate,
		Date:  &DateConfig{EndKey: "date_end"},
		Width: 140,
	}

	props := []Property{pTitle, pStatus, pTags, pDate}
	vis := []ViewProperty{
		{PropertyID: pTitle.ID, Visible: true, Width: 260},
		{PropertyID: pStatus.ID, Visible: true, Width: 120},
		{PropertyID: pTags.ID, Visible: true, Width: 160},
		{PropertyID: pDate.ID, Visible: true, Width: 140},
	}

	tableID := "view_table"
	boardID := "view_board"
	return &Database{
		Version:    CurrentVersion,
		ID:         NewID(),
		Title:      title,
		Source:     source.Clone(),
		Properties: props,
		Views: []ViewConfig{
			{
				ID:         tableID,
				Name:       "表格",
				Type:       ViewTable,
				Properties: slices.Clone(vis),
				OpenMode:   "side",
				Table: &TableViewConfig{
					FreezeFirst: true,
					ShowIcon:    true,
					Aggregations: map[string]AggregationType{
						pTitle.ID: AggCountAll,
					},
				},
			},
			{
				ID:         boardID,
				Name:       "看板",
				Type:       ViewBoard,
				Group:      &GroupConfig{PropertyID: pStatus.ID, StatusBy: "option"},
				Properties: slices.Clone(vis),
				OpenMode:   "side",
				Board: &BoardViewConfig{
					CardSize:    "medium",
					ColorColumn: true,
					ShowCount:   true,
				},
			},
		},
		ActiveView: tableID,
	}
}

// Validate verifies structural invariants and rejects unsupported versions or unsafe paths.
func (d *Database) Validate() error {
	if d == nil {
		return errors.New("database: nil schema")
	}
	if d.Version <= 0 || d.Version > CurrentVersion {
		return fmt.Errorf("database: unsupported schema version %d", d.Version)
	}
	if strings.TrimSpace(d.ID) == "" {
		return errors.New("database: empty id")
	}
	if strings.TrimSpace(d.Title) == "" {
		return errors.New("database: empty title")
	}
	if !ValidSourceType(d.Source.Type) {
		return fmt.Errorf("database: invalid source type %q", d.Source.Type)
	}
	if !validRelativeDir(d.Source.Dir) {
		return fmt.Errorf("database: unsafe source dir %q", d.Source.Dir)
	}
	if len(d.Properties) == 0 {
		return errors.New("database: properties must not be empty")
	}

	propIDs := make(map[string]struct{}, len(d.Properties))
	propKeys := make(map[string]struct{}, len(d.Properties))
	titleCount := 0
	for _, p := range d.Properties {
		if strings.TrimSpace(p.ID) == "" {
			return errors.New("database: property id must not be empty")
		}
		if _, dup := propIDs[p.ID]; dup {
			return fmt.Errorf("database: duplicate property id %q", p.ID)
		}
		propIDs[p.ID] = struct{}{}

		if strings.TrimSpace(p.Key) == "" {
			return fmt.Errorf("database: property %q key must not be empty", p.ID)
		}
		if _, dup := propKeys[p.Key]; dup {
			return fmt.Errorf("database: duplicate property key %q", p.Key)
		}
		propKeys[p.Key] = struct{}{}

		if !ValidPropertyType(p.Type) {
			return fmt.Errorf("database: unknown property type %q", p.Type)
		}
		if p.Type == PropTitle {
			titleCount++
		}
		if p.Type == PropStatus {
			for _, opt := range p.Options {
				if opt.Group != "" && opt.Group != StatusTodo && opt.Group != StatusInProgress && opt.Group != StatusComplete {
					return fmt.Errorf("database: invalid status group %q", opt.Group)
				}
			}
		}
	}
	if titleCount != 1 {
		return fmt.Errorf("database: expected exactly 1 title property, got %d", titleCount)
	}

	viewIDs := make(map[string]struct{}, len(d.Views))
	for _, v := range d.Views {
		if strings.TrimSpace(v.ID) == "" {
			return errors.New("database: view id must not be empty")
		}
		if _, dup := viewIDs[v.ID]; dup {
			return fmt.Errorf("database: duplicate view id %q", v.ID)
		}
		viewIDs[v.ID] = struct{}{}
		if !ValidViewType(v.Type) {
			return fmt.Errorf("database: unknown view type %q", v.Type)
		}
	}
	if d.ActiveView != "" {
		if _, ok := viewIDs[d.ActiveView]; !ok {
			return fmt.Errorf("database: activeView %q does not exist", d.ActiveView)
		}
	}
	return nil
}

func validRelativeDir(dir string) bool {
	if dir == "" {
		return true
	}
	if strings.ContainsAny(dir, "\\\x00:") {
		return false
	}
	if path.IsAbs(dir) {
		return false
	}
	clean := path.Clean(dir)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return false
	}
	for seg := range strings.SplitSeq(clean, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// PropertyByID returns the property with id and its index, or (nil, -1).
func (d *Database) PropertyByID(id string) (*Property, int) {
	if d == nil {
		return nil, -1
	}
	for i := range d.Properties {
		if d.Properties[i].ID == id {
			return &d.Properties[i], i
		}
	}
	return nil, -1
}

// TitleProperty returns the schema's unique PropTitle property, or nil.
func (d *Database) TitleProperty() *Property {
	if d == nil {
		return nil
	}
	for i := range d.Properties {
		if d.Properties[i].Type == PropTitle {
			return &d.Properties[i]
		}
	}
	return nil
}

// ViewByID returns the view with id and its index, or (nil, -1).
func (d *Database) ViewByID(id string) (*ViewConfig, int) {
	if d == nil {
		return nil, -1
	}
	for i := range d.Views {
		if d.Views[i].ID == id {
			return &d.Views[i], i
		}
	}
	return nil, -1
}

// Clone returns a deep copy of d.
func (d *Database) Clone() *Database {
	if d == nil {
		return nil
	}
	out := *d
	out.Source = d.Source.Clone()
	if d.Properties != nil {
		out.Properties = make([]Property, len(d.Properties))
		for i, p := range d.Properties {
			out.Properties[i] = p.Clone()
		}
	}
	if d.Views != nil {
		out.Views = make([]ViewConfig, len(d.Views))
		for i, v := range d.Views {
			out.Views[i] = v.Clone()
		}
	}
	if d.Templates != nil {
		out.Templates = make([]Template, len(d.Templates))
		for i, t := range d.Templates {
			out.Templates[i] = t.Clone()
		}
	}
	if d.InlineRows != nil {
		out.InlineRows = make([]*Record, len(d.InlineRows))
		for i, r := range d.InlineRows {
			out.InlineRows[i] = r.Clone()
		}
	}
	return &out
}

// Digest computes a deterministic SHA-256 hash of the Database state.
func (d *Database) Digest() [32]byte {
	data, err := json.Marshal(d)
	if err != nil {
		return [32]byte{}
	}
	return sha256.Sum256(data)
}
