package app

import (
	"time"

	"github.com/Ciao1019/ArcMD/internal/spacefs"
	"github.com/egoist/mygo/ui/markdown/parser"
)

type qaRowKind uint8

const (
	qaEmptyRow qaRowKind = iota
	qaQuestionRow
	qaStepsRow
	qaAnswerRow
	qaSourcesRow
	qaStatusRow
)

// A row's identity belongs to its turn and block, so inserting tool results
// or finishing a streamed answer cannot move another turn's focus or anchor.
type qaRow struct {
	turn  *qaTurn
	kind  qaRowKind
	index int
}

func (r qaRow) gap() float32 {
	switch r.kind {
	case qaAnswerRow:
		if r.turn.view.blocks[r.index].kind == qaTableRow {
			return 0
		}
		return 7
	case qaSourcesRow:
		return 10
	default:
		return 10
	}
}

type qaBlockKind uint8

const (
	qaParagraph qaBlockKind = iota
	qaHeading
	qaQuote
	qaListItem
	qaRule
	qaCodeBlock
	qaTableRow
)

type qaBlock struct {
	kind       qaBlockKind
	text       string
	raw        string
	marker     string
	indent     int
	size       float32
	start, end int
	revision   uint32
	language   string
	runs       []qaRun
	cells      []qaCell
	header     bool
	fade       *qaFade
	partBase   int
	partCount  int
	reused     bool
}

type qaTurnView struct {
	epoch                      uint64
	source                     string
	enrichedFor                string
	blocks                     []qaBlock
	refs                       []citeRef
	byNum                      map[int]citeRef
	numbers                    map[string]int
	chipWidths                 map[string]float32
	doc                        *parser.Document
	streaming                  bool
	tailUnits                  int
	revealAt                   time.Time
	animatedSource             string
	animated, animationEnabled bool
}

type qaRowsState struct {
	turn                   *qaTurn
	epoch                  uint64
	answer                 string
	running                bool
	steps, sources, status bool
}

func (t *qaTurn) rowsState() qaRowsState {
	active := t.running || t.revealing
	waiting := active && t.displayAnswer() == ""
	hasSteps := len(t.steps) > 0
	return qaRowsState{
		turn:    t,
		epoch:   t.epoch,
		answer:  t.displayAnswer(),
		running: active,
		steps:   hasSteps,
		sources: !active,
		status:  (waiting && !hasSteps) || t.stopped || t.err != nil,
	}
}

// visibleRows flattens turns into independently virtualized blocks.
func (s *qaSession) visibleRows() []qaRow {
	return s.visibleRowsWith(nil)
}

func (s *qaSession) visibleRowsWith(fs func() *spacefs.FS) []qaRow {
	changed := len(s.rows) == 0 || len(s.turns) != len(s.rowState)
	for i, turn := range s.turns {
		if changed || s.rowState[i] != turn.rowsState() {
			changed = true
			break
		}
	}
	if !changed {
		return s.rows
	}
	clear(s.rows)
	clear(s.rowState)
	s.rows = s.rows[:0]
	s.rowState = s.rowState[:0]
	if len(s.turns) == 0 {
		s.rows = append(s.rows, qaRow{kind: qaEmptyRow})
	}
	var cachedFS *spacefs.FS
	gotFS := false
	getFS := func() *spacefs.FS {
		if !gotFS {
			gotFS = true
			if fs != nil {
				cachedFS = fs()
			}
		}
		return cachedFS
	}
	for _, turn := range s.turns {
		s.rowState = append(s.rowState, turn.rowsState())
		if turn.view.epoch != turn.epoch {
			turn.view = qaTurnView{epoch: turn.epoch}
		}
		active := turn.running || turn.revealing
		turn.view.parse(turn.displayAnswer(), active)
		if len(turn.view.refs) > 0 {
			turn.view.enrich(getFS())
		}
		s.rows = append(s.rows, qaRow{turn: turn, kind: qaQuestionRow})
		// While waiting before any tool starts, show the standalone Ghost status
		// card. Once tool calls arrive, the exploration card integrates the Ghost
		// and live step status into a single unified card.
		waiting := active && turn.displayAnswer() == ""
		if waiting && len(turn.steps) == 0 {
			s.rows = append(s.rows, qaRow{turn: turn, kind: qaStatusRow})
		}
		if len(turn.steps) > 0 {
			s.rows = append(s.rows, qaRow{turn: turn, kind: qaStepsRow})
		}
		for i := range turn.view.blocks {
			s.rows = append(s.rows, qaRow{turn: turn, kind: qaAnswerRow, index: i})
		}
		if !active && len(turn.view.refs) > 0 {
			s.rows = append(s.rows, qaRow{turn: turn, kind: qaSourcesRow})
		}
		if turn.stopped || turn.err != nil {
			s.rows = append(s.rows, qaRow{turn: turn, kind: qaStatusRow})
		}
	}
	return s.rows
}
