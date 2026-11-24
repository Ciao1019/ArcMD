package dbview

import (
	"strings"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// ViewSpec describes a registered database view type. Adding or enabling a new
// view type (such as Kanban, Calendar, Gallery, Chart, Feed, or Form) only
// requires updating this registry table.
type ViewSpec struct {
	Type       database.ViewType
	LabelKey   string
	Icon       *ui.SVG
	Enabled    bool
	InitConfig func(vc *database.ViewConfig, db *database.Database)
	Render     func(c *ui.Context, st *State, view database.ViewConfig, db *database.Database, res *database.QueryResult, opts Options, pal Palette)
}

// Label returns the localized display name of this view type.
func (s ViewSpec) Label() string {
	return i18n.T(s.LabelKey)
}

var viewRegistry = []ViewSpec{
	{
		Type:     database.ViewTable,
		LabelKey: "表格视图",
		Icon:     iconViewTable,
		Enabled:  true,
		InitConfig: func(vc *database.ViewConfig, _ *database.Database) {
			vc.Table = &database.TableViewConfig{FreezeFirst: true, ShowIcon: true}
		},
		Render: tableView,
	},
	{
		Type:     database.ViewTimeline,
		LabelKey: "甘特视图",
		Icon:     iconViewTimeline,
		Enabled:  true,
		InitConfig: func(vc *database.ViewConfig, db *database.Database) {
			ensureTimelineConfig(vc, db)
		},
		Render: timelineView,
	},
	{
		Type:     database.ViewBoard,
		LabelKey: "看板",
		Icon:     iconViewBoard,
		Enabled:  true,
		InitConfig: func(vc *database.ViewConfig, db *database.Database) {
			if gp := defaultBoardGroupProp(db); gp != nil {
				vc.Group = &database.GroupConfig{PropertyID: gp.ID}
				if gp.Type == database.PropStatus {
					vc.Group.StatusBy = "option"
				} else if gp.Type == database.PropDate || gp.Type == database.PropCreatedTime || gp.Type == database.PropEditedTime {
					vc.Group.DateBy = "month"
				}
			}
			vc.Board = &database.BoardViewConfig{
				CardSize:       "medium",
				CoverSource:    "none",
				ColorColumn:    true,
				ShowCount:      true,
				AssigneePropID: defaultBoardAssigneePropID(db),
				PriorityPropID: defaultBoardPriorityPropID(db),
			}
			if dp := defaultDateProp(db); dp != nil {
				vc.Board.DatePropID = dp.ID
			}
		},
		Render: boardView,
	},
	{
		Type:     database.ViewCalendar,
		LabelKey: "日历",
		Icon:     iconViewCalendar,
		Enabled:  true,
		InitConfig: func(vc *database.ViewConfig, db *database.Database) {
			ensureCalendarConfig(vc, db)
		},
		Render: calendarView,
	},
	{
		Type:     database.ViewList,
		LabelKey: "列表",
		Icon:     iconViewList,
		Enabled:  true,
		Render:   listView,
	},
	{
		Type:     database.ViewGallery,
		LabelKey: "画廊",
		Icon:     iconViewGallery,
		Enabled:  true,
		InitConfig: func(vc *database.ViewConfig, _ *database.Database) {
			vc.Gallery = &database.GalleryViewConfig{
				CardSize:    "medium",
				CoverSource: "cover",
				ShowIcon:    true,
			}
		},
		Render: galleryView,
	},
	{
		Type:     database.ViewChart,
		LabelKey: "图表",
		Icon:     iconViewChart,
		Enabled:  true,
		InitConfig: func(vc *database.ViewConfig, db *database.Database) {
			ensureChartConfig(vc, db)
		},
		Render: chartView,
	},
	{
		Type:     database.ViewFeed,
		LabelKey: "动态流",
		Icon:     iconViewFeed,
		Enabled:  true,
		InitConfig: func(vc *database.ViewConfig, db *database.Database) {
			ensureFeedConfig(vc)
			if cp := defaultColorPropID(db); cp != "" {
				vc.Feed.StatusPropID = cp
			}
			if dp := defaultDateProp(db); dp != nil {
				vc.Feed.DatePropID = dp.ID
			}
		},
		Render: feedView,
	},
	{
		Type:     database.ViewForm,
		LabelKey: "表单",
		Icon:     iconViewForm,
		Enabled:  true,
		InitConfig: func(vc *database.ViewConfig, db *database.Database) {
			ensureFormConfig(vc, db)
		},
		Render: formView,
	},
}

// EnabledViewSpecs returns all view types currently offered for creation in UI
// menus (currently all 9 registered view types).
func EnabledViewSpecs() []ViewSpec {
	var out []ViewSpec
	for _, spec := range viewRegistry {
		if spec.Enabled {
			out = append(out, spec)
		}
	}
	return out
}

// ViewSpecFor looks up the registered specification for vt.
func ViewSpecFor(vt database.ViewType) (ViewSpec, bool) {
	for _, spec := range viewRegistry {
		if spec.Type == vt {
			return spec, true
		}
	}
	return ViewSpec{}, false
}

// NewViewConfig creates a fresh, isolated ViewConfig for vt against db using
// the registered ViewSpec initializer.
func NewViewConfig(vt database.ViewType, name string, db *database.Database) database.ViewConfig {
	spec, ok := ViewSpecFor(vt)
	if !ok {
		spec, _ = ViewSpecFor(database.ViewTable)
		vt = database.ViewTable
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = spec.Label()
	}
	var vps []database.ViewProperty
	if db != nil {
		for _, p := range db.Properties {
			vps = append(vps, database.ViewProperty{
				PropertyID: p.ID,
				Visible:    true,
				Width:      p.Width,
				Wrap:       p.Wrap,
			})
		}
	}
	vc := database.ViewConfig{
		ID:         "view_" + database.NewID()[:8],
		Name:       name,
		Type:       vt,
		OpenMode:   OpenSidePeek,
		Properties: vps,
	}
	if spec.InitConfig != nil {
		spec.InitConfig(&vc, db)
	}
	return vc
}
