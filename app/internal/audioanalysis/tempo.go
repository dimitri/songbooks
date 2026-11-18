package audioanalysis

import (
	"math"
	"sort"
)

// Onset envelope parameters. Frames of 512 samples (46 ms) resolve the
// low bands well enough; the 110-sample hop (~10 ms, 100.2 frames/s)
// gives the autocorrelation 1% lag resolution at 100 BPM.
const (
	envFrame = 512
	envHop   = 110
)

// bandEdges splits the spectrum into roughly octave-wide bands. Rhythm
// is read per band rather than from the broadband level: a kick drum, a
// snare and a hi-hat each live in their own bands, so the kick's
// once-a-bar or twice-a-bar pattern stays visible instead of being
// averaged into the hi-hat's steady eighth notes, and a sustained bass or
// pad that dominates the total energy can't mask a light percussive part.
var bandEdges = []float64{30, 120, 250, 500, 1000, 2000, 4000, 5500}

// onsetEnvelopes returns one onset-strength envelope per band, and their
// frame rate. For each ~10 ms frame a band's onset strength is the
// positive change of its log energy since the previous frame (the
// frame-to-frame rise of the band's RMS level in dB: how hard something
// just started), minus its ±0.25 s moving average and half-wave
// rectified, so slow swells don't masquerade as rhythm.
//
// The log is taken of the band energy plus the band's mean energy, i.e.
// log(1 + E/Ē) up to a constant: logarithmic (loudness-like) for events
// well above the band's average, close to linear below it. A bare log
// would turn noise in a quiet band into large relative jumps and flatten
// accents — a kick twice as loud as the rest rises from silence by
// barely more log units than a ghost note does — and accents are what
// reveal the metre.
func onsetEnvelopes(x []float32) ([][]float64, float64) {
	rate := float64(SampleRate) / envHop
	if len(x) < envFrame {
		return nil, rate
	}
	n := 1 + (len(x)-envFrame)/envHop
	nb := len(bandEdges) - 1
	binHz := float64(SampleRate) / envFrame
	lo := make([]int, nb)
	hi := make([]int, nb)
	for b := range nb {
		lo[b] = max(1, int(math.Ceil(bandEdges[b]/binHz)))
		hi[b] = min(envFrame/2, int(math.Floor(bandEdges[b+1]/binHz)))
	}
	energy := make([][]float64, nb)
	for b := range energy {
		energy[b] = make([]float64, n)
	}
	s := newSTFT(envFrame)
	for t := range n {
		mag := s.magnitudes(x, t*envHop)
		for b := range nb {
			e := 0.0
			for k := lo[b]; k <= hi[b]; k++ {
				e += mag[k] * mag[k]
			}
			energy[b][t] = e
		}
	}
	w := int(0.25 * rate)
	flux := make([]float64, n)
	prefix := make([]float64, n+1)
	envs := make([][]float64, nb)
	for b := range nb {
		mean := 0.0
		for _, e := range energy[b] {
			mean += e
		}
		mean /= float64(n)
		floor := mean + 1e-12
		prev := math.Log(energy[b][0] + floor)
		flux[0] = 0
		for t := 1; t < n; t++ {
			cur := math.Log(energy[b][t] + floor)
			flux[t] = max(0, cur-prev)
			prev = cur
		}
		for i, v := range flux {
			prefix[i+1] = prefix[i] + v
		}
		env := make([]float64, n)
		for t := range n {
			lo, hi := max(0, t-w), min(n, t+w+1)
			env[t] = max(0, flux[t]-(prefix[hi]-prefix[lo])/float64(hi-lo))
		}
		envs[b] = env
	}
	return envs, rate
}

// autocorr returns the normalized, unbiased autocorrelation of the band
// envelopes for lags 0..maxLag (r[0] == 1): the sum over bands of each
// mean-removed envelope's autocorrelation, so periodicities are matched
// within a band (kick to kick, snare to snare) and louder, busier bands
// weigh more. Each is computed through the FFT (|FFT|², inverse,
// zero-padded against circular wrap). "Unbiased" divides lag l's sum by
// the n−l products it contains, so long lags (bar-level periodicities)
// are not penalized merely for overlapping less of the song.
func autocorr(envs [][]float64, maxLag int) []float64 {
	if len(envs) == 0 {
		return nil
	}
	n := len(envs[0])
	maxLag = min(maxLag, n-1)
	if n < 2 || maxLag < 1 {
		return nil
	}
	sum := make([]float64, maxLag+1)
	buf := make([]complex128, nextPow2(2*n))
	for _, env := range envs {
		mean := 0.0
		for _, v := range env {
			mean += v
		}
		mean /= float64(n)
		clear(buf)
		for i, v := range env {
			buf[i] = complex(v-mean, 0)
		}
		fft(buf)
		for i, v := range buf {
			buf[i] = complex(real(v)*real(v)+imag(v)*imag(v), 0)
		}
		ifft(buf)
		for l := range sum {
			sum[l] += real(buf[l])
		}
	}
	if sum[0] <= 0 {
		return nil
	}
	r := make([]float64, maxLag+1)
	for l := range r {
		r[l] = sum[l] / float64(n-l) * float64(n) / sum[0]
	}
	return r
}

// rAt linearly interpolates r at a fractional lag (0 outside the range).
func rAt(r []float64, lag float64) float64 {
	if lag < 0 || lag > float64(len(r)-1) {
		return 0
	}
	i := int(lag)
	if i == len(r)-1 {
		return r[i]
	}
	f := lag - float64(i)
	return r[i]*(1-f) + r[i+1]*f
}

// rPeak is the largest r within ±tol (relative) of lag: real recordings
// drift and swing, so a periodicity several beats long rarely lands on
// exactly k times the beat lag.
func rPeak(r []float64, lag, tol float64) float64 {
	a := int(math.Floor(lag * (1 - tol)))
	b := int(math.Ceil(lag * (1 + tol)))
	best := math.Inf(-1)
	for l := max(a, 1); l <= b && l < len(r); l++ {
		best = max(best, r[l])
	}
	if math.IsInf(best, -1) {
		return 0
	}
	return best
}

const (
	minBPM = 40
	maxBPM = 200
	// The tempo prior: a log-normal bump centred on priorBPM with
	// priorOctaves standard deviation. Autocorrelation is periodic —
	// a beat periodicity at lag L also shows at 2L, 3L — so the data
	// alone cannot say which octave is "the beat"; listeners settle on
	// a tactus around 60–140 BPM, and this weight encodes that. It is
	// gentle enough (0.85 at 75 BPM, 0.77 at 160, 0.55 at 55 and 200)
	// that
	// clearly stronger evidence an octave away still wins.
	priorBPM     = 105
	priorOctaves = 0.85
)

func tempoPrior(bpm float64) float64 {
	z := math.Log2(bpm/priorBPM) / priorOctaves
	return math.Exp(-0.5 * z * z)
}

// beatStrength scores lag L (in envelope frames) as a beat period: the
// periodicity at L itself plus half the periodicity at 2L, so a lag has
// to repeat to count (a one-off echo or a pickup pattern does not).
func beatStrength(r []float64, L float64) float64 {
	return rAt(r, L) + 0.5*rAt(r, 2*L)
}

type tempoResult struct {
	bpm               float64
	candidates        []float64
	timeSig           string
	timeSigConfidence float64
}

// estimateTempo finds the beat tempo and metre from the band onset
// envelopes.
//
// Tempo: sweep 40–200 BPM in 0.25 BPM steps, scoring each candidate beat
// lag with beatStrength × tempoPrior; the best one is then refined from
// the exact positions (parabolic interpolation) of the autocorrelation
// peaks at 1–4 times its lag, which pins it to a small fraction of a
// BPM. Other local maxima of the sweep, plus half and double the winner,
// become TempoCandidates.
//
// Metre, given the beat lag L: first the subdivision — a stronger
// periodicity at L/3 (or 2L/3) than at L/2 means the beat divides in
// three, i.e. a compound metre whose beat is the dotted quarter. Then
// the grouping — accents recur once per bar, so the periodicity at the
// bar length (2, 3 or 4 beats) stands out: 3 → 3/4; otherwise 4/4
// (2/4 is folded into 4/4, as charts are). Compound metres likewise
// default to 12/8; 6/8 needs the 2-beat periodicity clearly stronger
// than the 4-beat one. A groove that repeats every two beats (kick on 1
// and 3, backbeat on 2 and 4) looks the same in either, so this is the
// least reliable call. Each decision's confidence is its relative
// margin; the reported confidence is their mean. Swung (shuffle) 4/4 has a triplet
// subdivision and will read as 12/8 — notationally the same thing.
func estimateTempo(envs [][]float64, rate float64) tempoResult {
	if len(envs) == 0 || float64(len(envs[0])) < 4*rate {
		return tempoResult{}
	}
	r := autocorr(envs, int(12*rate))
	if r == nil {
		return tempoResult{}
	}

	type cand struct{ bpm, score float64 }
	var sweep []cand
	for bpm := float64(minBPM); bpm <= maxBPM; bpm += 0.25 {
		L := 60 * rate / bpm
		sweep = append(sweep, cand{bpm, beatStrength(r, L) * tempoPrior(bpm)})
	}
	var peaks []cand
	for i := 1; i < len(sweep)-1; i++ {
		if sweep[i].score > sweep[i-1].score && sweep[i].score >= sweep[i+1].score && sweep[i].score > 0 {
			peaks = append(peaks, sweep[i])
		}
	}
	if len(peaks) == 0 {
		return tempoResult{}
	}
	sort.SliceStable(peaks, func(i, j int) bool { return peaks[i].score > peaks[j].score })

	L := refineLag(r, 60*rate/peaks[0].bpm)
	bpm := 60 * rate / L
	res := tempoResult{bpm: round1(bpm)}

	// Candidates: the winner, the next strongest distinct sweep peaks,
	// then half and double tempo if not already present.
	add := func(c float64) {
		if c < minBPM/2 || c > maxBPM*2 {
			return
		}
		for _, have := range res.candidates {
			if math.Abs(have-c)/have < 0.04 {
				return
			}
		}
		res.candidates = append(res.candidates, round1(c))
	}
	add(bpm)
	for _, p := range peaks[1:] {
		if len(res.candidates) >= 4 || p.score < 0.25*peaks[0].score {
			break
		}
		add(p.bpm)
	}
	add(bpm / 2)
	add(bpm * 2)

	// Metre.
	const tol = 0.04
	sub2 := rPeak(r, L/2, tol)
	sub3 := max(rPeak(r, L/3, tol), rPeak(r, 2*L/3, tol))
	compound := sub3 > sub2
	subConf := margin(max(sub2, sub3), min(sub2, sub3))
	g2, g3, g4 := rPeak(r, 2*L, tol), rPeak(r, 3*L, tol), rPeak(r, 4*L, tol)
	var groupConf float64
	switch {
	case compound && margin(g2, g4) > 0.1:
		res.timeSig, groupConf = "6/8", margin(g2, g4)
	case compound:
		res.timeSig, groupConf = "12/8", margin(g4, g2)
	case g3 > max(g2, g4):
		res.timeSig, groupConf = "3/4", margin(g3, max(g2, g4))
	default:
		res.timeSig, groupConf = "4/4", margin(max(g2, g4), g3)
	}
	res.timeSigConfidence = clamp01((subConf + groupConf) / 2)
	return res
}

// refineLag moves the beat lag L to the weighted average of the exact
// autocorrelation peak positions near k·L (k = 1..4) divided by k, each
// located by parabolic interpolation around the local maximum. Higher
// multiples weigh more (weight k) since a one-frame error there is k
// times smaller per beat. Peaks that are not real local maxima inside
// the search window, or that are negative, are ignored.
func refineLag(r []float64, L float64) float64 {
	sumPos, sumK := 0.0, 0.0
	for k := 1; k <= 4; k++ {
		c := float64(k) * L
		a := max(int(math.Floor(c*0.96)), 1)
		b := int(math.Ceil(c * 1.04))
		if b >= len(r)-1 {
			break
		}
		best := a
		for l := a; l <= b; l++ {
			if r[l] > r[best] {
				best = l
			}
		}
		if best == a || best == b || r[best] <= 0 {
			continue
		}
		y0, y1, y2 := r[best-1], r[best], r[best+1]
		pos := float64(best)
		if d := y0 - 2*y1 + y2; d < 0 {
			pos += 0.5 * (y0 - y2) / d
		}
		sumPos += pos
		sumK += float64(k)
	}
	if sumK == 0 {
		return L
	}
	refined := sumPos / sumK
	if math.Abs(refined-L)/L > 0.04 {
		return L
	}
	return refined
}

// margin is the relative advantage of a over b in [0, 1].
func margin(a, b float64) float64 {
	return clamp01((a - b) / (math.Abs(a) + math.Abs(b) + 1e-9))
}

func clamp01(x float64) float64 {
	return min(1, max(0, x))
}

func round1(x float64) float64 {
	return math.Round(x*10) / 10
}
