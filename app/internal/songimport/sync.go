package songimport

import (
	"context"
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
	Notes        []string      `json:"notes,omitempty"`
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
		p.Notes = append(p.Notes, "the voice isn't separated from the backing yet: sections are placed by tempo only, try again once it is")
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
		p.Tempo = int(math.Round(60 * float64(beats) / fit.SecondsPerBar))
	}
	labels := map[int]string{}
	for _, s := range sections {
		labels[s.Index] = s.Label
	}
	for _, m := range fit.Markers {
		p.Sections = append(p.Sections, SectionTime{Index: m.SectionIndex, Label: labels[m.SectionIndex], Time: math.Round(m.Time*100) / 100})
	}
	if t, err := strconv.ParseFloat(sg.Get("tempo"), 64); err == nil && t > 0 && p.Tempo > 0 && math.Abs(float64(p.Tempo)-t)/t > 0.05 {
		p.Notes = append(p.Notes, fmt.Sprintf("the chart's tempo says %s, its sections fit the recording at %d: kept unless you replace it", sg.Get("tempo"), p.Tempo))
	}
	if stemsReady && fit.Score < 0.5 {
		p.Notes = append(p.Notes, "the sung sections don't line up well with the voice: check the chart's sections and bar counts, then the markers by ear")
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
	old, err := st.ListAudioMarkers(ctx, meta.ID)
	if err != nil {
		return "", "", err
	}
	keep := map[int]bool{}
	for _, s := range p.Sections {
		keep[s.Index] = true
		if err := st.SetAudioMarker(ctx, meta.ID, s.Index, s.Time); err != nil {
			return "", "", err
		}
	}
	for _, m := range old {
		if !keep[m.SectionIndex] {
			if err := st.DeleteAudioMarker(ctx, meta.ID, m.SectionIndex); err != nil {
				return "", "", err
			}
		}
	}
	return nb, version, nil
}
