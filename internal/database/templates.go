package database

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/vault"
)

var zhWeekdays = []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}

// ExpandTemplatePlaceholders replaces {{date}}, {{time}}, {{weekday}}, and {{title}}.
func ExpandTemplatePlaceholders(text string, now time.Time, title string) string {
	if !strings.Contains(text, "{{") {
		return text
	}
	if now.IsZero() {
		now = time.Now()
	}
	return strings.NewReplacer(
		"{{date}}", now.Format("2006-01-02"),
		"{{time}}", now.Format("15:04"),
		"{{weekday}}", zhWeekdays[now.Weekday()],
		"{{title}}", title,
	).Replace(text)
}

// AddTemplate appends tmpl to Database.Templates with full Undo/Redo support.
func (s *Store) AddTemplate(tmpl Template) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return errors.New("database: nil schema")
	}
	snap := tmpl.Clone()
	if strings.TrimSpace(snap.ID) == "" {
		snap.ID = "tpl_" + NewID()[:8]
	}
	if strings.TrimSpace(snap.Name) == "" {
		snap.Name = "未命名模板"
	}
	idx := len(s.db.Templates)
	s.db.Templates = append(s.db.Templates, snap.Clone())
	s.history.push(historyStep{
		label: "add template",
		undo: func(st *Store) []string {
			if idx < len(st.db.Templates) {
				st.db.Templates = slices.Delete(st.db.Templates, idx, idx+1)
			}
			return nil
		},
		redo: func(st *Store) []string {
			pos := min(idx, len(st.db.Templates))
			st.db.Templates = slices.Insert(st.db.Templates, pos, snap.Clone())
			return nil
		},
	})
	s.bumpRevisionLocked()
	return nil
}

// UpdateTemplate replaces an existing template matching tmpl.ID with full Undo/Redo support.
func (s *Store) UpdateTemplate(tmpl Template) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return errors.New("database: nil schema")
	}
	idx := slices.IndexFunc(s.db.Templates, func(t Template) bool { return t.ID == tmpl.ID })
	if idx < 0 {
		return fmt.Errorf("database: template %q not found", tmpl.ID)
	}
	oldSnap := s.db.Templates[idx].Clone()
	newSnap := tmpl.Clone()
	if strings.TrimSpace(newSnap.Name) == "" {
		newSnap.Name = "未命名模板"
	}
	s.db.Templates[idx] = newSnap.Clone()
	s.history.push(historyStep{
		label: "update template",
		undo: func(st *Store) []string {
			if idx < len(st.db.Templates) {
				st.db.Templates[idx] = oldSnap.Clone()
			}
			return nil
		},
		redo: func(st *Store) []string {
			if idx < len(st.db.Templates) {
				st.db.Templates[idx] = newSnap.Clone()
			}
			return nil
		},
	})
	s.bumpRevisionLocked()
	return nil
}

// DeleteTemplate removes the template matching templateID with full Undo/Redo support.
func (s *Store) DeleteTemplate(templateID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return errors.New("database: nil schema")
	}
	idx := slices.IndexFunc(s.db.Templates, func(t Template) bool { return t.ID == templateID })
	if idx < 0 {
		return fmt.Errorf("database: template %q not found", templateID)
	}
	oldSnap := s.db.Templates[idx].Clone()
	s.db.Templates = slices.Delete(s.db.Templates, idx, idx+1)
	s.history.push(historyStep{
		label: "delete template",
		undo: func(st *Store) []string {
			pos := min(idx, len(st.db.Templates))
			st.db.Templates = slices.Insert(st.db.Templates, pos, oldSnap.Clone())
			return nil
		},
		redo: func(st *Store) []string {
			if idx < len(st.db.Templates) {
				st.db.Templates = slices.Delete(st.db.Templates, idx, idx+1)
			}
			return nil
		},
	})
	s.bumpRevisionLocked()
	return nil
}

// CreateRecordFromTemplate creates a new database record instantiated from templateID,
// expanding {{date}}, {{time}}, {{weekday}}, and {{title}} placeholders across title,
// body, and preset properties, and preserving Template.Icon in frontmatter.
func (s *Store) CreateRecordFromTemplate(templateID, titleOverride string, extraProps map[string]Value, now time.Time) (*Record, error) {
	if now.IsZero() {
		now = time.Now()
	}

	s.mu.RLock()
	if s.db == nil {
		s.mu.RUnlock()
		return nil, errors.New("database: nil schema")
	}
	tIdx := slices.IndexFunc(s.db.Templates, func(t Template) bool { return t.ID == templateID })
	if tIdx < 0 {
		s.mu.RUnlock()
		return nil, fmt.Errorf("database: template %q not found", templateID)
	}
	tmpl := s.db.Templates[tIdx].Clone()
	propMap := make(map[string]Property, len(s.propMap))
	for k, v := range s.propMap {
		propMap[k] = v.Clone()
	}
	inline := s.isInlineLocked()
	spaceRoot := s.spaceRoot
	s.mu.RUnlock()

	rawTitle := strings.TrimSpace(titleOverride)
	if rawTitle == "" {
		rawTitle = tmpl.Name
	}
	title := strings.TrimSpace(ExpandTemplatePlaceholders(rawTitle, now, ""))
	if title == "" {
		title = "未命名"
	}

	props := make(map[string]Value, len(tmpl.Props)+len(extraProps))
	for pid, val := range tmpl.Props {
		ev := val.Clone()
		prop, ok := propMap[pid]
		switch {
		case ok && prop.Type == PropDate:
			if ev.Kind == ValText {
				txt := strings.TrimSpace(ExpandTemplatePlaceholders(ev.Text, now, title))
				if strings.EqualFold(txt, "today") {
					txt = now.Format("2006-01-02")
				}
				ev = parseDateTextValue(txt)
			} else if ev.Kind == ValDate {
				ev.Date.Start = ExpandTemplatePlaceholders(ev.Date.Start, now, title)
				if strings.EqualFold(ev.Date.Start, "today") {
					ev.Date.Start = now.Format("2006-01-02")
				}
				ev.Date.End = ExpandTemplatePlaceholders(ev.Date.End, now, title)
			}
		case ev.Kind == ValText:
			ev.Text = ExpandTemplatePlaceholders(ev.Text, now, title)
		case ev.Kind == ValStringList:
			for i, item := range ev.List {
				ev.List[i] = ExpandTemplatePlaceholders(item, now, title)
			}
		}
		if !ev.IsEmpty() {
			props[pid] = ev
		}
	}
	for pid, val := range extraProps {
		if !val.IsEmpty() {
			props[pid] = val.Clone()
		}
	}

	body := ExpandTemplatePlaceholders(tmpl.Body, now, title)
	rec, err := s.CreateRecord(title, props, body)
	if err != nil {
		return nil, err
	}

	icon := strings.TrimSpace(tmpl.Icon)
	if icon != "" {
		s.mu.Lock()
		if cur := s.byID[rec.ID]; cur != nil {
			cur.Icon = icon
		}
		s.mu.Unlock()

		if !inline && rec.Path != "" && spaceRoot != "" {
			if rawMD, rerr := vault.Read(spaceRoot, rec.Path); rerr == nil {
				updated := ApplyFrontmatter(rawMD, func(fm *FrontmatterDoc) {
					fm.Set("icon", FMValue{Kind: FMScalar, Scalar: icon})
				})
				if updated != rawMD && vault.Write(spaceRoot, rec.Path, updated) == nil {
					if full, ferr := vault.Resolve(spaceRoot, rec.Path); ferr == nil {
						if fi, serr := os.Stat(full); serr == nil {
							s.mu.Lock()
							if cur := s.byID[rec.ID]; cur != nil {
								cur.FileSize = fi.Size()
								cur.ModTime = fi.ModTime()
							}
							delete(s.fileCache, rec.Path)
							s.mu.Unlock()
						}
					}
				}
			}
		}
	}
	return rec, nil
}
