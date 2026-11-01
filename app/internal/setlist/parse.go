package setlist

import (
	"os"
	"strconv"
	"strings"
)

func ParseFile(path string) (*Setlist, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(string(data)), nil
}

// Parse follows the same shape as internal/song.Parse: header "key: value"
// lines, then [Label] groups, here containing "- book/stem" entries with an
// optional indented "note:" line attached to the entry above it.
func Parse(src string) *Setlist {
	l := &Setlist{}
	var cur *Set
	inHead := true

	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if cur != nil {
				l.Sets = append(l.Sets, *cur)
			}
			cur = &Set{Label: line[1 : len(line)-1]}
			inHead = false
			continue
		}

		if inHead {
			if line == "" {
				continue
			}
			if i := strings.Index(line, ":"); i >= 0 {
				k := strings.TrimSpace(line[:i])
				if isMetaKey(k) {
					l.Set(k, strings.TrimSpace(line[i+1:]))
				}
			}
			continue
		}

		if cur == nil || line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "- "):
			ref := strings.TrimSpace(line[2:])
			if vid, ok := strings.CutPrefix(ref, "variant:"); ok {
				// Person/band-scoped entry: "- variant:123" instead of a
				// book/stem pair — see setlist.go's Entry doc comment.
				id, err := strconv.ParseInt(strings.TrimSpace(vid), 10, 64)
				if err == nil && id > 0 {
					cur.Entries = append(cur.Entries, Entry{VariantID: id})
				}
				continue
			}
			book, stem, _ := strings.Cut(ref, "/")
			cur.Entries = append(cur.Entries, Entry{Book: book, Stem: stem})
		case strings.HasPrefix(line, "note:") && len(cur.Entries) > 0:
			cur.Entries[len(cur.Entries)-1].Note = strings.TrimSpace(line[5:])
		}
	}
	if cur != nil {
		l.Sets = append(l.Sets, *cur)
	}
	return l
}
