// Package setlist is a small, deliberately .song-shaped format for gig/
// rehearsal set lists: header fields, then one or more [Set N] groups of
// songs, each entry a "book/stem" reference plus an optional free-text note
// — "multiple sets" (first set, second set, encore...) is first-class, not
// a single flat song list.
package setlist

// MetaKeys are the recognized header fields.
var MetaKeys = []string{"title", "date", "venue"}

func isMetaKey(k string) bool {
	for _, mk := range MetaKeys {
		if mk == k {
			return true
		}
	}
	return false
}

type MetaField struct {
	Key   string
	Value string
}

// Entry is one setlist line: either a project-scoped book/stem reference
// (the original, still-supported shape) or, for a person/band-scoped
// setlist whose entries point directly into that owner's catalog, a
// VariantID reference instead. Exactly one of (Book+Stem) or VariantID is
// set on any given Entry — never both, never neither (Parse never
// produces such a row).
type Entry struct {
	Book      string
	Stem      string
	VariantID int64
	Note      string
}

// IsVariant reports whether e references a variant directly rather than a
// book/stem pair.
func (e Entry) IsVariant() bool { return e.VariantID != 0 }

type Set struct {
	Label   string // e.g. "Set 1", "Encore" — free text, like a .song section label
	Entries []Entry
}

type Setlist struct {
	Meta []MetaField
	Sets []Set
}

func (l *Setlist) Get(key string) string {
	for _, f := range l.Meta {
		if f.Key == key {
			return f.Value
		}
	}
	return ""
}

func (l *Setlist) Set(key, value string) {
	for i, f := range l.Meta {
		if f.Key == key {
			l.Meta[i].Value = value
			return
		}
	}
	l.Meta = append(l.Meta, MetaField{Key: key, Value: value})
}
