// Package app is ArcMD's interface: one native window, drawn by MyGo's
// package ui, in the manner of Arc and Zen — a sidebar of the active
// space's pinned documents and folders over the window's tinted material,
// a toolbar with the document's address, the document in a rounded card,
// a command bar, and the spaces at the sidebar's foot.
//
// The package holds state and draws it; files, spaces, plugins and the
// Markdown editing engine live in their own packages.
package app

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/history"
	"github.com/Ciao1019/ArcMD/internal/plugin"
	"github.com/Ciao1019/ArcMD/internal/prefs"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/trashbin"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/locales/zh-Hans"
	"github.com/egoist/mygo/ui"
)

// Version is ArcMD's version, which plugins' engines are checked against.
const Version = "0.0.9"

// Config says where ArcMD keeps its files.
type Config struct {
	// ConfigDir holds spaces.json and the plugins folder.
	ConfigDir string
	// DocumentsDir is where the folders of new spaces go by default.
	DocumentsDir string
}

// DefaultConfig returns the user's usual places: the system's
// configuration folder and ~/Documents/ArcMD.
func DefaultConfig() (Config, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return Config{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}
	return Config{
		ConfigDir:    filepath.Join(cfg, "ArcMD"),
		DocumentsDir: filepath.Join(home, "Documents", "ArcMD"),
	}, nil
}

// place is a document of a space, a step of the navigation history.
type place struct{ space, rel string }

// spaceSession keeps each space's last editor and sidebar scroll position
// while the window is open. The Markdown file remains authoritative.
type spaceSession struct {
	// doc is the document the space showed when it was left: its own, or
	// a favorite of another space, which it shows again on coming back.
	doc        *document
	tree       ui.ScrollState
	split      *splitGroup
	splitShown bool
}

// App is ArcMD's state.
type App struct {
	// bookmarkGen counts changes to the bookmarks, which documents resolve
	// again (bookmarks.go).
	bookmarkGen uint64

	cfg     Config
	win     *mygo.Window
	store   *space.Store
	state   *space.State
	plugins *plugin.Manager

	installed     []*plugin.Installed
	brokenPlugins []plugin.Broken

	// tree and docs are the folders and documents of treeFor, the space
	// they were read for; docs lists the documents in tree order.
	tree    []*vault.Node
	docs    []*vault.Node
	treeFor string
	treeErr error

	doc *document
	// split owns the visible panes; doc and history follow its focused pane.
	split           *splitGroup
	paneNavigation  bool
	documentContext int
	splitResizing   bool
	sessions        map[string]*spaceSession
	// openDocs are the pinned documents kept open behind the one shown,
	// and bounce the row springing from a close (opendocs.go).
	openDocs map[place]*document
	bounce   bounce
	// overClose is the row whose minus is under the pointer this frame,
	// overClosed the one the frame before; it shows no preview.
	overClose, overClosed place
	spaceScroll           ui.ScrollState
	spaceBarFor           string
	// history is the documents shown, as a browser's; at is the one
	// shown now.
	history []place
	at      int

	// sidebarHidden folds the sidebar away with ⌘\; it peeks out
	// over the content while the pointer is at the window's edge.
	sidebarHidden bool
	peek          bool
	peekLeave     time.Time
	// renaming is the path of the file or folder whose name is being
	// edited in the sidebar, and newName the name typed.
	renaming      string
	newName       string
	renameFocused bool
	// renameSpace is the ID of the space holding the item being renamed.
	renameSpace string

	palette palette
	// pager is the strip of spaces the sidebar pages along (spaceswitch.go),
	// and pageTrees the folders of the spaces it shows beside the active
	// one; now and reduceMotion are this frame's clock and the system's
	// wish for less motion.
	pager        pager
	pageTrees    map[string]pageTree
	now          time.Time
	reduceMotion bool
	// listDrop is where the file or folder dragged over the sidebar's list
	// would land (treedrag.go), and favDrop the slot the favorites hold
	// open for one dragged over them (favorites.go).
	listDrop   *listDrop
	springOpen springOpen
	// spaceSpring is the space icon a drag rests on (crossspace.go);
	// carrying is the row seen dragged this frame and carried the one the
	// frame before, which outlive the row's element when its space pages
	// away.
	spaceSpring spaceSpring
	carrying    *dragged
	carried     *dragged
	// listDragging is whether something is dragged over the list, which
	// opens the empty pinned section's placeholder; dividerY is where the
	// line between the pinned and the rest was.
	listDragging bool
	dividerY     float32
	rows         []rowFrame
	favDrop      *favDrop
	// preview is the card a folder or a document shows when the pointer
	// rests on it (preview.go).
	preview preview
	// overview shows every space at once, as Zen's.
	library   library
	spaceForm spaceForm
	// settingsOpen shows the settings window on settingsTab.
	settingsOpen bool
	settingsTab  string
	// look is the window's look this frame, the two spaces either side
	// mixed while paging; docTheme the system's theme the document's card
	// keeps. paperDark is the active space's light or dark, which the
	// paper takes: whole, as the document shown is the active space's,
	// rather than flipping halfway through every space paged past.
	look      spaceLook
	docTheme  *ui.Theme
	paperDark bool
	// pickFiles, when set, asks for pictures in place of the system's
	// dialog: tests answer it.
	pickFiles func(done func(paths []string))
	// revealFile substitutes for the platform file manager in image tests.
	revealFile func(path string)
	// notices are messages for the user, shown as toasts by the next frame.
	notices []string
	// watchFiles follows a space's folder (watch.go), and watches are the
	// folders followed, by space ID.
	watchFiles watchFunc
	watches    map[string]folderWatch
	// search is the command bar's full-text search (search.go).
	search searchState
	// prefs are the preferences beyond the spaces (prefs.json); prefsBroken
	// keeps a file this ArcMD cannot read from being overwritten.
	prefStore   prefs.Store
	prefs       *prefs.Prefs
	prefsBroken bool
	// recording is the command whose new key the settings wait for.
	recording   string
	recordFocus bool
	// focus is focus mode; narrow whether the window is narrow enough to
	// fold the sidebar away, narrowShown whether it is shown anyway
	// (focus.go).
	focus, narrow, narrowShown bool
	// versions is the history window (versions.go); bin keeps what was
	// deleted recently, which the library lists (trash.go).
	versions versionsState
	bin      trashbin.Bin
	// printFile, when set, takes the PDF to print in place of the system's
	// viewer: tests answer it.
	pdfExport *pdfExport
	printFile func(path string)
	// trashFile, when set, moves a file to the trash in place of the
	// system: tests answer it.
	trashFile func(path string) error
	// updates are the software updates (update.go).
	updates updateState
	// tour introduces the sidebar on the first run (tour.go).
	tour tour
}

// trashItem moves the file or folder at p to the system's trash.
func (a *App) trashItem(p string) error {
	if a.trashFile != nil {
		return a.trashFile(p)
	}
	return mygo.Shell.TrashItem(p)
}

// New loads ArcMD's state, making the first space and its welcome
// document on the first run.
func New(cfg Config) (*App, error) {
	a := &App{
		cfg:       cfg,
		store:     space.NewStore(filepath.Join(cfg.ConfigDir, "spaces.json")),
		plugins:   plugin.NewManager(filepath.Join(cfg.ConfigDir, "plugins")),
		sessions:  make(map[string]*spaceSession),
		openDocs:  make(map[place]*document),
		prefStore: prefs.Store{Path: filepath.Join(cfg.ConfigDir, "prefs.json")},
		bin:       trashbin.Bin{Dir: filepath.Join(cfg.ConfigDir, "trash")},
		// The spaces fade in on coming back from the library, not at start.
		library: library{frames: 1},
	}
	if p, err := a.prefStore.Load(); err == nil {
		a.prefs = p
	} else {
		a.prefs, a.prefsBroken = &prefs.Prefs{Version: prefs.CurrentVersion}, true
		a.notify("读取偏好设置失败，本次使用默认设置且不会覆盖原文件。%v", err)
	}
	st, err := a.store.Load()
	if err != nil {
		return nil, err
	}
	a.state = st
	if len(st.Spaces) == 0 {
		if err := a.firstRun(); err != nil {
			return nil, err
		}
	}
	a.startUpdates()
	a.reloadPlugins()
	a.syncTree()
	if sp := a.state.ActiveSpace(); sp != nil && sp.LastOpen != "" {
		a.open(sp.LastOpen)
	}
	a.restoreSplits()
	// The tour shows until it was seen once. Preferences that cannot be
	// saved could not remember that, and would show it at every launch.
	if !a.prefs.TourSeen && !a.prefsBroken {
		a.startTour()
	}
	return a, nil
}

// firstRun makes the Personal space with a welcome document.
func (a *App) firstRun() error {
	root := filepath.Join(a.cfg.DocumentsDir, "Personal")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	sp := a.state.Add("Personal", root)
	if docs, _ := vault.List(root); len(docs) == 0 {
		if err := writeNative(root, "Welcome.arcmd", welcome); err != nil {
			return err
		}
		if err := writeNative(root, "Journal/Ideas.arcmd", ideas); err != nil {
			return err
		}
		sp.LastOpen = "Welcome.arcmd"
		a.state.ToggleFavorite(sp.ID, "Welcome.arcmd")
		sp.Expanded = []string{"Journal"}
	}
	return a.store.Save(a.state)
}

// Run opens the window and runs the app until it quits.
func (a *App) Run() error {
	mygo.App.SetPath(mygo.PathUserData, a.cfg.ConfigDir)
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}
	var pending []string
	if !mygo.App.IsPackaged() {
		wd, _ := os.Getwd()
		pending = documentArguments(os.Args[1:], wd)
	}
	open := func(file string) {
		if a.openSystemFile(file) {
			a.win.Show()
			a.win.Focus()
			_ = mygo.App.AddRecentDocument(file)
		}
	}
	mygo.App.OnOpenFile(func(file string) {
		if a.win == nil {
			pending = append(pending, file)
			return
		}
		a.win.Update(func() { open(file) })
	})
	mygo.App.OnSecondInstance(func(args []string, workingDir string) {
		if !mygo.App.IsPackaged() {
			// MyGo forwards os.Args[1:], so the first argument is a file.
			for _, file := range documentArguments(args, workingDir) {
				if a.win == nil {
					pending = append(pending, file)
				} else {
					a.win.Update(func() { open(file) })
				}
			}
		}
		if a.win != nil {
			a.win.Show()
			a.win.Focus()
		}
	})
	mygo.App.SetLanguages("zh-Hans")
	mygo.App.WhenReady(func() {
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			a.appMenu(),
			{Role: mygo.RoleFileMenu},
			{Role: mygo.RoleEditMenu},
			{Role: mygo.RoleFormatMenu},
			{Role: mygo.RoleViewMenu},
			{Role: mygo.RoleWindow, Submenu: []*mygo.MenuItem{
				{Role: mygo.RoleMinimize},
				{Role: mygo.RoleZoom},
				mygo.Separator(),
				{Label: "下一个空间", Accelerator: "CmdOrCtrl+Alt+Right", Click: func(*mygo.MenuItem, *mygo.Window) {
					a.win.Update(func() { a.stepSpace(1) })
				}},
				{Label: "上一个空间", Accelerator: "CmdOrCtrl+Alt+Left", Click: func(*mygo.MenuItem, *mygo.Window) {
					a.win.Update(func() { a.stepSpace(-1) })
				}},
				mygo.Separator(),
				{Role: mygo.RoleFront},
			}},
		}))
		a.watchFiles = a.watchFolder
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:                "ArcMD",
			Width:                1240,
			Height:               800,
			MinWidth:             640,
			MinHeight:            420,
			StateKey:             "main",
			TitleBarStyle:        mygo.TitleBarHidden,
			TrafficLightPosition: &mygo.Point{X: 18, Y: 20},
			Vibrancy:             mygo.VibrancySidebar,
			Content:              a.Content(),
		})
		if len(pending) > 0 {
			files := pending
			pending = nil
			a.win.Update(func() {
				for _, file := range files {
					open(file)
				}
			})
		}
		// Index the documents now, on a goroutine, so that the first search
		// finds text at once.
		a.search.async = true
		a.refreshSearch()
		a.updates.svc, a.updates.async = mygoUpdates{}, true
		a.win.OnClose(func(event *mygo.CloseEvent) {
			if !a.flush() {
				event.PreventDefault()
			} else {
				a.cancelPDF()
				a.finishSpaceEdit()
				if a.captureSplits() {
					a.saveState()
				}
			}
		})
	})
	return mygo.App.Run()
}

// appMenu is the application menu, with "检查更新…" after About on macOS
// as Sparkle's; the other systems keep their own, and reach the updates
// from the ⋯ menu and the command bar.
func (a *App) appMenu() *mygo.MenuItem {
	if runtime.GOOS != "darwin" {
		return &mygo.MenuItem{Role: mygo.RoleAppMenu}
	}
	return &mygo.MenuItem{Label: "ArcMD", Submenu: []*mygo.MenuItem{
		{Role: mygo.RoleAbout},
		{Label: "检查更新…", Click: func(*mygo.MenuItem, *mygo.Window) {
			a.win.Update(a.checkForUpdates)
		}},
		mygo.Separator(),
		{Label: "设置…", Click: func(*mygo.MenuItem, *mygo.Window) {
			a.win.Update(func() { a.openSettings(tabGeneral) })
		}},
		mygo.Separator(),
		{Role: mygo.RoleServices},
		mygo.Separator(),
		{Role: mygo.RoleHide},
		{Role: mygo.RoleHideOthers},
		{Role: mygo.RoleUnhide},
		mygo.Separator(),
		{Role: mygo.RoleQuit},
	}}
}

// notify queues a message for the user.
func (a *App) notify(format string, args ...any) {
	a.notices = append(a.notices, fmt.Sprintf(format, args...))
}

// savePrefs writes prefs.json, telling the user when it fails.
func (a *App) savePrefs() {
	if a.prefsBroken {
		a.notify("偏好设置文件无法读取，修改不会保存。")
		return
	}
	if err := a.prefStore.Save(a.prefs); err != nil {
		a.notify("保存偏好设置失败。%v", err)
	}
}

// saveState writes spaces.json, telling the user when it fails.
func (a *App) saveState() {
	a.captureSplits()
	if err := a.store.Save(a.state); err != nil {
		a.notify("保存空间设置失败。%v", err)
	}
}

// flush saves the open document now and reports whether it is safe to leave.
func (a *App) flush() bool {
	for _, d := range a.residentDocuments() {
		ok := false
		a.withDocument(d, func() { ok = a.flushCurrent() })
		if !ok {
			a.showSaveFailure(d)
			return false
		}
	}
	return true
}

func (a *App) flushCurrent() bool {
	if a.doc == nil || a.doc.loadErr != nil {
		return true
	}
	a.doc.ed.UnmarkText()
	if err := a.doc.save(); err != nil {
		if errors.Is(err, errConflict) || errors.Is(err, errMissing) {
			a.notify("「%s」%v，请先在文档上方选择保留哪个版本。", a.doc.title(), err)
		} else {
			a.notify("保存 %s 失败。%v", a.doc.title(), err)
		}
		return false
	}
	a.followMigration()
	a.followTitle()
	return true
}

func (a *App) session(id string) *spaceSession {
	s := a.sessions[id]
	if s == nil {
		s = &spaceSession{}
		a.sessions[id] = s
	}
	return s
}

// refreshTree reads the active space's folder again.
func (a *App) refreshTree() {
	a.treeFor = ""
	a.syncTree()
}

// syncTree reads the active space's folder when the space changed.
func (a *App) syncTree() {
	sp := a.state.ActiveSpace()
	if sp == nil || a.treeFor == sp.ID {
		return
	}
	a.treeFor = sp.ID
	a.tree, a.treeErr = readTree(sp)
	if a.treeErr == nil {
		// Pins of what is gone go with it.
		kept := pinnedNodes(sp, a.tree)
		if len(kept) != len(sp.Pins) {
			sp.Pins = sp.Pins[:0]
			for _, n := range kept {
				sp.Pins = append(sp.Pins, n.Path)
			}
			a.saveState()
		}
	}
	a.docs = a.docs[:0]
	var walk func([]*vault.Node)
	walk = func(nodes []*vault.Node) {
		for _, n := range nodes {
			if n.Dir {
				walk(n.Children)
			} else {
				a.docs = append(a.docs, n)
			}
		}
	}
	walk(a.tree)
}

// open shows the document at rel of the active space, adding it to the
// history.
func (a *App) open(rel string) { a.openIn(a.state.ActiveSpace(), rel) }

// openFavorite shows a favorite where it is, without leaving the active
// space, as Arc opens a favorite in any space.
func (a *App) openFavorite(f space.Favorite) {
	if sp := a.state.Get(f.Space); sp != nil && a.canEnter(sp.Root) {
		a.openIn(sp, f.Path)
	}
}

// openIn shows the document at rel of the space sp, adding it to the
// history.
func (a *App) openIn(sp *space.Space, rel string) {
	if sp != nil && a.showIn(sp, rel) {
		p := place{sp.ID, rel}
		if a.at < len(a.history) && a.history[a.at] == p {
			return
		}
		if len(a.history) > 0 {
			a.history = a.history[:a.at+1]
		}
		a.history = append(a.history, p)
		a.at = len(a.history) - 1
	}
}

// show shows the document at rel of the active space, saving the one
// shown before, and reports whether it could.
func (a *App) show(rel string) bool { return a.showIn(a.state.ActiveSpace(), rel) }

// showIn shows the document at rel of the space sp: the active one, or a
// favorite's.
func (a *App) showIn(sp *space.Space, rel string) bool {
	if sp == nil {
		return false
	}
	if a.paneNavigation && a.split != nil {
		a.syncSplit()
		for i, p := range a.split.panes {
			if p.doc != nil && p.doc.space == sp && p.doc.rel == rel {
				a.focusPane(i, true)
				return true
			}
		}
	}
	if !a.paneNavigation {
		if group := a.session(a.state.ActiveSpace().ID).split; group != nil {
			a.syncSplit()
			for i, p := range group.panes {
				if p.doc != nil && p.doc.space == sp && p.doc.rel == rel {
					if a.split != group {
						return a.restoreSplit(group, i)
					}
					a.focusPane(i, false)
					return true
				}
			}
		}
	}
	active := sp == a.state.ActiveSpace()
	if a.doc != nil && a.doc.space == sp && a.doc.rel == rel {
		return true
	}
	restoring := a.doc == nil && sp.LastOpen == rel
	text, err := vault.Read(sp.Root, rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			a.state.Forget(sp.ID, rel)
			a.saveState()
			a.refreshTree()
		} else {
			a.notify("打开 %s 失败。%v", vault.Title(rel), err)
		}
		return false
	}
	if !a.flush() {
		return false
	}
	if a.split != nil && a.doc != nil && !a.paneNavigation {
		a.suspendSplit()
	}
	a.syncOpen() // the document left, if pinned, stays open
	var cached *document
	if restoring && active {
		cached = a.session(sp.ID).doc
	} else if a.doc == nil && !active {
		// A favorite of another space the active space had open when it
		// was left, as it was.
		if d := a.session(a.state.ActiveSpace().ID).doc; d != nil && d.space == sp {
			cached = d
		}
	}
	if resident := a.residentDocument(sp.ID, rel); resident != nil {
		if !resident.dirty() && resident.diskText != text {
			replacement := openDocument(sp, rel, text, resident.source)
			replacement.ed.Scroll = resident.ed.Scroll
			a.replaceReferences(resident, replacement)
			resident = replacement
		}
		a.doc = resident
	} else if kept := a.keptDoc(sp, rel, text); kept != nil {
		a.doc = kept // a pinned document kept open, as it was left
	} else if cached != nil && cached.rel == rel && cached.diskText == text {
		a.doc = cached
	} else {
		preferSource := cached != nil && cached.rel == rel && cached.source
		a.doc = openDocument(sp, rel, text, preferSource)
	}
	if a.paneNavigation {
		a.doc.ed.RequestFocus = true
	}
	if a.search.async {
		// The backlinks below the document read the index.
		a.refreshSearch()
	}
	if !active {
		return true // a favorite of another space leaves that space as it was
	}
	if !restoring {
		a.reveal(rel)
	}
	if sp.LastOpen != rel {
		sp.LastOpen = rel
		a.saveState()
	}
	return true
}

// reveal opens the folders holding rel in the sidebar.
func (a *App) reveal(rel string) {
	sp := a.state.ActiveSpace()
	changed := false
	for dir := path.Dir(rel); dir != "." && dir != "/"; dir = path.Dir(dir) {
		if !sp.IsExpanded(dir) {
			sp.SetExpanded(dir, true)
			changed = true
		}
	}
	if changed {
		a.saveState()
	}
}

// canGo reports whether the history has a document step steps away.
func (a *App) canGo(step int) bool {
	i := a.at + step
	return i >= 0 && i < len(a.history)
}

// goHistory goes back (-1) or forward (+1) in the history, switching space
// where the document is in another.
func (a *App) goHistory(step int) {
	if a.split != nil {
		a.paneNavigation = true
		defer func() { a.paneNavigation = false; a.syncSplit() }()
	}
	if !a.flush() {
		return
	}
	for a.canGo(step) {
		a.at += step
		p := a.history[a.at]
		if a.state.Get(p.space) == nil {
			continue
		}
		sp := a.state.Get(p.space)
		if a.state.IsFavorite(p.space, p.rel) {
			// A favorite opens where the window is, as from its tile.
			if a.showIn(sp, p.rel) {
				return
			}
			continue
		}
		if a.split != nil {
			if a.showIn(sp, p.rel) {
				return
			}
			continue
		}
		a.switchSpaceOnly(p.space)
		if a.show(p.rel) {
			return
		}
	}
}

// reload reads the open document from its file again, after saving it.
func (a *App) reload() {
	if a.doc == nil {
		return
	}
	if a.doc.conflict || a.doc.missing {
		// Reloading is what the user asks for: the file wins.
		a.useDisk()
		return
	}
	if !a.flush() {
		return
	}
	rel, source := a.doc.rel, a.doc.source
	text, err := vault.Read(a.doc.space.Root, rel)
	if err != nil {
		a.notify("重新载入 %s 失败。%v", vault.Title(rel), err)
		return
	}
	a.replaceReferences(a.doc, openDocument(a.doc.space, rel, text, source))
	a.refreshTree()
}

// toggleSource switches the open document between live preview and source.
func (a *App) toggleSource() {
	if a.doc == nil {
		return
	}
	a.doc.toggleMode()
}

// newDoc makes a document in the folder dir of the active space and
// opens it.
func (a *App) newDoc(dir string) {
	sp := a.state.ActiveSpace()
	if sp == nil {
		return
	}
	// An empty title to type, its placeholder showing, and the starter
	// below; the file is named for the title once it has one.
	doc := model.New()
	title := model.NewBlock("heading", "")
	title.Level = 1
	doc.Blocks = []*model.Block{title, model.NewBlock("paragraph", "")}
	content, _ := doc.Encode()
	rel, err := vault.CreateFile(sp.Root, dir, untitled, ".arcmd", content)
	if err != nil {
		a.notify("新建文档失败。%v", err)
		return
	}
	if dir != "" {
		sp.SetExpanded(dir, true)
	}
	a.refreshTree()
	a.open(rel)
	if d := a.doc; d != nil && d.rel == rel && d.loadErr == nil {
		d.ed.SetSelection(0, 0)
		d.ed.RequestFocus = true
	}
}

// untitled is a new document's file name until its title names it.
const untitled = "未命名"

// isUntitled reports whether a document's name is one newDoc gives:
// 未命名, or 未命名 numbered when that was taken.
func isUntitled(name string) bool {
	rest, ok := strings.CutPrefix(name, untitled)
	if !ok {
		return false
	}
	if rest == "" {
		return true
	}
	n, ok := strings.CutPrefix(rest, " ")
	if !ok || n == "" {
		return false
	}
	for _, r := range n {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// followTitle names the open document's file for its title once it is
// saved, while the file is still untitled or has the name this did give
// it, so that a new page is named by writing its title and a page renamed
// by hand keeps its name. A title taken by another file is numbered.
func (a *App) followTitle() {
	d := a.doc
	if d == nil || d.loadErr != nil || d.conflict || d.missing || !strings.EqualFold(path.Ext(d.rel), ".arcmd") || d.dirty() {
		return
	}
	if _, composing := d.ed.MarkedRange(); composing {
		return // renaming now would end the input method's composition
	}
	name := vault.Title(d.rel)
	if !isUntitled(name) && name != d.named {
		return
	}
	title := docTitle(d.ed.Doc)
	if title == "" || title == name || title == d.namedFor && name == d.named {
		return // nothing to do, or done for this title already
	}
	sp := d.space
	for i := 1; i < 100; i++ {
		want := title
		if i > 1 {
			want = fmt.Sprintf("%s %d", title, i)
		}
		sources := a.renameSources(sp, d.rel)
		to, err := vault.Rename(sp.Root, d.rel, want)
		if errors.Is(err, vault.ErrExists) {
			continue
		}
		if err != nil {
			// A name the file system refuses: keep the file's, and do not
			// try this title again.
			d.named, d.namedFor = name, title
			return
		}
		focus := d.ed.Focused()
		from := d.rel
		d.named, d.namedFor = vault.Title(to), title
		if to != from {
			a.moved(sp, from, to)
			if !isUntitled(name) {
				a.relink(name, vault.Title(to), sources)
			}
		}
		d.ed.RequestFocus = focus
		return
	}
}

// docTitle is the text of a document's leading level 1 heading, on one
// line and at most 60 characters, as a file name can hold it.
func docTitle(doc *model.Document) string {
	if len(doc.Blocks) == 0 || doc.Blocks[0].Kind != "heading" || doc.Blocks[0].Level != 1 {
		return ""
	}
	title := strings.Join(strings.Fields(doc.Blocks[0].Text()), " ")
	if r := []rune(title); len(r) > 60 {
		title = strings.TrimSpace(string(r[:60]))
	}
	return strings.Trim(title, ". ")
}

// newFolder makes a folder in dir and starts naming it.
func (a *App) newFolder(dir string) {
	sp := a.state.ActiveSpace()
	rel, err := vault.Mkdir(sp.Root, dir, "新建文件夹")
	if err != nil {
		a.notify("新建文件夹失败。%v", err)
		return
	}
	if dir != "" {
		sp.SetExpanded(dir, true)
		a.saveState()
	}
	a.refreshTree()
	a.startRename(rel)
}

// startRename edits the name of the file or folder at rel of the active
// space in the sidebar.
func (a *App) startRename(rel string) {
	if sp := a.state.ActiveSpace(); sp != nil {
		a.renameIn(sp, rel)
	}
}

// renameIn edits the name of the file or folder at rel of the space sp: in
// its row, or under its tile for a favorite, which may be another space's.
func (a *App) renameIn(sp *space.Space, rel string) {
	a.renaming, a.renameSpace, a.renameFocused = rel, sp.ID, false
	a.newName = path.Base(rel)
	if vault.IsDocument(rel) {
		a.newName = vault.Title(rel)
	}
}

// commitRename gives the file or folder being renamed the name typed.
func (a *App) commitRename() {
	rel, name := a.renaming, strings.TrimSpace(a.newName)
	a.renaming = ""
	if rel == "" || name == "" {
		return
	}
	sp := a.state.Get(a.renameSpace)
	if sp == nil || !a.flush() {
		return
	}
	sources := a.renameSources(sp, rel)
	to, err := vault.Rename(sp.Root, rel, name)
	if err != nil {
		a.notify("重命名 %s 失败。%v", path.Base(rel), err)
		return
	}
	if to != rel {
		a.moved(sp, rel, to)
		if vault.IsDocument(rel) {
			a.relink(vault.Title(rel), vault.Title(to), sources)
		}
	}
}

// moveInto moves the file or folder at rel into the folder dir.
func (a *App) moveInto(rel, dir string) {
	if path.Dir(rel) == dir || dir == "." && !strings.Contains(rel, "/") {
		return
	}
	sp := a.state.ActiveSpace()
	if !a.flush() {
		return
	}
	to, _, err := a.moveWithResources(sp, rel, sp, dir)
	if err != nil {
		a.notify("移动 %s 失败。%v", path.Base(rel), err)
		return
	}
	if dir != "" {
		sp.SetExpanded(dir, true)
	}
	a.moved(sp, rel, to)
}

// moved follows a file or folder of the space sp that moved from one path
// to another.
func (a *App) moved(sp *space.Space, from, to string) {
	_ = history.Move(sp.Root, from, to)
	a.state.Renamed(sp.ID, from, to)
	a.followOpen(sp.ID, from, sp.ID, to)
	a.followSplitPaths(sp.ID, from, sp.ID, to)
	a.saveState()
	a.refreshTree()
}

// switchSpaceOnly makes the space with the ID active, without opening a
// document.
func (a *App) switchSpaceOnly(id string) bool {
	if cur := a.state.ActiveSpace(); cur != nil && cur.ID == id {
		return false
	}
	next := a.state.Get(id)
	if next == nil || !a.canEnter(next.Root) || !a.flush() {
		return false
	}
	a.finishSpaceEdit()
	from := a.pagerShown(a.now)
	if cur := a.state.ActiveSpace(); cur != nil {
		a.session(cur.ID).doc = a.doc
		a.session(cur.ID).splitShown = a.split != nil
	}
	a.suspendSplit()
	a.state.SetActive(id)
	a.saveState()
	a.syncOpen()
	a.doc = nil
	a.renaming = ""
	a.syncTree()
	a.pager.swipe = 0
	a.pagerSettle(a.spaceIndex(id), from)
	delete(a.pageTrees, id) // the active space reads its own
	return true
}

// switchSpace makes the space with the ID active and opens what it showed
// when it was left: a favorite of another space it had open, or else its
// last document. Each space keeps its own, as Arc's spaces keep their
// tabs, so a favorite opened in one does not follow into the next.
func (a *App) switchSpace(id string) {
	if !a.switchSpaceOnly(id) || a.doc != nil {
		return
	}
	sp := a.state.ActiveSpace()
	if session := a.session(id); session.splitShown && session.split != nil {
		a.restoreSplit(session.split, session.split.active)
		return
	}
	if d := a.session(id).doc; d != nil && d.space != sp && a.state.IsFavorite(d.space.ID, d.rel) {
		if a.showIn(d.space, d.rel) {
			return
		}
	}
	if sp.LastOpen != "" {
		a.open(sp.LastOpen)
	}
}

// switchSpaceAt switches to the space at index i, for Cmd+1…9.
func (a *App) switchSpaceAt(i int) {
	if i >= 0 && i < len(a.state.Spaces) {
		a.switchSpace(a.state.Spaces[i].ID)
	}
}

// toggleFavorite adds the document at rel of the space sp to the
// favorites, out of the space's pins, or removes it.
func (a *App) toggleFavorite(sp *space.Space, rel string) {
	if sp != nil {
		a.state.ToggleFavorite(sp.ID, rel)
		if a.state.IsFavorite(sp.ID, rel) {
			sp.Unpin(rel)
		}
		a.saveState()
	}
}

// togglePin pins the document or folder at rel at the end of the space's
// pinned section, out of the favorites, or unpins it.
func (a *App) togglePin(sp *space.Space, rel string) {
	if sp == nil {
		return
	}
	if sp.IsPinned(rel) {
		sp.Unpin(rel)
		a.saveState()
		return
	}
	a.pinIn(sp, rel, len(pinnedNodes(sp, a.tree)))
}

// newFolderWith makes a folder beside the document at rel, moves the
// document into it, and starts naming the folder, as Cra's "New Folder with
// Tab".
func (a *App) newFolderWith(rel string) {
	sp := a.state.ActiveSpace()
	dir := parentDir(rel)
	folder, err := vault.Mkdir(sp.Root, dir, "新建文件夹")
	if err != nil {
		a.notify("新建文件夹失败。%v", err)
		return
	}
	a.refreshTree()
	if sp.IsPinned(rel) {
		sp.Unpin(rel)
	}
	a.placeEntry(rel, folder, 0)
	sp.SetExpanded(folder, true)
	a.saveState()
	a.startRename(folder)
}

// toggleFolder opens or closes a folder in the sidebar.
func (a *App) toggleFolder(rel string) {
	sp := a.state.ActiveSpace()
	sp.SetExpanded(rel, !sp.IsExpanded(rel))
	a.saveState()
}

// trash moves the file or folder at rel of the active space to the
// system's trash, which never deletes it for good.
func (a *App) trash(rel string) {
	sp := a.state.ActiveSpace()
	p, err := vault.Resolve(sp.Root, rel)
	if err != nil {
		return
	}
	if !a.flush() {
		return
	}
	kept, ok := a.keepDeleted(sp.ID, sp.Name, sp.Root, rel)
	if err := a.trashItem(p); err != nil {
		if ok {
			a.bin.Forget(kept)
		}
		a.notify("将 %s 移到废纸篓失败。%v", path.Base(rel), err)
		return
	}
	for _, d := range a.residentDocuments() {
		if d.space == sp && within(d.rel, rel) {
			a.replaceReferences(d, nil)
		}
	}
	a.state.Forget(sp.ID, rel)
	a.saveState()
	a.refreshTree()
}

// revealInFinder shows the file or folder at rel of the active space in
// the file manager.
func (a *App) revealInFinder(rel string) { a.revealIn(a.state.ActiveSpace(), rel) }

// revealIn shows the file or folder at rel of the space sp in the file
// manager.
func (a *App) revealIn(sp *space.Space, rel string) {
	if p, err := vault.Resolve(sp.Root, rel); err == nil {
		mygo.Shell.ShowItemInFolder(p)
	}
}

// reloadPlugins scans the plugins folder again.
func (a *App) reloadPlugins() {
	var err error
	a.installed, a.brokenPlugins, err = a.plugins.Scan()
	if err != nil {
		a.notify("读取插件文件夹失败。%v", err)
	}
}

// defaultRoot returns the folder a new space named name gets by default.
func (a *App) defaultRoot(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" {
		name = "空间"
	}
	return filepath.Join(a.cfg.DocumentsDir, name)
}

const welcome = `# Welcome to ArcMD

ArcMD is a calm place for writing, in the spirit of Arc and Zen. Documents save as structured .arcmd files. Markdown is available for import and export. Use the formatting bar or type ` + "`#`" + ` and a space for a heading, ` + "`-`" + ` and a space for a list, ` + "`**bold**`" + ` for **bold**.

- **Spaces** keep separate worlds apart: each is a folder of local documents with its own color. Switch with the icons at the foot of the sidebar, or ⌘1…⌘9.
- **The command bar** (⌘K) finds documents and runs commands; click the address above to open it.
- **⌘N** makes a document, **⌘\\** folds the sidebar away (point at the window's left edge to peek at it), **⇧⌘M** opens the Markdown export preview.
- [ ] Drag a document onto a folder to move it
- [ ] Double-click a name to rename it

## Plugins

AI and other powers will come from the plugin marketplace. ArcMD's core stays small.
`

const ideas = `# Ideas

Folders open and close in the sidebar, as Arc's do.
`

func writeNative(root, rel, markdown string) error {
	text, err := editor.ImportMarkdown(markdown).Encode()
	if err != nil {
		return err
	}
	return vault.Write(root, rel, text)
}
func (a *App) followMigration() {
	if a.doc == nil || a.doc.migratedFrom == "" {
		return
	}
	from := a.doc.migratedFrom
	a.doc.migratedFrom = ""
	focus := a.doc.ed.Focused()
	a.moved(a.doc.space, from, a.doc.rel)
	a.doc.ed.RequestFocus = focus
}
func (a *App) exportMarkdown() {
	if a.doc == nil || a.doc.loadErr != nil {
		return
	}
	a.doc.ed.UnmarkText()
	rel, err := vault.CreateFile(a.doc.space.Root, path.Dir(a.doc.rel), a.doc.title()+" Export", ".md", a.doc.text())
	if err != nil {
		a.notify("导出失败。%v", err)
		return
	}
	a.refreshTree()
	a.notify("已导出 %s", rel)
}
