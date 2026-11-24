package dbview_test

import (
	"image"
	"image/color"
	"slices"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/dbview"
	"github.com/egoist/mygo/ui"
)

func makeTestCoverBitmap(w, h int) *ui.Bitmap {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(70 + (x*140)/max(1, w)),
				G: uint8(110 + (y*90)/max(1, h)),
				B: 225,
				A: 255,
			})
		}
	}
	return ui.NewBitmap(img)
}

func makeTesterWithOptions(t *testing.T, st *database.Store, loadImg func(recPath, imgURL string) *ui.Bitmap) (*dbview.State, *ui.Tester, *[]string, *[]*database.Record, *[]string) {
	t.Helper()
	state := dbview.NewState(st)
	var changed []string
	var created []*database.Record
	var opened []string
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	opts := dbview.Options{
		Now:       now,
		LoadImage: loadImg,
		OnChanged: func(affected []string) {
			changed = append(changed, affected...)
		},
		OnAfterCreate: func(rec *database.Record) {
			created = append(created, rec)
		},
		OnOpenRecord: func(rec *database.Record, mode string) {
			if rec != nil {
				opened = append(opened, mode+":"+rec.Title)
			}
		},
	}

	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(ui.LightTheme())
		ui.Column(c.Key("host")).Fill().Children(func() {
			dbview.View(c, state, opts)
		})
	}, 1160, 740)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	return state, tt, &changed, &created, &opened
}

func dragBetween(tt *ui.Tester, x0, y0, x1, y1 float32) {
	tt.Press(x0, y0)
	tt.Move(x0+6, y0+6)
	tt.Frame()
	steps := 8
	for i := 1; i <= steps; i++ {
		f := float32(i) / float32(steps)
		tt.Move(x0+(x1-x0)*f, y0+(y1-y0)*f)
		tt.Frame()
	}
	tt.Release(x1, y1)
	tt.Frame()
	tt.Frame()
}

func TestBoardRenderDragCrossColumnAndUndo(t *testing.T) {
	st := buildTestStore(t)
	state, tt, changed, _, _ := makeTesterWithOptions(t, st, nil)

	if err := tt.Click("看板"); err != nil {
		t.Fatalf("switch to Board view failed: %v", err)
	}
	tt.Frame()

	for _, want := range []string{"未开始", "进行中", "已完成", "质量验收", "计划开发", "界面设计"} {
		if !tt.HasText(want) {
			t.Fatalf("expected %q in Board view, texts=%v", want, tt.Texts())
		}
	}

	qaBox, ok := tt.Find("质量验收")
	if !ok {
		t.Fatal("could not find 质量验收 card")
	}
	planBox, ok := tt.Find("计划开发")
	if !ok {
		t.Fatal("could not find 计划开发 card in 进行中 column")
	}

	// Drag 质量验收 (from 未开始) onto 计划开发 (in 进行中).
	dragBetween(tt, qaBox.X+qaBox.W/2, qaBox.Y+qaBox.H/2, planBox.X+planBox.W/2, planBox.Y+planBox.H/2)

	rec := st.RecordByPath("产品路线图/qa.md")
	if rec == nil || rec.Props["prop_status"].Text != "进行中" {
		t.Fatalf("expected qa.md moved to 进行中 after drag-drop, rec=%#v", rec)
	}
	if !slices.Contains(*changed, "产品路线图/qa.md") {
		t.Fatalf("expected OnChanged for qa.md, got %v", *changed)
	}

	// Undo with Cmd+Z reverts prop_status back to 未开始 in one step.
	state.RequestFocus = true
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()

	rec = st.RecordByPath("产品路线图/qa.md")
	if rec == nil || rec.Props["prop_status"].Text != "未开始" {
		t.Fatalf("expected Undo to revert qa.md back to 未开始, got %#v", rec)
	}
}

func TestBoardColumnCreateCollapseAddGroupAndSwimlanes(t *testing.T) {
	st := buildTestStore(t)
	state, tt, _, created, _ := makeTesterWithOptions(t, st, nil)

	if err := tt.Click("看板"); err != nil {
		t.Fatalf("switch to Board view failed: %v", err)
	}
	tt.Frame()

	// 1. Create record inside "进行中" column header "+" button.
	addHdr, ok := tt.Access("在此分组新建: 进行中")
	if !ok {
		t.Fatalf("missing column header add button, access=%v", tt.AccessAll())
	}
	tt.ClickAt(addHdr.Bounds.X+addHdr.Bounds.W/2, addHdr.Bounds.Y+addHdr.Bounds.H/2)
	tt.Frame()
	if len(*created) != 1 {
		t.Fatalf("expected 1 record created in 进行中 column, created=%d", len(*created))
	}
	if got := (*created)[0].Props["prop_status"].Text; got != "进行中" {
		t.Fatalf("expected new card status '进行中', got %q", got)
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()

	// 2. Collapse & expand "已完成" column.
	foldBtn, ok := tt.Access("折叠此列: 已完成")
	if !ok {
		t.Fatalf("missing collapse button for 已完成 column")
	}
	tt.ClickAt(foldBtn.Bounds.X+foldBtn.Bounds.W/2, foldBtn.Bounds.Y+foldBtn.Bounds.H/2)
	tt.Frame()
	if !slices.Contains(state.ActiveView().Group.Collapsed, "已完成") {
		t.Fatalf("expected 已完成 in Group.Collapsed, got %v", state.ActiveView().Group.Collapsed)
	}
	expandBtn, ok := tt.Access("展开此列: 已完成")
	if !ok {
		t.Fatal("missing expand button for collapsed column 已完成")
	}
	tt.ClickAt(expandBtn.Bounds.X+expandBtn.Bounds.W/2, expandBtn.Bounds.Y+expandBtn.Bounds.H/2)
	tt.Frame()
	if slices.Contains(state.ActiveView().Group.Collapsed, "已完成") {
		t.Fatalf("expected 已完成 uncollapsed, got %v", state.ActiveView().Group.Collapsed)
	}

	// 3. Add a new group option via "+ 添加分组".
	if err := tt.Click("添加分组"); err != nil {
		t.Fatalf("click 添加分组 failed: %v", err)
	}
	tt.Frame()
	tt.Type("待审核")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	pStatus, _ := st.Database().PropertyByID("prop_status")
	hasOption := false
	for _, opt := range pStatus.Options {
		if opt.Name == "待审核" {
			hasOption = true
		}
	}
	if !hasOption || !tt.HasText("待审核") {
		t.Fatalf("expected new status option 待审核, opts=%v texts=%v", pStatus.Options, tt.Texts())
	}

	// 4. Enable Swimlanes (SubGroup by prop_done) via Layout popover.
	vc := state.ActiveView().Clone()
	vc.Board.SubGroup = &database.GroupConfig{PropertyID: "prop_done"}
	if err := st.UpdateView(vc); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("已勾选") || !tt.HasText("未勾选") {
		t.Fatalf("expected swimlane headers 已勾选 / 未勾选, texts=%v", tt.Texts())
	}

	// Drag 质量验收 (status=未开始, done=false) onto 界面设计 (status=已完成, done=true) -> atomic two-prop move!
	qaBox, ok := tt.Find("质量验收")
	if !ok {
		t.Fatal("missing 质量验收 in swimlane")
	}
	designBox, ok := tt.Find("界面设计")
	if !ok {
		t.Fatal("missing 界面设计 in swimlane")
	}
	dragBetween(tt, qaBox.X+qaBox.W/2, qaBox.Y+qaBox.H/2, designBox.X+designBox.W/2, designBox.Y+designBox.H/2)

	rec := st.RecordByPath("产品路线图/qa.md")
	if rec.Props["prop_status"].Text != "已完成" || !rec.Props["prop_done"].Bool {
		t.Fatalf("expected atomic swimlane move to status=已完成, done=true; got status=%q done=%v",
			rec.Props["prop_status"].Text, rec.Props["prop_done"].Bool)
	}

	// Single Cmd+Z undoes BOTH status and checkbox edits at once.
	state.RequestFocus = true
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	rec = st.RecordByPath("产品路线图/qa.md")
	if rec.Props["prop_status"].Text != "未开始" || rec.Props["prop_done"].Bool {
		t.Fatalf("expected single Cmd+Z to revert both swimlane props, got status=%q done=%v",
			rec.Props["prop_status"].Text, rec.Props["prop_done"].Bool)
	}
}

func TestListViewRenderGroupAndCheckboxIsolation(t *testing.T) {
	st := buildTestStore(t)
	db := st.Database()
	listView := database.ViewConfig{
		ID:         "view_list",
		Name:       "列表",
		Type:       database.ViewList,
		OpenMode:   dbview.OpenSidePeek,
		Properties: slices.Clone(db.Views[0].Properties),
	}
	if err := st.AddView(listView, true); err != nil {
		t.Fatal(err)
	}

	state, tt, _, _, opened := makeTesterWithOptions(t, st, nil)

	for _, want := range []string{"计划开发", "界面设计", "质量验收", "进行中", "重要"} {
		if !tt.HasText(want) {
			t.Fatalf("expected %q in List view, texts=%v", want, tt.Texts())
		}
	}

	// Clicking row checkbox must toggle prop_done WITHOUT opening the record.
	chkInfo, ok := tt.Access("完成: 计划开发")
	if !ok {
		t.Fatalf("missing checkbox '完成: 计划开发', access=%v", tt.AccessAll())
	}
	tt.ClickAt(chkInfo.Bounds.X+chkInfo.Bounds.W/2, chkInfo.Bounds.Y+chkInfo.Bounds.H/2)
	tt.Frame()

	planRec := st.RecordByPath("产品路线图/plan.md")
	if !planRec.Props["prop_done"].Bool {
		t.Fatal("expected checkbox toggle to set prop_done=true")
	}
	if len(*opened) != 0 {
		t.Fatalf("clicking checkbox must NOT fire OnOpenRecord, got %v", *opened)
	}

	// Clicking row title opens record in Side Peek.
	if err := tt.Click("计划开发"); err != nil {
		t.Fatalf("click row title failed: %v", err)
	}
	tt.Frame()
	if len(*opened) != 1 || (*opened)[0] != "side:计划开发" {
		t.Fatalf("expected OnOpenRecord side:计划开发, got %v", *opened)
	}

	// Group List by Status and collapse a group header.
	vc := state.ActiveView().Clone()
	vc.Group = &database.GroupConfig{PropertyID: "prop_status"}
	if err := st.UpdateView(vc); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	hdr, ok := tt.Access("已完成")
	if !ok {
		t.Fatal("missing List group header 已完成")
	}
	tt.ClickAt(hdr.Bounds.X+hdr.Bounds.W/2, hdr.Bounds.Y+hdr.Bounds.H/2)
	tt.Frame()
	if tt.HasText("界面设计") {
		t.Fatalf("expected 已完成 group collapsed hiding 界面设计, texts=%v", tt.Texts())
	}
}

func TestGalleryViewRenderSizesCoverAndLayoutPopover(t *testing.T) {
	st := buildTestStore(t)
	db := st.Database()
	planRec := st.RecordByPath("产品路线图/plan.md")
	planRec.Cover = "assets/hero.png"

	galleryView := database.ViewConfig{
		ID:         "view_gallery",
		Name:       "画廊",
		Type:       database.ViewGallery,
		OpenMode:   dbview.OpenCenterPeek,
		Properties: slices.Clone(db.Views[0].Properties),
		Gallery: &database.GalleryViewConfig{
			CardSize:    "medium",
			CoverSource: "cover",
			ShowIcon:    true,
		},
	}
	if err := st.AddView(galleryView, true); err != nil {
		t.Fatal(err)
	}

	coverBmp := makeTestCoverBitmap(240, 120)
	loadCalls := 0
	state, tt, _, _, opened := makeTesterWithOptions(t, st, func(recPath, imgURL string) *ui.Bitmap {
		if imgURL == "assets/hero.png" {
			loadCalls++
			return coverBmp
		}
		return nil
	})

	if loadCalls == 0 {
		t.Fatal("expected Gallery card to invoke Options.LoadImage for assets/hero.png")
	}
	for _, want := range []string{"计划开发", "界面设计", "质量验收"} {
		if !tt.HasText(want) {
			t.Fatalf("expected %q in Gallery view, texts=%v", want, tt.Texts())
		}
	}

	// Open Layout popover and switch CardSize -> small -> large and preview -> content.
	if err := tt.Click("布局"); err != nil {
		t.Fatalf("open 布局 popover failed: %v", err)
	}
	tt.Frame()
	if !state.LayoutOpen {
		t.Fatal("expected LayoutOpen true")
	}
	if err := tt.Click("小"); err != nil {
		t.Fatalf("click 小 size failed: %v", err)
	}
	tt.Frame()
	if got := state.ActiveView().Gallery.CardSize; got != "small" {
		t.Fatalf("CardSize = %q, want small", got)
	}
	if err := tt.Click("大"); err != nil {
		t.Fatalf("click 大 size failed: %v", err)
	}
	tt.Frame()
	if got := state.ActiveView().Gallery.CardSize; got != "large" {
		t.Fatalf("CardSize = %q, want large", got)
	}

	if err := tt.Click("页面封面"); err != nil {
		t.Fatalf("open cover source menu failed: %v", err)
	}
	tt.Frame()
	if err := tt.ChooseMenuItem("页面内容"); err != nil {
		t.Fatalf("choose 页面内容 failed: %v", err)
	}
	tt.Frame()
	if got := state.ActiveView().Gallery.CoverSource; got != "content" {
		t.Fatalf("CoverSource = %q, want content", got)
	}
	if !tt.HasText("详细内容。") {
		t.Fatalf("expected page excerpt in content preview mode, texts=%v", tt.Texts())
	}

	// Close popover and click a card to open in Center Peek (configured on this Gallery view).
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if err := tt.Click("质量验收"); err != nil {
		t.Fatalf("click gallery card failed: %v", err)
	}
	tt.Frame()
	if len(*opened) != 1 || (*opened)[0] != "center:质量验收" {
		t.Fatalf("expected OnOpenRecord center:质量验收, got %v", *opened)
	}
}
