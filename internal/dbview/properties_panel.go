package dbview

import (
	"fmt"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// PagePropertiesView renders a collapsible Notion-style 2-column property sheet
// for one database record inside Side/Center Peek or at the top of a full-page
// Markdown document belonging to a database.
func PagePropertiesView(
	c *ui.Context,
	store *database.Store,
	recordID string,
	st *State,
	collapsed *bool,
	opts Options,
) {
	if store == nil || st == nil {
		return
	}
	db := store.SchemaSnapshot()
	rec := store.Record(recordID)
	if db == nil || rec == nil {
		return
	}
	pal := paletteOf(c.Theme())

	var editableProps []database.Property
	for _, p := range db.Properties {
		if p.Type == database.PropTitle {
			continue
		}
		editableProps = append(editableProps, p)
	}

	isCollapsed := collapsed != nil && *collapsed

	ui.Column(c.Key("page-props:"+recordID)).Padding(8, 16, 4, 16).Gap(4).Children(func() {
		head := ui.Row(c.Key("props-toggle")).Height(28).Padding(0, 6).Radius(6).Gap(6).
			AlignItems(ui.Center).Cursor(ui.CursorPointer).Role(ui.RoleButton).Expanded(!isCollapsed).
			Label(i18n.T("页面属性"))
		if head.Hovered() {
			head.Background(pal.RowHover)
		}
		head.Children(func() {
			chev := iconChevronDown
			if isCollapsed {
				chev = iconChevronRight
			}
			ui.Icon(c, chev).FontSize(13).TextColor(pal.TextMuted)
			ui.Text(c, i18n.T("属性")).FontSize(12.5).FontWeight(600).TextColor(pal.TextMuted)
			ui.Text(c, fmt.Sprintf("%d", len(editableProps))).FontSize(11.5).TextColor(pal.TextMuted)
		})
		if head.Clicked() && collapsed != nil {
			*collapsed = !*collapsed
		}

		if isCollapsed {
			return
		}

		ui.Column(c.Key("props-rows")).Gap(2).Padding(2, 0, 6, 0).Children(func() {
			for _, prop := range editableProps {
				ui.Row(c.Key("prop-row:" + prop.ID)).MinHeight(32).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Row(c).Width(128).Shrink(0).Gap(6).Padding(4, 6).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, PropertyIcon(prop.Type)).FontSize(13).TextColor(pal.TextMuted)
						ui.Text(c, prop.Name).FontSize(12.5).TextColor(pal.TextMuted).SingleLine().Grow(1).MinWidth(0)
					})
					cellView(c, st, rec, prop, false, true, true, opts, pal)
				})
			}

			if !opts.ReadOnly {
				addBtn := ui.Row(c.Key("props-add")).Height(28).Padding(0, 6).Radius(6).Gap(6).
					AlignItems(ui.Center).Cursor(ui.CursorPointer)
				if addBtn.Hovered() {
					addBtn.Background(pal.RowHover)
				}
				addBtn.Children(func() {
					ui.Icon(c, iconPlus).FontSize(13).TextColor(pal.TextMuted)
					ui.Text(c, i18n.T("添加属性")).FontSize(12.5).TextColor(pal.TextMuted)
				})
				if addBtn.Clicked() {
					st.AddPropOpen = !st.AddPropOpen
					st.NewPropName = ""
				}
				if st.AddPropOpen {
					addPropertyPopover(c, addBtn, st, db, opts, pal)
				}
			}
		})
		ui.Box(c).Height(1).Background(pal.Border)
	})
}
