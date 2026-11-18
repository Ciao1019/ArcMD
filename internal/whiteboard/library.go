package whiteboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// LibraryItem is a reusable multi-element whiteboard component stamped onto any canvas.
type LibraryItem struct {
	ID       string     `json:"id"`
	Name     string     `json:"name,omitempty"`
	Category string     `json:"category,omitempty"`
	Elements []*Element `json:"elements"`
}

// BuiltinLibrary returns ArcMD's built-in curated library components covering
// system architecture, UI wireframes, and flowchart/thinking diagrams.
func BuiltinLibrary() []LibraryItem {
	return []LibraryItem{
		libDatabaseCluster(),
		libMessageQueue(),
		libCloudMicroservice(),
		libThreeTierArch(),
		libDesktopBrowser(),
		libMobilePhone(),
		libStarCallout(),
		libQuadrantMatrix(),
		libRadialMindMap(),
		libSwimlaneFlow(),
		libActorUseCase(),
		libServiceSequence(),
	}
}

// InstantiateLibraryItem clones item's elements centered at scene point (cx, cy),
// assigning fresh IDs, preserving internal bindings and frame links, and binding
// them in a shared group.
func InstantiateLibraryItem(item LibraryItem, cx, cy float32) []*Element {
	return instantiateElements(item.Elements, cx, cy, true)
}

// InstantiateClipboard clones clipboard elements centered at scene point (cx, cy),
// assigning fresh IDs, preserving internal group structures, bindings, and frame
// links, while clearing external bindings to uncopied elements.
func InstantiateClipboard(elems []*Element, cx, cy float32) []*Element {
	return instantiateElements(elems, cx, cy, false)
}

func instantiateElements(elems []*Element, cx, cy float32, forceSharedGroup bool) []*Element {
	if len(elems) == 0 {
		return nil
	}
	temp := &Scene{Elements: make([]*Element, 0, len(elems))}
	idMap := make(map[string]string, len(elems))
	groupMap := make(map[string]string)
	sharedGroupID := ""
	if forceSharedGroup {
		sharedGroupID = NewID()
	}
	for _, src := range elems {
		if src == nil {
			continue
		}
		cp := src.Clone()
		oldID := cp.ID
		cp.ID = NewID()
		cp.Seed = NewSeed()
		cp.Version = 1
		if oldID != "" {
			idMap[oldID] = cp.ID
		}
		if len(cp.GroupIDs) > 0 {
			remapped := make([]string, 0, len(cp.GroupIDs)+1)
			for _, gid := range cp.GroupIDs {
				if gid == "" {
					continue
				}
				nextGID, ok := groupMap[gid]
				if !ok {
					nextGID = NewID()
					groupMap[gid] = nextGID
				}
				remapped = append(remapped, nextGID)
			}
			cp.GroupIDs = remapped
		}
		if sharedGroupID != "" {
			cp.GroupIDs = append(cp.GroupIDs, sharedGroupID)
		}
		temp.Elements = append(temp.Elements, cp)
	}
	if len(temp.Elements) == 0 {
		return nil
	}
	for _, el := range temp.Elements {
		if el.StartBinding != nil {
			if next, ok := idMap[el.StartBinding.ElementID]; ok {
				el.StartBinding.ElementID = next
			} else {
				el.StartBinding = nil
			}
		}
		if el.EndBinding != nil {
			if next, ok := idMap[el.EndBinding.ElementID]; ok {
				el.EndBinding.ElementID = next
			} else {
				el.EndBinding = nil
			}
		}
		if el.FrameID != "" {
			if next, ok := idMap[el.FrameID]; ok {
				el.FrameID = next
			} else {
				el.FrameID = ""
			}
		}
	}
	UpdateBindings(temp)
	bounds := SceneBounds(temp)
	bx, by := bounds.Center()
	dx, dy := cx-bx, cy-by
	for _, el := range temp.Elements {
		el.X += dx
		el.Y += dy
	}
	UpdateBindings(temp)
	return temp.Elements
}

type rawLibraryFile struct {
	Type         string           `json:"type"`
	Version      int              `json:"version"`
	LibraryItems []rawLibraryItem `json:"libraryItems,omitempty"`
	Library      [][]rawElement   `json:"library,omitempty"`
}

type rawLibraryItem struct {
	ID       string       `json:"id"`
	Status   string       `json:"status,omitempty"`
	Name     string       `json:"name,omitempty"`
	Category string       `json:"category,omitempty"`
	Elements []rawElement `json:"elements"`
}

// EncodeLibrary serializes items to an Excalidraw v2 .excalidrawlib JSON document.
func EncodeLibrary(items []LibraryItem) (string, error) {
	out := rawLibraryFile{
		Type:         "excalidrawlib",
		Version:      CurrentFormatVersion,
		LibraryItems: make([]rawLibraryItem, 0, len(items)),
	}
	for _, item := range items {
		id := item.ID
		if id == "" {
			id = NewID()
		}
		raws := make([]rawElement, 0, len(item.Elements))
		for _, el := range item.Elements {
			if el != nil {
				raws = append(raws, toRawElement(el))
			}
		}
		out.LibraryItems = append(out.LibraryItems, rawLibraryItem{
			ID:       id,
			Status:   "published",
			Name:     item.Name,
			Category: item.Category,
			Elements: raws,
		})
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(append(data, '\n')), nil
}

type rawClipboard struct {
	Type     string       `json:"type"`
	Elements []rawElement `json:"elements"`
}

func decodeRawElements(list []rawElement) []*Element {
	byID := map[string]*Element{}
	var out []*Element
	var boundTexts []rawElement
	for _, re := range list {
		if re.IsDeleted {
			continue
		}
		if re.Type == "text" && re.ContainerID != "" {
			boundTexts = append(boundTexts, re)
			continue
		}
		el := fromRawElement(re)
		out = append(out, el)
		byID[el.ID] = el
	}
	for _, bt := range boundTexts {
		if host, ok := byID[bt.ContainerID]; ok && host.Text == "" {
			host.Text = bt.Text
			if bt.FontSize > 0 {
				host.FontSize = bt.FontSize
			}
		} else {
			out = append(out, fromRawElement(bt))
		}
	}
	return out
}

// EncodeClipboard serializes elements as an Excalidraw clipboard JSON payload
// compatible with both cross-board pasting in ArcMD and official Excalidraw.
func EncodeClipboard(elems []*Element) (string, error) {
	raws := make([]rawElement, 0, len(elems))
	for _, el := range elems {
		if el != nil {
			raws = append(raws, toRawElement(el))
		}
	}
	payload := rawClipboard{
		Type:     "excalidraw/clipboard",
		Elements: raws,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// DecodeClipboard parses an Excalidraw clipboard payload, raw scene JSON, or
// amphibious .excalidraw.svg snippet into elements. Returns (nil, false) if src
// is plain text rather than whiteboard data.
func DecodeClipboard(src string) ([]*Element, bool) {
	trimmed := strings.TrimSpace(src)
	if trimmed == "" {
		return nil, false
	}
	if strings.Contains(trimmed, svgPayloadStart) || strings.HasPrefix(trimmed, "<svg") || strings.HasPrefix(trimmed, "<?xml") {
		sc, err := Decode(trimmed)
		if err == nil && sc != nil && len(sc.Elements) > 0 {
			return sc.Elements, true
		}
		return nil, false
	}
	if strings.HasPrefix(trimmed, "{") {
		var cb rawClipboard
		if err := json.Unmarshal([]byte(trimmed), &cb); err == nil {
			switch cb.Type {
			case "excalidraw/clipboard":
				if elems := decodeRawElements(cb.Elements); len(elems) > 0 {
					return elems, true
				}
				return nil, false
			case "excalidraw":
				if sc, decErr := Decode(trimmed); decErr == nil && sc != nil && len(sc.Elements) > 0 {
					return sc.Elements, true
				}
				return nil, false
			}
		}
	}
	return nil, false
}

// DecodeLibrary parses Excalidraw .excalidrawlib v1 or v2 JSON files into LibraryItems.
func DecodeLibrary(src string) ([]LibraryItem, error) {
	if src == "" {
		return nil, nil
	}
	var raw rawLibraryFile
	if err := json.Unmarshal([]byte(src), &raw); err != nil {
		return nil, fmt.Errorf("whiteboard: invalid library JSON: %w", err)
	}
	if raw.Type != "" && raw.Type != "excalidrawlib" {
		return nil, fmt.Errorf("whiteboard: unexpected library type %q", raw.Type)
	}

	var items []LibraryItem
	for i, it := range raw.LibraryItems {
		elems := decodeRawElements(it.Elements)
		if len(elems) == 0 {
			continue
		}
		name := it.Name
		if name == "" {
			name = fmt.Sprintf("组件 %d", i+1)
		}
		cat := it.Category
		if cat == "" {
			cat = "我的"
		}
		id := it.ID
		if id == "" {
			id = NewID()
		}
		items = append(items, LibraryItem{ID: id, Name: name, Category: cat, Elements: elems})
	}
	for i, group := range raw.Library {
		elems := decodeRawElements(group)
		if len(elems) == 0 {
			continue
		}
		items = append(items, LibraryItem{
			ID:       NewID(),
			Name:     fmt.Sprintf("导入组件 %d", i+1),
			Category: "我的",
			Elements: elems,
		})
	}
	if len(items) == 0 && len(raw.LibraryItems) == 0 && len(raw.Library) == 0 {
		return nil, errors.New("whiteboard: library contains no items")
	}
	return items, nil
}

func libElem(seed uint32, id string, kind ElementKind, x, y, w, h float32, stroke, fill, text string) *Element {
	el := NewElement(kind, x, y, w, h)
	el.ID = id
	el.Seed = seed
	if stroke != "" {
		el.StrokeColor = stroke
	}
	if fill != "" {
		el.FillColor = fill
	}
	el.Text = text
	return el
}

func libArrow(seed uint32, id, fromID, toID, stroke, text string, smooth, elbowed bool) *Element {
	ar := NewElement(KindArrow, 0, 0, 60, 0)
	ar.ID = id
	ar.Seed = seed
	if stroke != "" {
		ar.StrokeColor = stroke
	}
	ar.Text = text
	ar.Smooth = smooth
	ar.Elbowed = elbowed
	if fromID != "" {
		ar.StartBinding = &Binding{ElementID: fromID, Gap: 6}
	}
	if toID != "" {
		ar.EndBinding = &Binding{ElementID: toID, Gap: 6}
	}
	return ar
}

func libDatabaseCluster() LibraryItem {
	m := libElem(101, "db-m", KindCylinder, 0, 0, 110, 96, "blue", "blue", "Primary DB")
	r := libElem(102, "db-r", KindCylinder, 180, 0, 110, 96, "purple", "purple", "Replica DB")
	ar := libArrow(103, "db-sync", "db-m", "db-r", "blue", "WAL Sync", false, false)
	ar.StrokeStyle = StrokeDashed
	return LibraryItem{ID: "builtin-db-cluster", Name: "数据库主从集群", Category: "系统架构", Elements: []*Element{m, r, ar}}
}

func libMessageQueue() LibraryItem {
	p := libElem(201, "mq-p", KindRectangle, 0, 10, 88, 54, "green", "green", "Producer")
	q := libElem(202, "mq-q", KindStadium, 134, 10, 130, 54, "orange", "orange", "Kafka Topic")
	q.FillStyle = FillCrossHatch
	c := libElem(203, "mq-c", KindRectangle, 310, 10, 88, 54, "blue", "blue", "Worker")
	a1 := libArrow(204, "mq-a1", "mq-p", "mq-q", "ink", "pub", false, false)
	a2 := libArrow(205, "mq-a2", "mq-q", "mq-c", "ink", "sub", false, false)
	return LibraryItem{ID: "builtin-mq", Name: "消息队列管道", Category: "系统架构", Elements: []*Element{p, q, c, a1, a2}}
}

func libCloudMicroservice() LibraryItem {
	cl := libElem(301, "cm-cl", KindCloud, 0, 36, 128, 86, "blue", "blue", "Cloud Edge")
	gw := libElem(302, "cm-gw", KindHexagon, 176, 42, 116, 74, "orange", "orange", "Gateway")
	s1 := libElem(303, "cm-s1", KindRectangle, 344, 0, 110, 56, "green", "green", "Auth Svc")
	s2 := libElem(304, "cm-s2", KindRectangle, 344, 102, 110, 56, "purple", "purple", "Core Svc")
	a1 := libArrow(305, "cm-a1", "cm-cl", "cm-gw", "ink", "", false, false)
	a2 := libArrow(306, "cm-a2", "cm-gw", "cm-s1", "green", "", false, true)
	a3 := libArrow(307, "cm-a3", "cm-gw", "cm-s2", "purple", "", false, true)
	return LibraryItem{ID: "builtin-cloud-ms", Name: "云端微服务网关", Category: "系统架构", Elements: []*Element{cl, gw, s1, s2, a1, a2, a3}}
}

func libThreeTierArch() LibraryItem {
	c := libElem(401, "tt-c", KindRectangle, 0, 0, 108, 62, "blue", "blue", "Web / App")
	s := libElem(402, "tt-s", KindHexagon, 158, 0, 116, 62, "green", "green", "API Server")
	d := libElem(403, "tt-d", KindCylinder, 326, -12, 102, 86, "purple", "purple", "Storage")
	a1 := libArrow(404, "tt-a1", "tt-c", "tt-s", "ink", "HTTPS", false, false)
	a2 := libArrow(405, "tt-a2", "tt-s", "tt-d", "ink", "SQL", false, false)
	return LibraryItem{ID: "builtin-3tier", Name: "三层系统架构", Category: "系统架构", Elements: []*Element{c, s, d, a1, a2}}
}

func libDesktopBrowser() LibraryItem {
	win := libElem(501, "wb-win", KindRectangle, 0, 0, 320, 210, "gray", "", "")
	win.Roughness = 1
	bar := libElem(502, "wb-bar", KindLine, 0, 32, 320, 0, "gray", "", "")
	bar.Points = [][2]float32{{0, 0}, {320, 0}}
	dots := libElem(503, "wb-url", KindStadium, 64, 7, 232, 18, "gray", "gray", "https://arcmd.app")
	dots.FontSize = 11
	dots.FillStyle = FillSolid
	hero := libElem(504, "wb-hero", KindRectangle, 18, 48, 284, 62, "blue", "blue", "Hero Section")
	c1 := libElem(505, "wb-c1", KindRectangle, 18, 124, 134, 68, "green", "green", "Card A")
	c2 := libElem(506, "wb-c2", KindRectangle, 168, 124, 134, 68, "orange", "orange", "Card B")
	return LibraryItem{ID: "builtin-browser", Name: "桌面浏览器线框", Category: "界面线框", Elements: []*Element{win, bar, dots, hero, c1, c2}}
}

func libMobilePhone() LibraryItem {
	ph := libElem(601, "mp-fr", KindRectangle, 0, 0, 152, 268, "ink", "", "")
	ph.StrokeWidth = 2.5
	hd := libElem(602, "mp-hd", KindStadium, 36, 12, 80, 12, "gray", "gray", "")
	hd.FillStyle = FillSolid
	card1 := libElem(603, "mp-c1", KindRectangle, 14, 38, 124, 72, "blue", "blue", "主视觉卡片")
	card1.FontSize = 13
	card2 := libElem(604, "mp-c2", KindRectangle, 14, 122, 124, 42, "green", "green", "列表项 1")
	card2.FontSize = 13
	card3 := libElem(605, "mp-c3", KindRectangle, 14, 174, 124, 42, "orange", "orange", "列表项 2")
	card3.FontSize = 13
	tab := libElem(606, "mp-tab", KindRectangle, 14, 228, 124, 26, "gray", "gray", "导航栏")
	tab.FontSize = 11
	return LibraryItem{ID: "builtin-mobile", Name: "移动端界面线框", Category: "界面线框", Elements: []*Element{ph, hd, card1, card2, card3, tab}}
}

func libStarCallout() LibraryItem {
	st := libElem(701, "sc-star", KindStar, 0, 14, 64, 64, "orange", "orange", "")
	cb := libElem(702, "sc-bubble", KindCallout, 78, 0, 196, 102, "blue", "blue", "重点批注\n待进一步验证")
	return LibraryItem{ID: "builtin-star-callout", Name: "高亮气泡批注", Category: "界面线框", Elements: []*Element{st, cb}}
}

func libQuadrantMatrix() LibraryItem {
	axH := libElem(801, "qm-h", KindArrow, 0, 118, 280, 0, "ink", "", "")
	axH.Points = [][2]float32{{0, 0}, {280, 0}}
	axH.Smooth = false
	axV := libElem(802, "qm-v", KindArrow, 140, 236, 0, -236, "ink", "", "")
	axV.Points = [][2]float32{{0, 0}, {0, -236}}
	axV.Smooth = false
	q1 := libElem(803, "qm-1", KindRectangle, 16, 16, 108, 84, "red", "red", "重要且紧急")
	q1.FontSize = 13
	q2 := libElem(804, "qm-2", KindRectangle, 156, 16, 108, 84, "blue", "blue", "重要不紧急")
	q2.FontSize = 13
	q3 := libElem(805, "qm-3", KindRectangle, 16, 134, 108, 84, "orange", "orange", "紧急不重要")
	q3.FontSize = 13
	q4 := libElem(806, "qm-4", KindRectangle, 156, 134, 108, 84, "gray", "gray", "暂缓处理")
	q4.FontSize = 13
	return LibraryItem{ID: "builtin-quadrant", Name: "四象限分析矩阵", Category: "流程与思维", Elements: []*Element{axH, axV, q1, q2, q3, q4}}
}

func libRadialMindMap() LibraryItem {
	c := libElem(901, "mm-c", KindEllipse, 140, 82, 120, 62, "purple", "purple", "核心主题")
	n1 := libElem(902, "mm-1", KindStadium, 0, 0, 104, 42, "blue", "blue", "分支 A")
	n2 := libElem(903, "mm-2", KindStadium, 296, 0, 104, 42, "green", "green", "分支 B")
	n3 := libElem(904, "mm-3", KindStadium, 0, 184, 104, 42, "orange", "orange", "分支 C")
	n4 := libElem(905, "mm-4", KindStadium, 296, 184, 104, 42, "red", "red", "分支 D")
	a1 := libArrow(906, "mm-a1", "mm-c", "mm-1", "blue", "", true, false)
	a2 := libArrow(907, "mm-a2", "mm-c", "mm-2", "green", "", true, false)
	a3 := libArrow(908, "mm-a3", "mm-c", "mm-3", "orange", "", true, false)
	a4 := libArrow(909, "mm-a4", "mm-c", "mm-4", "red", "", true, false)
	return LibraryItem{ID: "builtin-mindmap", Name: "中心辐射思维导图", Category: "流程与思维", Elements: []*Element{c, n1, n2, n3, n4, a1, a2, a3, a4}}
}

func libSwimlaneFlow() LibraryItem {
	f1 := libElem(1001, "sl-f1", KindFrame, 0, 0, 190, 260, "gray", "", "")
	f1.Name = "产品 / 需求"
	f2 := libElem(1002, "sl-f2", KindFrame, 206, 0, 190, 260, "gray", "", "")
	f2.Name = "研发 / 测试"
	s1 := libElem(1003, "sl-s", KindStadium, 35, 28, 120, 44, "green", "green", "开始")
	s1.FrameID = "sl-f1"
	p1 := libElem(1004, "sl-p", KindRectangle, 241, 24, 120, 52, "blue", "blue", "开发实现")
	p1.FrameID = "sl-f2"
	d1 := libElem(1005, "sl-d", KindDiamond, 236, 114, 130, 68, "orange", "orange", "验收通过?")
	d1.FrameID = "sl-f2"
	doc := libElem(1006, "sl-doc", KindDocument, 30, 152, 130, 72, "purple", "purple", "发布归档")
	doc.FrameID = "sl-f1"
	a1 := libArrow(1007, "sl-a1", "sl-s", "sl-p", "ink", "", false, true)
	a2 := libArrow(1008, "sl-a2", "sl-p", "sl-d", "ink", "", false, false)
	a3 := libArrow(1009, "sl-a3", "sl-d", "sl-doc", "purple", "是", false, true)
	return LibraryItem{ID: "builtin-swimlane", Name: "双泳道流程图", Category: "流程与思维", Elements: []*Element{f1, f2, s1, p1, d1, doc, a1, a2, a3}}
}

func libActorUseCase() LibraryItem {
	ac := libElem(1101, "uc-a", KindActor, 0, 36, 86, 128, "ink", "", "用户")
	u1 := libElem(1102, "uc-1", KindEllipse, 148, 0, 140, 64, "blue", "blue", "登录鉴权")
	u2 := libElem(1103, "uc-2", KindEllipse, 148, 116, 140, 64, "green", "green", "协同编辑")
	l1 := libArrow(1104, "uc-l1", "uc-a", "uc-1", "ink", "", false, false)
	l1.EndArrowhead = ArrowNone
	l2 := libArrow(1105, "uc-l2", "uc-a", "uc-2", "ink", "", false, false)
	l2.EndArrowhead = ArrowNone
	return LibraryItem{ID: "builtin-usecase", Name: "用例角色图", Category: "流程与思维", Elements: []*Element{ac, u1, u2, l1, l2}}
}

func libServiceSequence() LibraryItem {
	p1 := libElem(1201, "sq-p1", KindRectangle, 0, 0, 108, 48, "blue", "blue", "Client")
	p2 := libElem(1202, "sq-p2", KindRectangle, 200, 0, 108, 48, "green", "green", "Server")
	l1 := libElem(1203, "sq-l1", KindLine, 54, 48, 0, 140, "gray", "", "")
	l1.Points = [][2]float32{{0, 0}, {0, 140}}
	l1.StrokeStyle = StrokeDashed
	l2 := libElem(1204, "sq-l2", KindLine, 254, 48, 0, 140, "gray", "", "")
	l2.Points = [][2]float32{{0, 0}, {0, 140}}
	l2.StrokeStyle = StrokeDashed
	m1 := libElem(1205, "sq-m1", KindArrow, 54, 88, 200, 0, "blue", "", "POST /sync")
	m1.Points = [][2]float32{{0, 0}, {200, 0}}
	m1.Smooth = false
	m2 := libElem(1206, "sq-m2", KindArrow, 254, 144, -200, 0, "green", "", "200 OK")
	m2.Points = [][2]float32{{0, 0}, {-200, 0}}
	m2.Smooth = false
	m2.StrokeStyle = StrokeDashed
	return LibraryItem{ID: "builtin-sequence", Name: "服务调用时序", Category: "系统架构", Elements: []*Element{p1, p2, l1, l2, m1, m2}}
}
