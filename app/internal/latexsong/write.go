package latexsong

import (
	"strings"

	"app/internal/song"
)

// escapeLatex re-escapes the one special character this corpus's titles/
// artists actually carry (see unescapeLatex) — the inverse direction.
func escapeLatex(s string) string {
	return strings.ReplaceAll(s, "&", `\&`)
}

// isChorus guesses whether a section should export as \beginchorus
// rather than \beginverse — the modern .song grammar has no scope/kind
// field of its own to carry this distinction losslessly (see Section's
// own doc comment: "Verse"/"Chorus" is just a label string here), so a
// case-insensitive label match is the best available signal, same as
// how \renewcommand{\everychorus} in the corpus's own preamble.tex
// treats "Chorus" as the one recognized kind.
func isChorus(label string) bool {
	return strings.Contains(strings.ToLower(label), "chorus")
}

// Export renders s as LaTeX "songs"-package source — the inverse of
// Parse, built to re-use the same four constructs Parse understands
// (\beginverse/\beginchorus, \textnote for a standalone label, \[Chord]
// word, {\nolyrics ...} for a chord-only section) so a round trip
// through Parse(Export(p)) recovers the same structure, though not
// necessarily byte-identical source (same caveat internal/song.Write
// already carries for its own round-trip: canonical re-formatting, not
// a reproduction of a human author's exact layout).
func Export(s *song.Song) string {
	var b strings.Builder
	title := escapeLatex(s.Get("title"))
	artist := escapeLatex(s.Get("artist"))
	b.WriteString(`\beginsong{` + title + `}`)
	if artist != "" {
		b.WriteString(`[by={` + artist + `}]`)
	}
	b.WriteString("\n")
	if capo := s.Get("capo"); capo != "" {
		b.WriteString("  \\capo{" + capo + "}\n")
	}
	if transpose := s.Get("transpose"); transpose != "" {
		b.WriteString("  \\transpose{" + transpose + "}\n")
	}

	for _, sec := range s.Sections {
		b.WriteString("\n")
		hasLyrics := len(sec.Lyrics) > 0
		hasGrid := len(sec.Measures) > 0

		if !hasLyrics {
			// A lyric-less section — either a bare instrumental marker
			// (no grid either) or a chord-only passage (grid, no words):
			// both only have a \textnote label in this format, since
			// \beginverse/\beginchorus always carries lyric text in the
			// real corpus.
			b.WriteString("  \\textnote{" + sec.Label + "}\n")
			if hasGrid {
				b.WriteString("  {\\nolyrics " + exportGridChords(sec.Measures) + "}\n")
			}
			continue
		}

		env := "verse"
		if isChorus(sec.Label) {
			env = "chorus"
		} else if sec.Label != "Verse" {
			// Only a label that ISN'T the plain default needs a
			// \textnote — reusing it for every verse would read back
			// through Parse as that label winning, which is still
			// correct (Parse defaults to "Verse" only when nothing
			// precedes it), but prints "Verse" labels nobody wrote.
			b.WriteString("  \\textnote{" + sec.Label + "}\n")
		}
		b.WriteString("  \\begin" + env + "\n")
		for _, ln := range sec.Lyrics {
			b.WriteString("  " + exportLyricLine(ln) + "\n")
		}
		b.WriteString("  \\end" + env + "\n")
	}
	b.WriteString("\\endsong\n")
	return b.String()
}

func exportGridChords(measures []song.Measure) string {
	var parts []string
	var last string
	for _, m := range measures {
		for _, tok := range m.Tokens {
			c := song.TokenChord(tok)
			if c == "%" {
				c = last
			}
			if c == "" {
				continue
			}
			parts = append(parts, `\[`+c+`]`)
			last = c
		}
	}
	return strings.Join(parts, " ")
}

func exportLyricLine(ln song.LyricLine) string {
	var b strings.Builder
	for _, seg := range ln.Segments {
		if seg.HasChord {
			b.WriteString(`\[` + seg.Chord + `]`)
		}
		b.WriteString(seg.Text)
	}
	return b.String()
}
