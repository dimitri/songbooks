// Package audioanalysis estimates, from a song's reference recording
// (and, when the Demucs worker has produced them, its "vocals" and
// "no_vocals" stems), the facts a chart needs to be played against that
// recording: where the audio starts, the tempo, the metre, the key, when
// the singer is singing — and from those, where each charted section
// starts (FitSections).
//
// Everything is plain signal processing on mono samples at SampleRate,
// standard library only (including the FFT). Every estimate is a
// suggestion the import wizard shows for confirmation, so the algorithms
// favour being simple, explainable and robust over being clever; each
// one documents where it is known to be ambiguous.
package audioanalysis

import (
	"context"
	"fmt"
	"math"
)

// Hop is the frame length, in seconds, of the level analyses (audio
// start, vocal activity). 50 ms is short enough to place a phrase start
// well within a beat and long enough that a frame's RMS is stable.
const Hop = 0.05

const (
	// startThresholdDB: the audio "starts" at the first frame within
	// this many dB of the loudest frame. 40 dB is far below any musical
	// content yet well above encoder noise, dither and tape hiss.
	startThresholdDB = 40
	// vocalThresholdDB: a vocals-stem frame is "singing" when within
	// this many dB of the stem's loudest frame. Source separation leaves
	// some bleed of the band in the vocal stem; 30 dB down sits above
	// typical Demucs bleed and below a soft sung line.
	vocalThresholdDB = 30
	// vocalFloorDBFS: absolute floor for vocal activity, so a vocals
	// stem that is essentially empty (an instrumental) doesn't have its
	// own noise floor promoted to "singing" by the relative threshold.
	vocalFloorDBFS = -60
	// minVocalRun drops activity runs shorter than this many frames
	// (0.15 s): clicks and consonant bleed, not sung phrases.
	minVocalRun = 3
	// phraseGap: a vocal onset is activity after at least this much
	// silence, in seconds.
	phraseGap = 0.5
)

// Analysis is everything AnalyzeSamples learns about a recording.
type Analysis struct {
	Duration          float64   // seconds
	AudioStart        float64   // first non-silent moment of the mix (seconds)
	Tempo             float64   // BPM in TimeSig's beat unit (quarter for x/4, dotted quarter for 6/8, 9/8, 12/8)
	TempoCandidates   []float64 // alternatives, best first (half/double etc.)
	TimeSig           string    // "4/4", "3/4", "6/8" or "12/8"
	TimeSigConfidence float64   // 0..1
	Key               string    // "C", "F#", "Bbm" style
	KeyConfidence     float64   // 0..1
	Hop               float64   // seconds per VocalActivity frame (0.05)
	VocalActivity     []bool    // per frame, from the vocals stem; nil without one
	VocalOnsets       []float64 // phrase starts (seconds): vocal activity after ≥0.5 s of silence
}

// BeatsPerBar returns how many of TimeSig's beat units make a bar:
// compound meters count dotted-quarter beats (6/8 → 2, 9/8 → 3,
// 12/8 → 4), simple meters count the numerator. Unknown → 4.
func BeatsPerBar(timeSig string) int {
	switch timeSig {
	case "6/8":
		return 2
	case "9/8":
		return 3
	case "12/8":
		return 4
	case "2/4", "2/2":
		return 2
	case "3/4":
		return 3
	case "4/4":
		return 4
	}
	var num, den int
	if _, err := fmt.Sscanf(timeSig, "%d/%d", &num, &den); err == nil && num > 0 {
		if den == 8 && num%3 == 0 && num > 3 {
			return num / 3
		}
		return num
	}
	return 4
}

// SecondsPerBar is the bar length implied by Tempo and TimeSig, or 0
// when no tempo was found.
func (a *Analysis) SecondsPerBar() float64 {
	if a == nil || a.Tempo <= 0 {
		return 0
	}
	return 60 / a.Tempo * float64(BeatsPerBar(a.TimeSig))
}

// Analyze decodes the recording (and the optional vocals/backing stems,
// nil to skip) with ffmpeg and analyzes them; see AnalyzeSamples.
func Analyze(ctx context.Context, mix, vocals, backing []byte) (*Analysis, error) {
	m, err := Decode(ctx, mix)
	if err != nil {
		return nil, fmt.Errorf("mix: %w", err)
	}
	var v, b []float32
	if len(vocals) > 0 {
		if v, err = Decode(ctx, vocals); err != nil {
			return nil, fmt.Errorf("vocals stem: %w", err)
		}
	}
	if len(backing) > 0 {
		if b, err = Decode(ctx, backing); err != nil {
			return nil, fmt.Errorf("backing stem: %w", err)
		}
	}
	return AnalyzeSamples(m, v, b), nil
}

// AnalyzeSamples works on decoded mono samples at SampleRate. vocals and
// backing may be nil. Tempo, metre and key prefer the backing stem when
// present (the voice adds rubato onsets and melody notes outside the
// harmony), else the mix. Vocal activity needs the vocals stem.
func AnalyzeSamples(mix, vocals, backing []float32) *Analysis {
	a := &Analysis{
		Duration: float64(len(mix)) / SampleRate,
		Hop:      Hop,
		TimeSig:  "4/4",
	}
	if len(mix) == 0 {
		return a
	}
	a.AudioStart = audioStart(frameLevelsDB(mix))

	music := mix
	if len(backing) > 0 {
		music = backing
	}
	envs, rate := onsetEnvelopes(music)
	if t := estimateTempo(envs, rate); t.bpm > 0 {
		a.Tempo = t.bpm
		a.TempoCandidates = t.candidates
		a.TimeSig = t.timeSig
		a.TimeSigConfidence = t.timeSigConfidence
	}
	a.Key, a.KeyConfidence = estimateKey(music)

	if len(vocals) > 0 {
		a.VocalActivity = vocalActivity(frameLevelsDB(vocals))
		a.VocalOnsets = vocalOnsets(a.VocalActivity, Hop)
	}
	return a
}

// frameLevelsDB returns the RMS level, in dBFS, of consecutive Hop-long
// frames. Frame boundaries are computed in floating point (551.25
// samples per frame at 11025 Hz) so frame i starts at exactly i·Hop
// seconds and frame indices convert to times without drift.
func frameLevelsDB(x []float32) []float64 {
	per := Hop * SampleRate
	n := int(math.Ceil(float64(len(x)) / per))
	db := make([]float64, n)
	for i := range n {
		lo := int(float64(i) * per)
		hi := min(int(float64(i+1)*per), len(x))
		sum := 0.0
		for _, v := range x[lo:hi] {
			sum += float64(v) * float64(v)
		}
		ms := 0.0
		if hi > lo {
			ms = sum / float64(hi-lo)
		}
		db[i] = 10 * math.Log10(ms+1e-12) // −120 dBFS for digital silence
	}
	return db
}

func peakDB(db []float64) float64 {
	p := math.Inf(-1)
	for _, v := range db {
		p = max(p, v)
	}
	return p
}

// audioStart is the time of the first frame within startThresholdDB of
// the loudest one: leading digital silence, a vinyl rip's run-in groove
// or a fade from nothing all read as "not started yet".
func audioStart(db []float64) float64 {
	peak := peakDB(db)
	if peak < -100 { // nothing but digital silence
		return 0
	}
	for i, v := range db {
		if v > peak-startThresholdDB {
			return float64(i) * Hop
		}
	}
	return 0
}

// vocalActivity marks frames of the vocals stem as sung when they are
// within vocalThresholdDB of the stem's peak (and above an absolute
// floor), then removes runs shorter than minVocalRun frames.
func vocalActivity(db []float64) []bool {
	peak := peakDB(db)
	thr := max(peak-vocalThresholdDB, vocalFloorDBFS)
	act := make([]bool, len(db))
	for i, v := range db {
		act[i] = v > thr
	}
	for i := 0; i < len(act); {
		if !act[i] {
			i++
			continue
		}
		j := i
		for j < len(act) && act[j] {
			j++
		}
		if j-i < minVocalRun {
			for k := i; k < j; k++ {
				act[k] = false
			}
		}
		i = j
	}
	return act
}

// vocalOnsets returns the start time of every active frame preceded by
// at least phraseGap seconds of inactivity. The start of the file counts
// as silence, so a song that opens on the voice has an onset at its
// first sung frame.
func vocalOnsets(act []bool, hop float64) []float64 {
	gap := int(math.Round(phraseGap / hop))
	var on []float64
	last := -1 // index of the last active frame seen
	for i, v := range act {
		if !v {
			continue
		}
		if last < 0 || i-last-1 >= gap {
			on = append(on, float64(i)*hop)
		}
		last = i
	}
	return on
}
