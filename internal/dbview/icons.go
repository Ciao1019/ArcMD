package dbview

import (
	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

func lucide(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

// View-type Lucide icons.
var (
	iconViewTable    = lucide(`<path d="M12 3v18"/><rect width="18" height="18" x="3" y="3" rx="2"/><path d="M3 9h18"/><path d="M3 15h18"/>`)
	iconViewBoard    = lucide(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M8 7v7"/><path d="M12 7v4"/><path d="M16 7v9"/>`)
	iconViewTimeline = lucide(`<path d="M3 6h18"/><path d="M6 12h10"/><path d="M8 18h12"/>`)
	iconViewCalendar = lucide(`<path d="M8 2v4"/><path d="M16 2v4"/><rect width="18" height="18" x="3" y="4" rx="2"/><path d="M3 10h18"/>`)
	iconViewList     = lucide(`<path d="M3 12h.01"/><path d="M3 18h.01"/><path d="M3 6h.01"/><path d="M8 12h13"/><path d="M8 18h13"/><path d="M8 6h13"/>`)
	iconViewGallery  = lucide(`<rect width="7" height="7" x="3" y="3" rx="1"/><rect width="7" height="7" x="14" y="3" rx="1"/><rect width="7" height="7" x="14" y="14" rx="1"/><rect width="7" height="7" x="3" y="14" rx="1"/>`)
	iconViewChart    = lucide(`<path d="M3 3v16a2 2 0 0 0 2 2h16"/><path d="M18 17V9"/><path d="M13 17V5"/><path d="M8 17v-3"/>`)
	iconViewFeed     = lucide(`<rect width="18" height="7" x="3" y="3" rx="2"/><rect width="18" height="7" x="3" y="14" rx="2"/>`)
	iconViewForm     = lucide(`<path d="M12 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.375 2.625a1 1 0 0 1 3 3l-9.013 9.014a2 2 0 0 1-.853.505l-2.873.84a.5.5 0 0 1-.62-.62l.84-2.873a2 2 0 0 1 .506-.852z"/>`)
)

// Property-type Lucide icons.
var (
	iconPropTitle    = lucide(`<path d="M4 7V4h16v3"/><path d="M9 20h6"/><path d="M12 4v16"/>`)
	iconPropText     = lucide(`<path d="M17 6.1H3"/><path d="M21 12.1H3"/><path d="M15.1 18H3"/>`)
	iconPropNumber   = lucide(`<line x1="4" x2="20" y1="9" y2="9"/><line x1="4" x2="20" y1="15" y2="15"/><line x1="10" x2="8" y1="3" y2="21"/><line x1="16" x2="14" y1="3" y2="21"/>`)
	iconPropSelect   = lucide(`<circle cx="12" cy="12" r="9"/><path d="m9 12 2 2 4-4"/>`)
	iconPropMulti    = lucide(`<path d="M9 5H2v7l6.29 6.29c.94.94 2.48.94 3.42 0l3.58-3.58c.94-.94.94-2.48 0-3.42L9 5Z"/><path d="M6 9.01V9"/>`)
	iconPropStatus   = lucide(`<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="3"/>`)
	iconPropDate     = iconViewCalendar
	iconPropCheckbox = lucide(`<rect width="18" height="18" x="3" y="3" rx="3"/><path d="m9 12 2 2 4-4"/>`)
	iconPropURL      = lucide(`<path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>`)
	iconPropEmail    = lucide(`<rect width="20" height="16" x="2" y="4" rx="2"/><path d="m22 7-8.97 5.7a1.94 1.94 0 0 1-2.06 0L2 7"/>`)
	iconPropPhone    = lucide(`<path d="M22 16.92v3a2 2 0 0 1-2.18 2 19.79 19.79 0 0 1-8.63-3.07 19.5 19.5 0 0 1-6-6 19.79 19.79 0 0 1-3.07-8.67A2 2 0 0 1 4.11 2h3a2 2 0 0 1 2 1.72c.127.96.361 1.903.7 2.81a2 2 0 0 1-.45 2.11L8.09 9.91a16 16 0 0 0 6 6l1.27-1.27a2 2 0 0 1 2.11-.45c.907.339 1.85.573 2.81.7A2 2 0 0 1 22 16.92z"/>`)
	iconPropFiles    = lucide(`<path d="m21.44 11.05-9.19 9.19a6 6 0 0 1-8.49-8.49l8.57-8.57A4 4 0 1 1 18 8.84l-8.59 8.57a2 2 0 0 1-2.83-2.83l8.49-8.48"/>`)
	iconPropPerson   = lucide(`<path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/>`)
	iconPropTime     = lucide(`<circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>`)
	iconPropUniqueID = lucide(`<rect width="18" height="12" x="3" y="6" rx="2"/><path d="M7 10h2v4H7z"/><path d="M12 10h5"/><path d="M12 14h3"/>`)
	iconPropRelation = lucide(`<path d="M7 7h10v10"/><path d="M7 17 17 7"/>`)
	iconPropRollup   = lucide(`<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/><path d="M11 8v6"/><path d="M8 11h6"/>`)
	iconPropFormula  = lucide(`<path d="M18 7V4H6l6 8-6 8h12v-3"/>`)
)

// Control Lucide icons.
var (
	iconFilter        = lucide(`<polygon points="22 3 2 3 10 12.46 10 19 14 21 14 12.46 22 3"/>`)
	iconSort          = lucide(`<path d="m3 16 4 4 4-4"/><path d="M7 20V4"/><path d="m21 8-4-4-4 4"/><path d="M17 4v16"/>`)
	iconGroup         = lucide(`<rect width="7" height="7" x="3" y="3" rx="1"/><path d="M14 4h7"/><path d="M14 9h7"/><rect width="7" height="7" x="3" y="14" rx="1"/><path d="M14 15h7"/><path d="M14 20h7"/>`)
	iconSearch        = lucide(`<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>`)
	iconSliders       = lucide(`<line x1="21" x2="14" y1="4" y2="4"/><line x1="10" x2="3" y1="4" y2="4"/><line x1="21" x2="12" y1="12" y2="12"/><line x1="8" x2="3" y1="12" y2="12"/><line x1="21" x2="16" y1="20" y2="20"/><line x1="12" x2="3" y1="20" y2="20"/><line x1="14" x2="14" y1="2" y2="6"/><line x1="8" x2="8" y1="10" y2="14"/><line x1="16" x2="16" y1="18" y2="22"/>`)
	iconPlus          = lucide(`<path d="M5 12h14"/><path d="M12 5v14"/>`)
	iconX             = lucide(`<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`)
	iconCheck         = lucide(`<path d="M20 6 9 17l-5-5"/>`)
	iconChevronDown   = lucide(`<path d="m6 9 6 6 6-6"/>`)
	iconChevronRight  = lucide(`<path d="m9 18 6-6-6-6"/>`)
	iconTrash         = lucide(`<path d="M10 11v6"/><path d="M14 11v6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/><path d="M3 6h18"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>`)
	iconGrip          = lucide(`<circle cx="9" cy="12" r="1"/><circle cx="9" cy="5" r="1"/><circle cx="9" cy="19" r="1"/><circle cx="15" cy="12" r="1"/><circle cx="15" cy="5" r="1"/><circle cx="15" cy="19" r="1"/>`)
	iconExternal      = lucide(`<path d="M15 3h6v6"/><path d="M10 14 21 3"/><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>`)
	iconUndo          = lucide(`<path d="M9 14 4 9l5-5"/><path d="M4 9h10.5a5.5 5.5 0 0 1 5.5 5.5a5.5 5.5 0 0 1-5.5 5.5H11"/>`)
	iconRedo          = lucide(`<path d="m15 14 5-5-5-5"/><path d="M20 9H9.5A5.5 5.5 0 0 0 4 14.5A5.5 5.5 0 0 0 9.5 20H13"/>`)
	iconEye           = lucide(`<path d="M2.062 12.348a1 1 0 0 1 0-.696 10.75 10.75 0 0 1 19.876 0 1 1 0 0 1 0 .696 10.75 10.75 0 0 1-19.876 0"/><circle cx="12" cy="12" r="3"/>`)
	iconEyeOff        = lucide(`<path d="M10.733 5.076a10.744 10.744 0 0 1 11.205 6.575 1 1 0 0 1 0 .696 10.747 10.747 0 0 1-1.444 2.49"/><path d="M14.084 14.158a3 3 0 0 1-4.242-4.242"/><path d="M17.479 17.499a10.75 10.75 0 0 1-15.417-5.151 1 1 0 0 1 0-.696 10.75 10.75 0 0 1 4.446-5.143"/><path d="m2 2 20 20"/>`)
	iconArrowUp       = lucide(`<path d="m5 12 7-7 7 7"/><path d="M12 19V5"/>`)
	iconArrowDown     = lucide(`<path d="M12 5v14"/><path d="m19 12-7 7-7-7"/>`)
	iconPanelRight    = lucide(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M15 3v18"/>`)
	iconFileDoc       = lucide(`<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/>`)
	iconLayout        = lucide(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M3 9h18"/><path d="M9 21V9"/>`)
	iconChevronLeft   = lucide(`<path d="m15 18-6-6 6-6"/>`)
	iconChevronUp     = lucide(`<path d="m18 15-6-6-6 6"/>`)
	iconChevronsLeft  = lucide(`<path d="m11 17-5-5 5-5"/><path d="m18 17-5-5 5-5"/>`)
	iconChevronsRight = lucide(`<path d="m6 17 5-5-5-5"/><path d="m13 17 5-5-5-5"/>`)
	iconPanelLeft     = lucide(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M9 3v18"/>`)
	iconClock         = lucide(`<circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>`)
	iconImage         = lucide(`<rect width="18" height="18" x="3" y="3" rx="2" ry="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21"/>`)
	iconMoreHoriz     = lucide(`<circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/><circle cx="5" cy="12" r="1"/>`)
	iconMoreVert      = lucide(`<circle cx="12" cy="12" r="1"/><circle cx="12" cy="5" r="1"/><circle cx="12" cy="19" r="1"/>`)
	iconLayers        = lucide(`<path d="m12.83 2.18a2 2 0 0 0-1.66 0L2.6 6.08a1 1 0 0 0 0 1.83l8.58 3.91a2 2 0 0 0 1.66 0l8.58-3.9a1 1 0 0 0 0-1.83Z"/><path d="m22 17.65-9.17 4.16a2 2 0 0 1-1.66 0L2 17.65"/><path d="m22 12.65-9.17 4.16a2 2 0 0 1-1.66 0L2 12.65"/>`)
	iconPalette       = lucide(`<circle cx="13.5" cy="6.5" r=".5" fill="currentColor"/><circle cx="17.5" cy="10.5" r=".5" fill="currentColor"/><circle cx="8.5" cy="7.5" r=".5" fill="currentColor"/><circle cx="6.5" cy="12.5" r=".5" fill="currentColor"/><path d="M12 2C6.5 2 2 6.5 2 12s4.5 10 10 10c.926 0 1.648-.746 1.648-1.688 0-.437-.18-.835-.437-1.125-.29-.289-.438-.652-.438-1.125a1.64 1.64 0 0 1 1.668-1.668h1.996c3.051 0 5.555-2.503 5.555-5.554C21.965 6.012 17.461 2 12 2z"/>`)
	iconSettings      = lucide(`<path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/>`)
	iconTemplate      = lucide(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M3 9h18"/><path d="M9 21V9"/><path d="M13 13h4"/><path d="M13 17h4"/>`)
)

// ViewIcon returns the Lucide icon for vt.
func ViewIcon(vt database.ViewType) *ui.SVG {
	if spec, ok := ViewSpecFor(vt); ok && spec.Icon != nil {
		return spec.Icon
	}
	return iconViewTable
}

// PropertyIcon returns the Lucide icon for pt.
func PropertyIcon(pt database.PropertyType) *ui.SVG {
	switch pt {
	case database.PropTitle:
		return iconPropTitle
	case database.PropNumber:
		return iconPropNumber
	case database.PropSelect:
		return iconPropSelect
	case database.PropMultiSelect:
		return iconPropMulti
	case database.PropStatus:
		return iconPropStatus
	case database.PropDate:
		return iconPropDate
	case database.PropCheckbox:
		return iconPropCheckbox
	case database.PropURL:
		return iconPropURL
	case database.PropEmail:
		return iconPropEmail
	case database.PropPhone:
		return iconPropPhone
	case database.PropFiles:
		return iconPropFiles
	case database.PropPerson:
		return iconPropPerson
	case database.PropCreatedTime, database.PropEditedTime:
		return iconPropTime
	case database.PropUniqueID:
		return iconPropUniqueID
	case database.PropRelation:
		return iconPropRelation
	case database.PropRollup:
		return iconPropRollup
	case database.PropFormula:
		return iconPropFormula
	default:
		return iconPropText
	}
}

func viewTypeLabel(vt database.ViewType) string {
	if spec, ok := ViewSpecFor(vt); ok {
		return spec.Label()
	}
	return string(vt)
}

func propertyTypeLabel(pt database.PropertyType) string {
	switch pt {
	case database.PropTitle:
		return i18n.T("标题")
	case database.PropText:
		return i18n.T("文本")
	case database.PropNumber:
		return i18n.T("数字")
	case database.PropSelect:
		return i18n.T("单选")
	case database.PropMultiSelect:
		return i18n.T("多选")
	case database.PropStatus:
		return i18n.T("状态")
	case database.PropDate:
		return i18n.T("日期")
	case database.PropCheckbox:
		return i18n.T("复选框")
	case database.PropURL:
		return i18n.T("链接")
	case database.PropEmail:
		return i18n.T("邮箱")
	case database.PropPhone:
		return i18n.T("电话")
	case database.PropFiles:
		return i18n.T("文件与媒体")
	case database.PropPerson:
		return i18n.T("人员")
	case database.PropCreatedTime:
		return i18n.T("创建时间")
	case database.PropEditedTime:
		return i18n.T("最后编辑时间")
	case database.PropUniqueID:
		return i18n.T("编号")
	case database.PropRelation:
		return i18n.T("关联")
	case database.PropRollup:
		return i18n.T("汇总")
	case database.PropFormula:
		return i18n.T("公式")
	default:
		return string(pt)
	}
}

func aggregationLabel(agg database.AggregationType) string {
	switch agg {
	case database.AggNone, "":
		return i18n.T("无计算")
	case database.AggCountAll:
		return i18n.T("总计行数")
	case database.AggCountValues:
		return i18n.T("值计数")
	case database.AggCountUnique:
		return i18n.T("唯一值数")
	case database.AggCountEmpty:
		return i18n.T("空值数")
	case database.AggCountNotEmpty:
		return i18n.T("非空数")
	case database.AggPercentEmpty:
		return i18n.T("空值占比")
	case database.AggPercentNotEmpty:
		return i18n.T("非空占比")
	case database.AggSum:
		return i18n.T("求和")
	case database.AggAverage:
		return i18n.T("平均值")
	case database.AggMedian:
		return i18n.T("中位数")
	case database.AggMin:
		return i18n.T("最小值")
	case database.AggMax:
		return i18n.T("最大值")
	case database.AggRange:
		return i18n.T("极差")
	case database.AggChecked:
		return i18n.T("已勾选")
	case database.AggUnchecked:
		return i18n.T("未勾选")
	case database.AggPercentChecked:
		return i18n.T("勾选占比")
	case database.AggPercentUnchecked:
		return i18n.T("未选占比")
	case database.AggEarliestDate:
		return i18n.T("最早日期")
	case database.AggLatestDate:
		return i18n.T("最晚日期")
	case database.AggDateRange:
		return i18n.T("日期跨度")
	case database.AggShowOriginal:
		return i18n.T("显示原始值")
	case database.AggShowUnique:
		return i18n.T("显示唯一值")
	default:
		return string(agg)
	}
}
