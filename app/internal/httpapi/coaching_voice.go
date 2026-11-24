package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"app/internal/i18n"
	"app/internal/store"
)

// A coaching student's voice (docs/coaching.md, "Voice types"): the range
// and the comfortable zone first; the type a working label, the coach's
// dated opinion (firm, or "leaning"), optional; a few words beside it
// (timbre, where the passages sit); her assessment; and its history,
// which the student reads too.

// timbreLabels name the timbres (English keys, translated where printed).
var timbreLabels = map[string]string{"bright": "bright timbre", "warm": "warm timbre", "dark": "dark timbre"}

// voiceLabelText is the working label in words, in lang: "leaning
// baritone", "Baritone"; "" when not classified.
func voiceLabelText(lang, voiceType string, leaning bool) string {
	label, ok := voiceTypeLabels[voiceType]
	if !ok {
		return ""
	}
	name := i18n.T(lang, label)
	if leaning {
		return i18n.T(lang, "leaning %s", strings.ToLower(name))
	}
	return name
}

// passaggiText says where the passages sit, in lang: "passages at mi3 and
// si3", "a passage at mi3"; "" when none is noted.
func passaggiText(lang string, low, high *int) string {
	switch {
	case low != nil && high != nil:
		return i18n.T(lang, "passages at %s and %s", i18n.NoteName(lang, *low), i18n.NoteName(lang, *high))
	case low != nil:
		return i18n.T(lang, "a passage at %s", i18n.NoteName(lang, *low))
	case high != nil:
		return i18n.T(lang, "a passage at %s", i18n.NoteName(lang, *high))
	}
	return ""
}

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
	Old, New   string
	Note       string
	IsNoteOnly bool
}

var voiceFieldLabels = map[string]string{
	"voice_type": "Working label",
	"tessitura":  "Tessitura",
	"timbre":     "Timbre",
	"passaggi":   "Passages",
	"assessment": "Assessment",
	"chest":      "Chest / belt",
	"mixed":      "voice|Mix",
	"head":       "Head / falsetto",
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
			ot, ol := store.ParseLabelText(c.Old)
			nt, nl := store.ParseLabelText(c.New)
			v.Old, v.New = voiceLabelText(lang, ot, ol), voiceLabelText(lang, nt, nl)
		case "timbre":
			v.Old, v.New = i18n.T(lang, timbreLabels[c.Old]), i18n.T(lang, timbreLabels[c.New])
			if c.Old == "" {
				v.Old = ""
			}
			if c.New == "" {
				v.New = ""
			}
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

// voiceData puts a student's voice type, tessitura, the assessment and its
// history in a student page's data (the coach's and the student's alike).
func (s *Server) voiceData(r *http.Request, communityID, studentID int64, coach bool, ranges []rangeView, data map[string]any) error {
	ctx := r.Context()
	lang := s.langOfRequest(r)
	p, err := s.store.StudentVoiceProfile(ctx, communityID, studentID)
	if err != nil {
		return err
	}
	data["Voice"] = p
	data["VoiceTypeLabel"] = voiceTypeLabels[p.VoiceType]
	data["VoiceLabel"] = voiceLabelText(lang, p.VoiceType, p.Leaning)
	if p.LabelAt != nil {
		data["VoiceLabelDate"] = i18n.Date(lang, *p.LabelAt, "Jan 2006")
	}
	data["VoiceTypes"] = voiceTypeOptions()
	data["Timbres"] = store.Timbres
	data["TimbreLabel"] = timbreLabels[p.Timbre]
	data["Passaggi"] = passaggiText(lang, p.PassaggioLow, p.PassaggioHigh)
	pl, ph := 0, 0
	if p.PassaggioLow != nil {
		pl = *p.PassaggioLow
	}
	if p.PassaggioHigh != nil {
		ph = *p.PassaggioHigh
	}
	data["PassLow"], data["PassHigh"] = pl, ph
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
	h, err := s.store.StudentVoiceHistory(ctx, communityID, studentID)
	if err != nil {
		return err
	}
	data["VoiceHistory"] = voiceChangeViews(h, lang)
	return nil
}

// handlePutStudentVoice is PUT /api/communities/{id}/students/{personID}/voice
// {voiceType, leaning, tessituraLow, tessituraHigh, timbre, passaggioLow,
// passaggioHigh, note}: the coach's assessment, each change kept.
func (s *Server) handlePutStudentVoice(w http.ResponseWriter, r *http.Request) {
	community, student, ok := s.coachRequest(w, r)
	if !ok {
		return
	}
	var req struct {
		VoiceType     string `json:"voiceType"`
		Leaning       bool   `json:"leaning"`
		TessituraLow  *int   `json:"tessituraLow"`
		TessituraHigh *int   `json:"tessituraHigh"`
		Timbre        string `json:"timbre"`
		PassaggioLow  *int   `json:"passaggioLow"`
		PassaggioHigh *int   `json:"passaggioHigh"`
		Note          string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	for _, n := range []*int{req.TessituraLow, req.TessituraHigh, req.PassaggioLow, req.PassaggioHigh} {
		if n != nil && (*n < 24 || *n > 108) {
			httpError(w, http.StatusBadRequest, "a note out of range")
			return
		}
	}
	a := store.VoiceAssessment{VoiceType: req.VoiceType, Leaning: req.Leaning, TessituraLow: req.TessituraLow, TessituraHigh: req.TessituraHigh,
		Timbre: req.Timbre, PassaggioLow: req.PassaggioLow, PassaggioHigh: req.PassaggioHigh, Note: req.Note}
	if err := s.store.SetStudentVoiceAssessment(r.Context(), community.ID, student.PersonID, a, personFromContext(r).ID); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
