package audioanalysis

import (
	"math"
	"testing"
)

// track builds frames from pieces: each a pitch (0 silence) held for
// secs, an optional vibrato depth (semitones) and a glide to the next
// piece's pitch.
type piece struct {
	pitch, secs, vibrato float64
	glide                bool
}

func track(ps ...piece) Frames {
	var f Frames
	for k, p := range ps {
		n := int(math.Round(p.secs / FrameSeconds))
		for i := range n {
			v := p.pitch
			if p.pitch > 0 && p.glide && k+1 < len(ps) {
				v += (ps[k+1].pitch - p.pitch) * float64(i) / float64(n)
			}
			if p.pitch > 0 && p.vibrato > 0 {
				v += p.vibrato * math.Sin(2*math.Pi*5.5*float64(i)*FrameSeconds)
			}
			f.Pitch = append(f.Pitch, v)
			e, c := 0.0, 0.0
			if p.pitch > 0 {
				e, c = 1, 0.9
			}
			f.Energy = append(f.Energy, e)
			f.Clarity = append(f.Clarity, c)
		}
	}
	return f
}

func pitches(ns []SungNote) []int {
	out := make([]int, len(ns))
	for i, n := range ns {
		out[i] = int(math.Round(n.Pitch))
	}
	return out
}

func eqInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSegmentSteadyNotes(t *testing.T) {
	// Three notes with breaths, then two notes with no breath between.
	f := track(piece{pitch: 60, secs: 0.5}, piece{secs: 0.1}, piece{pitch: 62, secs: 0.25}, piece{secs: 0.1},
		piece{pitch: 64, secs: 1}, piece{pitch: 67, secs: 0.5}, piece{secs: 0.3})
	ns := SegmentNotes(f)
	if got := pitches(ns); !eqInts(got, []int{60, 62, 64, 67}) {
		t.Fatalf("notes %v, want 60 62 64 67", got)
	}
	if math.Abs(ns[0].Start) > 0.03 || math.Abs(ns[0].End-0.5) > 0.03 {
		t.Errorf("first note %.2f–%.2f, want 0–0.5", ns[0].Start, ns[0].End)
	}
	if math.Abs(ns[3].Start-1.95) > 0.1 {
		t.Errorf("the note after the leap starts at %.2f, want ~1.95", ns[3].Start)
	}
	for _, n := range ns {
		if n.Conf < LowConf {
			t.Errorf("steady note %v marked unsure (%.2f)", n.Pitch, n.Conf)
		}
	}
}

func TestSegmentVibratoIsOneNote(t *testing.T) {
	// A held note with a wide vibrato (±0.6 semitone) is one note, at
	// its centre.
	ns := SegmentNotes(track(piece{pitch: 65, secs: 2, vibrato: 0.6}, piece{secs: 0.2}))
	if len(ns) != 1 || math.Round(ns[0].Pitch) != 65 {
		t.Fatalf("vibrato: %v, want one note 65", pitches(ns))
	}
	if ns[0].Conf < LowConf {
		t.Errorf("a vibrato is steady: conf %.2f", ns[0].Conf)
	}
}

func TestSegmentSlide(t *testing.T) {
	// 60 held, a slide up to 67 over 0.3 s, 67 held: the two held
	// notes, and whatever the slide leaves is short and unsure or gone.
	ns := SegmentNotes(track(piece{pitch: 60, secs: 0.6}, piece{pitch: 60, secs: 0.3, glide: true}, piece{pitch: 67, secs: 0.6}, piece{secs: 0.2}))
	if len(ns) < 2 {
		t.Fatalf("slide: %v", pitches(ns))
	}
	first, last := ns[0], ns[len(ns)-1]
	if math.Round(first.Pitch) != 60 || math.Round(last.Pitch) != 67 {
		t.Fatalf("slide: %v, want 60 … 67", pitches(ns))
	}
	for _, n := range ns[1 : len(ns)-1] {
		if n.End-n.Start > 0.3 {
			t.Errorf("a note %.0f of %.2f s in the slide", n.Pitch, n.End-n.Start)
		}
	}
}

func TestSegmentSilenceAndBlips(t *testing.T) {
	if ns := SegmentNotes(track(piece{secs: 3})); len(ns) != 0 {
		t.Fatalf("silence: %v", pitches(ns))
	}
	// A 50 ms blip between breaths is no note; a frame YIN missed inside
	// a note doesn't cut it.
	f := track(piece{pitch: 70, secs: 0.05}, piece{secs: 0.2}, piece{pitch: 62, secs: 0.8}, piece{secs: 0.2})
	f.Pitch[int(math.Round(0.25/FrameSeconds))+10] = 0
	ns := SegmentNotes(f)
	if got := pitches(ns); !eqInts(got, []int{62}) {
		t.Fatalf("blip and dropout: %v, want 62", got)
	}
}

func TestSegmentOctaveSlip(t *testing.T) {
	// A frame or two an octave up in a held note is folded back.
	f := track(piece{pitch: 57, secs: 1}, piece{secs: 0.2})
	f.Pitch[20], f.Pitch[21] = 69, 69
	ns := SegmentNotes(f)
	if got := pitches(ns); !eqInts(got, []int{57}) {
		t.Fatalf("octave slip: %v, want 57", got)
	}
}

func TestSegmentRepeatedNoteOnTwoSyllables(t *testing.T) {
	// One pitch, the loudness dipping between two syllables: two notes.
	f := track(piece{pitch: 64, secs: 0.8}, piece{secs: 0.2})
	mid := int(math.Round(0.4 / FrameSeconds))
	for i := mid - 2; i <= mid+2; i++ {
		f.Energy[i] = 0.1
	}
	f.Energy[mid] = 0.05
	ns := SegmentNotes(f)
	if got := pitches(ns); !eqInts(got, []int{64, 64}) {
		t.Fatalf("two syllables: %v, want 64 64", got)
	}
}

// TestMelodyNotesOfSungLine: the whole path on a sung line (generated,
// as for the range): its notes in order.
func TestMelodyNotesOfSungLine(t *testing.T) {
	line := []int{60, 62, 64, 65, 67, 65, 64, 62, 60, 67}
	m, ok := MelodyNotesOf(sungLine(line, 0.4))
	if !ok {
		t.Fatalf("no melody: %d notes", len(m.Notes))
	}
	if got := pitches(m.Notes); !eqInts(got, line) {
		t.Fatalf("notes %v, want %v", got, line)
	}
}
