package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"app/internal/i18n"
	"app/internal/store"
)

// A coaching student's voice type (docs/coaching.md, "Voice types"): the
// coach's assessment on the student's page (a type or none yet, the
// tessitura, a few words), its history, and whether the student reads
// them. The student always sees their type and their range.

// voiceTypeLabels name the voice types (English keys, translated where
// printed).
var voiceTypeLabels = map[string]string{
	"soprano":      "Soprano",
	"mezzo":        "Mezzo-soprano",
	"contralto":    "Contralto",
	"countertenor": "Countertenor",
	"tenor":        "Tenor",
	"baritone":     "Baritone",
	"bass":         "Bass",
}

// voiceTypeOption is one choice of the coach's voice type select.
type voiceTypeOption struct {
	Value, Label string
}

func voiceTypeOptions() []voiceTypeOption {
	out := []voiceTypeOption{{"", "Not classified yet"}}
	for _, t := range store.VoiceTypes {
		out = append(out, voiceTypeOption{t, voiceTypeLabels[t]})
	}
	return out
}

// voiceChangeView is a history line to print: what changed, from what to
// what, said in the page's language (types as English keys, translated
// where printed; ranges in note names).
type voiceChangeView struct {
	At         time.Time
	By         string
	Field      string // English label
	IsType     bool
	Old, New   string
	Note       string
	IsNoteOnly bool
}

var voiceFieldLabels = map[string]string{
	"voice_type": "Voice type",
	"tessitura":  "Tessitura",
	"assessment": "Assessment",
	"chest":      "Chest voice",
	"mixed":      "Mixed voice",
	"head":       "Head voice",
}

// spanName is a range as "do3–la4" in lang, "" when not noted.
func spanName(low, high *int, lang string) string {
	if low == nil || high == nil {
		return ""
	}
	return i18n.NoteName(lang, *low) + "–" + i18n.NoteName(lang, *high)
}

func voiceChangeViews(cs []store.VoiceChange, lang string) []voiceChangeView {
	out := make([]voiceChangeView, len(cs))
	for i, c := range cs {
		v := voiceChangeView{At: c.ChangedAt, By: c.ChangedBy, Field: voiceFieldLabels[c.Field], Note: c.Note}
		switch c.Field {
		case "voice_type":
			v.IsType = true
			v.Old, v.New = voiceTypeLabels[c.Old], voiceTypeLabels[c.New]
		case "assessment":
			v.IsNoteOnly = true
		default:
			ol, oh := store.ParseRangeText(c.Old)
			nl, nh := store.ParseRangeText(c.New)
			v.Old, v.New = spanName(ol, oh, lang), spanName(nl, nh, lang)
		}
		out[i] = v
	}
	// One save's changes share its note: said once, under the last.
	for i := range out[:max(0, len(out)-1)] {
		next := out[i+1]
		if out[i].Note != "" && out[i].Note == next.Note && !out[i].IsNoteOnly && out[i].At.Sub(next.At).Abs() < 2*time.Second {
			out[i].Note = ""
		}
	}
	return out
}

// tessituraView is the tessitura on the voice chart, as one more bar.
func tessituraView(p store.VoiceProfile, lang string) *rangeView {
	if p.TessituraLow == nil || p.TessituraHigh == nil {
		return nil
	}
	rv := rangeViews([]store.VoiceRange{{Register: "tessitura", Low: p.TessituraLow, High: p.TessituraHigh}}, lang)[0]
	rv.Label = "Tessitura"
	return &rv
}

// voiceData puts a student's voice type, tessitura and (for the coach, or
// when shared) the assessment and its history in a student page's data.
func (s *Server) voiceData(r *http.Request, communityID, studentID int64, coach bool, ranges []rangeView, data map[string]any) error {
	ctx := r.Context()
	lang := s.langOfRequest(r)
	p, err := s.store.StudentVoiceProfile(ctx, communityID, studentID)
	if err != nil {
		return err
	}
	data["Voice"] = p
	data["VoiceTypeLabel"] = voiceTypeLabels[p.VoiceType]
	data["VoiceTypes"] = voiceTypeOptions()
	chart := append([]rangeView{}, ranges...)
	if t := tessituraView(p, lang); t != nil {
		data["Tessitura"] = t
		chart = append(chart, *t)
	}
	data["ChartRanges"] = chart
	lo, hi := 0, 0
	for _, rv := range ranges {
		if rv.LowMidi != 0 && (lo == 0 || rv.LowMidi < lo) {
			lo = rv.LowMidi
		}
		if rv.HighMidi > hi {
			hi = rv.HighMidi
		}
	}
	if lo != 0 && hi != 0 {
		data["VoiceSpan"] = i18n.NoteName(lang, lo) + "–" + i18n.NoteName(lang, hi)
	}
	tl, th := 0, 0
	if p.TessituraLow != nil {
		tl = *p.TessituraLow
	}
	if p.TessituraHigh != nil {
		th = *p.TessituraHigh
	}
	data["TessLow"], data["TessHigh"] = tl, th
	data["ShowAssessment"] = coach || p.Shared
	if coach || p.Shared {
		h, err := s.store.StudentVoiceHistory(ctx, communityID, studentID)
		if err != nil {
			return err
		}
		data["VoiceHistory"] = voiceChangeViews(h, lang)
	}
	return nil
}

// handlePutStudentVoice is PUT /api/communities/{id}/students/{personID}/voice
// {voiceType, tessituraLow, tessituraHigh, note}: the coach's assessment,
// each change kept.
func (s *Server) handlePutStudentVoice(w http.ResponseWriter, r *http.Request) {
	community, student, ok := s.coachRequest(w, r)
	if !ok {
		return
	}
	var req struct {
		VoiceType     string `json:"voiceType"`
		TessituraLow  *int   `json:"tessituraLow"`
		TessituraHigh *int   `json:"tessituraHigh"`
		Note          string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	for _, n := range []*int{req.TessituraLow, req.TessituraHigh} {
		if n != nil && (*n < 24 || *n > 108) {
			httpError(w, http.StatusBadRequest, "a note out of range")
			return
		}
	}
	if err := s.store.SetStudentVoiceAssessment(r.Context(), community.ID, student.PersonID, req.VoiceType, req.TessituraLow, req.TessituraHigh, req.Note, personFromContext(r).ID); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePutStudentVoiceShared is PUT
// /api/communities/{id}/students/{personID}/voice/shared {shared}: the
// student reads the assessment and its history, or not (not, until the
// coach says so).
func (s *Server) handlePutStudentVoiceShared(w http.ResponseWriter, r *http.Request) {
	community, student, ok := s.coachRequest(w, r)
	if !ok {
		return
	}
	var req struct {
		Shared bool `json:"shared"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetStudentVoiceShared(r.Context(), community.ID, student.PersonID, req.Shared, personFromContext(r).ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"shared": req.Shared})
}
