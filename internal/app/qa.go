package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/agent"
	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/pdfdoc"
	"github.com/Ciao1019/ArcMD/internal/spacefs"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// Questions about a space (Q&A): the panel right of the document asks a
// model about the active space's documents. The model explores them with
// read-only tools (package spacefs) the way a coding agent explores a
// repository, and answers citing the lines it read; a citation opens the
// document at that block. There is no vector index.
//
// Each space keeps its own conversation for the session. A question runs
// on a goroutine bound to the space it was asked in; its events come back
// to the main thread through win.Update.

// apiKeySecret is the API key's filename in the local credential store.
const apiKeySecret = "ai-api-key"

// secretStore is the local credential store; tests can use a map.
type secretStore interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte) error
	Delete(key string) error
}

type qaState struct {
	open  bool
	input string
	// sessions are the conversations by space ID.
	sessions map[string]*qaSession
	// spaces are the read-only views of the spaces, by ID, kept so their
	// rendered pages are reused between questions.
	spaces map[string]*spacefs.FS
	// focusInput puts the caret in the question field next frame.
	focusInput bool
	// shown is the panel's width as last drawn, sliding open or closed.
	shown    float32
	resizing bool

	secrets secretStore
	// key is the API key read from the store, keyRead once it was asked.
	key     string
	keyRead bool
	keyErr  error
	// keyInput is the settings' field; a saved key is never shown.
	keyInput string

	// models are the service's models, listed for modelsFor (its address
	// and key) so a change of either discards the list; another list is
	// fetched only on request. manualModel types a name instead of choosing one.
	models      []string
	modelsFor   string
	modelsBusy  bool
	modelsErr   error
	manualModel bool
	// listModels lists a service's models; tests replace it.
	listModels func(agent.Settings) ([]string, error)

	// provider makes the model service of the settings; tests replace it.
	provider func(agent.Settings) agent.Provider
	// sync runs questions on the calling goroutine, for tests.
	sync bool
}

// qaSession is one space's conversation.
type qaSession struct {
	history  []agent.Message
	turns    []*qaTurn
	cancel   context.CancelFunc
	list     ui.ListState
	rows     []qaRow
	rowState []qaRowsState
	preview  qaPreviewState
}

func (s *qaSession) running() bool {
	return s != nil && (s.cancel != nil || len(s.turns) > 0 && s.turns[len(s.turns)-1].revealing)
}

// qaTurn is a question and what was done to answer it.
type qaTurn struct {
	question string
	// start is the length of the history before the question, to which a
	// retry rolls it back.
	start    int
	steps    []*qaStep
	answer   string
	thinking bool
	running  bool
	// revealing outlives the provider: buffered text and its final fade must
	// finish even if all deltas and the done event arrive before one UI frame.
	revealing bool
	retrying  bool
	epoch     uint64
	stopped   bool
	err       error
	avatar    qaBotState
	// expanded and userToggled control whether the exploration timeline is open.
	expanded        bool
	userToggled     bool
	sourcesOpen     bool
	hoverSource     int
	nextHoverSource int
	view            qaTurnView
	stream          qaStream
}

// stepsOpen reports whether the agent exploration timeline is expanded.
// By default it stays open while exploring and folds once the answer streams,
// unless the user explicitly toggled it.
func (t *qaTurn) stepsOpen() bool {
	if t.userToggled {
		return t.expanded
	}
	return t.expanded || ((t.running || t.revealing) && t.displayAnswer() == "")
}

// qaStep is one tool call, as the panel lists it.
type qaStep struct {
	call       agent.ToolCall
	done       bool
	failed     bool
	result     string
	open       bool
	hitsParsed bool
	hits       []qaStepHit
}

// qaStepHit is one previewable item inside a search/grep/glob/links result.
type qaStepHit struct {
	path string
	line int
	head string
	loc  string
	text string
}

// qaSettings are the model settings, the key read from the store once.
func (a *App) qaSettings() agent.Settings {
	q := &a.qa
	if !q.keyRead {
		q.keyRead = true
		if q.secrets != nil {
			v, err := q.secrets.Get(apiKeySecret)
			switch {
			case err == nil:
				q.key = string(v)
			case !errors.Is(err, os.ErrNotExist):
				q.keyErr = err
			}
		}
	}
	ai := a.prefs.AI
	base := ai.BaseURL
	if base == "" {
		base = aiPreset(ai.Provider).BaseURL
	}
	return agent.Settings{BaseURL: base, Model: ai.Model, APIKey: q.key, Protocol: ai.Protocol}
}

// qaReady reports whether a question can be asked, and why not.
func (a *App) qaReady() error {
	if a.prefs == nil {
		return errors.New(i18n.T("无法读取设置"))
	}
	settings := a.qaSettings()
	switch err := settings.Check(); {
	case err == nil:
		return nil
	case errors.Is(err, agent.ErrNoKey) && a.qa.keyErr != nil:
		return errors.New(i18n.T("无法读取 API Key：") + a.qa.keyErr.Error())
	case errors.Is(err, agent.ErrBadAddress):
		return errors.New(i18n.T("服务地址须以 https:// 开头（本机服务可用 http://）"))
	case errors.Is(err, agent.ErrNoModel):
		return errors.New(i18n.T("还没有设置模型"))
	default:
		return errors.New(i18n.T("还没有设置 API Key"))
	}
}

// aiPreset is the preset with the ID; none chosen yet is the first.
func aiPreset(id string) agent.Preset {
	if id == "" {
		return agent.Presets[0]
	}
	return agent.PresetOf(id)
}

func (a *App) qaSession(spaceID string) *qaSession {
	q := &a.qa
	if q.sessions == nil {
		q.sessions = map[string]*qaSession{}
	}
	s := q.sessions[spaceID]
	if s == nil {
		s = &qaSession{list: ui.ListState{FollowEnd: true, FollowAnimation: &ui.Smooth}}
		q.sessions[spaceID] = s
	}
	return s
}

// QAPage is a document file as the agent reads it: its Markdown, each
// line knowing its top-level block.
func QAPage(name, content string) (*spacefs.Page, error) {
	if vault.IsPDF(name) {
		doc, err := pdfdoc.OpenBytes([]byte(content))
		if err != nil {
			return nil, err
		}
		defer doc.Close()
		cnt := min(doc.PageCount(), 200)
		var parts []spacefs.Part
		for i := range cnt {
			txt := strings.TrimSpace(doc.PageText(i))
			head := fmt.Sprintf("## Page %d", i+1)
			if txt != "" {
				head += "\n" + txt
			}
			parts = append(parts, spacefs.Part{
				Block: fmt.Sprintf("page:%d", i+1),
				Text:  head,
			})
		}
		return spacefs.PageOf(name, parts), nil
	}
	if vault.IsWhiteboard(name) {
		scene, err := whiteboard.Decode(content)
		if err != nil {
			return nil, err
		}
		return pageOfWhiteboard(name, scene), nil
	}
	var doc *model.Document
	if strings.EqualFold(path.Ext(name), ".arcmd") {
		d, err := model.Decode(content)
		if err != nil {
			return nil, err
		}
		doc = d
	} else {
		doc = editor.ImportMarkdown(content)
	}
	return pageOfDocument(name, doc), nil
}

func pageOfWhiteboard(name string, scene *whiteboard.Scene) *spacefs.Page {
	lines := scene.MarkdownSummary()
	out := make([]spacefs.Part, len(lines))
	for i, l := range lines {
		out[i] = spacefs.Part{Block: l.ElementID, Text: l.Text}
	}
	return spacefs.PageOf(name, out)
}

func pageOfDocument(name string, doc *model.Document) *spacefs.Page {
	parts := editor.ExportMarkdownParts(doc)
	out := make([]spacefs.Part, len(parts))
	for i, p := range parts {
		out[i] = spacefs.Part{Block: p.Block, Text: p.Text}
	}
	return spacefs.PageOf(name, out)
}

// qaSpace is the space's read-only view, with the unsaved text of its
// open documents.
func (a *App) qaSpace(spaceID string) *spacefs.FS {
	sp := a.state.Get(spaceID)
	if sp == nil {
		return nil
	}
	q := &a.qa
	if q.spaces == nil {
		q.spaces = map[string]*spacefs.FS{}
	}
	fs := q.spaces[spaceID]
	if fs == nil || fs.Root != sp.Root {
		fs = spacefs.New(sp.Root, QAPage)
		q.spaces[spaceID] = fs
	}
	if sp.IsExternal() {
		files := sp.ExternalFiles
		if files == nil {
			files = map[string]string{}
		}
		fs.SetFiles(files)
	}
	live := map[string]*spacefs.Page{}
	for _, d := range a.residentDocuments() {
		if d.space == sp && d.loadErr == nil && d.dirty() {
			if d.isWhiteboard() {
				live[d.rel] = pageOfWhiteboard(d.rel, d.board.Scene)
			} else {
				live[d.rel] = pageOfDocument(d.rel, d.ed.Doc)
			}
		}
	}
	fs.SetLive(live)
	return fs
}

// toggleQA opens or closes the panel; opening puts the caret in it.
func (a *App) toggleQA() {
	a.qa.open = !a.qa.open
	a.qa.focusInput = a.qa.open
}

// askQA asks the active space the question typed.
func (a *App) askQA() {
	text := strings.TrimSpace(a.qa.input)
	sp := a.state.ActiveSpace()
	if text == "" || sp == nil || a.qaSession(sp.ID).running() {
		return
	}
	a.qa.input = ""
	a.ask(sp.ID, text)
}

// ask runs a question in the space with the ID.
func (a *App) ask(spaceID, question string) {
	s := a.qaSession(spaceID)
	turn := &qaTurn{question: question, start: len(s.history), running: true}
	s.turns = append(s.turns, turn)
	s.list.FollowEnd = true
	s.list.ScrollToEnd()
	if err := a.qaReady(); err != nil {
		turn.running, turn.err = false, err
		return
	}
	fs := a.qaSpace(spaceID)
	if fs == nil {
		turn.running, turn.err = false, errors.New(i18n.T("空间已不存在"))
		return
	}
	settings := a.qaSettings()
	var provider agent.Provider = settings.Provider()
	if a.qa.provider != nil {
		provider = a.qa.provider(settings)
	}
	open := ""
	if a.doc != nil && a.doc.space.ID == spaceID && a.doc.loadErr == nil {
		open = a.doc.rel
	}
	name := a.state.Get(spaceID).Name
	now := a.now
	s.history = append(s.history, agent.Message{Role: agent.User, Text: question})
	history := append([]agent.Message(nil), s.history...)
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	deliver := func(f func()) { f() }
	if !a.qa.sync && a.win != nil {
		deliver = func(f func()) { a.win.Update(f) }
	}
	run := func() {
		overview, _ := fs.Overview(6000)
		ag := &agent.Agent{
			Provider: provider,
			System:   agent.Prompt(agent.Context{Space: name, Overview: overview, Open: open, Now: now}),
			Tools:    agent.SpaceTools(fs),
			Retries:  2,
		}
		added, err := ag.Run(ctx, history, func(e agent.Event) { deliver(func() { turn.apply(e) }) })
		deliver(func() { a.finishQA(s, turn, added, err) })
	}
	if a.qa.sync || a.win == nil {
		run()
		return
	}
	go run()
}

// apply shows an event of the run.
func (t *qaTurn) apply(e agent.Event) {
	switch e.Kind {
	case agent.Retrying:
		t.retrying = true
	case agent.ReasoningDelta:
		t.retrying = false
		t.thinking = true
	case agent.TextDelta:
		t.retrying = false
		t.thinking = false
		t.answer += e.Text
		if e.Text != "" && !t.stopped {
			t.revealing = true
		}
	case agent.StepDone:
		t.retrying = false
		t.thinking = false
		if len(e.Message.Calls) > 0 {
			// What the model said before calling tools was thinking aloud,
			// not the answer.
			t.answer = ""
			t.revealing = false
			t.epoch++
			t.stream = qaStream{}
		}
	case agent.ToolStart:
		t.retrying = false
		t.steps = append(t.steps, &qaStep{call: e.Call})
	case agent.ToolDone:
		for _, st := range t.steps {
			if st.call.ID == e.Call.ID && !st.done {
				st.done, st.failed, st.result = true, e.IsError, e.Result
				break
			}
		}
	}
}

// finishQA ends a run: its messages join the conversation, closed with an
// answer even when it stopped, as a model must see every call answered.
func (a *App) finishQA(s *qaSession, t *qaTurn, added []agent.Message, err error) {
	s.cancel = nil
	t.running, t.thinking, t.retrying = false, false, false
	s.history = append(s.history, added...)
	if n := len(s.history); n == 0 || s.history[n-1].Role != agent.Assistant || len(s.history[n-1].Calls) > 0 {
		note := "[The answer was stopped by the user.]"
		if err != nil && !errors.Is(err, context.Canceled) {
			note = "[The answer failed: " + err.Error() + "]"
		}
		if t.answer != "" {
			note = t.answer + "\n\n" + note
		}
		s.history = append(s.history, agent.Message{Role: agent.Assistant, Text: note})
	}
	switch {
	case errors.Is(err, context.Canceled):
		t.stopped = true
	case err != nil:
		t.err = err
	}
}

// stopQA stops the active space's question.
func (a *App) stopQA() {
	sp := a.state.ActiveSpace()
	if sp == nil {
		return
	}
	s := a.qaSession(sp.ID)
	if s.cancel != nil {
		s.cancel()
	}
	if len(s.turns) > 0 {
		t := s.turns[len(s.turns)-1]
		t.revealing = false
		if t.running {
			t.stopped = true
		}
		t.stream.sync(t.answer, a.now)
	}
}

// retryQA asks a failed question again, in place of its turn.
func (a *App) retryQA(spaceID string, t *qaTurn) {
	s := a.qaSession(spaceID)
	if s.running() || len(s.turns) == 0 || s.turns[len(s.turns)-1] != t {
		return
	}
	if s.preview.turn == t || s.preview.pendingTurn == t {
		s.preview = qaPreviewState{}
	}
	s.turns = s.turns[:len(s.turns)-1]
	s.history = s.history[:t.start]
	a.ask(spaceID, t.question)
}

// clearQA starts a new conversation in the active space.
func (a *App) clearQA() {
	sp := a.state.ActiveSpace()
	if sp == nil {
		return
	}
	s := a.qaSession(sp.ID)
	if s.cancel != nil {
		s.cancel()
	}
	a.qa.sessions[sp.ID] = &qaSession{list: ui.ListState{FollowEnd: true, FollowAnimation: &ui.Smooth}}
	a.qa.focusInput = true
}

// openCitation opens the cited document of the space at the cited block.
func (a *App) openCitation(spaceID string, c agent.Citation) {
	fs := a.qa.spaces[spaceID]
	if fs == nil || !a.openAnywhere(spaceID, c.Path) || a.doc == nil || a.doc.loadErr != nil {
		a.notify("找不到引用的文档：%s", c.Path)
		return
	}
	if a.doc.isPDF() {
		if page, err := fs.Page(c.Path); err == nil {
			if at, ok := page.Locate(c.From); ok {
				if pStr, has := strings.CutPrefix(at.Block, "page:"); has {
					if pNum, convErr := strconv.Atoi(pStr); convErr == nil && pNum >= 1 {
						a.doc.pdf.GoToPage(pNum-1, false)
					}
				}
			}
		}
		a.doc.pdf.RequestFocus = true
		return
	}
	if a.doc.isWhiteboard() {
		page, err := fs.Page(c.Path)
		if err != nil {
			return
		}
		at, ok := page.Locate(c.From)
		if !ok {
			return
		}
		if at.Block != "" {
			a.doc.board.RevealElement(at.Block)
			return
		}
		if elems := a.doc.board.Scene.Elements; at.Index >= 0 && at.Index < len(elems) {
			a.doc.board.RevealElement(elems[at.Index].ID)
		}
		return
	}
	if a.doc.source {
		a.doc.toggleMode()
	}
	page, err := fs.Page(c.Path)
	if err != nil {
		return
	}
	at, ok := page.Locate(c.From)
	if !ok {
		return
	}
	blocks := a.doc.ed.Doc.Blocks
	id := ""
	for _, b := range blocks {
		if at.Block != "" && b.ID == at.Block {
			id = b.ID
		}
	}
	if id == "" && at.Index < len(blocks) {
		// A Markdown file's blocks get new IDs each time it is read; its
		// order is the same.
		id = blocks[at.Index].ID
	}
	if id != "" {
		a.doc.ed.RevealBlock(id)
	}
}

// saveAIKey keeps the API key in the local credential store; the
// address and model are in prefs.json.
func (a *App) saveAIKey() error {
	key := strings.TrimSpace(a.qa.keyInput)
	if key == "" {
		return nil
	}
	if a.qa.secrets == nil {
		return fmt.Errorf(i18n.T("无法保存 API Key：%s"), i18n.T("本地凭据存储不可用"))
	}
	if err := a.qa.secrets.Set(apiKeySecret, []byte(key)); err != nil {
		a.notify("无法保存 API Key：%s", err.Error())
		return fmt.Errorf(i18n.T("无法保存 API Key：%s"), err.Error())
	}
	a.qa.key, a.qa.keyRead, a.qa.keyErr, a.qa.keyInput = key, true, nil, ""
	return nil
}

func (a *App) removeAIKey() {
	if a.qa.secrets != nil {
		if err := a.qa.secrets.Delete(apiKeySecret); err != nil {
			a.notify("无法移除 API Key：%s", err.Error())
			return
		}
	}
	a.qa.key, a.qa.keyRead, a.qa.keyErr = "", true, nil
}

// qaStepMeta breaks a tool call into structured UI parts for the timeline.
type qaStepMeta struct {
	verb    string
	active  string
	subject string
	scope   string
	docPath string
	from    int
	to      int
}

func stepMeta(call agent.ToolCall) qaStepMeta {
	var args struct {
		Query   string `json:"query"`
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Start   int    `json:"start_line"`
		End     int    `json:"end_line"`
	}
	_ = json.Unmarshal([]byte(call.Args), &args)
	switch call.Name {
	case agent.ToolSearch:
		return qaStepMeta{
			verb:    i18n.T("检索"),
			active:  i18n.T("正在检索"),
			subject: "“" + args.Query + "”",
			scope:   cleanScopeLabel(args.Path),
		}
	case agent.ToolGrep:
		return qaStepMeta{
			verb:    i18n.T("搜索"),
			active:  i18n.T("正在搜索"),
			subject: "“" + args.Pattern + "”",
			scope:   cleanScopeLabel(args.Path),
		}
	case agent.ToolRead:
		title := vault.Title(args.Path)
		scope := ""
		from := max(args.Start, 1)
		to := from
		if args.Start > 0 || args.End > 0 {
			if args.End > 0 {
				to = max(args.End, from)
				scope = humanLines(from, to)
			} else {
				scope = fmt.Sprintf(i18n.T("第 %d 行起"), from)
			}
		} else if dir := docFolderBreadcrumb(args.Path); dir != "" {
			scope = dir
		}
		return qaStepMeta{
			verb:    i18n.T("阅读"),
			active:  i18n.T("正在阅读"),
			subject: title,
			scope:   scope,
			docPath: args.Path,
			from:    from,
			to:      to,
		}
	case agent.ToolGlob:
		sub := args.Pattern
		if sub == "" {
			sub = i18n.T("全部文档")
		}
		return qaStepMeta{
			verb:    i18n.T("浏览"),
			active:  i18n.T("正在浏览"),
			subject: sub,
		}
	case agent.ToolOutline:
		return qaStepMeta{
			verb:    i18n.T("目录"),
			active:  i18n.T("正在查看目录"),
			subject: vault.Title(args.Path),
			scope:   docFolderBreadcrumb(args.Path),
			docPath: args.Path,
			from:    1,
			to:      1,
		}
	case agent.ToolLinks:
		return qaStepMeta{
			verb:    i18n.T("双链"),
			active:  i18n.T("正在追踪链接"),
			subject: vault.Title(args.Path),
			scope:   docFolderBreadcrumb(args.Path),
			docPath: args.Path,
			from:    1,
			to:      1,
		}
	}
	return qaStepMeta{verb: call.Name, active: call.Name, subject: call.Args}
}

func cleanScopeLabel(p string) string {
	p = strings.Trim(strings.TrimSpace(p), "/")
	if p == "" || p == "." {
		return ""
	}
	if strings.HasSuffix(strings.ToLower(p), ".md") || strings.HasSuffix(strings.ToLower(p), ".arcmd") {
		return vault.Title(p)
	}
	return strings.ReplaceAll(p, "/", " / ")
}

func docFolderBreadcrumb(rel string) string {
	dir := path.Dir(strings.Trim(rel, "/"))
	if dir == "" || dir == "." || dir == "/" {
		return ""
	}
	return strings.ReplaceAll(dir, "/", " / ")
}

func humanLines(from, to int) string {
	from = max(from, 1)
	if to > from {
		return fmt.Sprintf(i18n.T("第 %d–%d 行"), from, to)
	}
	return fmt.Sprintf(i18n.T("第 %d 行"), from)
}

var (
	countRe      = regexp.MustCompile(`^(\d+) (relevant passages|matches|documents)`)
	searchHeadRe = regexp.MustCompile(`^\[\d+\] ([^\(\n]+) \((.+), L(\d+)(?:-(\d+))?\)$`)
	grepHitRe    = regexp.MustCompile(`^([^:\n]+):(\d+):(.*)$`)
	linkOutRe    = regexp.MustCompile(`^\s*\[\[([^\]]+)\]\] → (.+)$`)
	linkInRe     = regexp.MustCompile(`^\s*([^:\n]+):(\d+):(.*)$`)
)

// stepResult is a short account of a call's result.
func stepResult(st *qaStep) string {
	switch {
	case !st.done:
		return ""
	case st.failed:
		return i18n.T("失败")
	case strings.HasPrefix(st.result, "No matches"),
		strings.HasPrefix(st.result, "No documents"),
		strings.HasPrefix(st.result, "No relevant passages"):
		return i18n.T("无结果")
	}
	if m := countRe.FindStringSubmatch(st.result); m != nil {
		switch m[2] {
		case "relevant passages":
			return fmt.Sprintf(i18n.T("召回 %s 段"), m[1])
		case "matches":
			return fmt.Sprintf(i18n.T("找到 %s 处"), m[1])
		default:
			return fmt.Sprintf(i18n.T("%s 篇"), m[1])
		}
	}
	switch st.call.Name {
	case agent.ToolRead:
		lines := strings.Count(st.result, "→")
		if lines > 0 {
			return fmt.Sprintf(i18n.T("已读 %d 行"), lines)
		}
	case agent.ToolOutline:
		if strings.Contains(st.result, "(no headings)") {
			return i18n.T("无标题")
		}
		n := max(0, strings.Count(strings.TrimSpace(st.result), "\n"))
		if n > 0 {
			return fmt.Sprintf(i18n.T("%d 节"), n)
		}
	}
	return ""
}

// ensureHits parses up to 5 clickable preview items for search/grep/glob/links steps.
func (st *qaStep) ensureHits() []qaStepHit {
	if !st.done || st.failed {
		return nil
	}
	if st.hitsParsed {
		return st.hits
	}
	st.hitsParsed = true
	lines := strings.Split(st.result, "\n")
	const maxPreview = 5

	switch st.call.Name {
	case agent.ToolSearch:
		for i := 0; i < len(lines) && len(st.hits) < maxPreview; i++ {
			m := searchHeadRe.FindStringSubmatch(lines[i])
			if m == nil {
				continue
			}
			from, _ := strconv.Atoi(m[3])
			to := from
			if m[4] != "" {
				to, _ = strconv.Atoi(m[4])
			}
			snippet := ""
			for j := i + 1; j < len(lines) && j <= i+5; j++ {
				if _, after, ok := strings.Cut(lines[j], "→"); ok {
					t := cleanPreviewSnippet(after)
					if t != "" {
						snippet = t
						break
					}
				} else {
					break
				}
			}
			st.hits = append(st.hits, qaStepHit{
				path: strings.TrimSpace(m[1]),
				line: max(from, 1),
				head: m[2],
				loc:  humanLines(from, to),
				text: snippet,
			})
		}
	case agent.ToolGrep:
		for _, l := range lines[min(1, len(lines)):] {
			if len(st.hits) >= maxPreview {
				break
			}
			if l == "--" || strings.HasPrefix(l, "(") || strings.HasPrefix(l, "Could not read") {
				continue
			}
			m := grepHitRe.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			ln, _ := strconv.Atoi(m[2])
			p := strings.TrimSpace(m[1])
			st.hits = append(st.hits, qaStepHit{
				path: p,
				line: max(ln, 1),
				head: vault.Title(p),
				loc:  humanLines(ln, ln),
				text: cleanPreviewSnippet(m[3]),
			})
		}
	case agent.ToolGlob:
		for _, l := range lines[min(1, len(lines)):] {
			if len(st.hits) >= maxPreview {
				break
			}
			parts := strings.Split(l, "\t")
			if len(parts) < 3 {
				continue
			}
			p := strings.TrimSpace(parts[0])
			title := strings.TrimSpace(parts[1])
			if title == "" {
				title = vault.Title(p)
			}
			cnt := strings.TrimSuffix(strings.TrimSpace(parts[2]), " lines")
			loc := ""
			if cnt != "" {
				loc = fmt.Sprintf(i18n.T("%s 行"), cnt)
			}
			st.hits = append(st.hits, qaStepHit{
				path: p,
				line: 1,
				head: title,
				loc:  loc,
				text: docFolderBreadcrumb(p),
			})
		}
	case agent.ToolLinks:
		for _, l := range lines {
			if len(st.hits) >= maxPreview {
				break
			}
			if m := linkOutRe.FindStringSubmatch(l); m != nil {
				p := strings.TrimSpace(m[2])
				st.hits = append(st.hits, qaStepHit{
					path: p,
					line: 1,
					head: vault.Title(p),
					loc:  i18n.T("引用文档"),
				})
				continue
			}
			if m := linkInRe.FindStringSubmatch(l); m != nil && strings.HasSuffix(strings.ToLower(strings.TrimSpace(m[1])), ".md") {
				p := strings.TrimSpace(m[1])
				ln, _ := strconv.Atoi(m[2])
				st.hits = append(st.hits, qaStepHit{
					path: p,
					line: max(ln, 1),
					head: vault.Title(p),
					loc:  humanLines(ln, ln),
					text: cleanPreviewSnippet(m[3]),
				})
			}
		}
	}
	return st.hits
}

func cleanPreviewSnippet(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "#> ")
	for _, pfx := range []string{"- [ ] ", "- [x] ", "- [X] ", "- ", "* "} {
		if strings.HasPrefix(s, pfx) {
			s = strings.TrimSpace(s[len(pfx):])
			break
		}
	}
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "`", "")
	return strings.TrimSpace(s)
}

// qaErrorText says what went wrong with a question in the user's words,
// the service's own message after it.
func qaErrorText(err error) string {
	var api *agent.APIError
	switch {
	case errors.As(err, &api):
		head := fmt.Sprintf(i18n.T("模型服务出错（%d）"), api.Status)
		switch {
		case api.Status == 401 || api.Status == 403:
			head = fmt.Sprintf(i18n.T("API Key 无效或没有权限（%d）"), api.Status)
		case api.Status == 404:
			head = i18n.T("找不到这个模型或服务地址（404）")
		case api.Status == 429:
			head = i18n.T("请求太频繁或额度已用完（429）")
		}
		if api.Message != "" {
			head += "：" + api.Message
		}
		return head
	case errors.Is(err, agent.ErrStreamCut):
		return i18n.T("回答中途断开了，请重试")
	case strings.HasPrefix(err.Error(), "cannot reach the model service"):
		return i18n.T("无法连接模型服务，请检查网络与服务地址")
	}
	return errText(err)
}

// modelsKey is what a model list is for: the service's address and key.
func modelsKey(s agent.Settings) string {
	return strings.TrimSpace(s.BaseURL) + "\x00" + s.APIKey
}

// modelListSettings includes a key still in the field, so getting the
// models needs no separate Save step and never requires a model name.
func (a *App) modelListSettings() agent.Settings {
	s := a.qaSettings()
	if key := strings.TrimSpace(a.qa.keyInput); key != "" {
		s.APIKey = key
	}
	return s
}

// fetchModels lists the service's models when the user asks, on a
// goroutine, after saving a key typed but not yet saved.
func (a *App) fetchModels() {
	q := &a.qa
	if q.modelsBusy {
		return
	}
	if strings.TrimSpace(q.keyInput) != "" {
		if err := a.saveAIKey(); err != nil {
			q.modelsErr = err
			return
		}
	}
	settings := a.qaSettings()
	q.models, q.modelsFor = nil, ""
	switch err := settings.CheckService(); {
	case errors.Is(err, agent.ErrBadAddress):
		q.modelsBusy, q.modelsErr = false, errors.New(i18n.T("服务地址须以 https:// 开头（本机服务可用 http://）"))
		return
	case err != nil:
		q.modelsBusy, q.modelsErr = false, errors.New(i18n.T("还没有设置 API Key"))
		return
	}
	key := modelsKey(settings)
	q.modelsFor, q.modelsBusy, q.modelsErr = key, true, nil
	list := q.listModels
	if list == nil {
		list = func(s agent.Settings) ([]string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			return s.Provider().Models(ctx)
		}
	}
	done := func(models []string, err error) {
		a.dropStaleModels()
		if q.modelsFor != key {
			return // the address or key changed meanwhile
		}
		q.modelsBusy, q.modelsErr, q.models = false, err, models
		if err == nil && len(models) > 0 {
			q.manualModel = false
		}
	}
	if q.sync || a.win == nil {
		done(list(settings))
		return
	}
	go func() {
		models, err := list(settings)
		a.win.Update(func() { done(models, err) })
	}()
}

// dropStaleModels forgets a list listed for another address or key.
func (a *App) dropStaleModels() {
	q := &a.qa
	if q.modelsFor != "" && q.modelsFor != modelsKey(a.qaSettings()) {
		q.models, q.modelsFor, q.modelsBusy, q.modelsErr = nil, "", false, nil
	}
}
