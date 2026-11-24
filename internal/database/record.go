package database

import (
	"maps"
	"slices"
	"time"
)

// Record represents a single row/page inside a database.
// In folder/space mode, Path points to the backing .md file relative to the space root.
// In inline mode, Path is empty and Body stores optional inline Markdown notes.
type Record struct {
	ID        string           `json:"id"`
	Path      string           `json:"path,omitempty"`
	Title     string           `json:"title,omitempty"`
	Icon      string           `json:"icon,omitempty"`
	Cover     string           `json:"cover,omitempty"`
	CoverY    float64          `json:"coverY,omitempty"`
	Excerpt   string           `json:"excerpt,omitempty"`
	Props     map[string]Value `json:"props,omitempty"`
	Computed  map[string]Value `json:"-"`
	Tags      []string         `json:"tags,omitempty"`
	Body      string           `json:"body,omitempty"`
	CreatedAt time.Time        `json:"createdAt,omitzero"`
	UpdatedAt time.Time        `json:"updatedAt,omitzero"`
	FileSize  int64            `json:"-"`
	ModTime   time.Time        `json:"-"`
}

// LiveDoc overlays unsaved in-memory edits from open editors onto the database
// store without writing to disk.
type LiveDoc struct {
	Path      string
	Text      string
	UpdatedAt time.Time
}

// Clone returns a deep copy of r.
func (r *Record) Clone() *Record {
	if r == nil {
		return nil
	}
	out := *r
	if r.Props != nil {
		out.Props = make(map[string]Value, len(r.Props))
		for k, v := range r.Props {
			out.Props[k] = v.Clone()
		}
	}
	if r.Computed != nil {
		out.Computed = make(map[string]Value, len(r.Computed))
		for k, v := range r.Computed {
			out.Computed[k] = v.Clone()
		}
	}
	if r.Tags != nil {
		out.Tags = append([]string(nil), r.Tags...)
	}
	return &out
}

// EqualContent reports whether two records have identical persistent fields
// (ignoring transient stat metadata FileSize/ModTime).
func (r *Record) EqualContent(other *Record) bool {
	if r == nil || other == nil {
		return r == other
	}
	if r.ID != other.ID || r.Path != other.Path || r.Title != other.Title ||
		r.Icon != other.Icon || r.Cover != other.Cover || r.CoverY != other.CoverY ||
		r.Excerpt != other.Excerpt || r.Body != other.Body ||
		!r.CreatedAt.Equal(other.CreatedAt) || !r.UpdatedAt.Equal(other.UpdatedAt) {
		return false
	}
	if !slices.Equal(r.Tags, other.Tags) {
		return false
	}
	return maps.EqualFunc(r.Props, other.Props, func(a, b Value) bool {
		return a.Equal(b)
	})
}

// ValueFor returns the record's value for prop, synthesizing built-in fields
// such as PropTitle, PropCreatedTime, and PropEditedTime when applicable.
func (r *Record) ValueFor(prop Property) Value {
	if r == nil {
		return EmptyValue()
	}
	switch prop.Type {
	case PropTitle:
		if r.Title != "" {
			return TextValue(r.Title)
		}
		if v, ok := r.Props[prop.ID]; ok {
			return v
		}
		return EmptyValue()
	case PropCreatedTime:
		if !r.CreatedAt.IsZero() {
			return DateRangeValue(r.CreatedAt.UTC().Format(time.RFC3339), "", true)
		}
	case PropEditedTime:
		if !r.UpdatedAt.IsZero() {
			return DateRangeValue(r.UpdatedAt.UTC().Format(time.RFC3339), "", true)
		}
	case PropRollup, PropFormula:
		if r.Computed != nil {
			if v, ok := r.Computed[prop.ID]; ok {
				return v
			}
		}
		return EmptyValue()
	}
	if v, ok := r.Props[prop.ID]; ok {
		return v
	}
	return EmptyValue()
}
