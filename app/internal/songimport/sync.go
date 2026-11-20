package songimport

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"app/internal/audioanalysis"
	"app/internal/song"
	"app/internal/store"
)

// SyncSection is one chart section as the player numbers its bars
// (httpapi.SyncSections builds these from the same code the chart
// renders with).
type SyncSection struct {
	Index     int    `json:"index"`
	Label     string `json:"label"`
	StartBar  int    `json:"startBar"`
	BarCount  int    `json:"barCount"`
	HasLyrics bool   `json:"hasLyrics"`
}

// SectionTime is where a section starts in the recording.
type SectionTime struct {
	Index int     `json:"index"`
	Label string  `json:"label"`
	Time  float64 `json:"time"`
	// When the recording plays another number of bars here than the
	// chart has (counted at the recording's own tempo, up to the next
	// section's start): the chart's count and the recording's, rounded.
	// 0 when they agree, or when it can't be told.
	ChartBars     int `json:"chartBars,omitempty"`
	RecordingBars int `json:"recordingBars,omitempty"`
	// HasLyrics: sung (the Sync sheet checks it against the voice).
	HasLyrics bool `json:"hasLyrics,omitempty"`
}

// SyncProposal is what the recording says about the chart: its tempo,
// metre and key, and where each section starts. Nothing is written
// until ApplySync.
type SyncProposal struct {
	StemsReady   bool          `json:"stemsReady"`
	Duration     float64       `json:"duration"`
	AudioStart   float64       `json:"audioStart"`
	Tempo        int           `json:"tempo"`        // what the chart's tempo: should say, in its time signature's beat
	TimeSig      string        `json:"timeSig"`      // what the chart's time: should say
	TimeSigFrom  string        `json:"timeSigFrom"`  // "chart", "ireal" or "audio"
	AudioTempo   float64       `json:"audioTempo"`   // the recording's own estimate
	AudioTimeSig string        `json:"audioTimeSig"` // the recording's own estimate
	TimeSigConf  float64       `json:"timeSigConfidence"`
	Key          KeyReport     `json:"key"`
	Sections     []SectionTime `json:"sections"`
	Score        float64       `json:"score"` // 0..1, how well the sung sections line up with the voice
	// FitTempo: the tempo the placed sections imply (the chart's bars
	// spread between them); TempoMismatch: it isn't the recording's own
	// (FitTempo is then not proposed for the chart, and the sections
	// whose bar counts differ say so).
	FitTempo      int      `json:"fitTempo,omitempty"`
	TempoMismatch bool     `json:"tempoMismatch,omitempty"`
	Notes         []string `json:"notes,omitempty"`
	notes         []note
}

// note is one of a proposal's notes, kept as its English format and
// values so it can be said in the page's language (Localize).
type note struct {
	msg  string
	args []any
}

func (p *SyncProposal) note(msg string, args ...any) {
	p.notes = append(p.notes, note{msg, args})
	p.Notes = append(p.Notes, fmt.Sprintf(msg, args...))
}

// Localize says the notes again with tr (i18n.T for the page's
// language, bound): tr(msg, args...) formats msg's translation.
func (p *SyncProposal) Localize(tr func(msg string, args ...any) string) {
	p.Notes = p.Notes[:0]
	for _, n := range p.notes {
		p.Notes = append(p.Notes, tr(n.msg, n.args...))
	}
}

// recordingTempo is the recording's own tempo, in the chart's beat, in
// the octave (×2, ×½) nearest want: a beat tracker can't tell 90 from
// 180. 0 when the recording gave none.
func recordingTempo(a *audioanalysis.Analysis, want float64) float64 {
	t := a.Tempo
	if t <= 0 || want <= 0 {
		return t
	}
	for t*1.5 < want {
		t *= 2
	}
	for t/1.5 > want {
		t /= 2
	}
	return t
}

// tempoContradicts: the recording's own tempo rec (already in the
// chart's octave, see recordingTempo) says the chart's tempo is wrong.
// More than 8% apart, and not 3:2, 2:3, 4:3 or 3:4 (within 4%): a beat
// tracker can count a compound or shuffled beat in thirds, and a chart
// another beat than it, without either being wrong. 0 (no estimate)
// contradicts nothing.
func tempoContradicts(chart, rec float64) bool {
	if chart <= 0 || rec <= 0 || math.Abs(chart/rec-1) <= 0.08 {
		return false
	}
	for _, r := range []float64{1.5, 2.0 / 3, 4.0 / 3, 0.75} {
		if math.Abs(chart/rec/r-1) <= 0.04 {
			return false
		}
	}
	return true
}

// beatsPerBar counts a time signature's beats the way the player does
// (compound metres count dotted quarters).
func beatsPerBar(sig string) int {
	n, d, ok := strings.Cut(sig, "/")
	if !ok {
		return 4
	}
	num, err := strconv.Atoi(strings.TrimSpace(n))
	if err != nil || num <= 0 {
		return 4
	}
	if strings.TrimSpace(d) == "8" && num%3 == 0 && num > 3 {
		return num / 3
	}
	return num
}

// analyses caches recent analyses by the recording's hash: decoding and
// analysing three files takes a few seconds, and the wizard asks again
// after each edit.
var (
	analysesMu sync.Mutex
	analyses   = map[string]*audioanalysis.Analysis{}
)

// Recording returns the analysis of a song's recording in category:
// the mix, and its vocals and backing parts when they're split.
func Recording(ctx context.Context, st *store.Store, variantID int64, category store.AudioCategory) (*audioanalysis.Analysis, bool, error) {
	meta, err := st.GetSongAudioMeta(ctx, variantID, category)
	if err != nil {
		return nil, false, fmt.Errorf("no %s recording for this song", category)
	}
	status, _ := st.GetStemStatus(ctx, meta.ID)
	ready := status.Status == "done"
	cacheKey := fmt.Sprintf("%s/%v", meta.SHA256, ready)
	analysesMu.Lock()
	a := analyses[cacheKey]
	analysesMu.Unlock()
	if a != nil {
		return a, ready, nil
	}
	mix, _, err := st.GetSongAudioData(ctx, meta.ID)
	if err != nil {
		return nil, false, err
	}
	var vocals, backing []byte
	if ready {
		vocals, _, _, _ = st.GetStemData(ctx, meta.ID, "vocals")
		backing, _, _, _ = st.GetStemData(ctx, meta.ID, "no_vocals")
	}
	a, err = audioanalysis.Analyze(ctx, mix, vocals, backing)
	if err != nil {
		return nil, false, err
	}
	analysesMu.Lock()
	if len(analyses) > 32 {
		clear(analyses)
	}
	analyses[cacheKey] = a
	analysesMu.Unlock()
	return a, ready, nil
}

var (
	cuesMu sync.Mutex
	cues   = map[string]*audioanalysis.VoiceCues{}
)

// Voice returns when the singer sings in a song's recording, from its
// vocals part (ErrNoVoice until the recording has been split). Cached by
// the recording's hash: the lyrics view asks on every visit.
func Voice(ctx context.Context, st *store.Store, variantID int64, category store.AudioCategory) (*audioanalysis.VoiceCues, error) {
	meta, err := st.GetSongAudioMeta(ctx, variantID, category)
	if err != nil {
		return nil, err
	}
	cuesMu.Lock()
	c := cues[meta.SHA256]
	cuesMu.Unlock()
	if c != nil {
		return c, nil
	}
	vocals, _, _, err := st.GetStemData(ctx, meta.ID, "vocals")
	if err != nil {
		return nil, ErrNoVoice
	}
	if c, err = audioanalysis.AnalyzeVoice(ctx, vocals); err != nil {
		return nil, err
	}
	cuesMu.Lock()
	if len(cues) > 128 {
		clear(cues)
	}
	cues[meta.SHA256] = c
	cuesMu.Unlock()
	return c, nil
}

// ErrNoVoice: the recording has no vocals part (yet).
var ErrNoVoice = errors.New("the recording isn't split into its parts yet")

// ProposeSync places body's sections on the song's recording. irealSig
// is iReal Pro's time signature for the song ("" when unknown); the
// chart's own time: wins over it, and it wins over the recording's
// estimate.
func ProposeSync(a *audioanalysis.Analysis, stemsReady bool, body string, sections []SyncSection, irealSig, irealKey string) (*SyncProposal, error) {
	sg := song.Parse(body)
	p := &SyncProposal{
		StemsReady: stemsReady, Duration: a.Duration, AudioStart: a.AudioStart,
		AudioTempo: a.Tempo, AudioTimeSig: a.TimeSig, TimeSigConf: a.TimeSigConfidence,
	}
	switch {
	case sg.Get("time") != "":
		p.TimeSig, p.TimeSigFrom = sg.Get("time"), "chart"
	case irealSig != "":
		p.TimeSig, p.TimeSigFrom = irealSig, "ireal"
	default:
		p.TimeSig, p.TimeSigFrom = a.TimeSig, "audio"
	}
	beats := beatsPerBar(p.TimeSig)
	// The recording's tempo is in its own metre's beat: back to seconds
	// per bar through that metre, and on to the chart's.
	hint := 0.0
	if t, err := strconv.ParseFloat(sg.Get("tempo"), 64); err == nil && t > 0 {
		hint = 60 / t * float64(beats)
	} else if a.Tempo > 0 && p.TimeSigFrom == "audio" {
		hint = 60 / a.Tempo * float64(beatsPerBar(a.TimeSig))
	}
	if !stemsReady {
		p.note("the voice isn't separated from the backing yet: sections are placed by tempo only, try again once it is")
	}
	var secs []audioanalysis.Section
	for _, s := range sections {
		if s.BarCount <= 0 {
			continue
		}
		secs = append(secs, audioanalysis.Section{Index: s.Index, StartBar: s.StartBar, BarCount: s.BarCount, HasLyrics: s.HasLyrics})
	}
	if len(secs) == 0 {
		return nil, fmt.Errorf("the chart has no bars to place yet")
	}
	fit, err := audioanalysis.FitSections(a, secs, hint)
	if err != nil {
		return nil, err
	}
	p.Score = fit.Score
	if fit.SecondsPerBar > 0 {
		p.FitTempo = int(math.Round(60 * float64(beats) / fit.SecondsPerBar))
		p.Tempo = p.FitTempo
	}
	labels := map[int]string{}
	bars := map[int]int{}
	sung := map[int]bool{}
	for _, s := range sections {
		labels[s.Index] = s.Label
		sung[s.Index] = s.HasLyrics
		bars[s.Index] = s.BarCount
	}
	for _, m := range fit.Markers {
		p.Sections = append(p.Sections, SectionTime{Index: m.SectionIndex, Label: labels[m.SectionIndex], Time: math.Round(m.Time*100) / 100, HasLyrics: sung[m.SectionIndex]})
	}
	chartTempo, _ := strconv.ParseFloat(sg.Get("tempo"), 64)
	// The sections are placed where the singing starts; the chart's bars
	// are then spread between them. When that asks for a tempo the
	// recording doesn't play at (or for one past the edge of what was
	// searched), the chart's bar counts are what's wrong, not the fitted
	// tempo: spreading 23 written bars over 16 played ones makes the
	// bar.beat counter run a third fast. Say so, section by section, and
	// don't propose the fitted tempo.
	//
	// The chart's own tempo can be wrong too: song 334's "tempo: 104" was
	// an earlier wizard's fit, stopped at the edge of its ±15% search
	// around the recording's 90. Kept, it would be proposed again on
	// every sync. The recording's own tempo replaces it when it
	// contradicts it (tempoContradicts).
	ref := chartTempo
	if ref <= 0 {
		ref = float64(p.FitTempo)
	}
	rec := recordingTempo(a, ref)
	if p.FitTempo > 0 && len(a.VocalOnsets) > 0 && (fit.Clamped || (rec > 0 && math.Abs(float64(p.FitTempo)/rec-1) > 0.08)) {
		p.TempoMismatch = true
		chartWrong := false
		switch {
		case chartTempo > 0 && !tempoContradicts(chartTempo, rec):
			p.Tempo = int(math.Round(chartTempo))
		case rec > 0:
			p.Tempo = int(math.Round(rec))
			chartWrong = chartTempo > 0
		default:
			p.Tempo = int(math.Round(chartTempo))
		}
		if rec > 0 {
			recBar := 60 / rec * float64(beats)
			wrong := 0
			for i := range p.Sections {
				if i+1 >= len(p.Sections) {
					break // the last section runs to a fade or an ending: can't be counted
				}
				s := &p.Sections[i]
				played := (p.Sections[i+1].Time - s.Time) / recBar
				chart := bars[s.Index]
				if math.Abs(played-float64(chart)) >= math.Max(1.5, 0.12*float64(chart)) {
					s.ChartBars, s.RecordingBars = chart, int(math.Round(played))
					wrong++
				}
			}
			p.note("the chart's sections fit the singing only at %d bpm, while the recording plays at about %d bpm: in %d of its sections the chart has more or fewer bars than the recording plays, so the bar counter would run too fast or too slow there. Fix those sections' bars in the chart, then sync again", p.FitTempo, int(math.Round(rec)), wrong)
		} else {
			p.note("the chart's sections fit the singing only at %d bpm, far from the chart's tempo: check the sections' bar counts", p.FitTempo)
		}
		if chartWrong {
			p.note("the chart's tempo says %s, but the recording plays at about %d bpm: %d is proposed instead", sg.Get("tempo"), p.Tempo, p.Tempo)
		}
	} else if chartTempo > 0 && p.Tempo > 0 && math.Abs(float64(p.Tempo)-chartTempo)/chartTempo > 0.05 {
		p.note("the chart's tempo says %s, its sections fit the recording at %d: kept unless you replace it", sg.Get("tempo"), p.Tempo)
	}
	if stemsReady && fit.Score < 0.5 {
		p.note("the sung sections don't line up well with the voice: check the chart's sections and bar counts, then the markers by ear")
	}
	p.Key = CheckKey(body, irealKey, a.Key, a.KeyConfidence)
	return p, nil
}

// SetHeader sets a "key: value" header line in body: replaced when
// present and overwrite is set, otherwise added after the last header
// line (before the first blank line or section).
func SetHeader(body, key, value string, overwrite bool) string {
	lines := strings.Split(body, "\n")
	end := 0
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "[") {
			break
		}
		if k, _, ok := strings.Cut(t, ":"); ok && k == key {
			if overwrite || strings.TrimSpace(strings.TrimPrefix(t, key+":")) == "" {
				lines[i] = key + ": " + value
			}
			return strings.Join(lines, "\n")
		}
		end = i + 1
	}
	out := append([]string{}, lines[:end]...)
	out = append(out, key+": "+value)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

// ApplySync writes a proposal: the tempo and time headers (overwriting
// existing ones only when overwrite is set) and every section's marker
// on the recording, replacing the old markers. It returns the new body
// and version.
func ApplySync(ctx context.Context, st *store.Store, variantID int64, category store.AudioCategory, p *SyncProposal, expectVersion string, overwrite bool, actor int64) (string, string, error) {
	title, artist, ref, body, err := st.VariantContent(ctx, variantID)
	if err != nil {
		return "", "", err
	}
	nb := body
	if p.TimeSig != "" {
		nb = SetHeader(nb, "time", p.TimeSig, overwrite)
	}
	if p.Tempo > 0 {
		nb = SetHeader(nb, "tempo", strconv.Itoa(p.Tempo), overwrite)
	}
	version := expectVersion
	if nb != body {
		if version, err = st.SaveVariantByID(ctx, variantID, title, artist, ref, nb, expectVersion, actor); err != nil {
			return "", "", err
		}
	}
	meta, err := st.GetSongAudioMeta(ctx, variantID, category)
	if err != nil {
		return "", "", err
	}
	// A program placed these: estimated until someone checks them by ear
	// (the player says "Synchro estimée" and offers to correct it).
	markers := make([]store.AudioMarker, 0, len(p.Sections))
	for _, s := range p.Sections {
		markers = append(markers, store.AudioMarker{SectionIndex: s.Index, TimeSeconds: s.Time, Origin: store.SyncAuto})
	}
	if err := st.ReplaceAudioMarkers(ctx, meta.ID, markers, actor); err != nil {
		return "", "", err
	}
	return nb, version, nil
}
