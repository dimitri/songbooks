package httpapi

import (
	"net/http"
	"strconv"

	"app/internal/i18n"
)

// A student's range strip with the song's melody over it (docs/coaching.md,
// "The song's melody over the student's range"), where a key is chosen
// for them: the song page's Voice & key panel, the coach's "Add to…" and
// request key pickers. The strip is the student page's (C2 to C7: chest,
// mix, head, the comfortable zone); the melody is the song's range, from
// its recording, at the key being tried: it slides as − / + is pressed.
// Information, not advice: no verdict, no number.

// The strip's notes: C2 (MIDI 36) to C7 (96), as rangeViews places them.
const (
	stripLow  = 36
	stripHigh = 96
)

// stripSpan is where a range sits on the strip, in %: from its lowest
// note to its highest, both included, cut at the strip's ends (clipped
// says so; nothing of it on the strip: ok false).
type stripSpan struct {
	Left    float64 `json:"left"`
	Width   float64 `json:"width"`
	Clipped bool    `json:"clipped,omitempty"`
}

// spanOn places low–high moved by n semitones on the strip.
func spanOn(low, high, n int) (stripSpan, bool) {
	lo, hi := low+n, high+n
	if lo > hi {
		lo, hi = hi, lo
	}
	if hi < stripLow || lo > stripHigh {
		return stripSpan{}, false
	}
	var s stripSpan
	if lo < stripLow {
		lo, s.Clipped = stripLow, true
	}
	if hi > stripHigh {
		hi, s.Clipped = stripHigh, true
	}
	s.Left = float64(lo-stripLow) * 100 / float64(stripHigh-stripLow)
	s.Width = float64(hi-lo+1) * 100 / float64(stripHigh-stripLow+1)
	return s, true
}

// melodyKeys is how far a key is tried either way: the song page's key
// goes 7 semitones each way, a coach's save 12.
const melodyKeys = 12

// voiceStrip is the JSON the strip is drawn from.
type voiceStrip struct {
	// Scale: the strip's C notes, named in the page's language.
	Scale     []string   `json:"scale"`
	Registers []stripBar `json:"registers"`
	Comfort   *stripSpan `json:"comfort,omitempty"`
	// Melody: where the song's melody sits in each key tried, by
	// semitones from the recording's ("-2", "0", "3"); absent when its
	// range isn't known (yet).
	Melody map[string]stripSpan `json:"melody,omitempty"`
}

type stripBar struct {
	Register string `json:"register"`
	Label    string `json:"label"`
	stripSpan
}

// handleGetVoiceStrip is GET /api/songs/{id}/voice-strip?community=C[&person=P]:
// a student's strip with the song's melody over it. Without person, the
// viewer's own (a student; the student an admin looks as); with person,
// for that community's coach only. The song must be the community's.
func (s *Server) handleGetVoiceStrip(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid song")
		return
	}
	communityID, _ := strconv.ParseInt(r.URL.Query().Get("community"), 10, 64)
	community, err := s.store.GetCommunity(ctx, communityID)
	if err != nil || !community.IsCoaching() {
		http.NotFound(w, r)
		return
	}
	v, err := s.coachingViewerOf(r, community.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	studentID := v.PersonID
	if p, _ := strconv.ParseInt(r.URL.Query().Get("person"), 10, 64); p != 0 && p != studentID {
		if !v.Coach {
			httpError(w, http.StatusForbidden, "only the coach sees another student's range")
			return
		}
		studentID = p
	}
	if _, ok := s.currentStudent(r, community.ID, studentID); !ok || studentID == 0 {
		http.NotFound(w, r)
		return
	}
	if ok, err := s.store.CommunityHasSong(ctx, community.ID, variantID); err != nil || !ok {
		http.NotFound(w, r)
		return
	}
	lang := s.langOfRequest(r)
	out := voiceStrip{Registers: []stripBar{}}
	for m := stripLow; m <= stripHigh; m += 12 {
		out.Scale = append(out.Scale, i18n.NoteName(lang, m))
	}
	rs, err := s.store.StudentRanges(ctx, community.ID, studentID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, rv := range rangeViews(rs, lang) {
		b := stripBar{Register: rv.Register, Label: i18n.T(lang, rv.Label)}
		if rv.Low != nil && rv.High != nil {
			b.stripSpan, _ = spanOn(*rv.Low, *rv.High, 0)
		}
		out.Registers = append(out.Registers, b)
	}
	p, err := s.store.StudentVoiceProfile(ctx, community.ID, studentID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p.TessituraLow != nil && p.TessituraHigh != nil {
		if c, ok := spanOn(*p.TessituraLow, *p.TessituraHigh, 0); ok {
			out.Comfort = &c
		}
	}
	low, high, known, err := s.store.SongMelodyRange(ctx, variantID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if known {
		out.Melody = map[string]stripSpan{}
		for n := -melodyKeys; n <= melodyKeys; n++ {
			if sp, ok := spanOn(low, high, n); ok {
				out.Melody[strconv.Itoa(n)] = sp
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}
