package app

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/agent"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/spacefs"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// qaWidth is the panel's width, Arc's side panel's.
const qaWidth = 380

var (
	iconAsk          = lucide(`<path d="M14 9a2 2 0 0 1-2 2H6l-4 4V4a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2z"/><path d="M18 9h2a2 2 0 0 1 2 2v11l-4-4h-6a2 2 0 0 1-2-2v-1"/>`)
	iconArrowUp      = lucide(`<path d="m5 12 7-7 7 7"/><path d="M12 19V5"/>`)
	iconArrowDown    = lucide(`<path d="M12 5v14"/><path d="m19 12-7 7-7-7"/>`)
	iconArrowUpRight = lucide(`<path d="M7 7h10v10"/><path d="M7 17 17 7"/>`)
	iconStop         = lucide(`<rect width="14" height="14" x="5" y="5" rx="2"/>`)
	iconList         = lucide(`<path d="M3 12h.01"/><path d="M3 18h.01"/><path d="M3 6h.01"/><path d="M8 12h13"/><path d="M8 18h13"/><path d="M8 6h13"/>`)
	iconListTree     = lucide(`<path d="M21 12h-8"/><path d="M21 6H8"/><path d="M21 18h-8"/><path d="M3 6v4c0 1.1.9 2 2 2h3"/><path d="M3 10v6c0 1.1.9 2 2 2h3"/>`)
	iconNewChat      = lucide(`<path d="M12 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.375 2.625a1 1 0 0 1 3 3l-9.013 9.014a2 2 0 0 1-.853.505l-2.873.84a.5.5 0 0 1-.62-.62l.84-2.873a2 2 0 0 1 .506-.852z"/>`)
)

// qaExamples are questions the empty panel offers.
var qaExamples = []string{
	"这个空间里有哪些还没完成的任务？",
	"总结一下最近修改的几篇笔记",
	"有没有前后矛盾的记录？",
}

// citeRef is a cited range, numbered in the order the answer first cites it,
// enriched with its document title, folder breadcrumb, enclosing section and
// recalled plain-text excerpt.
type citeRef struct {
	n       int
	cite    agent.Citation
	title   string
	folder  string
	section string
	excerpt string
}

func citeKey(c agent.Citation) string { return fmt.Sprintf("%s#%d-%d", c.Path, c.From, c.To) }

// qaPanel is the question panel right of the document: its header level
// with the toolbar, a card with the conversation and the question field.
// It slides open as the sidebar does.
func (a *App) qaPanel(c *ui.Context) {
	t := c.Theme()
	sp := a.state.ActiveSpace()
	col := ui.Column(c.Key("qa-dock")).Shrink(0).Clip()
	w := a.qa.shown
	col.Width(w).Hidden(w < 0.5).Children(func() {
		if w < 0.5 || sp == nil {
			return
		}
		bar := c.TitleBar()
		ui.Column(c.Key("qa")).Width(qaWidth).Grow(1).MinHeight(0).Padding(0, 8, 8, 0).Children(func() {
			s := a.qaSession(sp.ID)
			ui.Row(c.Key("qa-head")).Height(max(bar.Height, 46)).Padding(0, bar.Right, 0, 6).Gap(6).AlignItems(ui.Center).DragWindow().Children(func() {
				ui.Icon(c, iconAsk).FontSize(15).TextColor(t.Text.Alpha(0.72))
				ui.Text(c, i18n.T("问答")).FontWeight(600).NoWrap()
				ui.Text(c, sp.Name).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
				ui.Spacer(c)
				if iconButton(c, "qa-new", iconNewChat, i18n.T("新对话"), i18n.T("新对话"), len(s.turns) == 0).Clicked() {
					a.clearQA()
				}
				if iconButton(c, "qa-close", iconX, i18n.T("关闭问答"), a.withKey(i18n.T("关闭问答"), "ask"), false).Clicked() {
					a.qa.open = false
				}
			})
			ui.Column(c.Key("qa-card")).Grow(1).MinHeight(0).Radius(10).Background(glass(t)).
				Border(1, t.Border.Alpha(0.35)).Clip().Children(func() { a.qaBody(c, sp.ID, sp.Name, s) })
		})
	})
}

func (a *App) qaBody(c *ui.Context, spaceID, name string, s *qaSession) {
	t := c.Theme()
	prevHover := make([]int, len(s.turns))
	prevInline := make([]bool, len(s.turns))
	for i, turn := range s.turns {
		prevHover[i] = turn.hoverCite
		prevInline[i] = turn.hoverInline
		turn.hoverCite = turn.nextHoverCite
		turn.hoverInline = turn.nextHoverInline
		turn.citeAnchor = turn.nextCiteAnchor
		turn.nextHoverCite = 0
		turn.nextHoverInline = false
		turn.nextCiteAnchor = ui.Rect{}
		turn.prepareStream(c)
	}
	rows := s.visibleRowsWith(func() *spacefs.FS {
		if fs := a.qa.spaces[spaceID]; fs != nil {
			return fs
		}
		return a.qaSpace(spaceID)
	})
	for _, turn := range s.turns {
		turn.view.animate(c.Now(), turn.animateAnswer(c))
	}
	follow := true
	if n := len(s.turns); n > 0 {
		last := s.turns[n-1]
		if !last.running && !last.revealing && last.stepsOpen() {
			follow = false
		}
	}
	s.list.FollowEnd = follow
	ui.Column(c.Key("qa-list-wrap")).Grow(1).MinHeight(0).Children(func() {
		list := ui.List(c.Key("qa-scroll-"+spaceID), &s.list, len(rows)).Grow(1).MinHeight(0).Padding(14).
			Label(i18n.T("问答")).ItemKey(func(i int) any { return rows[i] })
		list.Rows(func(row ui.ListRow) {
			c := row.Context
			r := rows[row.Index]
			gap := r.gap()
			if row.Index+1 < len(rows) && rows[row.Index+1].turn != r.turn {
				gap = 18
			}
			ui.Column(c.Key("content")).Padding(0, 0, gap, 0).Children(func() {
				switch r.kind {
				case qaEmptyRow:
					a.qaEmpty(c, name)
				case qaQuestionRow:
					a.qaQuestion(c, r.turn)
				case qaStepsRow:
					a.qaSteps(c, spaceID, r.turn)
				case qaAnswerRow:
					a.qaAnswerBlock(c, spaceID, r.turn, &r.turn.view.blocks[r.index])
				case qaSourcesRow:
					qaSourcesHeader(c, r.turn)
				case qaSourceRow:
					a.qaSourceGroup(c, spaceID, r.turn, r.turn.view.groups[r.index])
				case qaStatusRow:
					a.qaStatus(c, spaceID, r.turn)
				}
			})
		})
		_, lastVis := s.list.Visible()
		if len(s.turns) > 0 && lastVis >= 0 && !s.list.AtEnd() {
			col := previewColors(t, a.look)
			ui.Row(c.Key("qa-jump-pos")).Absolute().Left(0).Right(0).Bottom(10).Justify(ui.Center).PassThrough().
				Transition(ui.ElementTransition{
					Duration: 150 * time.Millisecond,
					Enter:    &ui.Motion{Y: 6, Opacity: 0},
					Exit:     &ui.Motion{Y: 6, Opacity: 0},
				}).Children(func() {
				pill := ui.Row(c.Key("qa-jump-latest")).Height(28).Padding(0, 11).Gap(5).Radius(14).AlignItems(ui.Center).
					Background(col.fill).Border(1, t.Accent.Alpha(0.42)).Shadow(0, 4, 14, 0, col.shadow).
					Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("回到最新"))
				if pill.Hovered() {
					pill.Background(col.fill.Mix(t.Accent, 0.08)).Border(1, t.Accent.Alpha(0.72))
				}
				pill.Children(func() {
					if s.running() {
						pulse := float32(0.55 + 0.45*math.Sin(float64(c.Now().UnixMilli())/260.0))
						ui.Box(c.Key("dot")).Size(6, 6).Radius(3).Background(t.Accent.Alpha(pulse)).Shrink(0)
						if !c.Preferences().ReduceMotion {
							c.After(33 * time.Millisecond)
						}
					}
					ui.Icon(c, iconArrowDown).FontSize(12).TextColor(t.Accent)
					ui.Text(c, i18n.T("回到最新")).FontSize(11.5).FontWeight(600).TextColor(col.title).NoWrap()
				})
				if pill.Clicked() {
					s.list.ScrollToEnd()
				}
			})
		}
	})
	a.qaCitationPreview(c, s)
	// Request one follow-up frame when hover synchronization between an inline
	// citation pill and a recalled source passage changes.
	for i, turn := range s.turns {
		if turn.nextHoverCite != turn.hoverCite || turn.hoverCite != prevHover[i] ||
			turn.nextHoverInline != turn.hoverInline || turn.hoverInline != prevInline[i] {
			c.After(time.Millisecond)
			break
		}
	}

	ui.Box(c).Height(1).Background(separator(t))
	ui.Row(c.Key("qa-input")).Padding(10).Gap(8).AlignItems(ui.Center).Children(func() {
		in := ui.TextInput(c.Key("qa-question"), &a.qa.input).Grow(1).MinWidth(0).Height(36).Radius(10).PaddingX(12).
			Background(field(t)).Placeholder(fmt.Sprintf(i18n.T("询问「%s」…"), name)).Label(i18n.T("问题"))
		if a.qa.focusInput {
			a.qa.focusInput = false
			in.Focus()
		}
		if in.Submitted() {
			a.askQA()
		}
		if s.running() {
			if qaRoundButton(c, "qa-stop", iconStop, i18n.T("停止"), false).Clicked() {
				a.stopQA()
			}
			return
		}
		if qaRoundButton(c, "qa-send", iconArrowUp, i18n.T("发送 (Enter)"), strings.TrimSpace(a.qa.input) == "").Clicked() {
			a.askQA()
		}
	})
}

// qaRoundButton is the round send or stop button beside the field.
func qaRoundButton(c *ui.Context, key string, icon *ui.SVG, label string, disabled bool) ui.Element {
	t := c.Theme()
	b := ui.Box(c.Key(key)).Size(32, 32).Radius(16).Center().Focusable().Role(ui.RoleButton).Label(label).Tooltip(label).Disabled(disabled)
	fill, ink := t.Accent, t.AccentText
	if disabled {
		fill, ink = t.Text.Alpha(0.08), t.Text.Alpha(0.3)
	} else {
		b.Cursor(ui.CursorPointer)
		if b.Hovered() {
			fill = fill.Mix(white, 0.12)
		}
	}
	b.Background(fill).Children(func() { ui.Icon(c, icon).FontSize(16).TextColor(ink) })
	return b
}

func (a *App) qaEmpty(c *ui.Context, name string) {
	t := c.Theme()
	ui.Column(c.Key("qa-empty")).Gap(10).Padding(24, 6, 6, 6).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconAsk).FontSize(28).TextColor(t.Accent)
		ui.Text(c, fmt.Sprintf(i18n.T("向「%s」提问"), name)).FontSize(15).FontWeight(600).TextAlign(ui.Center)
		ui.Text(c, i18n.T("ArcMD 会像翻资料一样搜索、阅读这个空间里的文档来回答，并标出每句话的出处。")).
			FontSize(12).TextColor(t.TextMuted).TextAlign(ui.Center).MaxWidth(290)
		if err := a.qaReady(); err != nil {
			ui.Text(c, err.Error()).FontSize(12).TextColor(t.TextMuted).TextAlign(ui.Center)
			if ui.PrimaryButton(c.Key("qa-setup"), i18n.T("设置模型…")).Clicked() {
				a.openSettings(tabAI)
			}
			return
		}
		ui.Column(c).Gap(6).Padding(8, 0, 0, 0).AlignItems(ui.Stretch).Width(300).Children(func() {
			for i, ex := range qaExamples {
				ex := i18n.T(ex)
				row := ui.Row(c.Key(fmt.Sprintf("qa-example-%d", i))).Padding(8, 12).Radius(9).Gap(8).AlignItems(ui.Center).
					Background(t.Text.Alpha(0.04)).Border(1, t.Border.Alpha(0.22)).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(ex)
				if row.Hovered() {
					row.Background(hoverFill(t).Mix(t.Text.Alpha(0.04), 0.5)).Border(1, t.Border.Alpha(0.42))
				}
				row.Children(func() {
					ui.Icon(c, iconSearch).FontSize(13).TextColor(t.TextMuted)
					ui.Text(c, ex).FontSize(13).Shrink(1).MinWidth(0)
				})
				if row.Clicked() {
					a.qa.input = ex
					a.askQA()
				}
			}
		})
		ui.Text(c, i18n.T("提问时，读到的文档片段会发送给你设置的模型服务。")).FontSize(11).TextColor(t.TextMuted.Alpha(0.8)).
			TextAlign(ui.Center).MaxWidth(290).Padding(6, 0, 0, 0)
	})
}

func (a *App) qaQuestion(c *ui.Context, turn *qaTurn) {
	t := c.Theme()
	ui.Row(c.Key("q")).Transition(ui.ElementTransition{
		Duration: 180 * time.Millisecond,
		Enter:    &ui.Motion{Y: 5, Opacity: 0},
	}).Children(func() {
		ui.Spacer(c)
		ui.Box(c.Key("bubble")).Padding(9, 13).Radius(13).
			Background(t.Accent.Alpha(0.13)).Border(1, t.Accent.Alpha(0.20)).MaxWidth(304).Children(func() {
			ui.Text(c, turn.question).FontSize(13.5).LineHeight(1.5).Selectable()
		})
	})
}

func (a *App) qaStatus(c *ui.Context, spaceID string, turn *qaTurn) {
	t := c.Theme()
	if (turn.running || turn.revealing) && turn.displayAnswer() == "" && len(turn.steps) == 0 {
		label := i18n.T("正在查找…")
		switch {
		case turn.retrying:
			label = i18n.T("正在重试")
		case turn.thinking:
			label = i18n.T("思考中…")
		}
		ui.Row(c.Key("working")).Gap(9).Padding(8, 11).Radius(11).AlignItems(ui.Center).
			Background(t.Text.Alpha(0.032)).Border(1, t.Border.Alpha(0.32)).
			Transition(ui.ElementTransition{
				Duration: 160 * time.Millisecond,
				Enter:    &ui.Motion{Y: 4, Opacity: 0},
				Exit:     &ui.Motion{Collapse: true, Opacity: 0},
			}).Children(func() {
			qaGhostAvatar(c, &turn.avatar)
			ui.Text(c, label).FontSize(12.5).FontWeight(500).TextColor(t.Text.Alpha(0.82)).Role(ui.RoleStatus)
			ui.Spacer(c)
			qaPulseDots(c, t.Accent)
		})
	}
	if turn.stopped {
		ui.Row(c.Key("stopped")).Gap(6).AlignItems(ui.Center).Padding(2, 0).Children(func() {
			ui.Box(c).Size(6, 6).Radius(3).Background(t.TextMuted.Alpha(0.55))
			ui.Text(c, i18n.T("已停止")).FontSize(12).TextColor(t.TextMuted)
		})
	}
	if turn.err != nil {
		ui.Column(c.Key("error")).Gap(8).Padding(10, 12).Radius(10).
			Background(t.Danger.Alpha(0.08)).Border(1, t.Danger.Alpha(0.22)).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
				ui.Icon(c, iconAlert).FontSize(14).TextColor(t.Danger).Padding(2, 0, 0, 0)
				ui.Text(c, qaErrorText(turn.err)).FontSize(13).LineHeight(1.45).TextColor(t.Danger).Selectable().Shrink(1).MinWidth(0)
			})
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c.Key("retry"), i18n.T("重试")).Clicked() {
					a.retryQA(spaceID, turn)
				}
				if a.qaReady() != nil && ui.Button(c.Key("setup"), i18n.T("设置模型…")).Clicked() {
					a.openSettings(tabAI)
				}
			})
		})
	}
}

// qaPulseDots renders three softly breathing dots during live agent work,
// falling back to a static ellipsis when ReduceMotion is enabled.
func qaPulseDots(c *ui.Context, color ui.Color) {
	if c.Preferences().ReduceMotion {
		return
	}
	c.After(45 * time.Millisecond)
	sec := float64(c.Now().UnixNano()) / 1e9
	ui.Row(c.Key("dots")).Gap(3).AlignItems(ui.Center).Children(func() {
		for i := range 3 {
			phase := sec*4.6 - float64(i)*0.65
			wave := float32(0.5 + 0.5*math.Sin(phase))
			alpha := 0.25 + 0.65*wave
			lift := -1.4 * wave
			ui.Box(c.Key(i)).Size(3.5, 3.5).Radius(2).Background(color.Alpha(alpha)).
				Translate(0, lift)
		}
	})
}

// liveStepHeadline describes what the agent is actively doing in the header.
func (t *qaTurn) liveStepHeadline() string {
	if t.retrying {
		return i18n.T("正在重试")
	}
	for i := len(t.steps) - 1; i >= 0; i-- {
		st := t.steps[i]
		if !st.done {
			m := stepMeta(st.call)
			if m.subject != "" {
				return m.active + " " + m.subject
			}
			return m.active
		}
	}
	if t.thinking {
		return i18n.T("思考中…")
	}
	return i18n.T("正在查找…")
}

// consultedTitles returns up to 3 distinct document titles inspected in steps.
func (t *qaTurn) consultedTitles() []string {
	var out []string
	seen := map[string]bool{}
	for _, st := range t.steps {
		m := stepMeta(st.call)
		if m.docPath == "" || m.subject == "" {
			continue
		}
		key := strings.ToLower(m.docPath)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m.subject)
		if len(out) == 3 {
			break
		}
	}
	return out
}

// qaSteps renders the unified Agent Exploration Card: live Ghost + active
// headline while exploring, compact docIcon-badged summary once answering,
// and a smoothly collapsible vertical timeline of interactive steps.
func (a *App) qaSteps(c *ui.Context, spaceID string, turn *qaTurn) {
	t := c.Theme()
	waiting := (turn.running || turn.revealing) && turn.displayAnswer() == ""
	open := turn.stepsOpen()

	reads := 0
	for _, st := range turn.steps {
		if st.call.Name == agent.ToolRead {
			reads++
		}
	}

	borderCol := t.Border.Alpha(0.34)
	if waiting {
		borderCol = t.Accent.Alpha(0.24)
	}

	card := ui.Column(c.Key("steps")).Radius(11).
		Background(t.Text.Alpha(0.032)).Border(1, borderCol).Clip().
		Transition(ui.ElementTransition{
			Duration: 180 * time.Millisecond,
			Colors:   true,
			Size:     true,
			Enter:    &ui.Motion{Y: 4, Opacity: 0},
		})

	card.Children(func() {
		if waiting {
			ui.Column(c.Key("live-banner")).
				Transition(ui.ElementTransition{
					Duration: 160 * time.Millisecond,
					Enter:    &ui.Motion{Collapse: true, Opacity: 0},
					Exit:     &ui.Motion{Collapse: true, Opacity: 0},
				}).Children(func() {
				ui.Row(c.Key("live-row")).Gap(8).Padding(7, 10).AlignItems(ui.Center).
					Background(t.Accent.Alpha(0.05)).Children(func() {
					qaGhostAvatar(c, &turn.avatar)
					ui.Text(c, turn.liveStepHeadline()).FontSize(12.5).FontWeight(550).
						TextColor(t.Text.Alpha(0.88)).SingleLine().Shrink(1).MinWidth(0).Role(ui.RoleStatus)
					ui.Spacer(c)
					qaPulseDots(c, t.Accent)
				})
				ui.Box(c).Height(1).Background(separator(t))
			})
		}

		head := ui.Row(c.Key("steps-head")).Gap(8).Padding(7, 10).MinHeight(32).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("查找过程"))
		if head.Hovered() {
			head.Background(hoverFill(t).Alpha(0.65))
		}
		head.Children(func() {
			titles := turn.consultedTitles()
			if len(titles) > 0 {
				ui.Row(c.Key("doc-stack")).Gap(3).AlignItems(ui.Center).Shrink(0).Children(func() {
					for idx, title := range titles {
						ui.Box(c.Key(idx)).Children(func() { docIcon(c, title, 15) })
					}
				})
			} else {
				ui.Box(c.Key("search-badge")).Size(17, 17).Radius(5).Background(t.Accent.Alpha(0.12)).Center().Shrink(0).Children(func() {
					ui.Icon(c, iconSparkles).FontSize(10.5).TextColor(t.Accent)
				})
			}
			ui.Text(c, fmt.Sprintf(i18n.T("查找了 %d 步 · 阅读 %d 处"), len(turn.steps), reads)).
				FontSize(12).FontWeight(500).TextColor(t.Text.Alpha(0.82)).SingleLine().Shrink(1).MinWidth(0)
			ui.Spacer(c)
			rot := float32(0)
			if open {
				rot = 90
			}
			ui.Box(c.Key("chev")).Size(16, 16).Center().Shrink(0).
				AnimatePose(ui.Pose{Rotate: rot}, ui.Snappy).Children(func() {
				ui.Icon(c, iconChevronRight).FontSize(12).TextColor(t.TextMuted)
			})
		})
		if head.Clicked() {
			turn.expanded = !open
			turn.userToggled = true
		}

		if !open {
			return
		}

		ui.Column(c.Key("steps-body")).
			Transition(ui.ElementTransition{
				Duration: 180 * time.Millisecond,
				Enter:    &ui.Motion{Collapse: true, Y: -4},
				Exit:     &ui.Motion{Collapse: true, Y: -4},
			}).Children(func() {
			ui.Box(c).Height(1).Background(separator(t))
			ui.Column(c.Key("timeline")).Padding(6, 8, 7, 10).Children(func() {
				for i, st := range turn.steps {
					a.qaStepItem(c, spaceID, st, i, len(turn.steps))
				}
			})
		})
	})
}

func (a *App) qaStepItem(c *ui.Context, spaceID string, st *qaStep, index, total int) {
	t := c.Theme()
	meta := stepMeta(st.call)
	hits := st.ensureHits()
	canJump := meta.docPath != "" && !st.failed
	canExpand := meta.docPath == "" && len(hits) > 0
	interactive := canJump || canExpand

	ui.Row(c.Key(fmt.Sprintf("step-%d", index))).Gap(8).AlignItems(ui.Stretch).
		Transition(ui.ElementTransition{
			Duration: 160 * time.Millisecond,
			Enter:    &ui.Motion{Collapse: true, Y: -3, Opacity: 0},
		}).Children(func() {
		// Left timeline rail + circular step node.
		ui.Column(c.Key("rail")).Width(18).AlignItems(ui.Center).Shrink(0).Children(func() {
			topAlpha := float32(0.55)
			if index == 0 {
				topAlpha = 0
			}
			ui.Box(c).Width(1.5).Height(5).Background(t.Border.Alpha(topAlpha))
			qaStepNode(c, st, meta)
			botAlpha := float32(0.55)
			if index == total-1 {
				botAlpha = 0
			}
			ui.Box(c).Width(1.5).Grow(1).MinHeight(5).Background(t.Border.Alpha(botAlpha))
		})

		// Right step body + optional inline hit preview drawer.
		ui.Column(c.Key("main")).Grow(1).MinWidth(0).Padding(1, 0, 2, 0).Children(func() {
			row := ui.Row(c.Key("bar")).Gap(5).Padding(4, 6).Radius(7).MinHeight(24).AlignItems(ui.Center)
			if interactive {
				row.Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).
					Label(strings.TrimSpace(meta.verb + " " + meta.subject))
				if row.Hovered() {
					row.Background(hoverFill(t))
				}
			}
			hovered := row.Hovered()
			row.Children(func() {
				verbInk := t.TextMuted
				subjInk := t.Text.Alpha(0.86)
				if st.failed {
					verbInk, subjInk = t.Danger, t.Danger
				}
				ui.Text(c, meta.verb).FontSize(11.5).TextColor(verbInk).NoWrap()
				if meta.subject != "" {
					ui.Text(c, docName(meta.subject)).FontSize(12).FontWeight(550).TextColor(subjInk).
						SingleLine().Shrink(1).MinWidth(0).Tooltip(meta.subject)
				}
				if meta.scope != "" {
					ui.Box(c.Key("scope")).Padding(1, 5).Radius(4).Background(t.Text.Alpha(0.055)).Shrink(0).Children(func() {
						ui.Text(c, meta.scope).FontSize(10.5).TextColor(t.TextMuted).NoWrap()
					})
				}
				ui.Spacer(c)
				if r := stepResult(st); r != "" {
					resInk := t.TextMuted
					resBg := ui.Color{}
					if st.failed {
						resInk = t.Danger
						resBg = t.Danger.Alpha(0.10)
					} else if !strings.HasPrefix(st.result, "No ") && (st.call.Name == agent.ToolSearch || st.call.Name == agent.ToolGrep) {
						resInk = t.Accent
						resBg = t.Accent.Alpha(0.10)
					}
					ui.Box(c.Key("res")).Padding(1, 6).Radius(5).Background(resBg).Shrink(0).Children(func() {
						ui.Text(c, r).FontSize(10.5).FontWeight(500).TextColor(resInk).NoWrap()
					})
				}
				if canExpand {
					rot := float32(0)
					if st.open {
						rot = 90
					}
					ui.Box(c.Key("hit-chev")).Size(13, 13).Center().Shrink(0).
						AnimatePose(ui.Pose{Rotate: rot}, ui.Snappy).Children(func() {
						ui.Icon(c, iconChevronRight).FontSize(11).TextColor(t.TextMuted)
					})
				} else if canJump && hovered {
					ui.Icon(c, iconArrowUpRight).FontSize(11).TextColor(t.Accent)
				}
			})

			if row.Clicked() {
				switch {
				case canJump:
					a.openCitation(spaceID, agent.Citation{Path: meta.docPath, From: meta.from, To: meta.to})
				case canExpand:
					st.open = !st.open
				}
			}

			if canExpand && st.open {
				ui.Column(c.Key("hits")).Gap(2).Padding(3, 2, 4, 4).
					Transition(ui.ElementTransition{
						Duration: 150 * time.Millisecond,
						Enter:    &ui.Motion{Collapse: true, Y: -3, Opacity: 0},
						Exit:     &ui.Motion{Collapse: true, Y: -3, Opacity: 0},
					}).Children(func() {
					for hi, h := range hits {
						hRow := ui.Column(c.Key(hi)).Gap(2).Padding(4, 7).Radius(6).
							Background(t.Text.Alpha(0.028)).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleLink).Label(h.head)
						if hRow.Hovered() {
							hRow.Background(hoverFill(t))
						}
						hRow.Children(func() {
							iconTitle := h.head
							if h.path != "" {
								iconTitle = vault.Title(h.path)
							}
							ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
								docIcon(c, iconTitle, 13)
								ui.Text(c, docName(h.head)).FontSize(11.5).FontWeight(500).TextColor(t.Text.Alpha(0.84)).
									SingleLine().Shrink(1).MinWidth(0)
								ui.Spacer(c)
								if h.loc != "" {
									ui.Text(c, h.loc).FontSize(10.5).TextColor(t.TextMuted).NoWrap()
								}
							})
							if h.text != "" {
								ui.Text(c, h.text).FontSize(11).TextColor(t.TextMuted).SingleLine().Padding(0, 0, 0, 18)
							}
						})
						if hRow.Clicked() && h.path != "" {
							a.openCitation(spaceID, agent.Citation{Path: h.path, From: max(h.line, 1), To: max(h.line, 1)})
						}
					}
				})
			}
		})
	})
}

func qaStepNode(c *ui.Context, st *qaStep, meta qaStepMeta) {
	t := c.Theme()
	switch {
	case !st.done:
		ui.Box(c.Key("spin")).Size(18, 18).Radius(9).Background(t.Accent.Alpha(0.13)).Center().Shrink(0).Children(func() {
			ui.Spinner(c).Size(11, 11)
		})
	case st.failed:
		ui.Box(c.Key("err")).Size(18, 18).Radius(9).Background(t.Danger.Alpha(0.13)).Center().Shrink(0).Children(func() {
			ui.Icon(c, iconAlert).FontSize(11).TextColor(t.Danger)
		})
	case meta.docPath != "" && st.call.Name == agent.ToolRead:
		ui.Box(c.Key("doc")).Size(18, 18).Center().Shrink(0).Children(func() {
			docIcon(c, meta.subject, 16)
		})
	case st.call.Name == agent.ToolSearch:
		ui.Box(c.Key("spark")).Size(18, 18).Radius(9).Background(t.Accent.Alpha(0.12)).Center().Shrink(0).Children(func() {
			ui.Icon(c, iconSparkles).FontSize(11).TextColor(t.Accent)
		})
	default:
		icon := iconSearch
		switch st.call.Name {
		case agent.ToolGlob:
			icon = iconFolder
		case agent.ToolOutline:
			icon = iconListTree
		case agent.ToolLinks:
			icon = iconLink
		}
		ui.Box(c.Key("ic")).Size(18, 18).Radius(9).Background(t.Text.Alpha(0.06)).Center().Shrink(0).Children(func() {
			ui.Icon(c, icon).FontSize(10.5).TextColor(t.TextMuted)
		})
	}
}

func qaSourcesHeader(c *ui.Context, turn *qaTurn) {
	t := c.Theme()
	ui.Row(c.Key("sources-head")).Padding(6, 2, 0, 2).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconSparkles).FontSize(12).TextColor(t.Accent.Alpha(0.85))
		ui.Text(c, i18n.T("引用来源")).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
		ui.Spacer(c)
		ui.Text(c, fmt.Sprintf(i18n.T("%d 篇文档 · %d 处原文"), len(turn.view.groups), len(turn.view.refs))).
			FontSize(11).TextColor(t.TextMuted.Alpha(0.75)).NoWrap()
	})
}

// qaSourceGroup renders one cited document and all of its recalled passages,
// with clean folder breadcrumbs, section headings, human line numbers and
// plain-text excerpts synced on hover with inline [n] pills.
func (a *App) qaSourceGroup(c *ui.Context, spaceID string, turn *qaTurn, g citeGroup) {
	t := c.Theme()
	groupLit := false
	for _, it := range g.items {
		if turn.hoverCite == it.n {
			groupLit = true
			break
		}
	}
	borderCol := t.Border.Alpha(0.35)
	if groupLit {
		borderCol = t.Accent.Alpha(0.38)
	}

	ui.Column(c.Key("source-group")).Radius(10).
		Background(t.Text.Alpha(0.032)).Border(1, borderCol).Clip().
		Transition(ui.ElementTransition{
			Duration: 180 * time.Millisecond,
			Colors:   true,
			Enter:    &ui.Motion{Y: 5, Opacity: 0},
		}).Children(func() {
		// Document header bar.
		docHead := ui.Row(c.Key("doc-head")).Gap(7).Padding(7, 10).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Focusable().Role(ui.RoleLink).Label(g.title)
		if docHead.Hovered() {
			docHead.Background(hoverFill(t).Alpha(0.65))
		}
		headHover := docHead.Hovered()
		docHead.Children(func() {
			docIcon(c, g.title, 16)
			ui.Text(c, docName(g.title)).FontSize(12.5).FontWeight(600).TextColor(t.Text).
				SingleLine().Shrink(1).MinWidth(0)
			if g.folder != "" {
				ui.Text(c, g.folder).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(2).MinWidth(0)
			}
			ui.Spacer(c)
			jumpColor := t.TextMuted.Alpha(0.65)
			if headHover || groupLit {
				jumpColor = t.Accent
			}
			ui.Icon(c, iconArrowUpRight).FontSize(12).TextColor(jumpColor)
		})
		if docHead.Clicked() && len(g.items) > 0 {
			a.openCitation(spaceID, g.items[0].cite)
		}

		ui.Box(c).Height(1).Background(separator(t))

		// Recalled passages within this document.
		ui.Column(c.Key("passages")).Padding(4).Gap(2).Children(func() {
			for idx, item := range g.items {
				pRow := ui.Column(c.Key(fmt.Sprintf("p-%d", idx))).Gap(3).Padding(5, 7).Radius(7).
					Cursor(ui.CursorPointer).Focusable().Role(ui.RoleLink).
					Label(fmt.Sprintf("%s %s", g.title, humanLines(item.cite.From, item.cite.To)))
				active := pRow.Hovered() || turn.hoverCite == item.n
				if pRow.Hovered() {
					turn.nextHoverCite = item.n
					if turn.hoverCite != item.n {
						c.AnimationFrame()
					}
				}
				if active {
					pRow.Background(t.Accent.Alpha(0.08))
				}
				pRow.Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						citeChip(c, "n", item.n, active)
						if item.section != "" {
							ui.Text(c, "§ "+item.section).FontSize(11.5).FontWeight(500).
								TextColor(t.Text.Alpha(0.78)).SingleLine().Shrink(1).MinWidth(0)
						} else if item.excerpt == "" {
							ui.Text(c, docName(g.title)).FontSize(11.5).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
						}
						ui.Spacer(c)
						coordBg := t.Text.Alpha(0.055)
						coordInk := t.TextMuted
						if active {
							coordBg = t.Accent.Alpha(0.14)
							coordInk = t.Accent
						}
						ui.Box(c.Key("loc")).Padding(1, 6).Radius(5).Background(coordBg).Shrink(0).Children(func() {
							ui.Text(c, humanLines(item.cite.From, item.cite.To)).FontSize(10.5).FontWeight(500).TextColor(coordInk).NoWrap()
						})
					})
					if item.excerpt != "" {
						barCol := t.Border.Alpha(0.85)
						exInk := t.TextMuted
						if active {
							barCol = t.Accent.Alpha(0.6)
							exInk = t.Text.Alpha(0.84)
						}
						ui.Row(c).Gap(7).Padding(1, 2, 1, 4).AlignItems(ui.Stretch).Children(func() {
							ui.Box(c).Width(2).Radius(1).Background(barCol).Shrink(0)
							ui.Text(c, item.excerpt).FontSize(12).LineHeight(1.45).TextColor(exInk).MaxLines(2).Shrink(1).MinWidth(0)
						})
					}
				})
				if pRow.Clicked() {
					a.openCitation(spaceID, item.cite)
				}
			}
		})
	})
}

// citeChip is a citation's number, a small accent pill that lights up solid
// when hovered or synced with an inline citation.
func citeChip(c *ui.Context, key string, n int, active bool) ui.Element {
	t := c.Theme()
	bg := t.Accent.Alpha(0.14)
	fg := t.Accent
	if active {
		bg = t.Accent
		fg = t.AccentText
	}
	return ui.Box(c.Key(key)).Height(16).MinWidth(16).PaddingX(4.5).Radius(5).Center().Background(bg).Shrink(0).
		Children(func() { ui.Text(c, strconv.Itoa(n)).FontSize(10).FontWeight(650).TextColor(fg).NoWrap() })
}

// qaCitationPreview renders a floating preview card above (or below) an inline
// [n] citation pill under the pointer, showing the document header, line range,
// section and recalled plain-text excerpt without scrolling to the sources list.
func (a *App) qaCitationPreview(c *ui.Context, s *qaSession) {
	var (
		ref    citeRef
		anchor ui.Rect
		found  bool
	)
	for _, turn := range s.turns {
		if !turn.hoverInline || turn.hoverCite <= 0 || turn.citeAnchor.W <= 0 {
			continue
		}
		if r, ok := turn.view.byNum[turn.hoverCite]; ok {
			ref = r
			anchor = turn.citeAnchor
			found = true
			break
		}
	}
	if !found {
		return
	}
	t := c.Theme()
	col := previewColors(t, a.look)
	winW, winH := c.Size()
	const cardW = 288
	x := max(min(anchor.X+anchor.W/2-cardW/2, winW-cardW-12), 12)
	above := anchor.Y >= 140
	ui.Overlay(c, func() {
		card := ui.Column(c.Key(fmt.Sprintf("qa-cite-preview-%d", ref.n))).Absolute().Left(x).Width(cardW).
			Padding(9, 11).Gap(5).Radius(10).Background(col.fill).
			Border(1, col.outline).Shadow(0, 10, 26, 0, col.shadow).
			IgnoreInput(true).Transition(ui.ElementTransition{
			Duration: 120 * time.Millisecond,
			Enter:    &ui.Motion{Y: 4, Opacity: 0},
			Exit:     &ui.Motion{Opacity: 0},
		})
		if above {
			card.Bottom(max(winH-anchor.Y+6, 8))
		} else {
			card.Top(min(anchor.Y+anchor.H+6, max(winH-130, 8)))
		}
		card.Children(func() {
			title := ref.title
			if title == "" {
				title = vault.Title(ref.cite.Path)
			}
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				citeChip(c, "chip", ref.n, true)
				docIcon(c, title, 15)
				ui.Text(c, docName(title)).FontSize(12).FontWeight(600).TextColor(col.title).
					SingleLine().Shrink(1).MinWidth(0)
				ui.Spacer(c)
				ui.Box(c).Padding(1, 6).Radius(5).Background(t.Accent.Alpha(0.14)).Shrink(0).Children(func() {
					ui.Text(c, humanLines(ref.cite.From, ref.cite.To)).FontSize(10).FontWeight(500).TextColor(t.Accent).NoWrap()
				})
			})
			if ref.section != "" {
				ui.Text(c, "§ "+ref.section).FontSize(11).FontWeight(500).TextColor(col.muted).SingleLine()
			}
			if ref.excerpt != "" {
				ui.Row(c).Gap(7).Padding(2, 0, 0, 2).AlignItems(ui.Stretch).Children(func() {
					ui.Box(c).Width(2).Radius(1).Background(t.Accent.Alpha(0.65)).Shrink(0)
					ui.Text(c, ref.excerpt).FontSize(11.5).LineHeight(1.45).TextColor(col.title.Alpha(0.88)).
						MaxLines(3).Shrink(1).MinWidth(0)
				})
			}
		})
	})
}
