package app

import (
	"encoding/json"
	"fmt"
	neturl "net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const maxHTMLWebViewsPerWindow = 8

type htmlWebViewKey struct {
	win  *mygo.Window
	slot string
}

type htmlWebViewHandle struct {
	win        *mygo.Window
	doc        *document
	wv         *mygo.WebView
	fullPath   string
	rev        uint64
	dark       bool
	allowDir   string
	allowEval  string
	loaded     atomic.Bool
	destroyed  atomic.Bool
	findSeq    atomic.Uint64
	mu         sync.Mutex
	pendingAnc string
	pendingIdx int
}

func newHTMLWebView(win *mygo.Window, doc *document, fullPath string, rev uint64, anchor string, scrollToContainer, dark bool) (*htmlWebViewHandle, error) {
	bgHex := "#FFFFFF"
	if dark {
		bgHex = "#1F2023"
	}
	wv, err := win.NewWebView(mygo.WebViewOptions{
		BackgroundColor:   bgHex,
		ScrollToContainer: scrollToContainer,
		Page:              mygo.PageOptions{Untrusted: true},
	})
	if err != nil {
		return nil, err
	}
	allowDir := filepath.Clean(filepath.Dir(fullPath))
	allowEval := allowDir
	if eval, evalErr := filepath.EvalSymlinks(allowDir); evalErr == nil {
		allowEval = filepath.Clean(eval)
	}
	h := &htmlWebViewHandle{
		win:        win,
		doc:        doc,
		wv:         wv,
		fullPath:   fullPath,
		rev:        rev,
		dark:       dark,
		allowDir:   allowDir,
		allowEval:  allowEval,
		pendingAnc: anchor,
		pendingIdx: -1,
	}
	pg := wv.Page()
	pg.SetWindowOpenHandler(func(req mygo.WindowOpenRequest) *mygo.WindowOptions {
		if req.UserInitiated && ui.SafeToOpen(req.URL) {
			mygo.Shell.OpenExternal(req.URL)
		}
		return nil
	})
	pg.OnWillNavigate(func(e *mygo.NavigateEvent) {
		u, parseErr := neturl.Parse(e.URL)
		if parseErr != nil {
			e.PreventDefault()
			return
		}
		scheme := strings.ToLower(u.Scheme)
		switch scheme {
		case "about", "blob", "data":
			return
		case "http", "https":
			e.PreventDefault()
			if e.UserInitiated && ui.SafeToOpen(e.URL) {
				mygo.Shell.OpenExternal(e.URL)
			}
			return
		case "file":
			p := u.Path
			if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
				p = p[1:]
			}
			target := filepath.Clean(filepath.FromSlash(p))
			if h.allowsLocalFile(target) {
				return
			}
			e.PreventDefault()
			return
		default:
			e.PreventDefault()
		}
	})
	pg.OnDidFinishLoad(func() {
		if h.destroyed.Load() {
			return
		}
		h.loaded.Store(true)
		h.mu.Lock()
		anc, idx := h.pendingAnc, h.pendingIdx
		h.pendingAnc, h.pendingIdx = "", -1
		h.mu.Unlock()
		if anc != "" || idx >= 0 {
			h.evalScrollJS(anc, idx)
		}
		win.Invalidate()
	})
	if err := pg.LoadFile(fullPath); err != nil {
		wv.Destroy()
		return nil, err
	}
	return h, nil
}

func (h *htmlWebViewHandle) allowsLocalFile(target string) bool {
	if isUnderDir(target, h.allowDir) || isUnderDir(target, h.allowEval) {
		return true
	}
	if eval, err := filepath.EvalSymlinks(target); err == nil {
		eval = filepath.Clean(eval)
		if isUnderDir(eval, h.allowDir) || isUnderDir(eval, h.allowEval) {
			return true
		}
	}
	return false
}

func isUnderDir(target, dir string) bool {
	if dir == "" || target == "" {
		return false
	}
	rel, err := filepath.Rel(dir, target)
	return err == nil && filepath.IsLocal(rel)
}

func (h *htmlWebViewHandle) Native() ui.NativeWebView {
	if h == nil || h.destroyed.Load() || h.wv == nil || h.wv.IsDestroyed() {
		return nil
	}
	return h.wv
}

func (h *htmlWebViewHandle) ScrollToAnchor(anchor string, headingIndex int) {
	if h == nil || h.destroyed.Load() || h.wv == nil || (anchor == "" && headingIndex < 0) {
		return
	}
	if !h.loaded.Load() {
		h.mu.Lock()
		h.pendingAnc = anchor
		h.pendingIdx = headingIndex
		h.mu.Unlock()
		return
	}
	h.evalScrollJS(anchor, headingIndex)
}

func (h *htmlWebViewHandle) evalScrollJS(anchor string, headingIndex int) {
	if h == nil || h.destroyed.Load() || h.wv == nil {
		return
	}
	ancJSON, _ := json.Marshal(anchor)
	js := fmt.Sprintf(`(() => {
		const a = %s;
		const hi = %d;
		let el = null;
		if (a) {
			el = document.getElementById(a);
			if (!el) {
				for (const n of document.querySelectorAll('[name]')) {
					if (n.getAttribute('name') === a) { el = n; break; }
				}
			}
		}
		if (!el && hi >= 0) {
			const hs = document.querySelectorAll('h1,h2,h3,h4');
			if (hi < hs.length) el = hs[hi];
		}
		if (el && el.scrollIntoView) {
			el.scrollIntoView({ behavior: 'smooth', block: 'start' });
		}
	})();`, string(ancJSON), headingIndex)
	pg := h.wv.Page()
	go func() {
		_, _ = pg.Eval(js)
	}()
}

func (h *htmlWebViewHandle) Find(query string, forward bool, findNext bool, onResult func(active, total int)) {
	if h == nil || h.destroyed.Load() || h.wv == nil || h.win == nil {
		return
	}
	seq := h.findSeq.Add(1)
	pg := h.wv.Page()
	win := h.win
	go func() {
		res, err := pg.FindInPage(query, mygo.FindOptions{
			Backward: !forward,
			FindNext: findNext,
		})
		if err != nil || h.destroyed.Load() || h.findSeq.Load() != seq {
			return
		}
		win.Update(func() {
			if onResult != nil && !h.destroyed.Load() && h.findSeq.Load() == seq {
				onResult(res.Active, res.Matches)
			}
		})
	}()
}

func (h *htmlWebViewHandle) StopFind() {
	if h == nil || h.destroyed.Load() || h.wv == nil {
		return
	}
	h.findSeq.Add(1)
	h.wv.Page().StopFindInPage()
}

func (h *htmlWebViewHandle) Reload() {
	if h == nil || h.destroyed.Load() || h.wv == nil {
		return
	}
	h.loaded.Store(false)
	allowDir := filepath.Clean(filepath.Dir(h.fullPath))
	h.allowDir = allowDir
	h.allowEval = allowDir
	if eval, err := filepath.EvalSymlinks(allowDir); err == nil {
		h.allowEval = filepath.Clean(eval)
	}
	_ = h.wv.Page().LoadFile(h.fullPath)
}

func (h *htmlWebViewHandle) Destroy() {
	if h == nil || !h.destroyed.CompareAndSwap(false, true) {
		return
	}
	h.findSeq.Add(1)
	if h.wv != nil && !h.wv.IsDestroyed() {
		h.wv.Destroy()
	}
}

func (a *App) hostWindowFor(d *document) *mygo.Window {
	if d != nil {
		for _, w := range a.windows {
			if w.doc == d {
				if mw, ok := w.win.(*mygo.Window); ok {
					return mw
				}
				return nil
			}
		}
	}
	return a.win
}

func (a *App) touchHTMLWebViewLocked(k htmlWebViewKey) {
	if idx := slices.Index(a.htmlViewOrder, k); idx >= 0 {
		a.htmlViewOrder = slices.Delete(a.htmlViewOrder, idx, idx+1)
	}
	a.htmlViewOrder = append(a.htmlViewOrder, k)
}

func (a *App) acquireHTMLWebView(win *mygo.Window, doc *document, slot, fullPath string, rev uint64, anchor string, scrollToContainer bool) editor.HTMLWebView {
	if a.makeHTMLWebView != nil {
		return a.makeHTMLWebView(win, fullPath, rev, anchor, scrollToContainer)
	}
	if win == nil || fullPath == "" {
		return nil
	}
	key := htmlWebViewKey{win: win, slot: slot}
	if prev, ok := a.htmlViews[key]; ok && prev != nil && !prev.destroyed.Load() && prev.wv != nil && !prev.wv.IsDestroyed() {
		prev.doc = doc
		if prev.dark != a.paperDark {
			prev.dark = a.paperDark
			bgHex := "#FFFFFF"
			if a.paperDark {
				bgHex = "#1F2023"
			}
			_ = prev.wv.SetBackgroundColor(bgHex)
		}
		if prev.fullPath != fullPath || prev.rev != rev {
			prev.fullPath = fullPath
			prev.rev = rev
			prev.mu.Lock()
			prev.pendingAnc = anchor
			prev.pendingIdx = -1
			prev.mu.Unlock()
			prev.Reload()
		}
		a.touchHTMLWebViewLocked(key)
		return prev
	}

	handle, err := newHTMLWebView(win, doc, fullPath, rev, anchor, scrollToContainer, a.paperDark)
	if err != nil {
		return nil
	}
	if a.htmlViews == nil {
		a.htmlViews = make(map[htmlWebViewKey]*htmlWebViewHandle, maxHTMLWebViewsPerWindow)
	}
	a.htmlViews[key] = handle
	a.touchHTMLWebViewLocked(key)

	// Bound live WebViews per window so memory stays small across large workspaces.
	count := 0
	for k := range a.htmlViews {
		if k.win == win {
			count++
		}
	}
	if count > maxHTMLWebViewsPerWindow {
		for i := 0; i < len(a.htmlViewOrder) && count > maxHTMLWebViewsPerWindow; {
			k := a.htmlViewOrder[i]
			if k.win != win || k == key {
				i++
				continue
			}
			a.htmlViewOrder = slices.Delete(a.htmlViewOrder, i, i+1)
			if old := a.htmlViews[k]; old != nil {
				old.Destroy()
				delete(a.htmlViews, k)
			}
			count--
		}
	}
	return handle
}

func (a *App) htmlWebViewForDoc(d *document) editor.HTMLWebView {
	if d == nil || d.space == nil || !d.isBook() || d.book == nil || !d.book.IsHTML() {
		return nil
	}
	full, err := resolveFile(d.space, d.rel)
	if err != nil {
		return nil
	}
	var rev uint64 = 1
	if d.bookModNano != 0 || d.bookSize != 0 {
		rev = uint64(d.bookModNano) ^ uint64(d.bookSize<<12)
		if rev == 0 {
			rev = 1
		}
	} else if info, stErr := os.Stat(full); stErr == nil {
		rev = uint64(info.ModTime().UnixNano()) ^ uint64(info.Size()<<12)
		if rev == 0 {
			rev = 1
		}
	}
	return a.acquireHTMLWebView(a.hostWindowFor(d), d, "doc:"+d.viewKey(), full, rev, "", false)
}

func (a *App) htmlWebViewForEmbed(d *document, rawURL, slot string, scrollToContainer bool) editor.HTMLWebView {
	if d == nil || d.space == nil || rawURL == "" {
		return nil
	}
	cleanURL, _, anchor := editor.ParseBookURLRef(rawURL)
	if cleanURL == "" || !editor.IsHTMLURL(cleanURL) {
		return nil
	}
	full, err := resolveSpaceImage(d.space, d.rel, cleanURL)
	if err != nil {
		return nil
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		return nil
	}
	rev := uint64(info.ModTime().UnixNano()) ^ uint64(info.Size()<<12)
	if rev == 0 {
		rev = 1
	}
	return a.acquireHTMLWebView(a.hostWindowFor(d), d, d.viewKey()+"|"+slot, full, rev, anchor, scrollToContainer)
}

func (a *App) destroyHTMLWebViewsForDoc(d *document) {
	if d == nil || len(a.htmlViews) == 0 {
		return
	}
	for k, h := range a.htmlViews {
		if h != nil && h.doc == d {
			h.Destroy()
			delete(a.htmlViews, k)
			if idx := slices.Index(a.htmlViewOrder, k); idx >= 0 {
				a.htmlViewOrder = slices.Delete(a.htmlViewOrder, idx, idx+1)
			}
		}
	}
}

func (a *App) destroyHTMLWebViewsForWindow(win *mygo.Window) {
	if win == nil || len(a.htmlViews) == 0 {
		return
	}
	for k, h := range a.htmlViews {
		if k.win == win {
			if h != nil {
				h.Destroy()
			}
			delete(a.htmlViews, k)
			if idx := slices.Index(a.htmlViewOrder, k); idx >= 0 {
				a.htmlViewOrder = slices.Delete(a.htmlViewOrder, idx, idx+1)
			}
		}
	}
}
