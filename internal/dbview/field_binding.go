package dbview

import (
	"strings"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

func isDateLikePropType(pt database.PropertyType) bool {
	return pt == database.PropDate || pt == database.PropCreatedTime || pt == database.PropEditedTime
}

func isColorablePropType(pt database.PropertyType) bool {
	return pt == database.PropStatus || pt == database.PropSelect
}

func isAssigneePropType(pt database.PropertyType) bool {
	return pt == database.PropPerson || pt == database.PropMultiSelect || pt == database.PropSelect
}

func virtualDateProp(bindID string) database.Property {
	if bindID == database.PropBindCreatedTime {
		return database.Property{
			ID:   database.PropBindCreatedTime,
			Key:  database.PropBindCreatedTime,
			Name: i18n.T("创建时间"),
			Type: database.PropCreatedTime,
		}
	}
	return database.Property{
		ID:   database.PropBindEditedTime,
		Key:  database.PropBindEditedTime,
		Name: i18n.T("修改时间"),
		Type: database.PropEditedTime,
	}
}

// resolveDateBinding resolves a view's date property binding. When no explicit or
// schema date property exists, it falls back cleanly to each record's built-in
// modified time (PropBindEditedTime) so views remain usable instead of blocking.
func resolveDateBinding(db *database.Database, propID string) (prop database.Property, isBuiltin bool, explicitBuiltin bool) {
	switch propID {
	case database.PropBindEditedTime, database.PropBindCreatedTime:
		return virtualDateProp(propID), true, true
	}
	if propID != "" && db != nil {
		if p, idx := db.PropertyByID(propID); idx >= 0 && isDateLikePropType(p.Type) {
			return *p, p.Type != database.PropDate, true
		}
	}
	if dp := defaultDateProp(db); dp != nil {
		return *dp, dp.Type != database.PropDate, false
	}
	return virtualDateProp(database.PropBindEditedTime), true, false
}

func colorablePropertiesOf(db *database.Database) []database.Property {
	if db == nil {
		return nil
	}
	var out []database.Property
	for _, p := range db.Properties {
		if isColorablePropType(p.Type) {
			out = append(out, p)
		}
	}
	return out
}

func defaultColorPropID(db *database.Database) string {
	if props := colorablePropertiesOf(db); len(props) > 0 {
		for _, p := range props {
			if p.Type == database.PropStatus {
				return p.ID
			}
		}
		return props[0].ID
	}
	return ""
}

func resolveColorProp(db *database.Database, propID string) *database.Property {
	if propID == database.PropBindNone || db == nil {
		return nil
	}
	if propID != "" {
		if p, idx := db.PropertyByID(propID); idx >= 0 && isColorablePropType(p.Type) {
			return p
		}
	}
	for i := range db.Properties {
		if db.Properties[i].Type == database.PropStatus {
			return &db.Properties[i]
		}
	}
	for i := range db.Properties {
		if db.Properties[i].Type == database.PropSelect {
			return &db.Properties[i]
		}
	}
	return nil
}

func assigneePropertiesOf(db *database.Database) []database.Property {
	if db == nil {
		return nil
	}
	var out []database.Property
	for _, p := range db.Properties {
		if isAssigneePropType(p.Type) {
			out = append(out, p)
		}
	}
	return out
}

func isLikelyAssigneeName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return strings.Contains(lower, "负责") ||
		strings.Contains(lower, "成员") ||
		strings.Contains(lower, "人员") ||
		strings.Contains(lower, "assign") ||
		strings.Contains(lower, "owner") ||
		strings.Contains(lower, "member")
}

func resolveBoardAssigneeProp(db *database.Database, bcfg database.BoardViewConfig) *database.Property {
	if bcfg.AssigneePropID == database.PropBindNone || db == nil {
		return nil
	}
	if bcfg.AssigneePropID != "" {
		if p, idx := db.PropertyByID(bcfg.AssigneePropID); idx >= 0 && isAssigneePropType(p.Type) {
			return p
		}
	}
	for i := range db.Properties {
		if db.Properties[i].Type == database.PropPerson {
			return &db.Properties[i]
		}
	}
	for i := range db.Properties {
		p := db.Properties[i]
		if (p.Type == database.PropMultiSelect || p.Type == database.PropSelect) && isLikelyAssigneeName(p.Name) {
			return &db.Properties[i]
		}
	}
	return nil
}

func defaultBoardAssigneePropID(db *database.Database) string {
	if p := resolveBoardAssigneeProp(db, database.BoardViewConfig{}); p != nil {
		return p.ID
	}
	return ""
}

func extractAssigneeNames(rec *database.Record, prop *database.Property) []string {
	if rec == nil || prop == nil {
		return nil
	}
	val := rec.ValueFor(*prop)
	if val.IsEmpty() {
		return nil
	}
	var out []string
	if len(val.List) > 0 {
		for _, item := range val.List {
			if n := strings.TrimSpace(item); n != "" {
				out = append(out, n)
			}
		}
		return out
	}
	if n := strings.TrimSpace(val.Text); n != "" {
		out = append(out, n)
	}
	return out
}

func resolveBoardPriorityProp(db *database.Database, bcfg database.BoardViewConfig, cardProps []database.Property) *database.Property {
	if bcfg.PriorityPropID == database.PropBindNone || db == nil {
		return nil
	}
	if bcfg.PriorityPropID != "" {
		if p, idx := db.PropertyByID(bcfg.PriorityPropID); idx >= 0 && isColorablePropType(p.Type) {
			return p
		}
	}
	for i := range cardProps {
		if isPriorityProperty(cardProps[i]) {
			cp := cardProps[i]
			return &cp
		}
	}
	return nil
}

func defaultBoardPriorityPropID(db *database.Database) string {
	if db == nil {
		return ""
	}
	for _, p := range db.Properties {
		if isPriorityProperty(p) {
			return p.ID
		}
	}
	return ""
}

func resolveBoardDateProp(db *database.Database, bcfg database.BoardViewConfig, cardProps []database.Property) *database.Property {
	if bcfg.DatePropID == database.PropBindNone || db == nil {
		return nil
	}
	if bcfg.DatePropID == database.PropBindEditedTime || bcfg.DatePropID == database.PropBindCreatedTime {
		vp := virtualDateProp(bcfg.DatePropID)
		return &vp
	}
	if bcfg.DatePropID != "" {
		if p, idx := db.PropertyByID(bcfg.DatePropID); idx >= 0 && isDateLikePropType(p.Type) {
			return p
		}
	}
	for i := range cardProps {
		if cardProps[i].Type == database.PropDate {
			cp := cardProps[i]
			return &cp
		}
	}
	return nil
}

func resolveFeedStatusProp(db *database.Database, fcfg database.FeedViewConfig) *database.Property {
	return resolveColorProp(db, fcfg.StatusPropID)
}

func resolveFeedDateProp(db *database.Database, fcfg database.FeedViewConfig) *database.Property {
	if fcfg.DatePropID == database.PropBindNone || db == nil {
		return nil
	}
	if fcfg.DatePropID == database.PropBindEditedTime || fcfg.DatePropID == database.PropBindCreatedTime {
		vp := virtualDateProp(fcfg.DatePropID)
		return &vp
	}
	if fcfg.DatePropID != "" {
		if p, idx := db.PropertyByID(fcfg.DatePropID); idx >= 0 && isDateLikePropType(p.Type) {
			return p
		}
	}
	for i := range db.Properties {
		if db.Properties[i].Type == database.PropDate {
			return &db.Properties[i]
		}
	}
	return nil
}

func makeViewPropVisible(vc *database.ViewConfig, db *database.Database, propID string) {
	if vc == nil || propID == "" {
		return
	}
	for i := range vc.Properties {
		if vc.Properties[i].PropertyID == propID {
			vc.Properties[i].Visible = true
			return
		}
	}
	if db != nil {
		if p, idx := db.PropertyByID(propID); idx >= 0 {
			vc.Properties = append(vc.Properties, database.ViewProperty{
				PropertyID: p.ID,
				Visible:    true,
				Width:      p.Width,
				Wrap:       p.Wrap,
			})
		}
	}
}

func addDefaultDateProperty(st *State, db *database.Database, view database.ViewConfig, opts Options) {
	if st == nil || st.Store == nil || db == nil || opts.ReadOnly {
		return
	}
	key := uniquePropKey(db, "date")
	p := database.Property{
		ID:   "prop_" + database.NewID()[:8],
		Key:  key,
		Name: i18n.T("排期"),
		Type: database.PropDate,
		Date: &database.DateConfig{EndKey: key + "_end"},
	}
	if err := st.Store.AddProperty(p); err != nil {
		if opts.OnNotify != nil {
			opts.OnNotify(err.Error())
		}
		return
	}
	vc := view.Clone()
	switch vc.Type {
	case database.ViewCalendar:
		ensureCalendarConfig(&vc, db).DatePropID = p.ID
	case database.ViewTimeline:
		ensureTimelineConfig(&vc, db).StartPropID = p.ID
	case database.ViewBoard:
		ensureBoardConfig(&vc).DatePropID = p.ID
	case database.ViewFeed:
		ensureFeedConfig(&vc).DatePropID = p.ID
	}
	makeViewPropVisible(&vc, st.Store.SchemaSnapshot(), p.ID)
	saveViewAndNotify(st, vc, opts)
}

func addDefaultStatusProperty(st *State, db *database.Database, view database.ViewConfig, opts Options) {
	if st == nil || st.Store == nil || db == nil || opts.ReadOnly {
		return
	}
	key := uniquePropKey(db, "status")
	p := database.Property{
		ID:   "prop_" + database.NewID()[:8],
		Key:  key,
		Name: i18n.T("状态"),
		Type: database.PropStatus,
		Options: []database.SelectOption{
			{ID: "opt_" + database.NewID()[:6], Name: i18n.T("待处理"), Color: "gray", Group: database.StatusTodo},
			{ID: "opt_" + database.NewID()[:6], Name: i18n.T("进行中"), Color: "blue", Group: database.StatusInProgress},
			{ID: "opt_" + database.NewID()[:6], Name: i18n.T("已完成"), Color: "green", Group: database.StatusComplete},
		},
	}
	if err := st.Store.AddProperty(p); err != nil {
		if opts.OnNotify != nil {
			opts.OnNotify(err.Error())
		}
		return
	}
	vc := view.Clone()
	switch vc.Type {
	case database.ViewBoard:
		vc.Group = &database.GroupConfig{PropertyID: p.ID, StatusBy: "option"}
	case database.ViewCalendar:
		ensureCalendarConfig(&vc, db).ColorPropID = p.ID
	case database.ViewTimeline:
		ensureTimelineConfig(&vc, db).ColorPropID = p.ID
	case database.ViewFeed:
		ensureFeedConfig(&vc).StatusPropID = p.ID
	}
	makeViewPropVisible(&vc, st.Store.SchemaSnapshot(), p.ID)
	saveViewAndNotify(st, vc, opts)
}

func renderFieldBindingPill(c *ui.Context, key, slotLabel, valueLabel string, muted bool, pal Palette) ui.Element {
	btn := ui.Row(c.Key(key)).Height(26).Padding(0, 8).Radius(6).Gap(4).
		Background(pal.Paper).Border(1, pal.Border).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Role(ui.RoleMenuButton).
		Label(slotLabel + ": " + valueLabel)
	if btn.Hovered() {
		btn.Background(pal.RowHover)
	}
	valCol := pal.Text
	if muted {
		valCol = pal.TextMuted
	}
	btn.Children(func() {
		ui.Text(c, slotLabel+":").FontSize(11.5).TextColor(pal.TextMuted).NoWrap()
		ui.Text(c, valueLabel).FontSize(12).FontWeight(500).TextColor(valCol).SingleLine()
		ui.Icon(c, iconChevronDown).FontSize(11).TextColor(pal.TextMuted)
	})
	return btn
}

func renderFallbackBindingBanner(c *ui.Context, key, message, actionLabel string, pal Palette, onAction func()) {
	ui.Row(c.Key(key)).Height(32).Padding(0, 14).Gap(8).AlignItems(ui.Center).
		Background(pal.AccentSoft.Alpha(0.5)).BorderWidth(0, 0, 1, 0).BorderColor(pal.Border).Children(func() {
		ui.Icon(c, iconClock).FontSize(12.5).TextColor(pal.Accent)
		ui.Text(c, message).FontSize(12).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)
		if actionLabel != "" && onAction != nil {
			act := ui.Row(c.Key(key+"-act")).Height(22).Padding(0, 8).Radius(5).
				Background(pal.Paper).Border(1, pal.Accent.Alpha(0.35)).AlignItems(ui.Center).
				Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(actionLabel)
			if act.Hovered() {
				act.Background(pal.AccentSoft)
			}
			act.Children(func() {
				ui.Text(c, actionLabel).FontSize(11.5).FontWeight(500).TextColor(pal.Accent).NoWrap()
			})
			if act.Clicked() {
				onAction()
			}
		}
	})
}

func appendDatePropMenuItems(
	m *ui.Menu,
	db *database.Database,
	curPropID string,
	allowNone bool,
	readOnly bool,
	onSelect func(propID string),
	onAddNew func(),
) {
	if allowNone {
		if m.Item(i18n.T("关闭")).Checked(curPropID == database.PropBindNone).Chosen() {
			onSelect(database.PropBindNone)
		}
		m.Separator()
	}
	dateProps := datePropertiesOf(db)
	for _, p := range dateProps {
		cand := p
		if m.Item(cand.Name).Checked(cand.ID == curPropID).Chosen() {
			onSelect(cand.ID)
		}
	}
	if len(dateProps) > 0 {
		m.Separator()
	}
	if m.Item(i18n.T("修改时间")).Checked(curPropID == database.PropBindEditedTime).Chosen() {
		onSelect(database.PropBindEditedTime)
	}
	if m.Item(i18n.T("创建时间")).Checked(curPropID == database.PropBindCreatedTime).Chosen() {
		onSelect(database.PropBindCreatedTime)
	}
	if !readOnly && onAddNew != nil {
		m.Separator()
		if m.Item("+ " + i18n.T("添加「排期」字段")).Chosen() {
			onAddNew()
		}
	}
}

func appendColorPropMenuItems(
	m *ui.Menu,
	db *database.Database,
	curProp *database.Property,
	explicitID string,
	noneLabel string,
	readOnly bool,
	onSelect func(propID string),
	onAddNew func(),
) {
	isNone := explicitID == database.PropBindNone || (curProp == nil && explicitID == "")
	if m.Item(noneLabel).Checked(isNone).Chosen() {
		onSelect(database.PropBindNone)
	}
	props := colorablePropertiesOf(db)
	if len(props) > 0 {
		m.Separator()
		for _, p := range props {
			cand := p
			checked := curProp != nil && curProp.ID == cand.ID && explicitID != database.PropBindNone
			if m.Item(cand.Name).Checked(checked).Chosen() {
				onSelect(cand.ID)
			}
		}
	}
	if !readOnly && onAddNew != nil {
		m.Separator()
		if m.Item("+ " + i18n.T("新建状态列")).Chosen() {
			onAddNew()
		}
	}
}
