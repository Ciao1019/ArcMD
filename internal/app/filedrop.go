package app

import (
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

const (
	fileDropFadeDur  = 180 * time.Millisecond
	fileDropEnterDur = 320 * time.Millisecond
	fileDropPulseDur = 700 * time.Millisecond
	fileDropLandDur  = 220 * time.Millisecond
)

type fileDropKind uint8

const (
	fileDropNone fileDropKind = iota
	fileDropWindow
	fileDropSidebar
	fileDropFolder
)

type fileDropState struct {
	over       bool
	targetKind fileDropKind
	targetDir  string
	targetName string
	overSince  time.Time
	lastKind   fileDropKind
	lastName   string
	pulse      map[place]time.Time
	landAt     time.Time
}

var (
	fileDropLabelsOnce sync.Once
	fileDropMDLabel    *ui.TextLayout
	fileDropPDFLabel   *ui.TextLayout
)

func fileDropCardLabels() (md, pdf *ui.TextLayout) {
	fileDropLabelsOnce.Do(func() {
		font := ui.Font{Family: "system-ui", Size: 9.5, Weight: 700}
		fileDropMDLabel = ui.ShapeText("MD", font, 0)
		fileDropPDFLabel = ui.ShapeText("PDF", font, 0)
	})
	return fileDropMDLabel, fileDropPDFLabel
}

// isImportableDoc reports whether name is a Markdown (.md) or PDF (.pdf) file
// allowed for external drag-and-drop import.
func isImportableDoc(name string) bool {
	ext := filepath.Ext(name)
	return strings.EqualFold(ext, ".md") || strings.EqualFold(ext, ".pdf")
}

func isMarkdownFile(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".md")
}

func (a *App) setFileDropOver(kind fileDropKind, dir, name string) {
	a.fileDrop.over = true
	a.fileDrop.targetKind = kind
	a.fileDrop.targetDir = dir
	a.fileDrop.targetName = name
}

// existingSpaceRel reports whether src already resides inside dir of sp.Root
// so dropping a file onto its own folder opens it instead of duplicating it.
func existingSpaceRel(sp *space.Space, dir, src string) (string, bool) {
	if sp == nil || sp.IsExternal() {
		return "", false
	}
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return "", false
	}
	srcDir := filepath.Dir(srcAbs)
	if eval, err := filepath.EvalSymlinks(srcDir); err == nil {
		srcDir = eval
	}
	dstDir := sp.Root
	if dir != "" {
		resolved, err := vault.Resolve(sp.Root, dir)
		if err != nil {
			return "", false
		}
		dstDir = resolved
	}
	if eval, err := filepath.EvalSymlinks(dstDir); err == nil {
		dstDir = eval
	}
	srcInfo, err1 := os.Stat(srcDir)
	dstInfo, err2 := os.Stat(dstDir)
	if err1 != nil || err2 != nil || !os.SameFile(srcInfo, dstInfo) {
		return "", false
	}
	rel := filepath.Base(srcAbs)
	if dir != "" {
		rel = path.Join(dir, rel)
	}
	return rel, true
}

// importDroppedFiles copies external .md and .pdf files into dir of sp (or
// registers them for the external space), rejecting any other file types or
// folders with a notice, then opens the first imported document.
func (a *App) importDroppedFiles(sp *space.Space, dir string, paths []string) {
	if sp == nil || len(paths) == 0 {
		return
	}
	if a.autosaveEnabled() && !a.flush() {
		return
	}
	var created, existing, skipped []string
	for _, src := range paths {
		info, err := os.Stat(src)
		if err != nil || !info.Mode().IsRegular() || !isImportableDoc(src) {
			skipped = append(skipped, filepath.Base(src))
			continue
		}
		if sp.IsExternal() {
			full, err := filepath.Abs(src)
			if err == nil {
				full, err = filepath.EvalSymlinks(full)
			}
			if err != nil {
				a.notify("导入 %s 失败。%v", filepath.Base(src), err)
				continue
			}
			created = append(created, sp.AddExternal(full))
			continue
		}
		if rel, ok := existingSpaceRel(sp, dir, src); ok {
			existing = append(existing, rel)
			continue
		}
		rel, err := vault.ImportFile(src, sp.Root, dir)
		if err != nil {
			a.notify("导入 %s 失败。%v", filepath.Base(src), err)
			continue
		}
		created = append(created, rel)
	}
	if len(skipped) > 0 {
		a.notify("仅支持拖入 Markdown (.md) 和 PDF (.pdf) 文档：%s 未导入。", strings.Join(skipped, "、"))
	}
	if len(created) == 0 && len(existing) == 0 {
		return
	}
	if len(created) > 0 {
		expandDirChain(sp, dir)
		a.clearPicks("")
		a.dismissPreview()
		a.saveState()
		delete(a.pageTrees, sp.ID)
		a.refreshTree()
		a.refreshSearch()
		if !a.now.IsZero() {
			if a.fileDrop.pulse == nil {
				a.fileDrop.pulse = make(map[place]time.Time, len(created))
			}
			for _, rel := range created {
				a.fileDrop.pulse[place{sp.ID, rel}] = a.now
			}
			a.fileDrop.landAt = a.now
		}
		if !sp.IsExternal() {
			targets := a.trackUndoCopies(sp.ID, created)
			a.offerUndo(fmt.Sprintf(i18n.T("已导入到「%s」"), folderName(sp, dir)), countLabel(created), func() {
				a.trashCopied(targets)
			})
		}
	} else if dir != "" {
		expandDirChain(sp, dir)
	}
	firstRel := ""
	if len(created) > 0 {
		firstRel = created[0]
	} else {
		firstRel = existing[0]
	}
	a.openIn(sp, firstRel)
}

// handleEditorFileDrop routes files dropped onto an open document editor:
// images keep inline insertion, Markdown drops import into the active space,
// and remaining drops fall back to insertImages (PDF blocks / skip notice).
func (a *App) handleEditorFileDrop(paths []string, at int) {
	hasImage, hasMD := false, false
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if isImage(p) {
			hasImage = true
			break
		}
		if isMarkdownFile(p) {
			hasMD = true
		}
	}
	if hasImage || !hasMD {
		a.insertImages(paths, at)
		return
	}
	a.importDroppedFiles(a.state.ActiveSpace(), a.currentDir(), paths)
}

// applyImportPulse animates a freshly imported sidebar row sliding in and
// glowing with an accent highlight pulse, returning the glow alpha in [0, 1].
func (a *App) applyImportPulse(c *ui.Context, row ui.Element, p place) float32 {
	if len(a.fileDrop.pulse) == 0 {
		return 0
	}
	start, ok := a.fileDrop.pulse[p]
	if !ok {
		return 0
	}
	if a.reduceMotion || start.IsZero() {
		delete(a.fileDrop.pulse, p)
		return 0
	}
	elapsed := c.Now().Sub(start)
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed >= fileDropPulseDur {
		delete(a.fileDrop.pulse, p)
		return 0
	}
	if elapsed < fileDropEnterDur {
		u := float32(elapsed) / float32(fileDropEnterDur)
		inv := 1 - u
		k := 1 - inv*inv*inv
		row.Translate(-10*(1-k), 0).Opacity(0.25 + 0.75*k)
	}
	c.After(0)
	return 1 - float32(elapsed)/float32(fileDropPulseDur)
}

// fileDropOverlayView draws the non-interactive drop target card or sidebar
// pill while external files are dragged over the window.
func (a *App) fileDropOverlayView(c *ui.Context) {
	active := a.fileDrop.over && a.fileDrop.targetKind != fileDropNone
	if !active && a.fileDrop.lastKind == fileDropNone {
		return
	}
	first := false
	if active {
		if a.fileDrop.lastKind == fileDropNone {
			first = true
			a.fileDrop.overSince = c.Now()
		}
		a.fileDrop.lastKind = a.fileDrop.targetKind
		a.fileDrop.lastName = a.fileDrop.targetName
	}
	kind := a.fileDrop.lastKind
	name := a.fileDrop.lastName

	ui.Overlay(c, func() {
		root := ui.Box(c.Key("file-drop-overlay")).Absolute().Left(0).Top(0).Right(0).Bottom(0).IgnoreInput(true)
		if first && !a.reduceMotion {
			root.Jump("in", 0)
		}
		target := float32(0)
		if active {
			target = 1
		}
		v := root.Animate("in", target, fileDropFadeDur)
		if a.reduceMotion {
			v = target
		}
		if v < 0.01 && !active {
			root.Hidden(true)
			a.fileDrop.overSince = time.Time{}
			a.fileDrop.lastKind = fileDropNone
			a.fileDrop.lastName = ""
			return
		}

		t := c.Theme()
		cols := previewColors(t, a.look)
		var breathe, floatOffset float32
		if active && !a.reduceMotion && !a.fileDrop.overSince.IsZero() {
			sec := c.Now().Sub(a.fileDrop.overSince).Seconds()
			breathe = float32(0.5 + 0.5*math.Sin(sec*3.2))
			floatOffset = float32(math.Sin(sec*2.4)) * 2.5
			c.AnimationFrame()
		}

		switch kind {
		case fileDropWindow:
			scrimAlpha := float32(0.14)
			if t.Dark {
				scrimAlpha = 0.32
			}
			root.Background(black.Alpha(scrimAlpha * v)).Center()
			root.Children(func() {
				card := ui.Column(c.Key("file-drop-card")).Width(340).Height(214).Radius(18).
					Padding(20, 22).Center().Gap(8).
					Background(cols.fill).
					Shadow(0, 14, 36, 0, scaleAlpha(cols.shadow, v)).
					Opacity(v).Scale(0.94+0.06*v, 0.94+0.06*v)
				rimAlpha := (0.65 + 0.25*breathe) * v
				card.Draw(func(p *ui.Painter, r ui.Rect) {
					p.Stroke(r, scaleAlpha(cols.rim, v), 18, 1)
					inset := ui.Rect{X: r.X + 7, Y: r.Y + 7, W: max(0, r.W-14), H: max(0, r.H-14)}
					p.Fill(inset, t.Accent.Alpha(0.045*v), 12)
					p.StrokeDashed(inset, t.Accent.Alpha(rimAlpha), 12, 1.5)
				})
				card.Children(func() {
					glyph := ui.Box(c.Key("file-drop-glyph")).Width(148).Height(84)
					glyph.Draw(func(p *ui.Painter, r ui.Rect) {
						paintFannedDocCards(p, r, t, cols, a.paperDark, v, floatOffset)
					})
					ui.Text(c, fmt.Sprintf(i18n.T("松手导入到「%s」"), name)).
						FontSize(15).FontWeight(600).TextColor(cols.title).SingleLine()
					ui.Text(c, i18n.T("仅支持 Markdown (.md) 和 PDF (.pdf)")).
						FontSize(12).TextColor(cols.muted).SingleLine()
				})
			})
		case fileDropSidebar, fileDropFolder:
			root.Children(func() {
				pill := ui.Row(c.Key("file-drop-pill")).Absolute().Left(10).Bottom(56).Width(sidebarWidth-20).
					Padding(10, 12).Radius(12).Gap(10).AlignItems(ui.Center).
					Background(cols.fill).
					Shadow(0, 8, 22, 0, scaleAlpha(cols.shadow, v)).
					Opacity(v).Translate(0, 8*(1-v))
				pill.Draw(func(p *ui.Painter, r ui.Rect) {
					p.Stroke(r, scaleAlpha(cols.rim, v), 12, 1)
					inset := ui.Rect{X: r.X + 2.5, Y: r.Y + 2.5, W: max(0, r.W-5), H: max(0, r.H-5)}
					p.StrokeDashed(inset, t.Accent.Alpha((0.55+0.25*breathe)*v), 10, 1.25)
				})
				pill.Children(func() {
					glyph := ui.Box(c.Key("file-drop-mini")).Size(34, 28).Shrink(0)
					glyph.Draw(func(p *ui.Painter, r ui.Rect) {
						paintMiniFannedDocCards(p, r, t, cols, a.paperDark, v)
					})
					ui.Column(c.Key("file-drop-pill-text")).Grow(1).MinWidth(0).Gap(2).Children(func() {
						ui.Text(c, fmt.Sprintf(i18n.T("导入到「%s」"), name)).
							FontSize(12.5).FontWeight(600).TextColor(cols.title).SingleLine()
						ui.Text(c, i18n.T("仅支持 .md / .pdf")).
							FontSize(11).TextColor(cols.muted).SingleLine()
					})
				})
			})
		}
	})
}

func paintFannedDocCards(p *ui.Painter, r ui.Rect, t *ui.Theme, cols previewLook, paperDark bool, v, floatOffset float32) {
	docTh := documentTheme(paperDark)
	paper := docTh.Background.Alpha(v)
	lineInk := docTh.TextMuted.Alpha(0.28 * v)
	rim := scaleAlpha(cols.outline, v)
	shadow := scaleAlpha(cols.shadow, 0.75*v)
	mdLabel, pdfLabel := fileDropCardLabels()

	cx, cy := r.X+r.W/2, r.Y+r.H/2
	const cardW, cardH = float32(48), float32(60)

	paintCard := func(card ui.Rect, deg float32, accent ui.Color, label *ui.TextLayout) {
		mx, my := card.X+card.W/2, card.Y+card.H/2
		p.Transform(ui.Rotation(deg), mx, my, func() {
			p.Shadow(card, 8, 0, 5, 14, 0, shadow)
			p.Fill(card, paper, 8)
			p.Stroke(card, rim, 8, 1)
			p.Fill(ui.Rect{X: card.X + 8, Y: card.Y + 9, W: 18, H: 3.5}, accent.Alpha(0.75*v), 1.75)
			p.Fill(ui.Rect{X: card.X + 8, Y: card.Y + 17, W: card.W - 16, H: 2.5}, lineInk, 1.25)
			p.Fill(ui.Rect{X: card.X + 8, Y: card.Y + 23, W: card.W - 22, H: 2.5}, lineInk, 1.25)
			pill := ui.Rect{X: card.X + 8, Y: card.Y + card.H - 21, W: card.W - 16, H: 14}
			p.Fill(pill, accent.Alpha(0.14*v), 4)
			lw, lh := label.Size()
			p.TextLayout(label, pill.X+(pill.W-lw)/2, pill.Y+(pill.H-lh)/2, accent.Alpha(v))
		})
	}

	left := ui.Rect{X: cx - cardW + 1 + 4*(1-v), Y: cy - cardH/2 - 2 - floatOffset, W: cardW, H: cardH}
	right := ui.Rect{X: cx - 1 - 4*(1-v), Y: cy - cardH/2 + 2 + floatOffset, W: cardW, H: cardH}
	paintCard(left, -8.5*v, t.Accent, mdLabel)
	paintCard(right, 8.5*v, t.Danger, pdfLabel)
}

func paintMiniFannedDocCards(p *ui.Painter, r ui.Rect, t *ui.Theme, cols previewLook, paperDark bool, v float32) {
	docTh := documentTheme(paperDark)
	paper := docTh.Background.Alpha(v)
	rim := scaleAlpha(cols.outline, v)
	shadow := scaleAlpha(cols.shadow, 0.5*v)
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	const cardW, cardH = float32(15), float32(20)

	paintMini := func(card ui.Rect, deg float32, accent ui.Color) {
		p.Transform(ui.Rotation(deg), card.X+card.W/2, card.Y+card.H/2, func() {
			p.Shadow(card, 4, 0, 2, 6, 0, shadow)
			p.Fill(card, paper, 4)
			p.Stroke(card, rim, 4, 0.85)
			p.Fill(ui.Rect{X: card.X + 3, Y: card.Y + 4, W: card.W - 6, H: 2.5}, accent.Alpha(0.8*v), 1)
			p.Fill(ui.Rect{X: card.X + 3, Y: card.Y + 9, W: card.W - 6, H: 1.5}, docTh.TextMuted.Alpha(0.3*v), 0.75)
			p.Fill(ui.Rect{X: card.X + 3, Y: card.Y + 13, W: card.W - 8, H: 1.5}, docTh.TextMuted.Alpha(0.3*v), 0.75)
		})
	}

	paintMini(ui.Rect{X: cx - cardW + 3, Y: cy - cardH/2 - 1, W: cardW, H: cardH}, -8*v, t.Accent)
	paintMini(ui.Rect{X: cx - 3, Y: cy - cardH/2 + 1, W: cardW, H: cardH}, 8*v, t.Danger)
}
