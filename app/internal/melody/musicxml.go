package melody

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

// MusicXML is the sheet as a MusicXML 4.0 score (partwise, one voice,
// its lyrics), for MuseScore, Sibelius, Dorico and the like.
func (s Score) MusicXML() []byte {
	var b bytes.Buffer
	b.WriteString(xml.Header)
	b.WriteString(`<!DOCTYPE score-partwise PUBLIC "-//Recordare//DTD MusicXML 4.0 Partwise//EN" "http://www.musicxml.org/dtds/partwise.dtd">` + "\n")
	b.WriteString(`<score-partwise version="4.0">` + "\n")
	if s.Title != "" {
		fmt.Fprintf(&b, "  <work><work-title>%s</work-title></work>\n", esc(s.Title))
	}
	b.WriteString("  <identification><encoding><software>Songbooks (estimated from the recording)</software></encoding></identification>\n")
	b.WriteString(`  <part-list><score-part id="P1"><part-name>Voice</part-name></score-part></part-list>` + "\n")
	b.WriteString(`  <part id="P1">` + "\n")
	low := s.medianNote() < lowClef
	var meter Meter
	for bi, bar := range s.Sheet.Bars {
		fmt.Fprintf(&b, "    <measure number=\"%d\">\n", bi+1)
		if bi == 0 || bar.Meter != meter {
			b.WriteString("      <attributes>")
			if bi == 0 {
				mode := "major"
				if s.Key.Minor {
					mode = "minor"
				}
				fmt.Fprintf(&b, "<divisions>4</divisions><key><fifths>%d</fifths><mode>%s</mode></key>", s.Key.Fifths, mode)
			}
			fmt.Fprintf(&b, "<time><beats>%d</beats><beat-type>%d</beat-type></time>", bar.Meter.Num, bar.Meter.Den)
			if bi == 0 {
				b.WriteString("<clef><sign>G</sign><line>2</line>")
				if low {
					b.WriteString("<clef-octave-change>-1</clef-octave-change>")
				}
				b.WriteString("</clef>")
			}
			b.WriteString("</attributes>\n")
			meter = bar.Meter
		}
		if bi == 0 && s.Tempo > 0 {
			fmt.Fprintf(&b, "      <direction placement=\"above\"><direction-type><metronome><beat-unit>quarter</beat-unit><per-minute>%d</per-minute></metronome></direction-type><sound tempo=\"%d\"/></direction>\n", int(s.Tempo+0.5), int(s.Tempo+0.5))
		}
		if l, ok := s.Sections[bar.Played]; ok && l != "" {
			fmt.Fprintf(&b, "      <direction placement=\"above\"><direction-type><rehearsal>%s</rehearsal></direction-type></direction>\n", esc(l))
		}
		for _, ev := range bar.Events {
			s.writeNote(&b, bar, ev)
		}
		b.WriteString("    </measure>\n")
	}
	b.WriteString("  </part>\n</score-partwise>\n")
	return b.Bytes()
}

var typeNames = map[int]string{1: "whole", 2: "half", 4: "quarter", 8: "eighth", 16: "16th"}

func (s Score) writeNote(b *bytes.Buffer, bar Bar, ev Event) {
	d := durations[ev.Len]
	b.WriteString("      <note>")
	if ev.Midi < 0 {
		if ev.Len == bar.Meter.Ticks() {
			fmt.Fprintf(b, `<rest measure="yes"/><duration>%d</duration></note>`+"\n", ev.Len)
			return
		}
		b.WriteString("<rest/>")
	} else {
		step, alter, oct := s.Key.spell(ev.Midi + s.Transpose)
		b.WriteString("<pitch><step>" + step + "</step>")
		if alter != 0 {
			fmt.Fprintf(b, "<alter>%d</alter>", alter)
		}
		fmt.Fprintf(b, "<octave>%d</octave></pitch>", oct)
	}
	fmt.Fprintf(b, "<duration>%d</duration>", ev.Len)
	if ev.Midi >= 0 {
		if ev.TieIn {
			b.WriteString(`<tie type="stop"/>`)
		}
		if ev.TieOut {
			b.WriteString(`<tie type="start"/>`)
		}
	}
	fmt.Fprintf(b, "<type>%s</type>", typeNames[d.v])
	if d.dot {
		b.WriteString("<dot/>")
	}
	if ev.Midi >= 0 {
		n := s.Sheet.Notes[ev.Note]
		if n.Low {
			// An unsure note: its head in parentheses.
			b.WriteString(`<notehead parentheses="yes">normal</notehead>`)
		}
		if ev.TieIn || ev.TieOut {
			b.WriteString("<notations>")
			if ev.TieIn {
				b.WriteString(`<tied type="stop"/>`)
			}
			if ev.TieOut {
				b.WriteString(`<tied type="start"/>`)
			}
			b.WriteString("</notations>")
		}
		if s.Lyrics != nil && !ev.TieIn {
			if l := s.Lyrics[ev.Note]; len(l.Syls) > 0 {
				first, last := l.Syls[0], l.Syls[len(l.Syls)-1]
				syllabic := "single"
				switch {
				case first.WordStart && !last.WordEnd:
					syllabic = "begin"
				case !first.WordStart && last.WordEnd:
					syllabic = "end"
				case !first.WordStart && !last.WordEnd:
					syllabic = "middle"
				}
				var txt strings.Builder
				for i, sy := range l.Syls {
					if i > 0 && sy.WordStart {
						txt.WriteByte(' ')
					}
					txt.WriteString(sy.Text)
				}
				fmt.Fprintf(b, `<lyric number="1"><syllabic>%s</syllabic><text>%s</text></lyric>`, syllabic, esc(txt.String()))
			}
		}
	}
	b.WriteString("</note>\n")
}

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
