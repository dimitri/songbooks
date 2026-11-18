package main

import (
	"regexp"
	"strings"
	"unicode"

	"app/internal/importer"
)

// A song's body, made a demo song's: the same chords, grids, sections,
// tempo, key, time, markers and Guitar Pro references, under an invented
// title and artist, with no lyrics, no comments and nothing that points
// at the real recording or album.

var sectionHeaderRE = regexp.MustCompile(`^\[([^\[\]]+)\](\s*\(repeat\))?$`)

// Header lines dropped outright: the reference video, the album cover.
var droppedHeaders = map[string]bool{"reference": true, "cover": true, "cover-credit": true}

// Words a section label may be made of and keep: the parts of a song,
// instruments, numbers. A label with any other word (a line of the song,
// a name) is renamed after its kind.
var labelWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`verse verses chorus refrain pre post intro outro solo bridge head
		interlude instrumental instru breakdown break riff riffs couplet couplets pont song chords
		bass basse guitar guitare harmonica synth piano drums batterie keys sax organ orgue
		ending end fade out turnaround vamp pickup opening shuffle spoken coda tag hook
		up down a semitone half step and et with avec x b c d e f g i ii iii iv v vi part theme
		chrous final last first second third double`) {
		labelWords[w] = true
	}
}

// labelOK reports whether a section label says only what part it is.
func labelOK(label string) bool {
	words := strings.FieldsFunc(strings.ToLower(label), func(r rune) bool { return !unicode.IsLetter(r) })
	for _, w := range words {
		if !labelWords[w] {
			return false
		}
	}
	return true
}

// relabel is what a label that doesn't pass labelOK becomes.
func relabel(label string) string {
	switch importer.SectionKind(label) {
	case "verse":
		return "Verse"
	case "chorus":
		return "Chorus"
	case "pre-chorus":
		return "Pre-Chorus"
	case "post-chorus":
		return "Post-Chorus"
	case "bridge":
		return "Bridge"
	case "solo":
		return "Solo"
	case "intro":
		return "Intro"
	case "outro":
		return "Outro"
	case "interlude":
		return "Interlude"
	}
	return "Part"
}

// sungKinds are the sections invented lyrics go in.
var sungKinds = map[string]bool{"verse": true, "chorus": true, "pre-chorus": true, "post-chorus": true, "bridge": true}

// DemoBody rewrites a song's body: every lyric and comment line out
// (importer.WithoutLyrics), title/artist/gp_source set, reference and
// cover lines out, labels that carry words renamed (in map: too). With
// lyrics, each sung section gets a few of the invented lines lyrics(i)
// gives for its i-th sung section.
func DemoBody(body, title, artist, gpSource string, lyrics func(kind string, i int) []string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	// Comments go first: WithoutLyrics keeps them.
	var kept []string
	for _, l := range strings.Split(body, "\n") {
		// So do free-text notes and the retired "score: <label>" pointer
		// (which the parser ignores), both free to name anyone.
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "note:") || strings.HasPrefix(t, "score:") {
			continue
		}
		kept = append(kept, l)
	}
	body = importer.WithoutLyrics(strings.Join(kept, "\n"))

	lines := strings.Split(body, "\n")
	renamed := map[string]string{}
	inHead := true
	sawTitle, sawArtist := false, false
	var out []string
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if m := sectionHeaderRE.FindStringSubmatch(line); m != nil {
			if inHead {
				out = headerFill(out, title, artist, sawTitle, sawArtist)
				inHead = false
			}
			label := m[1]
			if !labelOK(label) {
				nl := relabel(label)
				renamed[strings.TrimSpace(label)] = nl
				raw = "[" + nl + "]" + m[2]
			}
			out = append(out, raw)
			continue
		}
		if inHead {
			k, _, ok := strings.Cut(line, ":")
			k = strings.TrimSpace(k)
			if ok {
				switch {
				case droppedHeaders[k]:
					continue
				case k == "title":
					raw, sawTitle = "title: "+title, true
				case k == "artist":
					raw, sawArtist = "artist: "+artist, true
				case k == "gp_source":
					if gpSource == "" {
						continue
					}
					raw = "gp_source: " + gpSource
				}
			}
		}
		out = append(out, raw)
	}
	if inHead {
		out = headerFill(out, title, artist, sawTitle, sawArtist)
	}
	if len(renamed) > 0 {
		for i, l := range out {
			if v, ok := strings.CutPrefix(strings.TrimSpace(l), "map:"); ok {
				parts := strings.Split(v, "·")
				for j, p := range parts {
					if nl, ok := renamed[strings.TrimSpace(p)]; ok {
						parts[j] = " " + nl + " "
					}
				}
				out[i] = "map: " + strings.TrimSpace(strings.Join(parts, "·"))
			}
		}
	}
	if lyrics != nil {
		out = addLyrics(out, lyrics)
	}
	return strings.Join(out, "\n")
}

// headerFill adds the title and artist lines a header lacked, first.
func headerFill(out []string, title, artist string, sawTitle, sawArtist bool) []string {
	var add []string
	if !sawTitle {
		add = append(add, "title: "+title)
	}
	if !sawArtist {
		add = append(add, "artist: "+artist)
	}
	if len(add) == 0 {
		return out
	}
	return append(add, out...)
}

// addLyrics puts lines at the end of each sung section, after a blank
// line when the section has a score block (which a blank line ends).
func addLyrics(lines []string, lyrics func(kind string, i int) []string) []string {
	var out []string
	sung := 0
	var pending []string
	flush := func() {
		if pending == nil {
			return
		}
		// Back over the section's trailing blank lines.
		n := len(out)
		for n > 0 && strings.TrimSpace(out[n-1]) == "" {
			n--
		}
		tail := append([]string{}, out[n:]...)
		out = append(out[:n], pending...)
		out = append(out, tail...)
		pending = nil
	}
	hasScore := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if m := sectionHeaderRE.FindStringSubmatch(t); m != nil {
			flush()
			hasScore = false
			out = append(out, l)
			kind := importer.SectionKind(m[1])
			if sungKinds[kind] && m[2] == "" {
				if ls := lyrics(kind, sung); len(ls) > 0 {
					pending = ls
				}
				sung++
			}
			continue
		}
		if strings.HasPrefix(t, "score[") {
			hasScore = true
		}
		if hasScore && pending != nil && len(pending) > 0 && pending[0] != "" {
			pending = append([]string{""}, pending...)
		}
		out = append(out, l)
	}
	flush()
	return out
}
