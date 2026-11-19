package httpapi

import (
	"encoding/json"
	"net/http"

	"app/internal/i18n"
	"app/internal/store"
)

// The coach's settings for checking singing (pitch-check.js, migration
// 0086): how strict each of the three levels is, and the words students
// get after a take, written by the coach in their own voice. The app's
// own words come filled in, as examples to rewrite.

// checkMessage is a situation after a take: its key, when it shows, the
// app's words (translated), the placeholders it may use.
type checkMessage struct {
	Key, When, Default, Vars string
}

// checkMessages are the situations, in the order a take's result says
// them. Their defaults are pitch-check.js's (the same English, the same
// translations).
var checkMessages = []checkMessage{
	{"goal_reached", "The coach’s goal is reached", "Goal reached! Well done, {name}.", "{name} {pitch} {rhythm}"},
	{"personal_best", "Their best take on this song", "Personal best!", "{name} {pitch} {rhythm}"},
	{"three_stars", "Three stars", "Three stars: beautifully sung, {name}.", "{name} {pitch}"},
	{"keep_going", "No star yet", "Keep going: every take counts, and the next one will be better.", "{name} {pitch}"},
	{"centre", "Right on the notes", "Right in the middle of the notes.", "{name}"},
	{"flat", "Under the notes", "A little flat ({cents} cents): aim a touch higher.", "{name} {cents}"},
	{"sharp", "Over the notes", "A little sharp ({cents} cents): aim a touch lower.", "{name} {cents}"},
	{"far", "Often far from the notes", "The notes are often far: listen to the line again, with the voice up.", "{name} {pitch}"},
	{"steady", "Entries falling together", "Steady: your entries fall together.", "{name} {ms}"},
	{"uneven", "Entries a little uneven", "A little uneven ({ms} ms): count the rests.", "{name} {ms}"},
	{"very_uneven", "Entries uneven", "Uneven ({ms} ms): clap the rhythm, then sing it.", "{name} {ms}"},
}

type checkLevelView struct {
	store.CheckLevel
	Label   string // its name, or the app's
	Default store.CheckLevel
}

type checkMessageView struct {
	checkMessage
	Text   string // the coach's, or the app's words to rewrite
	Custom bool
}

// checkLevelLabel is a level's name: the coach's, or the app's.
func checkLevelLabel(lang string, l store.CheckLevel) string {
	if l.Name != "" {
		return l.Name
	}
	switch l.Key {
	case "advanced":
		return i18n.T(lang, "Advanced")
	case "intermediate":
		return i18n.T(lang, "Intermediate")
	}
	return i18n.T(lang, "Beginner")
}

// handleCheckSettingsPage is GET /communities/{id}/checking: the coach's
// levels and messages.
func (s *Server) handleCheckSettingsPage(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	if !coach {
		http.NotFound(w, r)
		return
	}
	set, err := s.store.GetCheckSettings(r.Context(), community.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	lang := s.langOfRequest(r)
	var levels []checkLevelView
	for _, l := range set.Levels {
		levels = append(levels, checkLevelView{CheckLevel: l, Label: checkLevelLabel(lang, l), Default: store.DefaultCheckLevels[l.Key]})
	}
	data := s.coachingPageData(r, community, coach, "students")
	data["Levels"] = levels
	data["Messages"] = s.checkMessageViews(r, set)
	s.render(w, "coaching_checking", data)
}

// handlePutCheckSettings is PUT /api/communities/{id}/check-settings
// {"levels": [{key, name, cents, ms}], "messages": {key: text}}: a
// message left as the app's words, or emptied, stays the app's.
func (s *Server) handlePutCheckSettings(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	if !coach {
		httpError(w, http.StatusForbidden, "only the coach can do that")
		return
	}
	var req struct {
		Levels   []store.CheckLevel `json:"levels"`
		Messages map[string]string  `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	keep := map[string]string{}
	for _, m := range checkMessages {
		v, ok := req.Messages[m.Key]
		if !ok || v == "" {
			continue
		}
		// the app's words, in any language: left alone
		app := false
		for _, l := range i18n.Langs {
			app = app || v == i18n.T(l, m.Default)
		}
		if !app {
			keep[m.Key] = v
		}
	}
	if err := s.store.SetCheckSettings(r.Context(), community.ID, req.Levels, keep); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// checkLevelsData puts a community's levels on a page (the goal form, the
// goals shown): CheckLevels, and CheckLevelNames by key.
func (s *Server) checkLevelsData(r *http.Request, communityID int64, data map[string]any) error {
	set, err := s.store.GetCheckSettings(r.Context(), communityID)
	if err != nil {
		return err
	}
	lang := s.langOfRequest(r)
	names := map[string]string{"": checkLevelLabel(lang, store.DefaultCheckLevels["beginner"])}
	var levels []checkLevelView
	for _, l := range set.Levels {
		v := checkLevelView{CheckLevel: l, Label: checkLevelLabel(lang, l), Default: store.DefaultCheckLevels[l.Key]}
		levels = append(levels, v)
		names[l.Key] = v.Label
	}
	data["CheckLevels"] = levels
	data["CheckLevelNames"] = names
	return nil
}

// checkMessageViews are the situations with the community's words (or the
// app's), for a form that rewrites them.
func (s *Server) checkMessageViews(r *http.Request, set store.CheckSettings) []checkMessageView {
	lang := s.langOfRequest(r)
	var out []checkMessageView
	for _, m := range checkMessages {
		v := checkMessageView{checkMessage: m, Text: i18n.T(lang, m.Default)}
		if c, ok := set.Messages[m.Key]; ok {
			v.Text, v.Custom = c, true
		}
		v.When = i18n.T(lang, m.When)
		v.Default = i18n.T(lang, m.Default)
		out = append(out, v)
	}
	return out
}
