package app

import (
	"math"
	"os"
	"time"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// Pinned documents stay open, as Arc's and Zen's pinned tabs stay loaded:
// a pinned document once shown keeps its editor (caret, scroll, undo) while
// other documents are shown, and its row looks open. The minus on an open
// pinned row closes it and keeps it pinned (Arc's "Close and Keep Pinned"),
// and a closed pinned row shows dimmed, as Zen shows an unloaded tab.
// Documents outside the pinned section are as before: shown, or not.
//
// A kept document is always saved: leaving it flushes it first, so closing
// one in the background drops nothing.

// openBounce is how the row of a document just closed springs: it jumps to
// 0.9 and springs back without overshooting, as Arc's (ω ≈ 14).
const (
	openBounceScale = 0.9
	openBounceOmega = 14
	openBounceTime  = 600 * time.Millisecond
)

// bounce is the row that springs because its document was closed.
type bounce struct {
	p  place
	at time.Time
}

// inPins reports whether rel of the space sp shows in its pinned section:
// pinned, or inside a pinned folder.
func inPins(sp *space.Space, rel string) bool {
	if sp == nil {
		return false
	}
	for _, p := range sp.Pins {
		if under(rel, p) {
			return true
		}
	}
	return false
}

// isShown reports whether the document at rel of the space sp is the one
// shown.
func (a *App) isShown(sp *space.Space, rel string) bool {
	return a.doc != nil && a.doc.space == sp && a.doc.rel == rel
}

// isOpen reports whether the pinned document at rel of the space sp is
// open: shown, or kept open behind the one shown.
func (a *App) isOpen(sp *space.Space, rel string) bool {
	if a.isShown(sp, rel) {
		return true
	}
	for _, session := range a.sessions {
		if session.split != nil {
			for _, p := range session.split.panes {
				if p.doc != nil && p.doc.space == sp && p.doc.rel == rel {
					return true
				}
			}
		}
	}
	return sp != nil && a.openDocs[place{sp.ID, rel}] != nil
}

// syncOpen keeps the open documents to the pinned ones: the shown document
// joins them when pinned, and those no longer pinned, or of a space gone,
// close. Pins change in many places (menus, drags, other spaces' moves), so
// each frame settles it rather than each of them.
func (a *App) syncOpen() {
	for p, d := range a.openDocs {
		sp := a.state.Get(p.space)
		if sp == nil || !inPins(sp, p.rel) || d.space != sp || d.rel != p.rel {
			if d != a.doc && d.dirty() {
				_ = d.save() // never drop an edit, though kept ones are saved
			}
			delete(a.openDocs, p)
		}
	}
	if d := a.doc; d != nil && d.loadErr == nil && inPins(d.space, d.rel) {
		a.openDocs[place{d.space.ID, d.rel}] = d
	}
}

// keptDoc is the open document kept for rel of the space sp, when it still
// says what the file does; one the file moved on from closes.
func (a *App) keptDoc(sp *space.Space, rel, text string) *document {
	p := place{sp.ID, rel}
	d := a.openDocs[p]
	if d == nil {
		return nil
	}
	if d.diskText != text || d.loadErr != nil {
		delete(a.openDocs, p)
		return nil
	}
	return d
}

// followOpen keeps the open documents under from of the space fromID open
// at to of the space toID, where they moved.
func (a *App) followOpen(fromID, from, toID, to string) {
	dst := a.state.Get(toID)
	if dst == nil {
		return
	}
	for p, d := range a.openDocs {
		if p.space != fromID || !under(p.rel, from) {
			continue
		}
		delete(a.openDocs, p)
		rel := to + p.rel[len(from):]
		d.space, d.rel = dst, rel
		a.openDocs[place{toID, rel}] = d
	}
}

// closeOpen closes the open pinned document at rel of the space sp and
// keeps it pinned. Closing the shown one shows the document before it that
// is still open, as Arc selects the tab used before; with none, nothing.
func (a *App) closeOpen(sp *space.Space, rel string) {
	if a.split != nil {
		a.syncSplit()
		for i, p := range a.split.panes {
			if p.doc != nil && p.doc.space == sp && p.doc.rel == rel {
				group := a.split
				count := len(group.panes)
				a.closePane(i)
				if len(group.panes) < count {
					delete(a.openDocs, place{sp.ID, rel})
					a.bounce = bounce{place{sp.ID, rel}, a.now}
				}
				return
			}
		}
	}
	p := place{sp.ID, rel}
	if !a.isShown(sp, rel) {
		if d := a.openDocs[p]; d != nil && d.dirty() {
			if err := d.save(); err != nil {
				a.notify("保存 %s 失败。%v", d.title(), err)
				return
			}
		}
		delete(a.openDocs, p)
		a.bounce = bounce{p, a.now}
		return
	}
	if !a.flush() {
		return
	}
	delete(a.openDocs, p)
	a.bounce = bounce{p, a.now}
	a.showAfter(sp, rel)
}

// showAfter shows, in place of the shown document at rel of the space sp
// that is leaving the main window (saved), the document before it that is
// still open, as Arc selects the tab used before; with none, nothing, and
// the space forgets it as its last document.
func (a *App) showAfter(sp *space.Space, rel string) {
	if next, ok := a.openBefore(place{sp.ID, rel}); ok {
		a.doc = nil // gone: showing the next must not keep it
		if a.openIn(sp, next); a.doc != nil {
			return
		}
	}
	a.doc = nil
	if sp.LastOpen == rel {
		sp.LastOpen = ""
		a.saveState()
	}
}

// openBefore is the document of the history, latest first, to show when
// the document p closes: one of the same space that is open, as a document
// outside the pinned section always is, a closed pinned one not.
func (a *App) openBefore(p place) (string, bool) {
	sp := a.state.Get(p.space)
	ok := func(q place) bool {
		if q == p || q.space != p.space || a.state.IsFavorite(q.space, q.rel) {
			return false
		}
		if inPins(sp, q.rel) && a.openDocs[q] == nil {
			return false
		}
		return a.exists(sp, q.rel)
	}
	for i := min(a.at, len(a.history)-1); i >= 0; i-- {
		if ok(a.history[i]) {
			return a.history[i].rel, true
		}
	}
	for i := a.at + 1; i < len(a.history); i++ {
		if ok(a.history[i]) {
			return a.history[i].rel, true
		}
	}
	return "", false
}

// rowBounce springs the row of p back from the close, asking for frames
// while it moves.
func (a *App) rowBounce(c *ui.Context, row ui.Element, p place) {
	if a.bounce.p != p || a.reduceMotion {
		return
	}
	t := a.now.Sub(a.bounce.at)
	if t < 0 || t >= openBounceTime {
		return
	}
	x := openBounceOmega * t.Seconds()
	s := 1 - (1-openBounceScale)*(1+x)*math.Exp(-x)
	row.TransformOrigin(0.5, 0.5).Scale(float32(s), float32(s))
	c.After(16 * time.Millisecond)
}

// minusMark draws the close button's minus: a 2 DIP round-capped bar 8.5
// DIPs long, as Arc strokes it, on a whole DIP.
func minusMark(p *ui.Painter, r ui.Rect, color ui.Color) {
	cx := float32(math.Round(float64(r.X + r.W/2)))
	cy := float32(math.Round(float64(r.Y + r.H/2)))
	p.Fill(ui.Rect{X: cx - 5.25, Y: cy - 1, W: 10.5, H: 2}, color, 1)
}

// exists reports whether the file at rel of the space sp is there.
func (a *App) exists(sp *space.Space, rel string) bool {
	p, err := vault.Resolve(sp.Root, rel)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}
