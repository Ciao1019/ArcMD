package document

import "strings"

// enterMath recognizes display fences and trailing double-Enter exits without
// reparsing the document or changing ordinary Enter inside multiline TeX.
func (e *Engine) enterMath(leaf Leaf) bool {
	if e.Anchor != e.Caret || e.Caret != leaf.End || e.CurrentStyle().Code {
		return false
	}
	b := leaf.Block
	source := b.Text()
	if b.Kind == "paragraph" && source == "$$" {
		e.Edit(func() {
			b.Kind, b.Runs = "math", nil
			e.Anchor, e.Caret = leaf.Start, leaf.Start
			e.Typing, e.Explicit = Style{}, false
		})
		return true
	}
	fenceExit := strings.HasSuffix(source, "\n$$") || source == "$$"
	doubleEnterExit := strings.HasSuffix(source, "\n")
	if b.Kind != "math" || !fenceExit && !doubleEnterExit {
		return false
	}
	e.Edit(func() {
		body := source
		if fenceExit {
			body = strings.TrimSuffix(strings.TrimSuffix(source, "$$"), "\n")
		} else {
			body = strings.TrimSuffix(source, "\n")
		}
		b.Runs = SliceRuns(b.Runs, 0, Len(body))
		next := NewBlock("paragraph", "")
		parent, index := leaf.Parent, leaf.Index+1
		*parent = append(append(append([]*Block(nil), (*parent)[:index]...), next), (*parent)[index:]...)
		e.FocusBlock(next.ID)
	})
	return true
}

// enterPipeTable compiles a single-line pipe header row such as
// "| Port | Low | High" into a two-row table when Enter is pressed at its end.
func (e *Engine) enterPipeTable(leaf Leaf) bool {
	if e.Anchor != e.Caret || e.Caret != leaf.End || e.CurrentStyle().Code {
		return false
	}
	b := leaf.Block
	if b.Kind != "paragraph" || leaf.Parent == nil {
		return false
	}
	raw := b.Text()
	if strings.ContainsAny(raw, "\r\n\u2028") {
		return false
	}
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "|") {
		return false
	}
	spans := splitPipeCellRanges(raw)
	if len(spans) < 2 {
		return false
	}
	allRule := true
	for _, sp := range spans {
		cellText := raw[ByteOffset(raw, sp[0]):ByteOffset(raw, sp[1])]
		if !isPipeRuleCell(cellText) {
			allRule = false
			break
		}
	}
	e.Edit(func() {
		table := Table(2, len(spans))
		if !allRule {
			for i, sp := range spans {
				runs := SliceRuns(b.Runs, sp[0], sp[1])
				for j := range runs {
					runs[j].Text = strings.ReplaceAll(runs[j].Text, `\|`, "|")
				}
				table.Children[0].Children[i].Runs = Compact(runs)
			}
		}
		parent, index := leaf.Parent, leaf.Index
		(*parent)[index] = table
		firstBody := table.Children[1].Children[0].ID
		e.FocusBlock(firstBody)
		e.Typing, e.Explicit = Style{}, false
	})
	return true
}

// splitPipeCellRanges returns trimmed UTF-16 [start, end) ranges for each cell
// in a pipe row beginning with "|". A trailing "|" is optional when at least
// two pipe delimiters are present.
func splitPipeCellRanges(raw string) [][2]int {
	lead := len(raw) - len(strings.TrimLeft(raw, " \t"))
	if lead >= len(raw) || raw[lead] != '|' {
		return nil
	}
	var pipes []int
	for i := lead; i < len(raw); i++ {
		if raw[i] == '\\' && i+1 < len(raw) {
			i++
			continue
		}
		if raw[i] == '|' {
			pipes = append(pipes, i)
		}
	}
	if len(pipes) < 2 {
		return nil
	}
	end := len(raw)
	tail := strings.TrimRight(raw, " \t")
	hasTrailingPipe := pipes[len(pipes)-1] == len(tail)-1
	if !hasTrailingPipe && len(pipes) < 2 {
		return nil
	}
	var out [][2]int
	count := len(pipes)
	if hasTrailingPipe {
		count--
	}
	for k := 0; k < count; k++ {
		segStart := pipes[k] + 1
		segEnd := end
		if k+1 < len(pipes) {
			segEnd = pipes[k+1]
		}
		seg := raw[segStart:segEnd]
		ltrim := len(seg) - len(strings.TrimLeft(seg, " \t"))
		rtrim := len(strings.TrimRight(seg, " \t"))
		aBytes := segStart + ltrim
		zBytes := segStart + max(ltrim, rtrim)
		out = append(out, [2]int{Len(raw[:aBytes]), Len(raw[:zBytes])})
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

func isPipeRuleCell(s string) bool {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, ":")
	s = strings.TrimSuffix(s, ":")
	if len(s) < 2 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '-' {
			return false
		}
	}
	return true
}
