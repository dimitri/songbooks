package tab

import (
	"strconv"
	"strings"
	"testing"
)

// whiskeyIntro is Mercy/whiskey-beer-and-wine/intro.ly's riff, as the
// song's "score[Guitar]:" carries it: ties, a grace note sliding into
// the next, minimumFret changing mid-riff, a dotted half rest, and a
// \set this subset ignores (beamExceptions).
const whiskeyIntro = `\new TabStaff {
   \tabFullNotation
   \tempo 4 = 94
   \set TabStaff.minimumFret = 8
   \set TabStaff.restrainOpenStrings = ##t
   \set Timing.beamExceptions = #'()

   c4 c'16 bes g f~ f8 ees8~ \grace f16( \glissando g8)

   \set TabStaff.minimumFret = 10
   ees'8 c'4 r2.

   \set TabStaff.minimumFret = 8
   c4 c'16 bes g f~ f8 ees8~ \grace f16( \glissando g8)

   \set TabStaff.minimumFret = 10
   bes8 c'4 r2.
}`

func TestWhiskeyRiffFeatures(t *testing.T) {
	r, err := ParseVoice("Guitar", whiskeyIntro)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Bars) != 4 {
		t.Fatalf("bars = %d, want 4 (LilyPond's bars: the riff's lines don't end on barlines)", len(r.Bars))
	}
	frets := func(b Bar) string {
		var s []string
		for _, beat := range b.Beats {
			if beat.Rest {
				s = append(s, "r"+beat.Duration)
				continue
			}
			for _, n := range beat.Notes {
				f := strconv.Itoa(n.Fret) + "/" + strconv.Itoa(n.String)
				if n.Tied {
					f = "~" + f
				}
				if beat.HasSlideFrom {
					f = strconv.Itoa(beat.SlideFromFret) + "\\" + f
				}
				s = append(s, f)
			}
		}
		return strings.Join(s, " ")
	}
	// As LilyPond prints it (the legacy Mercy book, page 37): the tied f
	// on the same string and fret, the grace f sliding into g on the A
	// string, ees' at minimumFret 10 on the D string, bes on the A string.
	want := []string{
		"8/6 10/4 8/4 10/5 8/5 ~8/5 11/6 8\\10/5 13/4",
		"10/4 r2.",
		"8/6 10/4 8/4 10/5 8/5 ~8/5 11/6 8\\10/5 13/5",
		"10/4 r2.",
	}
	for i, w := range want {
		if got := frets(r.Bars[i]); got != w {
			t.Errorf("bar %d = %q, want %q", i+1, got, w)
		}
	}
	tex := ToAlphaTex(r, "", []string{"Cm", "", "", ""})
	for _, s := range []string{`(8.5{t}).8`, `(8.5{sl}).32{gr} 10.5.8`, `r.2{d}`, `{ch "Cm"}`} {
		if !strings.Contains(tex, s) {
			t.Errorf("alphaTeX lacks %q:\n%s", s, tex)
		}
	}
}

// A tie only joins notes of the same pitch (LilyPond ignores the rest,
// with a warning): the tied note keeps its string and fret even when
// minimumFret changes in between; a chord ties too.
func TestTies(t *testing.T) {
	r, err := ParseVoice("Guitar", `\new TabStaff { \set TabStaff.minimumFret = 0 g4~ \set TabStaff.minimumFret = 5 g4 a4~ b4 <c' e'>4~ <c' e'>4 }`)
	if err != nil {
		t.Fatal(err)
	}
	b := r.Bars[0].Beats
	if !b[1].Notes[0].Tied || b[1].Notes[0].Fret != b[0].Notes[0].Fret || b[1].Notes[0].String != b[0].Notes[0].String {
		t.Errorf("tied g = %+v after %+v: want the same string and fret, tied", b[1].Notes[0], b[0].Notes[0])
	}
	if b[3].Notes[0].Tied {
		t.Errorf("a~ b: b tied, want not (different pitch)")
	}
	c := r.Bars[1].Beats
	for _, n := range c[1].Notes {
		if !n.Tied {
			t.Errorf("chord tie: %+v not tied", n)
		}
	}
}

// ToAlphaTexBars writes each bar's metadata ahead of its notes.
func TestToAlphaTexBarsMeta(t *testing.T) {
	r, err := ParseVoice("Guitar", `\new TabStaff { e4 e e e e e e e }`)
	if err != nil {
		t.Fatal(err)
	}
	tex := ToAlphaTexBars(r, "", nil, []string{`\tempo 94 \ro`, `\rc 2`})
	if !strings.Contains(tex, ".\n\\tempo 94 \\ro 2.4.4") || !strings.Contains(tex, `| \rc 2 2.4.4`) {
		t.Errorf("tex = %q", tex)
	}
}
