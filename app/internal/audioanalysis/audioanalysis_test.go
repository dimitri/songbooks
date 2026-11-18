package audioanalysis

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"math/cmplx"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFFTMatchesNaiveDFT(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 8))
	const n = 64
	x := make([]complex128, n)
	for i := range x {
		x[i] = complex(rng.Float64()-0.5, rng.Float64()-0.5)
	}
	want := make([]complex128, n)
	for k := range n {
		for j := range n {
			want[k] += x[j] * cmplx.Exp(complex(0, -2*math.Pi*float64(k*j)/n))
		}
	}
	got := append([]complex128(nil), x...)
	fft(got)
	for k := range n {
		if cmplx.Abs(got[k]-want[k]) > 1e-9 {
			t.Fatalf("bin %d: got %v, want %v", k, got[k], want[k])
		}
	}
	ifft(got)
	for i := range n {
		if cmplx.Abs(got[i]-x[i]) > 1e-12 {
			t.Fatalf("round trip sample %d: got %v, want %v", i, got[i], x[i])
		}
	}
}

func TestTempoAndMetre(t *testing.T) {
	cases := []struct {
		name    string
		bpm     float64 // in the beat unit
		groove  groove
		wantSig string
	}{
		{"4/4 rock at 120, eighth-note hats", 120, rock4, "4/4"},
		{"4/4 at 92, sixteenth-note hats", 92, funk4, "4/4"},
		{"4/4 rock at 72", 72, rock4, "4/4"},
		{"3/4 waltz at 100", 100, waltz3, "3/4"},
		{"3/4 waltz at 150", 150, waltz3, "3/4"},
		{"12/8 ballad at 60 dotted quarters", 60, ballad128, "12/8"},
		{"12/8 ballad at 110 dotted quarters", 110, ballad128, "12/8"},
		{"12/8 shuffle at 80 dotted quarters", 80, shuffle128, "12/8"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := drumTrack(40, 60/c.bpm, c.groove)
			a := AnalyzeSamples(x, nil, nil)
			t.Logf("tempo %.1f candidates %v sig %s (%.2f)", a.Tempo, a.TempoCandidates, a.TimeSig, a.TimeSigConfidence)
			if math.Abs(a.Tempo-c.bpm) > 1 {
				t.Errorf("tempo = %.1f, want %.0f ±1", a.Tempo, c.bpm)
			}
			if a.TimeSig != c.wantSig {
				t.Errorf("time signature = %s, want %s", a.TimeSig, c.wantSig)
			}
			if a.TimeSigConfidence <= 0 || a.TimeSigConfidence > 1 {
				t.Errorf("time signature confidence = %.2f, want in (0, 1]", a.TimeSigConfidence)
			}
			if len(a.TempoCandidates) < 2 || a.TempoCandidates[0] != a.Tempo {
				t.Errorf("candidates %v should start with the tempo and offer alternatives", a.TempoCandidates)
			}
			hasHalfOrDouble := false
			for _, c := range a.TempoCandidates[1:] {
				if math.Abs(c-a.Tempo/2) < 1 || math.Abs(c-a.Tempo*2) < 2 {
					hasHalfOrDouble = true
				}
			}
			if !hasHalfOrDouble {
				t.Errorf("candidates %v should include half or double tempo", a.TempoCandidates)
			}
			if got := a.SecondsPerBar(); math.Abs(got-60/c.bpm*float64(c.groove.beatsPerBar)) > 0.05 {
				t.Errorf("seconds per bar = %.3f", got)
			}
		})
	}
}

func TestKey(t *testing.T) {
	cases := []struct {
		name   string
		chords [][]int
		want   string
	}{
		// I–IV–V–I in D major: D, G, A, D.
		{"D major", [][]int{{62, 66, 69}, {67, 71, 74}, {69, 73, 76}, {62, 66, 69}}, "D"},
		// i–iv–V–i in A minor: Am, Dm, E (with the leading tone G#), Am.
		{"A minor", [][]int{{57, 60, 64}, {62, 65, 69}, {64, 68, 71}, {57, 60, 64}}, "Am"},
		// I–IV–V–I in Bb major, to check flat spelling.
		{"Bb major", [][]int{{58, 62, 65}, {63, 67, 70}, {65, 69, 72}, {58, 62, 65}}, "Bb"},
		// i–iv–V–i in F# minor, to check sharp spelling.
		{"F# minor", [][]int{{54, 57, 61}, {59, 62, 66}, {61, 65, 68}, {54, 57, 61}}, "F#m"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := progression(c.chords, 2, 24)
			key, conf := estimateKey(x)
			t.Logf("key %s confidence %.2f", key, conf)
			if key != c.want {
				t.Errorf("key = %q, want %q", key, c.want)
			}
			if conf <= 0 || conf > 1 {
				t.Errorf("confidence = %.2f, want in (0, 1]", conf)
			}
		})
	}
}

func TestKeySpelling(t *testing.T) {
	flat := map[string]bool{"F": true, "Bb": true, "Eb": true, "Ab": true, "Db": true,
		"Dm": true, "Gm": true, "Cm": true, "Fm": true, "Bbm": true, "Ebm": true}
	for _, names := range [][12]string{majorNames, minorNames} {
		for _, n := range names {
			if hasFlat := len(n) > 1 && n[1] == 'b'; hasFlat && !flat[n] {
				t.Errorf("%s is spelled with a flat but is not a flat key", n)
			}
		}
	}
	for n := range flat {
		found := false
		for _, names := range [][12]string{majorNames, minorNames} {
			for _, m := range names {
				found = found || m == n
			}
		}
		if !found {
			t.Errorf("flat key %s is not spelled as such", n)
		}
	}
}

func TestAudioStartAndVocalOnsets(t *testing.T) {
	const dur = 30.0
	mix := drumTrack(dur, 0.5, rock4)
	for i := range seconds(1.3) { // silence until 1.3 s (the groove starts at 0.5 s)
		mix[i] = 0
	}
	phrases := [][2]float64{{3.0, 6.0}, {6.3, 9.0}, {10.0, 14.0}, {16.5, 20.0}, {20.2, 22.0}, {25.0, 27.5}}
	vocals := vocalStem(dur, phrases)
	a := AnalyzeSamples(mix, vocals, nil)
	if math.Abs(a.Duration-dur) > 0.01 {
		t.Errorf("duration = %.3f, want %.0f", a.Duration, dur)
	}
	if math.Abs(a.AudioStart-1.5) > 0.06 { // the first hit after the silence is at 1.5 s
		t.Errorf("audio start = %.2f, want 1.5", a.AudioStart)
	}
	if a.Hop != Hop || len(a.VocalActivity) != int(math.Ceil(dur/Hop)) {
		t.Errorf("hop %.3f, %d activity frames", a.Hop, len(a.VocalActivity))
	}
	// Gaps of 0.3 s and 0.2 s are breaths inside a phrase, not onsets.
	want := []float64{3.0, 10.0, 16.5, 25.0}
	if len(a.VocalOnsets) != len(want) {
		t.Fatalf("onsets = %v, want %v", a.VocalOnsets, want)
	}
	for i, w := range want {
		if math.Abs(a.VocalOnsets[i]-w) > 0.06 {
			t.Errorf("onset %d = %.2f, want %.2f", i, a.VocalOnsets[i], w)
		}
	}
	if a.VocalActivity[int(12/Hop)] != true || a.VocalActivity[int(15/Hop)] != false {
		t.Error("activity should be on at 12 s and off at 15 s")
	}
}

func TestNoVocalsStem(t *testing.T) {
	a := AnalyzeSamples(drumTrack(20, 0.5, rock4), nil, nil)
	if a.VocalActivity != nil || a.VocalOnsets != nil {
		t.Error("without a vocals stem there should be no vocal activity")
	}
	if a := AnalyzeSamples(nil, nil, nil); a.Tempo != 0 || a.Duration != 0 {
		t.Errorf("empty input: %+v", a)
	}
}

// synthSong is a three-section song at 2.0 s per bar starting at 1.0 s:
// Intro 4 bars (instrumental, 1–9 s), Verse 8 bars (9–25 s), Chorus
// 8 bars (25–41 s), then a 4 s ring-out. Phrases are two bars long with
// a breath before the next one; the chorus phrases are longer.
var (
	synthSections = []Section{
		{Index: 0, StartBar: 0, BarCount: 4},
		{Index: 1, StartBar: 4, BarCount: 8, HasLyrics: true},
		{Index: 2, StartBar: 12, BarCount: 8, HasLyrics: true},
	}
	synthPhrases = [][2]float64{
		{9.0, 12.2}, {13.0, 16.3}, {17.0, 20.1}, {21.0, 24.0}, // verse
		{25.0, 28.6}, {29.0, 32.5}, {33.0, 36.6}, {37.0, 40.2}, // chorus
	}
	synthWant = []float64{1.0, 9.0, 25.0}
)

func TestFitSectionsSynthetic(t *testing.T) {
	const dur = 45.0
	mix := drumTrack(dur, 0.5, rock4)
	for i := range seconds(1.0) {
		mix[i] = 0
	}
	addClick(mix, 1.0, 1.0, rand.New(rand.NewPCG(5, 6))) // the downbeat at 1.0 s
	vocals := vocalStem(dur, synthPhrases)
	a := AnalyzeSamples(mix, vocals, nil)
	t.Logf("audio start %.2f, onsets %v", a.AudioStart, a.VocalOnsets)

	for _, hint := range []float64{2.0, 2.2, 0} {
		fit, err := FitSections(a, synthSections, hint)
		if err != nil {
			t.Fatalf("hint %.1f: %v", hint, err)
		}
		t.Logf("hint %.1f: spb %.3f score %.2f markers %v", hint, fit.SecondsPerBar, fit.Score, fit.Markers)
		checkMarkers(t, fit, synthSections, synthWant, 0.3)
		if math.Abs(fit.SecondsPerBar-2.0) > 0.05 {
			t.Errorf("hint %.1f: seconds per bar = %.3f, want 2.0", hint, fit.SecondsPerBar)
		}
		if fit.Score < 0.7 {
			t.Errorf("hint %.1f: score = %.2f, want a confident fit", hint, fit.Score)
		}
	}
}

// TestFitSectionsPickup: a five-bar intro from 1.0 s, and the singer
// comes in half a bar before each lyric section's downbeat (the verse's
// downbeat is at 12 s, its first phrase at 11 s). Markers follow the
// sung pickups, as a person placing them by ear does, and the intro
// counts back five bars from the verse — but not before the audio.
func TestFitSectionsPickup(t *testing.T) {
	const dur = 50.0
	shift := 3.0
	var phrases [][2]float64
	for _, p := range synthPhrases {
		phrases = append(phrases, [2]float64{p[0] + shift - 1.0, p[1] + shift - 0.5})
	}
	a := &Analysis{Duration: dur, AudioStart: 1.0, Hop: Hop, Tempo: 120, TimeSig: "4/4"}
	a.VocalActivity = activityFromPhrases(dur, phrases)
	a.VocalOnsets = vocalOnsets(a.VocalActivity, Hop)
	sections := []Section{
		{Index: 0, StartBar: 0, BarCount: 5},
		{Index: 1, StartBar: 5, BarCount: 8, HasLyrics: true},
		{Index: 2, StartBar: 13, BarCount: 8, HasLyrics: true},
	}
	fit, err := FitSections(a, sections, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("spb %.3f score %.2f markers %v", fit.SecondsPerBar, fit.Score, fit.Markers)
	// Lyric sections start on the sung pickups; the intro counts back
	// five bars from the verse but not before the audio starts.
	checkMarkers(t, fit, sections, []float64{1.0, 11.0, 27.0}, 0.3)
}

// TestFitSectionsReferenceRecording rebuilds the vocal activity of a
// real recording measured by hand (a 1950s 12/8 ballad, 180.65 s, audio
// from 0.85 s; phrase onsets taken from its Demucs vocals stem) and
// checks the chart lands where a person placed it. The chart's bar
// counts are approximate: its choruses are notated as 10 bars but sung
// in about 8, so a single global bar grid drifts by several seconds by
// the last verse — the per-section snapping has to absorb that.
func TestFitSectionsReferenceRecording(t *testing.T) {
	onsets := []float64{8.2, 22.1, 36.8, 50.9, 65.5, 83.4, 97.4, 111.8, 129.4, 143.4, 157.6}
	// Phrase lengths (5–12 s), each with a short breath (< 0.5 s) inside
	// so activity isn't one solid block.
	lengths := []float64{11.5, 12.0, 10.5, 12.0, 9.0, 11.0, 12.0, 10.0, 8.5, 11.5, 11.0}
	var phrases [][2]float64
	for i, o := range onsets {
		mid := o + lengths[i]*0.45
		phrases = append(phrases, [2]float64{o, mid}, [2]float64{mid + 0.3, o + lengths[i]})
	}
	const dur = 180.65
	a := &Analysis{Duration: dur, AudioStart: 0.85, Hop: Hop, Tempo: 109.6, TimeSig: "12/8"}
	a.VocalActivity = activityFromPhrases(dur, phrases)
	a.VocalOnsets = vocalOnsets(a.VocalActivity, Hop)
	if len(a.VocalOnsets) != len(onsets) {
		t.Fatalf("rebuilt onsets %v, want %v", a.VocalOnsets, onsets)
	}
	sections := []Section{
		{Index: 0, StartBar: 0, BarCount: 4},                    // Intro
		{Index: 1, StartBar: 4, BarCount: 26, HasLyrics: true},  // Verse
		{Index: 2, StartBar: 30, BarCount: 10, HasLyrics: true}, // Chorus
		{Index: 3, StartBar: 40, BarCount: 13, HasLyrics: true}, // Verse
		{Index: 4, StartBar: 53, BarCount: 10, HasLyrics: true}, // Chorus
		{Index: 5, StartBar: 63, BarCount: 14, HasLyrics: true}, // Verse
	}
	want := []float64{0.85, 8.2, 65.5, 83.4, 111.8, 129.4}
	for _, hint := range []float64{2.19, a.SecondsPerBar(), 0} {
		fit, err := FitSections(a, sections, hint)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("hint %.3f: spb %.3f score %.2f markers %v", hint, fit.SecondsPerBar, fit.Score, fit.Markers)
		checkMarkers(t, fit, sections, want, 1.5)
		if math.Abs(fit.SecondsPerBar-2.19) > 0.1 {
			t.Errorf("hint %.3f: seconds per bar = %.3f, want ≈2.19", hint, fit.SecondsPerBar)
		}
	}
}

func TestFitSectionsWithoutVocals(t *testing.T) {
	a := &Analysis{Duration: 60, AudioStart: 0.5, Hop: Hop, Tempo: 120, TimeSig: "4/4"}
	fit, err := FitSections(a, synthSections, 0)
	if err != nil {
		t.Fatal(err)
	}
	checkMarkers(t, fit, synthSections, []float64{0.5, 8.5, 24.5}, 1e-9)
	if fit.SecondsPerBar != 2 || fit.Score != 0 {
		t.Errorf("spb %.3f score %.2f, want 2 and 0", fit.SecondsPerBar, fit.Score)
	}
	fit, err = FitSections(a, synthSections, 3)
	if err != nil {
		t.Fatal(err)
	}
	checkMarkers(t, fit, synthSections, []float64{0.5, 12.5, 36.5}, 1e-9)

	if _, err := FitSections(&Analysis{Hop: Hop}, synthSections, 0); err == nil {
		t.Error("no vocals and no tempo should be an error")
	}
}

func TestFitSectionsErrors(t *testing.T) {
	a := &Analysis{Hop: Hop, Tempo: 120}
	if _, err := FitSections(nil, synthSections, 2); err == nil {
		t.Error("nil analysis should be an error")
	}
	if _, err := FitSections(a, nil, 2); err == nil {
		t.Error("no sections should be an error")
	}
	bad := []Section{{Index: 0, StartBar: 4, BarCount: 4}, {Index: 1, StartBar: 0, BarCount: 4}}
	if _, err := FitSections(a, bad, 2); err == nil {
		t.Error("out-of-order sections should be an error")
	}
}

func checkMarkers(t *testing.T, fit *Fit, sections []Section, want []float64, tol float64) {
	t.Helper()
	if len(fit.Markers) != len(sections) {
		t.Fatalf("%d markers for %d sections", len(fit.Markers), len(sections))
	}
	for i, m := range fit.Markers {
		if m.SectionIndex != sections[i].Index {
			t.Errorf("marker %d is for section %d, want %d", i, m.SectionIndex, sections[i].Index)
		}
		if i > 0 && m.Time <= fit.Markers[i-1].Time {
			t.Errorf("markers not strictly increasing at %d: %v", i, fit.Markers)
		}
		if math.Abs(m.Time-want[i]) > tol {
			t.Errorf("section %d starts at %.2f, want %.2f ±%.2f", m.SectionIndex, m.Time, want[i], tol)
		}
	}
}

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
}

// TestDecodeWAV has ffmpeg synthesize a 2 s, 440 Hz stereo WAV at
// 44.1 kHz and checks Decode returns 2 s of mono 11025 Hz samples at the
// right level and pitch.
func TestDecodeWAV(t *testing.T) {
	requireFFmpeg(t)
	path := filepath.Join(t.TempDir(), "tone.wav")
	out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2:sample_rate=44100",
		"-ac", "2", "-c:a", "pcm_s16le", path).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg: %v: %s", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	x, err := Decode(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(float64(len(x))-2*SampleRate) > 0.01*SampleRate {
		t.Errorf("decoded %d samples, want ≈%d", len(x), 2*SampleRate)
	}
	// ffmpeg's sine source has amplitude 1/8: RMS 0.125/√2.
	sum := 0.0
	crossings := 0
	for i, v := range x {
		sum += float64(v) * float64(v)
		if i > 0 && (x[i-1] < 0) != (v < 0) {
			crossings++
		}
	}
	if rms := math.Sqrt(sum / float64(len(x))); math.Abs(rms-0.125/math.Sqrt2) > 0.01 {
		t.Errorf("rms = %.4f, want ≈%.4f", rms, 0.125/math.Sqrt2)
	}
	if hz := float64(crossings) / 2 / (float64(len(x)) / SampleRate); math.Abs(hz-440) > 5 {
		t.Errorf("pitch ≈ %.0f Hz, want 440", hz)
	}

	if _, err := Decode(context.Background(), []byte("not audio at all")); err == nil {
		t.Error("garbage input should fail to decode")
	}
}

// TestAnalyzeWAV runs the whole pipeline from file bytes: a WAV (written
// here, 22.05 kHz 16-bit) holding a 4/4 click track at 120 BPM over a
// D major progression, plus a vocals stem.
func TestAnalyzeWAV(t *testing.T) {
	requireFFmpeg(t)
	const dur = 30.0
	clicks := drumTrack(dur, 0.5, rock4)
	chords := progression([][]int{{62, 66, 69}, {67, 71, 74}, {69, 73, 76}, {62, 66, 69}}, 2, dur)
	mix := make([]float32, len(clicks))
	for i := range mix {
		mix[i] = 0.6*clicks[i] + chords[i]
	}
	vocals := vocalStem(dur, [][2]float64{{4, 8}, {10, 14}})
	a, err := Analyze(context.Background(), wavBytes(mix), wavBytes(vocals), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("tempo %.1f %s key %s (%.2f) onsets %v", a.Tempo, a.TimeSig, a.Key, a.KeyConfidence, a.VocalOnsets)
	if math.Abs(a.Tempo-120) > 1.5 || a.TimeSig != "4/4" {
		t.Errorf("tempo %.1f %s, want 120 4/4", a.Tempo, a.TimeSig)
	}
	if a.Key != "D" {
		t.Errorf("key %s, want D", a.Key)
	}
	if len(a.VocalOnsets) != 2 || math.Abs(a.VocalOnsets[0]-4) > 0.1 || math.Abs(a.VocalOnsets[1]-10) > 0.1 {
		t.Errorf("onsets %v, want [4 10]", a.VocalOnsets)
	}
}

// wavBytes encodes x (at SampleRate) as a 22.05 kHz mono 16-bit WAV by
// repeating samples, so the decoder has a real resampling job to do.
func wavBytes(x []float32) []byte {
	const rate = 2 * SampleRate
	var data bytes.Buffer
	for _, v := range x {
		s := int16(max(-1, min(1, v)) * 32767)
		binary.Write(&data, binary.LittleEndian, s)
		binary.Write(&data, binary.LittleEndian, s)
	}
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+data.Len()))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(&b, binary.LittleEndian, uint16(1)) // mono
	binary.Write(&b, binary.LittleEndian, uint32(rate))
	binary.Write(&b, binary.LittleEndian, uint32(rate*2))
	binary.Write(&b, binary.LittleEndian, uint16(2))
	binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(data.Len()))
	b.Write(data.Bytes())
	return b.Bytes()
}
