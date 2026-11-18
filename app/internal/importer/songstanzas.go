package importer

import "strings"

// SongStanzas cuts a song's own lyrics into stanzas for PlaceLyrics, to
// carry them into another chart of the song (a rebuilt song map, say).
// Lyrics spread over sections keep their section's label; lyrics an
// import left in a single section are cut at their blank lines and
// around the chorus, found as the longest run of two or more lines sung
// again later: each time it comes back it is a stanza labelled Chorus,
// and what's between is labelled by its section (a Verse when that
// section was a chorus). Words and their order are the song's own.
func SongStanzas(body string) []Stanza {
	type ln struct {
		text, label string
		start       bool // a section or blank-line boundary before it
	}
	var lines []ln
	for _, sec := range scanDoc(body).secs {
		boundary := true
		for _, l := range sec.lines {
			switch l.kind {
			case lkLyric:
				if s := sanitizeLyric(l.text); s != "" {
					lines = append(lines, ln{s, strings.TrimSpace(sec.label), boundary})
					boundary = false
				}
			case lkBlank:
				boundary = true
			}
		}
	}
	if len(lines) == 0 {
		return nil
	}
	keys := make([]string, len(lines))
	for i, l := range lines {
		keys[i] = strings.Join(normalizeWords(l.text), " ")
	}
	eq := func(a, b, n int) bool {
		for k := 0; k < n; k++ {
			if keys[a+k] == "" || keys[a+k] != keys[b+k] {
				return false
			}
		}
		return true
	}
	// The chorus: the longest run sung again later, earliest first.
	cs, cl := -1, 0
	for i := range keys {
		for j := i + 2; j < len(keys); j++ {
			n := 0
			for i+n < j && j+n < len(keys) && keys[i+n] != "" && keys[i+n] == keys[j+n] {
				n++
			}
			if n >= 2 && n > cl {
				cs, cl = i, n
			}
		}
	}
	isChorus := make([]bool, len(lines))
	chorusStart := make([]bool, len(lines))
	if cs >= 0 {
		for p := 0; p+cl <= len(lines); {
			if eq(cs, p, cl) {
				chorusStart[p] = true
				for k := 0; k < cl; k++ {
					isChorus[p+k] = true
				}
				p += cl
				continue
			}
			p++
		}
	}
	var out []Stanza
	for i, l := range lines {
		switch {
		case chorusStart[i]:
			out = append(out, Stanza{Label: "Chorus"})
		case isChorus[i]:
		case i == 0 || l.start || isChorus[i-1]:
			label := l.label
			if k := SectionKind(label); k == "chorus" || k == "" {
				label = "Verse"
			}
			out = append(out, Stanza{Label: label})
		}
		out[len(out)-1].Lines = append(out[len(out)-1].Lines, l.text)
	}
	return out
}
