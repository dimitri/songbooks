package httpapi

import (
	"context"
	"strings"

	"app/internal/i18n"
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

// melodyRanges is SongMelodyRanges, empty on an error (a row without its
// range is no reason to fail a page).
func (s *Server) melodyRanges(ctx context.Context, ids []int64) map[int64]store.MelodyRange {
	m, err := s.store.SongMelodyRanges(ctx, ids)
	if err != nil {
		return map[int64]store.MelodyRange{}
	}
	return m
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
