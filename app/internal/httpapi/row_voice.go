package httpapi

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"app/internal/audioanalysis"
	"app/internal/i18n"
	"app/internal/melody"
	"app/internal/store"
)

// A songbook row's melody range (docs/coaching.md, "The song's melody on
// songbook rows"): wherever a songbook lists its songs (a person's
// songbook, a band's, the coach's one songbook, a student's), the song's
// melody, lowest to highest note, from the recording the player uses
// (song_melody_ranges, store.SongMelodyRanges), at the row's key: "A2–E4".
// For a student looking at their own songbook (or the coach filtering
// theirs), how it sits in their voice at that key: "fits", "2 high", "1
// low" — semitones out of their comfortable zone when the coach noted
// it, else out of their whole range. Information, no verdict; nothing
// when the melody isn't known. Template "row-voice" (row_voice.html).

// rowVoice is what a row says of the song's melody.
type rowVoice struct {
	Range string // "A2–E4" (FR "la1–mi3"), at the row's key
	// Staff: the range as one bar, its lowest and highest notes
	// (melody.RangeSVG); Label its words for a screen reader ("Range
	// A2–E4"). Src: the melody is analysed, its lowest and highest bars
	// are fetched from there when the row is in view (row-range.js,
	// handleGetRowRange). Zone: "low,high,comfort" for the fit the bars
	// are weighed against, "" with none.
	Staff template.HTML
	Label string
	Src   string
	Zone  string
	// Fit: "fits", "2 high", "1 low", "2 high · 1 low"; "" with no voice
	// to weigh it against. Out: it doesn't fit.
	Fit   string
	Out   bool
	Title string // "melody A2–E4 · comfortable zone C3–A4"
}

// melodySpan names a range moved by n semitones: "A2–E4".
func melodySpan(lang string, m store.MelodyRange, n int) string {
	return i18n.NoteName(lang, m.Low+n) + "–" + i18n.NoteName(lang, m.High+n)
}

// rowVoiceOf is a row's melody at n semitones from the recording's key,
// weighed against zone when there is one; the zero rowVoice (nothing to
// show) when the melody isn't known (ok false).
func rowVoiceOf(lang string, m store.MelodyRange, ok bool, n int, zone *store.VoiceZone) rowVoice {
	if !ok || m.Low <= 0 || m.High < m.Low {
		return rowVoice{}
	}
	v := rowVoice{Range: melodySpan(lang, m, n)}
	v.Title = i18n.T(lang, "melody %s", v.Range)
	if zone == nil {
		return v
	}
	zoneText := i18n.NoteName(lang, zone.Low) + "–" + i18n.NoteName(lang, zone.High)
	if zone.Comfort {
		v.Title = i18n.T(lang, "melody %s · comfortable zone %s", v.Range, zoneText)
	} else {
		v.Title = i18n.T(lang, "melody %s · range %s", v.Range, zoneText)
	}
	high, low := m.High+n-zone.High, zone.Low-(m.Low+n)
	var parts []string
	if high > 0 {
		parts = append(parts, i18n.T(lang, "%d high", high))
	}
	if low > 0 {
		parts = append(parts, i18n.T(lang, "%d low", low))
	}
	if len(parts) == 0 {
		v.Fit = i18n.T(lang, "fits")
		return v
	}
	v.Fit, v.Out = strings.Join(parts, " · "), true
	return v
}

// rowMelodies is what a page's rows know of their songs' melodies: the
// ranges known, the songs whose melody is analysed.
type rowMelodies struct {
	ranges   map[int64]store.MelodyRange
	analysed map[int64]bool
}

// melodyRanges is SongMelodyRanges and SongsWithMelody, empty on an
// error (a row without its range is no reason to fail a page).
func (s *Server) melodyRanges(ctx context.Context, ids []int64) rowMelodies {
	out := rowMelodies{ranges: map[int64]store.MelodyRange{}, analysed: map[int64]bool{}}
	if m, err := s.store.SongMelodyRanges(ctx, ids); err == nil {
		out.ranges = m
	}
	if m, err := s.store.SongsWithMelody(ctx, ids, audioanalysis.MelodyNotesAnalyzer); err == nil {
		out.analysed = m
	}
	return out
}

// voice is song's row: its melody at n semitones from the recording's
// key (key: the song's own, for the notes' spelling), weighed against
// zone; drawn as one bar when its range is known, its lowest and
// highest bars to come when it's analysed.
func (rm rowMelodies) voice(lang string, song int64, key string, n int, zone *store.VoiceZone) rowVoice {
	m, known := rm.ranges[song]
	v := rowVoiceOf(lang, m, known, n, zone)
	if v.Range != "" {
		k, _ := melody.KeyOf(key, n, false)
		v.Label = i18n.T(lang, "Range %s", v.Range)
		v.Staff = template.HTML(melody.RangeSVG(m.Low, m.High, n, k, v.Label))
	}
	if rm.analysed[song] {
		v.Src = fmt.Sprintf("/api/songs/%d/row-range?k=%d", song, n)
		if v.Title == "" {
			v.Title = i18n.T(lang, "the melody’s lowest and highest notes")
		}
		if zone != nil {
			c := 0
			if zone.Comfort {
				c = 1
			}
			v.Zone = fmt.Sprintf("%d,%d,%d", zone.Low, zone.High, c)
		}
	}
	return v
}

// The melody's lowest and highest bars, drawn once per song, key and
// state of its melody (store.MelodyStamp), for a while: a songbook's
// rows ask for them as they come into view.
var rowRangeCache sync.Map // rowRangeKey → rowRangeEntry

type rowRangeKey struct {
	st   *store.Store
	song int64
	k    int
	lang string
}

type rowRangeEntry struct {
	stamp string
	at    time.Time
	out   *rowRangeResponse
}

// rowRangeTTL: a sync moved since (the bars follow it) shows within that.
const rowRangeTTL = 10 * time.Minute

// rowRangeBar is one bar drawn: the played bar it is, which extreme it
// holds ("low", "high", or both), its SVG.
type rowRangeBar struct {
	Bar  int    `json:"bar"`
	Kind string `json:"kind"`
	SVG  string `json:"svg"`
	// Label: its words ("lowest note, bar 12").
	Label string `json:"label"`
}

// rowRangeResponse is GET /api/songs/{id}/row-range's answer.
type rowRangeResponse struct {
	Low   int           `json:"low"` // MIDI, at the key asked
	High  int           `json:"high"`
	Label string        `json:"label"` // "Range A2–E4; lowest in bar 12, highest in bar 31"
	Bars  []rowRangeBar `json:"bars"`
}

// handleGetRowRange is GET /api/songs/{id}/row-range?k=N: the bars where
// the song's melody first reaches its lowest note and its highest, as
// sung (the melody sheet's bars, played order: a repeat's first pass),
// moved N semitones (the row's key), with the syllables under them, the
// note marked. 204 when the melody isn't there to draw (not analysed, no
// bars in its recording yet).
func (s *Server) handleGetRowRange(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	k, _ := strconv.Atoi(r.URL.Query().Get("k"))
	k = max(-12, min(12, k))
	ctx := r.Context()
	lang := s.langOfRequest(r)
	stamp, err := s.store.MelodyStamp(ctx, id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	key := rowRangeKey{s.store, id, k, lang}
	if e, ok := rowRangeCache.Load(key); ok {
		if e := e.(rowRangeEntry); e.stamp == stamp && time.Since(e.at) < rowRangeTTL {
			s.writeRowRange(w, e.out)
			return
		}
	}
	_, sc, err := s.melodyScore(ctx, id, k)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	lo, hi, _, err := s.store.SongMelodyRange(ctx, id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := rowRangeOf(lang, sc, k, lo, hi)
	rowRangeCache.Store(key, rowRangeEntry{stamp: stamp, at: time.Now(), out: out})
	s.writeRowRange(w, out)
}

func (s *Server) writeRowRange(w http.ResponseWriter, out *rowRangeResponse) {
	w.Header().Set("Cache-Control", "private, max-age=300")
	if out == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// rowRangeOf draws a score's lowest and highest bars (nil: no score, no
// notes); k: the semitones the score is moved by; within the melody's
// known range (rangeLo, rangeHi: MIDI, the recording's key; 0: none
// known), as the row's range says it.
func rowRangeOf(lang string, sc *melody.Score, k, rangeLo, rangeHi int) *rowRangeResponse {
	if sc == nil {
		return nil
	}
	lo, hi, ok := sc.Sheet.ExtremesWithin(rangeLo, rangeHi)
	if !ok {
		return nil
	}
	bl, bh := sc.Sheet.BarOf(lo), sc.Sheet.BarOf(hi)
	if bl < 0 || bh < 0 {
		return nil
	}
	low, high := sc.Sheet.Notes[lo].Midi+k, sc.Sheet.Notes[hi].Midi+k
	span := i18n.NoteName(lang, low) + "–" + i18n.NoteName(lang, high)
	pl, ph := sc.Sheet.Bars[bl].Played+1, sc.Sheet.Bars[bh].Played+1
	out := &rowRangeResponse{Low: low, High: high,
		Label: i18n.T(lang, "Range %s; lowest in bar %d, highest in bar %d", span, pl, ph)}
	if bl == bh {
		l := i18n.T(lang, "lowest and highest notes, bar %d", pl)
		svg := sc.BarsSVG([]int{bl}, []map[int]bool{{lo: true, hi: true}}, []string{l})
		out.Bars = []rowRangeBar{{Bar: pl - 1, Kind: "both", Label: l, SVG: svg[0]}}
		return out
	}
	ll, lh := i18n.T(lang, "lowest note, bar %d", pl), i18n.T(lang, "highest note, bar %d", ph)
	svg := sc.BarsSVG([]int{bl, bh}, []map[int]bool{{lo: true}, {hi: true}}, []string{ll, lh})
	out.Bars = []rowRangeBar{
		{Bar: pl - 1, Kind: "low", Label: ll, SVG: svg[0]},
		{Bar: ph - 1, Kind: "high", Label: lh, SVG: svg[1]},
	}
	return out
}

// songbookVoiceZone is whose voice a songbook's rows are weighed against
// for viewerID: a student's songbook, for that student (and their coach);
// the viewer's own songbook, for them when they're a student somewhere
// (their voice there). nil otherwise, and when no voice is noted.
func (s *Server) songbookVoiceZone(ctx context.Context, viewerID int64, sb *store.Songbook) *store.VoiceZone {
	switch {
	case sb.IsStudent() && sb.StudentPersonID != 0 && sb.OwnerCommunityID != 0:
		student := sb.StudentPersonID
		if student != viewerID {
			if ok, err := s.store.HasAnyCommunityManagePrivilege(ctx, sb.OwnerCommunityID, viewerID); err != nil || !ok {
				return nil
			}
		}
		z, _ := s.store.StudentVoiceZone(ctx, student, sb.OwnerCommunityID)
		return z
	case sb.OwnerKind == "person" && sb.OwnerPersonID == viewerID:
		z, _ := s.store.StudentVoiceZone(ctx, viewerID, 0)
		return z
	}
	return nil
}
