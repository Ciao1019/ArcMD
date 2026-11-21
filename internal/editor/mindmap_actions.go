package editor

import (
	"strings"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/mindmap"
	"github.com/egoist/mygo/ui"
)

type mapBreadcrumb struct {
	ID   string
	Text string
}

func (m *MindMap) setFocusRoot(id string) {
	if id == m.title.ID {
		id = ""
	}
	if m.focusRoot == id {
		return
	}
	m.focusRoot = id
	m.layoutRevision++
	m.autoFit = true
	m.fitRequested = true
	m.locatePending = true
}

func (m *MindMap) setHeadingsOnly(on bool) {
	if m.headingsOnly == on {
		return
	}
	m.headingsOnly = on
	m.layoutRevision++
	m.autoFit = true
	m.fitRequested = true
	m.locatePending = true
}

// viewTree returns the active root, flattened items and breadcrumb trail for
// the current view (either the full map or a focused branch subtree).
func (m *MindMap) viewTree() (mindmap.Item, []mindmap.Item, []mapBreadcrumb) {
	if m.focusRoot == "" || m.focusRoot == m.title.ID {
		return m.title, m.items, nil
	}
	idx := -1
	for i, it := range m.items {
		if it.ID == m.focusRoot {
			idx = i
			break
		}
	}
	if idx < 0 {
		m.focusRoot = ""
		return m.title, m.items, nil
	}

	root := m.items[idx]
	baseDepth := root.Depth
	root.Depth = 0

	var sub []mindmap.Item
	for j := idx + 1; j < len(m.items); j++ {
		if m.items[j].Depth <= baseDepth {
			break
		}
		it := m.items[j]
		it.Depth -= baseDepth + 1
		sub = append(sub, it)
	}

	// Build ancestor chain from the document root down to the focused branch.
	var ancestors []mapBreadcrumb
	wantDepth := baseDepth
	for j := idx - 1; j >= 0 && wantDepth > 0; j-- {
		if m.items[j].Depth < wantDepth {
			ancestors = append(ancestors, mapBreadcrumb{ID: m.items[j].ID, Text: m.items[j].Text})
			wantDepth = m.items[j].Depth
		}
	}
	crumbs := make([]mapBreadcrumb, 0, len(ancestors)+2)
	crumbs = append(crumbs, mapBreadcrumb{ID: "", Text: m.title.Text})
	for i := len(ancestors) - 1; i >= 0; i-- {
		crumbs = append(crumbs, ancestors[i])
	}
	crumbs = append(crumbs, mapBreadcrumb{ID: root.ID, Text: root.Text})
	return root, sub, crumbs
}

// focusBreadcrumbs draws the floating Arc-style breadcrumb bar while focused
// on a branch so the user can step up or return to the full document.
func (m *MindMap) focusBreadcrumbs(c *ui.Context, t *ui.Theme, crumbs []mapBreadcrumb) {
	bg := t.Background.Alpha(.92)
	bar := ui.Row(c.Key("map-breadcrumbs")).Absolute().Left(10).Top(10).Height(28).Padding(0, 6, 0, 8).Gap(2).
		AlignItems(ui.Center).Background(bg).Border(1, t.Border.Alpha(.75)).Radius(8).
		Shadow(0, 2, 8, 0, t.Text.Alpha(.08))
	bar.Children(func() {
		for i, cr := range crumbs {
			if i > 0 {
				ui.Text(c, "›").FontSize(11).TextColor(t.TextMuted.Alpha(.7)).NoWrap()
			}
			label := cr.Text
			if rs := []rune(strings.TrimSpace(label)); len(rs) > 18 {
				label = string(rs[:17]) + "…"
			}
			last := i == len(crumbs)-1
			if last {
				ui.Text(c, label).FontSize(11.5).FontWeight(600).TextColor(t.Text).NoWrap()
				continue
			}
			b := ui.ButtonBase(c.Key("crumb:" + cr.ID)).Height(22).PaddingX(4).Radius(5).Center().
				Label(label).Tooltip(cr.Text).KeepFocus().Cursor(ui.CursorPointer)
			if b.Hovered() {
				b.Background(t.Text.Alpha(.06))
			}
			b.Children(func() {
				ui.Text(c, label).FontSize(11.5).TextColor(t.TextMuted).NoWrap()
			})
			if b.Clicked() {
				m.setFocusRoot(cr.ID)
				c.After(0)
			}
		}
		exitBtn := ui.ButtonBase(c.Key("crumb-exit")).Size(20, 20).Radius(5).Center().
			Label(i18n.T("退出聚焦")).Tooltip(i18n.T("退出聚焦 (Esc)")).KeepFocus().Cursor(ui.CursorPointer)
		if exitBtn.Hovered() {
			exitBtn.Background(t.Text.Alpha(.08))
		}
		exitBtn.Children(func() {
			ui.Icon(c, iconClose).FontSize(12).TextColor(t.TextMuted)
		})
		if exitBtn.Clicked() {
			m.setFocusRoot("")
			c.After(0)
		}
	})
}

func blockIndexByID(blocks []*document.Block, id string) int {
	for i, b := range blocks {
		if b.ID == id {
			return i
		}
	}
	return -1
}

// branchSpan returns the contiguous [start, end) range in e.Doc.Blocks that
// belongs to nodeID and all of its descendants in the mind map.
func (m *MindMap) branchSpan(e *Editor, nodeID string) (start, end int, ok bool) {
	if e == nil || e.Doc == nil || len(e.Doc.Blocks) == 0 || nodeID == "" {
		return 0, 0, false
	}
	if nodeID == m.title.ID {
		if nodeID == "__map_root__" {
			return 0, len(e.Doc.Blocks), true
		}
		idx := blockIndexByID(e.Doc.Blocks, nodeID)
		if idx < 0 {
			return 0, 0, false
		}
		return idx, len(e.Doc.Blocks), true
	}
	itemIdx := -1
	for i, it := range m.items {
		if it.ID == nodeID {
			itemIdx = i
			break
		}
	}
	if itemIdx < 0 {
		return 0, 0, false
	}
	start = blockIndexByID(e.Doc.Blocks, nodeID)
	if start < 0 {
		return 0, 0, false
	}
	baseDepth := m.items[itemIdx].Depth
	end = len(e.Doc.Blocks)
	for j := itemIdx + 1; j < len(m.items); j++ {
		if m.items[j].Depth <= baseDepth {
			if next := blockIndexByID(e.Doc.Blocks, m.items[j].ID); next > start {
				end = next
			}
			break
		}
	}
	return start, end, true
}

// nodeHasChildInFullMap reports whether nodeID has at least one child node in
// the full mind map (independent of current fold state).
func (m *MindMap) nodeHasChildInFullMap(nodeID string) bool {
	if nodeID == m.title.ID {
		return len(m.items) > 0
	}
	for i, it := range m.items {
		if it.ID == nodeID {
			return i+1 < len(m.items) && m.items[i+1].Depth > it.Depth
		}
	}
	return false
}

func (m *MindMap) nodeTitle(e *Editor, nodeID string) string {
	if nodeID == m.title.ID {
		return strings.TrimSpace(m.title.Text)
	}
	if e != nil && e.Doc != nil {
		if idx := blockIndexByID(e.Doc.Blocks, nodeID); idx >= 0 {
			if text := strings.TrimSpace(e.Doc.Blocks[idx].Text()); text != "" {
				return text
			}
		}
	}
	for _, it := range m.items {
		if it.ID == nodeID {
			return strings.TrimSpace(it.Text)
		}
	}
	return i18n.T("未命名")
}

// branchMarkdown serializes the document blocks of nodeID's branch to clean
// Markdown. When normalizeHeadings is true (for extracting to a standalone
// note), heading levels are shifted so the branch root starts at H1.
func (m *MindMap) branchMarkdown(e *Editor, nodeID string, normalizeHeadings bool) string {
	start, end, ok := m.branchSpan(e, nodeID)
	if !ok {
		return ""
	}
	clones := make([]*document.Block, 0, end-start)
	headingShift := 0
	if normalizeHeadings && e.Doc.Blocks[start].Kind == "heading" && e.Doc.Blocks[start].Level > 1 {
		headingShift = e.Doc.Blocks[start].Level - 1
	}
	for i := start; i < end; i++ {
		c := e.Doc.Blocks[i].Clone()
		if headingShift > 0 && c.Kind == "heading" {
			c.Level = max(1, c.Level-headingShift)
		}
		clones = append(clones, c)
	}
	return strings.TrimSpace(ExportMarkdown(&document.Document{Blocks: clones}))
}

// branchPlainOutline exports nodeID's subtree as an indented plain-text outline.
func (m *MindMap) branchPlainOutline(nodeID string) string {
	var b strings.Builder
	if nodeID == m.title.ID {
		b.WriteString(strings.TrimSpace(m.title.Text))
		for _, it := range m.items {
			b.WriteByte('\n')
			b.WriteString(strings.Repeat("  ", it.Depth))
			b.WriteString("- ")
			b.WriteString(strings.TrimSpace(strings.ReplaceAll(it.Text, "\n", " ")))
		}
		return b.String()
	}
	idx := -1
	for i, it := range m.items {
		if it.ID == nodeID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ""
	}
	baseDepth := m.items[idx].Depth
	b.WriteString(strings.TrimSpace(strings.ReplaceAll(m.items[idx].Text, "\n", " ")))
	for j := idx + 1; j < len(m.items); j++ {
		if m.items[j].Depth <= baseDepth {
			break
		}
		b.WriteByte('\n')
		b.WriteString(strings.Repeat("  ", m.items[j].Depth-baseDepth-1))
		b.WriteString("- ")
		b.WriteString(strings.TrimSpace(strings.ReplaceAll(m.items[j].Text, "\n", " ")))
	}
	return b.String()
}

// siblingNodeID returns the previous (dir < 0) or next (dir > 0) sibling of
// nodeID at the same depth under the same parent in m.items.
func (m *MindMap) siblingNodeID(nodeID string, dir int) string {
	if nodeID == "" || nodeID == m.title.ID {
		return ""
	}
	idx := -1
	for i, it := range m.items {
		if it.ID == nodeID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ""
	}
	depth := m.items[idx].Depth
	if dir < 0 {
		for j := idx - 1; j >= 0; j-- {
			if m.items[j].Depth < depth {
				return ""
			}
			if m.items[j].Depth == depth {
				return m.items[j].ID
			}
		}
		return ""
	}
	for j := idx + 1; j < len(m.items); j++ {
		if m.items[j].Depth < depth {
			return ""
		}
		if m.items[j].Depth == depth {
			return m.items[j].ID
		}
	}
	return ""
}

func (m *MindMap) moveBranch(e *Editor, nodeID string, dir int) bool {
	if e == nil || e.ReadOnly {
		return false
	}
	sibID := m.siblingNodeID(nodeID, dir)
	if sibID == "" {
		return false
	}
	a0, a1, okA := m.branchSpan(e, nodeID)
	b0, b1, okB := m.branchSpan(e, sibID)
	if !okA || !okB {
		return false
	}
	if dir < 0 {
		// Swap [b0, b1) before [a0, a1) -> [a0, a1) then [b0, b1).
		if b1 > a0 {
			return false
		}
		e.Engine.Edit(func() {
			blocks := e.Doc.Blocks
			before := append([]*document.Block(nil), blocks[:b0]...)
			sib := append([]*document.Block(nil), blocks[b0:b1]...)
			mid := append([]*document.Block(nil), blocks[b1:a0]...)
			cur := append([]*document.Block(nil), blocks[a0:a1]...)
			after := append([]*document.Block(nil), blocks[a1:]...)
			next := make([]*document.Block, 0, len(blocks))
			next = append(next, before...)
			next = append(next, cur...)
			next = append(next, mid...)
			next = append(next, sib...)
			next = append(next, after...)
			e.Doc.Blocks = next
			e.Engine.FocusBlock(nodeID)
		})
	} else {
		// Swap [a0, a1) before [b0, b1) -> [b0, b1) then [a0, a1).
		if a1 > b0 {
			return false
		}
		e.Engine.Edit(func() {
			blocks := e.Doc.Blocks
			before := append([]*document.Block(nil), blocks[:a0]...)
			cur := append([]*document.Block(nil), blocks[a0:a1]...)
			mid := append([]*document.Block(nil), blocks[a1:b0]...)
			sib := append([]*document.Block(nil), blocks[b0:b1]...)
			after := append([]*document.Block(nil), blocks[b1:]...)
			next := make([]*document.Block, 0, len(blocks))
			next = append(next, before...)
			next = append(next, sib...)
			next = append(next, mid...)
			next = append(next, cur...)
			next = append(next, after...)
			e.Doc.Blocks = next
			e.Engine.FocusBlock(nodeID)
		})
	}
	e.sync()
	m.selected = nodeID
	m.locatePending = true
	return true
}

func (m *MindMap) addChildNode(e *Editor, nodeID, mode string) bool {
	if e == nil || e.ReadOnly {
		return false
	}
	start, end, ok := m.branchSpan(e, nodeID)
	if !ok {
		return false
	}
	var newBlk *document.Block
	if nodeID == "__map_root__" {
		if mode == "todo" {
			newBlk = &document.Block{ID: document.NewID(), Kind: "todo", Runs: []document.Run{{Text: ""}}}
		} else {
			newBlk = &document.Block{ID: document.NewID(), Kind: "heading", Level: 2, Runs: []document.Run{{Text: ""}}}
		}
	} else {
		parent := e.Doc.Blocks[start]
		switch {
		case mode == "todo":
			indent := 0
			if isList(parent.Kind) {
				indent = parent.Indent + 1
			}
			newBlk = &document.Block{ID: document.NewID(), Kind: "todo", Indent: indent, Runs: []document.Run{{Text: ""}}}
		case parent.Kind == "heading":
			if parent.Level < 6 {
				newBlk = &document.Block{ID: document.NewID(), Kind: "heading", Level: parent.Level + 1, Runs: []document.Run{{Text: ""}}}
			} else {
				newBlk = &document.Block{ID: document.NewID(), Kind: "bullet", Runs: []document.Run{{Text: ""}}}
			}
		case isList(parent.Kind):
			newBlk = &document.Block{ID: document.NewID(), Kind: parent.Kind, Indent: parent.Indent + 1, Runs: []document.Run{{Text: ""}}}
		default:
			newBlk = &document.Block{ID: document.NewID(), Kind: "bullet", Indent: parent.Indent, Runs: []document.Run{{Text: ""}}}
		}
	}
	if m.headingsOnly && newBlk.Kind != "heading" {
		m.headingsOnly = false
	}
	m.tree.SetFolded(nodeID, false)
	e.Engine.Edit(func() {
		blocks := e.Doc.Blocks
		next := make([]*document.Block, 0, len(blocks)+1)
		next = append(next, blocks[:end]...)
		next = append(next, newBlk)
		next = append(next, blocks[end:]...)
		e.Doc.Blocks = next
		e.Engine.FocusBlock(newBlk.ID)
	})
	e.sync()
	m.selected = newBlk.ID
	m.locatePending = true
	e.RevealBlock(newBlk.ID)
	return true
}

func (m *MindMap) addSiblingNode(e *Editor, nodeID string) bool {
	if e == nil || e.ReadOnly || nodeID == "" || nodeID == m.title.ID {
		return false
	}
	start, end, ok := m.branchSpan(e, nodeID)
	if !ok {
		return false
	}
	cur := e.Doc.Blocks[start]
	var newBlk *document.Block
	switch {
	case cur.Kind == "heading":
		newBlk = &document.Block{ID: document.NewID(), Kind: "heading", Level: cur.Level, Runs: []document.Run{{Text: ""}}}
	case isList(cur.Kind):
		newBlk = &document.Block{ID: document.NewID(), Kind: cur.Kind, Indent: cur.Indent, Runs: []document.Run{{Text: ""}}}
	default:
		newBlk = &document.Block{ID: document.NewID(), Kind: "paragraph", Indent: cur.Indent, Runs: []document.Run{{Text: ""}}}
	}
	e.Engine.Edit(func() {
		blocks := e.Doc.Blocks
		next := make([]*document.Block, 0, len(blocks)+1)
		next = append(next, blocks[:end]...)
		next = append(next, newBlk)
		next = append(next, blocks[end:]...)
		e.Doc.Blocks = next
		e.Engine.FocusBlock(newBlk.ID)
	})
	e.sync()
	m.selected = newBlk.ID
	m.locatePending = true
	e.RevealBlock(newBlk.ID)
	return true
}

func (m *MindMap) deleteBranch(e *Editor, nodeID string) bool {
	if e == nil || e.ReadOnly || nodeID == "" || nodeID == m.title.ID {
		return false
	}
	start, end, ok := m.branchSpan(e, nodeID)
	if !ok {
		return false
	}
	e.Engine.Edit(func() {
		blocks := e.Doc.Blocks
		next := make([]*document.Block, 0, max(1, len(blocks)-(end-start)))
		next = append(next, blocks[:start]...)
		next = append(next, blocks[end:]...)
		if len(next) == 0 {
			next = []*document.Block{document.NewBlock("paragraph", "")}
		}
		e.Doc.Blocks = next
		focusIdx := min(start, len(next)-1)
		e.Engine.FocusBlock(next[focusIdx].ID)
	})
	if m.focusRoot == nodeID {
		m.focusRoot = ""
	}
	e.sync()
	m.locatePending = true
	return true
}

func (m *MindMap) deleteOnlyNode(e *Editor, nodeID string) bool {
	if e == nil || e.ReadOnly || nodeID == "" || nodeID == m.title.ID {
		return false
	}
	start, end, ok := m.branchSpan(e, nodeID)
	if !ok {
		return false
	}
	removed := e.Doc.Blocks[start]
	e.Engine.Edit(func() {
		blocks := e.Doc.Blocks
		for i := start + 1; i < end; i++ {
			b := blocks[i]
			switch {
			case removed.Kind == "heading" && b.Kind == "heading" && b.Level > 1:
				b.Level--
			case isList(removed.Kind) && b.Indent > 0:
				b.Indent--
			}
		}
		next := make([]*document.Block, 0, max(1, len(blocks)-1))
		next = append(next, blocks[:start]...)
		next = append(next, blocks[start+1:]...)
		if len(next) == 0 {
			next = []*document.Block{document.NewBlock("paragraph", "")}
		}
		e.Doc.Blocks = next
		focusIdx := min(start, len(next)-1)
		e.Engine.FocusBlock(next[focusIdx].ID)
	})
	if m.focusRoot == nodeID {
		m.focusRoot = ""
	}
	e.sync()
	m.locatePending = true
	return true
}

func (m *MindMap) extractBranch(e *Editor, nodeID string) bool {
	if e == nil || e.ReadOnly || nodeID == "" || nodeID == m.title.ID || m.actions.ExtractBranch == nil {
		return false
	}
	start, end, ok := m.branchSpan(e, nodeID)
	if !ok {
		return false
	}
	title := m.nodeTitle(e, nodeID)
	md := m.branchMarkdown(e, nodeID, true)
	if md == "" {
		return false
	}
	linkTarget, created := m.actions.ExtractBranch(title, md)
	if !created || strings.TrimSpace(linkTarget) == "" {
		return false
	}
	orig := e.Doc.Blocks[start]
	kind := "paragraph"
	indent := 0
	if isList(orig.Kind) {
		kind = orig.Kind
		indent = orig.Indent
	}
	linkBlk := &document.Block{
		ID:     document.NewID(),
		Kind:   kind,
		Indent: indent,
		Runs:   []document.Run{{Text: "[[" + linkTarget + "]]"}},
	}
	e.Engine.Edit(func() {
		blocks := e.Doc.Blocks
		next := make([]*document.Block, 0, len(blocks)-(end-start)+1)
		next = append(next, blocks[:start]...)
		next = append(next, linkBlk)
		next = append(next, blocks[end:]...)
		e.Doc.Blocks = next
		e.Engine.FocusBlock(linkBlk.ID)
	})
	if m.focusRoot == nodeID {
		m.focusRoot = ""
	}
	e.sync()
	m.selected = linkBlk.ID
	m.locatePending = true
	return true
}

func (m *MindMap) notify(msg string) {
	if m.actions.Notify != nil && msg != "" {
		m.actions.Notify(msg)
	}
}

func (m *MindMap) buildNodeMenu(menu *ui.Menu, c *ui.Context, e *Editor, nodeID string, viewItems []mindmap.Item) {
	isRoot := nodeID == m.title.ID
	isFocusedRoot := m.focusRoot != "" && nodeID == m.focusRoot
	hasKids := m.tree.HasChildren(nodeID)
	hasChildInFull := m.nodeHasChildInFullMap(nodeID)
	canEdit := e != nil && !e.ReadOnly
	writeClip := c.Services().WriteClipboard

	if nodeID != "__map_root__" {
		if menu.Item(i18n.T("在文档中编辑")).Chosen() {
			m.selected = nodeID
			e.RevealBlock(nodeID)
			c.After(0)
		}
	}
	if isFocusedRoot {
		if menu.Item(i18n.T("退出聚焦")).Chosen() {
			m.setFocusRoot("")
			c.After(0)
		}
	} else if !isRoot && hasChildInFull {
		if menu.Item(i18n.T("聚焦此分支")).Chosen() {
			m.setFocusRoot(nodeID)
			c.After(0)
		}
	}
	if hasKids && !isRoot && !isFocusedRoot {
		if m.tree.IsFolded(nodeID) {
			if menu.Item(i18n.T("展开子分支")).Chosen() {
				m.tree.SetFolded(nodeID, false)
				m.locatePending = true
				c.After(0)
			}
		} else {
			if menu.Item(i18n.T("折叠子分支")).Chosen() {
				m.tree.SetFolded(nodeID, true)
				m.locatePending = true
				c.After(0)
			}
		}
	}
	if m.tree.HasFoldableDescendant(nodeID) {
		if m.tree.HasFoldedDescendant(viewItems, nodeID) {
			if menu.Item(i18n.T("展开所有子分支")).Chosen() {
				m.tree.FoldSubtree(viewItems, nodeID, false)
				m.locatePending = true
				c.After(0)
			}
		} else {
			if menu.Item(i18n.T("折叠所有子分支")).Chosen() {
				m.tree.FoldSubtree(viewItems, nodeID, true)
				m.locatePending = true
				c.After(0)
			}
		}
	}

	if canEdit {
		menu.Separator()
		menu.Submenu(i18n.T("添加"), func(sub *ui.Menu) {
			if sub.Item(i18n.T("添加子节点")).Chosen() {
				m.addChildNode(e, nodeID, "auto")
				c.After(0)
			}
			if !isRoot {
				if sub.Item(i18n.T("添加同级节点")).Chosen() {
					m.addSiblingNode(e, nodeID)
					c.After(0)
				}
			}
			if sub.Item(i18n.T("添加子任务")).Chosen() {
				m.addChildNode(e, nodeID, "todo")
				c.After(0)
			}
		})
		if !isRoot {
			canUp := m.siblingNodeID(nodeID, -1) != ""
			canDown := m.siblingNodeID(nodeID, 1) != ""
			if menu.Item(i18n.T("上移分支")).Disabled(!canUp).Chosen() {
				m.moveBranch(e, nodeID, -1)
				c.After(0)
			}
			if menu.Item(i18n.T("下移分支")).Disabled(!canDown).Chosen() {
				m.moveBranch(e, nodeID, 1)
				c.After(0)
			}
		}
	}

	menu.Separator()
	if canEdit && !isRoot {
		if menu.Item(i18n.T("剪切分支")).Chosen() {
			if md := m.branchMarkdown(e, nodeID, false); md != "" && writeClip != nil {
				writeClip(md)
			}
			m.deleteBranch(e, nodeID)
			c.After(0)
		}
	}
	menu.Submenu(i18n.T("复制分支为"), func(sub *ui.Menu) {
		if sub.Item("Markdown").Chosen() {
			if md := m.branchMarkdown(e, nodeID, false); md != "" && writeClip != nil {
				writeClip(md)
				m.notify(i18n.T("已复制分支 Markdown"))
			}
		}
		if sub.Item(i18n.T("纯文本大纲")).Chosen() {
			if txt := m.branchPlainOutline(nodeID); txt != "" && writeClip != nil {
				writeClip(txt)
				m.notify(i18n.T("已复制纯文本大纲"))
			}
		}
		if m.actions.CopyPNG != nil {
			if sub.Item(i18n.T("PNG 图片")).Chosen() {
				m.actions.CopyPNG(nodeID)
			}
		}
	})
	if m.actions.ExportPNG != nil || m.actions.ExportSVG != nil {
		menu.Submenu(i18n.T("导出分支为"), func(sub *ui.Menu) {
			if m.actions.ExportPNG != nil {
				if sub.Item(i18n.T("PNG 图片…")).Chosen() {
					m.actions.ExportPNG(nodeID)
				}
			}
			if m.actions.ExportSVG != nil {
				if sub.Item(i18n.T("SVG 矢量图…")).Chosen() {
					m.actions.ExportSVG(nodeID)
				}
			}
		})
	}
	if canEdit && !isRoot && m.actions.ExtractBranch != nil {
		if menu.Item(i18n.T("提取为新文档")).Chosen() {
			m.extractBranch(e, nodeID)
			c.After(0)
		}
	}

	if canEdit && !isRoot {
		menu.Separator()
		if menu.Item(i18n.T("删除整个分支")).Chosen() {
			m.deleteBranch(e, nodeID)
			c.After(0)
		}
		if hasChildInFull {
			if menu.Item(i18n.T("仅删除当前节点")).Chosen() {
				m.deleteOnlyNode(e, nodeID)
				c.After(0)
			}
		}
	}
}

func (m *MindMap) buildCanvasMenu(menu *ui.Menu, viewItems []mindmap.Item, bothSides bool, setBothSides func(bool), c *ui.Context) {
	menu.Submenu(i18n.T("显示层级"), func(sub *ui.Menu) {
		if sub.Item(i18n.T("仅显示第 1 层")).Chosen() {
			m.tree.ShowDepth(viewItems, 1)
			m.locatePending = true
			c.After(0)
		}
		if sub.Item(i18n.T("展开到第 2 层")).Chosen() {
			m.tree.ShowDepth(viewItems, 2)
			m.locatePending = true
			c.After(0)
		}
		if sub.Item(i18n.T("展开到第 3 层")).Chosen() {
			m.tree.ShowDepth(viewItems, 3)
			m.locatePending = true
			c.After(0)
		}
		sub.Separator()
		if sub.Item(i18n.T("展开全部")).Chosen() {
			m.tree.ShowDepth(viewItems, 0)
			m.locatePending = true
			c.After(0)
		}
	})
	if menu.Item(i18n.T("仅看标题大纲")).Checked(m.headingsOnly).Chosen() {
		m.setHeadingsOnly(!m.headingsOnly)
		c.After(0)
	}
	if m.focusRoot != "" {
		if menu.Item(i18n.T("退出聚焦")).Chosen() {
			m.setFocusRoot("")
			c.After(0)
		}
	}

	menu.Separator()
	if menu.Item(i18n.T("单侧布局")).Checked(!bothSides).Chosen() {
		if bothSides && setBothSides != nil {
			setBothSides(false)
			m.locatePending = true
			c.After(0)
		}
	}
	if menu.Item(i18n.T("双侧布局")).Checked(bothSides).Chosen() {
		if !bothSides && setBothSides != nil {
			setBothSides(true)
			m.locatePending = true
			c.After(0)
		}
	}
	if menu.Item(i18n.T("居中并适应画布")).Chosen() {
		m.CenterAndFit()
		c.After(0)
	}
	if menu.Item(i18n.T("重置缩放 (100%)")).Chosen() {
		m.autoFit = false
		m.fitRequested = false
		m.zoom = 1
		m.locatePending = true
		c.After(0)
	}

	if m.actions.CopyPNG != nil || m.actions.ExportPNG != nil || m.actions.ExportSVG != nil {
		menu.Separator()
		targetID := m.focusRoot
		menu.Submenu(i18n.T("导出导图"), func(sub *ui.Menu) {
			if m.actions.CopyPNG != nil {
				if sub.Item(i18n.T("复制为 PNG 图片")).Chosen() {
					m.actions.CopyPNG(targetID)
				}
			}
			if m.actions.ExportPNG != nil {
				if sub.Item(i18n.T("导出 PNG…")).Chosen() {
					m.actions.ExportPNG(targetID)
				}
			}
			if m.actions.ExportSVG != nil {
				if sub.Item(i18n.T("导出 SVG…")).Chosen() {
					m.actions.ExportSVG(targetID)
				}
			}
		})
	}
}

// exportSubtree resolves the root and items for exporting either the whole map
// (branchID == "") or a single branch subtree.
func (m *MindMap) exportSubtree(e *Editor, o Options, branchID string) (mindmap.Item, []mindmap.Item, string) {
	if m.wrapWidth <= 0 {
		m.wrapWidth = 360
	}
	m.sync(e, o)
	if branchID == "" || branchID == m.title.ID {
		return m.title, m.items, strings.TrimSpace(m.title.Text)
	}
	idx := -1
	for i, it := range m.items {
		if it.ID == branchID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return m.title, m.items, strings.TrimSpace(m.title.Text)
	}
	root := m.items[idx]
	baseDepth := root.Depth
	root.Depth = 0
	var sub []mindmap.Item
	for j := idx + 1; j < len(m.items); j++ {
		if m.items[j].Depth <= baseDepth {
			break
		}
		it := m.items[j]
		it.Depth -= baseDepth + 1
		sub = append(sub, it)
	}
	return root, sub, strings.TrimSpace(root.Text)
}

// ExportSVG renders the whole mind map (branchID == "") or a single branch as
// a standalone vector SVG string.
func (m *MindMap) ExportSVG(c *ui.Context, e *Editor, o Options, colors []ui.Color, bothSides bool, branchID string) (string, string) {
	root, items, name := m.exportSubtree(e, o, branchID)
	svg := mindmap.ExportSVG(c, &m.tree, root, items, mindmap.Options{
		Colors:    colors,
		MaxWidth:  m.wrapWidth,
		BothSides: bothSides && branchID == "",
	})
	return svg, name
}

// RenderExportView builds a standalone offscreen view for PNG export and
// returns its natural dimensions (including padding).
func (m *MindMap) RenderExportView(c *ui.Context, e *Editor, o Options, colors []ui.Color, bothSides bool, branchID string) (width, height float32, name string) {
	root, items, title := m.exportSubtree(e, o, branchID)
	// Measure every rich node so offscreen export has final sizes on first pass.
	for i := range items {
		if body := m.bodies[items[i].ID]; body != nil && body.needsMeasure(o) {
			body.measure(o)
			items[i].Width, items[i].Height = body.w, body.h
		}
	}
	if body := m.bodies[root.ID]; body != nil && body.needsMeasure(o) {
		body.measure(o)
		root.Width, root.Height = body.w, body.h
	}
	var st mindmap.State
	pad := float32(28)
	t := o.Theme
	bg := t.Background.Mix(t.Text, .016)
	if t.Dark {
		bg = t.Background.Mix(t.Text, .028)
	}
	useBoth := bothSides && branchID == ""
	ui.Box(c.Key("map-export-root")).Background(bg).Padding(pad).Children(func() {
		mindmap.View(c.Key("map-export-tree"), &st, root, items, mindmap.Options{
			Colors:    colors,
			MaxWidth:  m.wrapWidth,
			BothSides: useBoth,
			Zoom:      1,
		})
	})
	return st.Width + 2*pad, st.Height + 2*pad, title
}
