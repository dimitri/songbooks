package audioanalysis

import (
	"math"
	"sort"
)

// Krumhansl–Kessler key profiles (Krumhansl 1990, "Cognitive Foundations
// of Musical Pitch"): listeners' ratings of how well each of the twelve
// pitch classes fits a major or minor key context, indexed from the
// tonic. Correlating a song's pitch-class distribution against all 24
// rotations is the classic Krumhansl–Schmuckler key-finding algorithm.
var (
	majorProfile = [12]float64{6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88}
	minorProfile = [12]float64{6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17}
)

// Key names by tonic pitch class (0 = C). Keys on the flat side of the
// circle of fifths are spelled with flats (F, Bb, Eb, Ab, Db and their
// relative minors Dm, Gm, Cm, Fm, Bbm), sharp-side keys with sharps.
// The enharmonic pairs at the bottom of the circle take the more common
// spelling: Db over C# (5 flats vs 7 sharps), F# over Gb, Ebm over D#m,
// G#m over Abm (5 sharps vs 7 flats).
var (
	majorNames = [12]string{"C", "Db", "D", "Eb", "E", "F", "F#", "G", "Ab", "A", "Bb", "B"}
	minorNames = [12]string{"Cm", "C#m", "Dm", "Ebm", "Em", "Fm", "F#m", "Gm", "G#m", "Am", "Bbm", "Bm"}
)

const (
	chromaFrame = 4096 // 0.37 s at 11025 Hz: 2.7 Hz bins, under a semitone from ~50 Hz up
	chromaHop   = chromaFrame / 2
	chromaLoHz  = 60
	chromaHiHz  = 2000
)

// chroma sums the song's pitch-class content. For each Hann-windowed
// 4096-sample frame (50% overlap), every spectral peak between 60 Hz and
// 2 kHz (a local maximum no more than 40 dB below the frame's loudest
// bin) is located by parabolic interpolation, converted to the nearest
// equal-tempered pitch (A4 = 440 Hz) and its magnitude added to that
// pitch class. Using peaks rather than every bin keeps a partial's
// window leakage out of the neighbouring semitones. Each frame's chroma
// is normalized to sum to one and weighted by the frame's RMS level, so
// loud, harmonically full passages count more than fades and silence
// without any single frame dominating.
//
// The 60 Hz–2 kHz range covers bass fundamentals through the melody and
// chord tones' low harmonics, and stops before the dense upper harmonics
// and cymbal noise that blur the pitch classes.
func chroma(x []float32) [12]float64 {
	var total [12]float64
	frames, weights := chromaFrames(x)
	for i, f := range frames {
		for pc := range f {
			total[pc] += weights[i] * f[pc]
		}
	}
	return total
}

// chromaFrames is chroma frame by frame: each frame's pitch-class
// content normalized to sum to one, and its RMS level as its weight.
//
// Pitches are rounded to the nearest semitone after the recording's own
// tuning offset (tuning) is taken out: a record pressed or played
// between two pitches would otherwise split its every chord between two
// neighbouring pitch classes.
func chromaFrames(x []float32) ([][12]float64, []float64) {
	return chromaFramesTuned(x, tuning(x))
}

// tuning is the recording's offset from A = 440 Hz, in semitones
// (-0.5..0.5): the circular mean of every spectral peak's distance from
// its nearest equal-tempered pitch, weighted by the peak's magnitude.
func tuning(x []float32) float64 {
	var sx, sy float64
	eachPeak(x, func(_ int, midi, m, _ float64) {
		a := 2 * math.Pi * (midi - math.Round(midi))
		sx += m * math.Cos(a)
		sy += m * math.Sin(a)
	})
	if sx == 0 && sy == 0 {
		return 0
	}
	return math.Atan2(sy, sx) / (2 * math.Pi)
}

func chromaFramesTuned(x []float32, offset float64) ([][12]float64, []float64) {
	var frames [][12]float64
	var weights []float64
	frame, rms, sum, last := [12]float64{}, 0.0, 0.0, -1
	flush := func() {
		if last >= 0 && sum > 0 && rms > 0 {
			for pc := range frame {
				frame[pc] /= sum
			}
			frames = append(frames, frame)
			weights = append(weights, rms)
		}
		frame, sum = [12]float64{}, 0
	}
	eachPeak(x, func(start int, midi, m, frameRMS float64) {
		if start != last {
			flush()
			last, rms = start, frameRMS
		}
		pc := ((int(math.Round(midi-offset)) % 12) + 12) % 12
		frame[pc] += m
		sum += m
	})
	flush()
	return frames, weights
}

// eachPeak calls fn for every spectral peak of every frame (see chroma):
// the frame's start, the peak's pitch as a fractional MIDI number, its
// magnitude, and the frame's RMS level.
func eachPeak(x []float32, fn func(start int, midi, m, rms float64)) {
	s := newSTFT(chromaFrame)
	binHz := float64(SampleRate) / chromaFrame
	kLo := max(2, int(math.Floor(chromaLoHz/binHz)))
	kHi := min(chromaFrame/2-1, int(math.Ceil(chromaHiHz/binHz)))
	for start := 0; start < len(x); start += chromaHop {
		mag := s.magnitudes(x, start)
		energy := 0.0
		for i := start; i < min(start+chromaFrame, len(x)); i++ {
			energy += float64(x[i]) * float64(x[i])
		}
		rms := math.Sqrt(energy / chromaFrame)
		peak := 0.0
		for k := kLo; k <= kHi; k++ {
			peak = max(peak, mag[k])
		}
		if peak == 0 || rms == 0 {
			continue
		}
		for k := kLo; k <= kHi; k++ {
			m := mag[k]
			if m < 0.01*peak || m <= mag[k-1] || m < mag[k+1] {
				continue
			}
			y0, y2 := mag[k-1], mag[k+1]
			pos := float64(k)
			if d := y0 - 2*m + y2; d < 0 {
				pos += 0.5 * (y0 - y2) / d
			}
			f := pos * binHz
			if f < chromaLoHz || f > chromaHiHz {
				continue
			}
			fn(start, 69+12*math.Log2(f/440), m, rms)
		}
	}
}

// estimateKey correlates the song's chroma with the Krumhansl–Kessler
// profiles in all 24 keys (Pearson correlation) and returns the best.
// The confidence is the best key's lead over the runner-up, relative to
// the room left above the runner-up: (r1−r2)/(1−r2). It is often modest
// because the relative major/minor shares all seven scale tones; the
// wizard asks the user to confirm.
//
// It's the fallback: estimateKey first recognises the song's chords and
// names the key they belong to (chordKey), which a whole-song profile
// gets wrong when a chord on the third degree is prominent (a song in D
// leaning on F#m reads as F#m).
//
// The two are combined: when they agree, that key with the better of
// their confidences; when the chords clearly point to one key, that key;
// when two keys come close on chords (a song's I and V share most of
// them), the profile picks between those, and the low confidence says
// it's a close call.
func estimateKey(x []float32) (string, float64) {
	frames, weights := chromaFrames(x)
	scores := profileScores(x)
	pk, pconf := bestProfile(scores)
	ranked := chordKeys(frames, weights)
	if len(ranked) == 0 || ranked[0].score <= 0 {
		return pk, pconf
	}
	best := ranked[0]
	conf := 1.0
	if len(ranked) > 1 {
		conf = clamp01((best.score - max(ranked[1].score, 0)) / best.score)
	}
	if best.name == pk {
		return pk, max(conf, pconf)
	}
	if conf >= 0.08 {
		return best.name, conf
	}
	choice := best.name
	for _, r := range ranked {
		if r.score < 0.92*best.score {
			break
		}
		if scores[r.name] > scores[choice] {
			choice = r.name
		}
	}
	return choice, conf
}

// profileScores is the Krumhansl–Schmuckler estimate described above:
// each key's correlation with the song's chroma, by key name.
func profileScores(x []float32) map[string]float64 {
	c := chroma(x)
	sum := 0.0
	for _, v := range c {
		sum += v
	}
	if sum == 0 {
		return nil
	}
	out := map[string]float64{}
	for tonic := range 12 {
		for _, mode := range []struct {
			profile *[12]float64
			names   *[12]string
		}{{&majorProfile, &majorNames}, {&minorProfile, &minorNames}} {
			var rotated [12]float64
			for pc := range 12 {
				rotated[pc] = mode.profile[(pc-tonic+12)%12]
			}
			out[mode.names[tonic]] = pearson(c[:], rotated[:])
		}
	}
	return out
}

// bestProfile is the best correlated key, with its lead over the
// runner-up relative to the room left above it: (r1−r2)/(1−r2).
func bestProfile(scores map[string]float64) (string, float64) {
	best, second := math.Inf(-1), math.Inf(-1)
	name := ""
	for _, n := range append(majorNames[:], minorNames[:]...) {
		r, ok := scores[n]
		if !ok {
			continue
		}
		switch {
		case r > best:
			second = best
			best, name = r, n
		case r > second:
			second = r
		}
	}
	if name == "" || second >= 1 {
		return name, 0
	}
	return name, clamp01((best - second) / (1 - second))
}

func pearson(a, b []float64) float64 {
	n := float64(len(a))
	ma, mb := 0.0, 0.0
	for i := range a {
		ma += a[i]
		mb += b[i]
	}
	ma /= n
	mb /= n
	var sab, saa, sbb float64
	for i := range a {
		da, db := a[i]-ma, b[i]-mb
		sab += da * db
		saa += da * da
		sbb += db * db
	}
	if saa == 0 || sbb == 0 {
		return 0
	}
	return sab / math.Sqrt(saa*sbb)
}

// chordSegment is about a second of frames: long enough for a chord's
// tones to show, short enough not to straddle many chord changes.
const chordSegment = 5

// chordKeys recognises a chord per segment (the major or minor triad
// whose three tones carry the most of its chroma, kept only when they
// clearly dominate) and scores each key the way a chord chart is read:
// a recognised chord that is one of the key's diatonic triads counts
// its weight, one on a diatonic root with the other quality (a
// secondary dominant, a borrowed chord) a third of it, one on a root
// outside the key counts a third against it, the tonic triad half again
// as much, and the first and last chords a little more, since songs
// start and end at home. (A song in B leaning on its V, F#, reads as
// F# unless its E, IV in B and outside F#, counts against F#.) The confidence is the best key's lead
// over the runner-up, relative to its score.
type rankedKey struct {
	name  string
	score float64
}

func chordKeys(frames [][12]float64, weights []float64) []rankedKey {
	type chord struct {
		root  int
		minor bool
		w     float64
	}
	var chords []chord
	for start := 0; start+chordSegment <= len(frames); start += chordSegment {
		var c [12]float64
		w := 0.0
		for i := start; i < start+chordSegment; i++ {
			for pc := range c {
				c[pc] += weights[i] * frames[i][pc]
			}
			w += weights[i]
		}
		total := 0.0
		for _, v := range c {
			total += v
		}
		if total == 0 {
			continue
		}
		best, bestRoot, bestMinor := 0.0, 0, false
		for root := range 12 {
			for _, minor := range []bool{false, true} {
				third := 4
				if minor {
					third = 3
				}
				share := (c[root] + c[(root+third)%12] + c[(root+7)%12]) / total
				if share > best {
					best, bestRoot, bestMinor = share, root, minor
				}
			}
		}
		// Three of twelve pitch classes: a quarter by chance. A chord
		// whose tones hold well over that is heard as that chord.
		if best >= 0.45 {
			chords = append(chords, chord{bestRoot, bestMinor, w})
		}
	}
	if len(chords) < 4 {
		return nil
	}
	// Degrees above the tonic with their triad's quality (true = minor):
	// major key I ii iii IV V vi; minor key i iv v and III VI VII, with
	// the harmonic minor's major V.
	majorDeg := map[int]bool{0: false, 2: true, 4: true, 5: false, 7: false, 9: true}
	minorDeg := map[int]bool{0: true, 3: false, 5: true, 7: false, 8: false, 10: false}
	edge := 0.0
	for _, c := range chords {
		edge += c.w
	}
	edge /= float64(len(chords))
	var ranked []rankedKey
	for tonic := range 12 {
		for _, minor := range []bool{false, true} {
			deg := majorDeg
			if minor {
				deg = minorDeg
			}
			score := 0.0
			for i, c := range chords {
				d := (c.root - tonic + 12) % 12
				q, ok := deg[d]
				switch {
				case ok && q == c.minor:
					score += c.w
				case ok:
					score += c.w / 3
				default:
					score -= c.w / 3
				}
				if d == 0 && c.minor == minor {
					score += c.w / 2
					if i == 0 || i == len(chords)-1 {
						score += edge
					}
				}
			}
			n := majorNames[tonic]
			if minor {
				n = minorNames[tonic]
			}
			ranked = append(ranked, rankedKey{n, score})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	return ranked
}
