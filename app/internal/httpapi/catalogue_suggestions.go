package httpapi

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"app/internal/chordsym"
	"app/internal/i18n"
	"app/internal/songdiff"
	"app/internal/store"
)

// Suggestions (the edit model, docs/catalogue-edit-review.md §4.4): what
// of an edit of The Big Songbook's song identifies the song or lines it
// up with its recording — the key's name, the lead-in, the Guitar Pro
// sync, the title and the artist, removing the recording — is a
// suggestion: a catalogue edit held for a reviewer (store.HoldCatalogueEdit),
// which its author sees in place on the song (and plays), and reviewers
// see on the song as well as in the queue. In a mixed save the rest (the
// chords, the lyrics, the sections) goes live at once, and only the
// protected part waits ("chords live, key suggested").

// suggestedHeaders are the header fields each protected reason covers.
var suggestedHeaders = map[string][]string{
	"key":       {"key"},
	"lead":      {"lead"},
	"gp":        {"gp_start"},
	"title":     {"title", "artist"},
	"reference": {"reference"},
}

// suggestionPart reports whether reasons make a part of the edit a
// suggestion that the rest can be applied without (the protected fields);
// false for a whole edit held (a passer-by's, past the daily limit).
func suggestionPart(held []string) bool {
	if len(held) == 0 {
		return false
	}
	for _, r := range held {
		if _, ok := suggestedHeaders[r]; !ok {
			return false
		}
	}
	return true
}

var sectionHeadRE = regexp.MustCompile(`^\[([^\[\]]+)\]`)

// withoutProtected is mine with the protected parts reasons name put back
// as they are in cur: the header fields (suggestedHeaders), and for "gp"
// the sections' Guitar Pro lines (gp[...]:, gp:), section by section (a
// section matched by its label and rank). What's left is what can go live
// at once.
func withoutProtected(cur, mine string, reasons []string) string {
	out := mine
	for _, r := range reasons {
		for _, k := range suggestedHeaders[r] {
			out = restoreHeaderLine(cur, out, k)
		}
	}
	for _, r := range reasons {
		if r == "gp" {
			out = withSectionGP(out, sectionGPLines(cur))
		}
	}
	return out
}

// restoreHeaderLine is out with header field key as cur has it: its
// value replaced in place, dropped, or put back where cur has it (after
// the field cur has before it).
func restoreHeaderLine(cur, out, key string) string {
	v := headerValue(cur, key)
	if v == "" {
		return removeHeaderLine(out, key)
	}
	if headerHas(out, key) {
		// One line of it, cur's value, where out has it first.
		set := setHeaderLine(out, key, v)
		lines := strings.Split(set, "\n")
		res := make([]string, 0, len(lines))
		seen, head := false, true
		for _, l := range lines {
			if head && sectionHeadRE.MatchString(strings.TrimSpace(l)) {
				head = false
			}
			if k, _, ok := strings.Cut(l, ":"); head && ok && !strings.HasPrefix(l, " ") && strings.TrimSpace(k) == key {
				if seen {
					continue
				}
				seen = true
			}
			res = append(res, l)
		}
		return strings.Join(res, "\n")
	}
	// Absent from out: after the field before it in cur.
	prev := ""
	for _, l := range strings.Split(cur, "\n") {
		if sectionHeadRE.MatchString(strings.TrimSpace(l)) {
			break
		}
		k, _, ok := strings.Cut(l, ":")
		if !ok || strings.HasPrefix(l, " ") {
			continue
		}
		if strings.TrimSpace(k) == key {
			break
		}
		prev = strings.TrimSpace(k)
	}
	lines := strings.Split(out, "\n")
	at := 0
	if prev != "" {
		for i, l := range lines {
			if sectionHeadRE.MatchString(strings.TrimSpace(l)) {
				break
			}
			if k, _, ok := strings.Cut(l, ":"); ok && !strings.HasPrefix(l, " ") && strings.TrimSpace(k) == prev {
				at = i + 1
				break
			}
		}
	}
	lines = append(lines[:at], append([]string{key + ": " + v}, lines[at:]...)...)
	return strings.Join(lines, "\n")
}

// headerHas reports whether a chart's header has field key (empty or not).
func headerHas(body, key string) bool {
	for _, l := range strings.Split(body, "\n") {
		if sectionHeadRE.MatchString(strings.TrimSpace(l)) {
			break
		}
		if k, _, ok := strings.Cut(l, ":"); ok && !strings.HasPrefix(l, " ") && strings.TrimSpace(k) == key {
			return true
		}
	}
	return false
}

// headerValue is a chart's header field as written ("" when absent).
func headerValue(body, key string) string {
	for _, l := range strings.Split(body, "\n") {
		if sectionHeadRE.MatchString(strings.TrimSpace(l)) {
			break
		}
		if k, v, ok := strings.Cut(l, ":"); ok && !strings.HasPrefix(l, " ") && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// removeHeaderLine drops a chart's header field (every line of it).
func removeHeaderLine(body, key string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	head := true
	for _, l := range lines {
		if head && sectionHeadRE.MatchString(strings.TrimSpace(l)) {
			head = false
		}
		if k, _, ok := strings.Cut(l, ":"); head && ok && !strings.HasPrefix(l, " ") && strings.TrimSpace(k) == key {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

func isGPLine(l string) bool {
	l = strings.TrimSpace(l)
	return strings.HasPrefix(l, "gp[") || strings.HasPrefix(l, "gp:")
}

// sectionGPLines is each section's Guitar Pro lines, by label and rank
// ("verse#2").
func sectionGPLines(body string) map[string][]string {
	out := map[string][]string{}
	rank := map[string]int{}
	key := ""
	for _, l := range strings.Split(body, "\n") {
		if m := sectionHeadRE.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			label := strings.ToLower(strings.TrimSpace(m[1]))
			rank[label]++
			key = label + "#" + strconv.Itoa(rank[label])
			continue
		}
		if key != "" && isGPLine(l) {
			out[key] = append(out[key], l)
		}
	}
	return out
}

// withSectionGP is body with each section's Guitar Pro lines replaced by
// gp's for that section (none when gp has none), right after its
// header.
func withSectionGP(body string, gp map[string][]string) string {
	var out []string
	rank := map[string]int{}
	for _, l := range strings.Split(body, "\n") {
		if isGPLine(l) {
			continue
		}
		out = append(out, l)
		if m := sectionHeadRE.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			label := strings.ToLower(strings.TrimSpace(m[1]))
			rank[label]++
			out = append(out, gp[label+"#"+strconv.Itoa(rank[label])]...)
		}
	}
	return strings.Join(out, "\n")
}

// catalogueSuggestion is the viewer's suggestion for a song, as its page
// shows it to them: Shown is the song with it (merged over what changed
// since, where it doesn't touch the same place; "" when it can't be), the
// changes it suggests, and how many reviewers may accept it.
type catalogueSuggestion struct {
	Edit      *store.CatalogueEdit
	Shown     string
	Changes   []songdiff.Change
	Sections  string // the indexes of the sections it changes, "1,3" (song.js marks them)
	Reviewers int
	Off       bool // the page shows the song as it is (?suggestion=off)
	// OnURL, OffURL: this page with the suggestion, and as it is.
	OnURL, OffURL string
}

// suggestionFor is personID's suggestion for songID (cur: its text now),
// nil when none.
func (s *Server) suggestionFor(r *http.Request, personID, songID int64, cur string) *catalogueSuggestion {
	e, err := s.store.PendingCatalogueEditOf(r.Context(), personID, songID)
	if err != nil || e == nil {
		return nil
	}
	sg := &catalogueSuggestion{Edit: e, Off: r.URL.Query().Get("suggestion") == "off"}
	q := r.URL.Query()
	q.Del("edit")
	q.Del("suggestion")
	sg.OnURL = r.URL.Path
	if len(q) > 0 {
		sg.OnURL += "?" + q.Encode()
	}
	q.Set("suggestion", "off")
	sg.OffURL = r.URL.Path + "?" + q.Encode()
	shown := e.Body
	if e.BaseBody != cur {
		shown = ""
		conflict := false
		for _, p := range songdiff.Plan(e.BaseBody, cur, e.Body) {
			conflict = conflict || p.Conflict
		}
		if !conflict {
			shown = songdiff.MergeAll(e.BaseBody, cur, e.Body)
		}
	}
	sg.Shown = shown
	sg.Changes = songdiff.Describe(e.BaseBody, e.Body)
	if shown != "" {
		var idx []string
		for _, i := range songdiff.ChangedSections(cur, shown) {
			idx = append(idx, strconv.Itoa(i))
		}
		sg.Sections = strings.Join(idx, ",")
	}
	if rv, err := s.store.CatalogueReviewers(r.Context()); err == nil {
		for _, id := range rv {
			if id != personID {
				sg.Reviewers++
			}
		}
	}
	return sg
}

// addSongSuggestions: a reviewer opening a catalogue song sees the
// suggestions waiting for it, in place, with Accept and Reject.
func (s *Server) addSongSuggestions(r *http.Request, person *store.Person, songID int64, data map[string]any) {
	if person == nil || data["Catalogue"] != true {
		return
	}
	ok, err := s.store.MayReviewCatalogue(r.Context(), person.ID)
	if err != nil || !ok {
		return
	}
	edits, err := s.store.PendingCatalogueEditsOfSong(r.Context(), songID)
	if err != nil || len(edits) == 0 {
		return
	}
	lang := s.langOfRequest(r)
	var views []catalogueEditView
	for _, e := range edits {
		if e.AuthorID == person.ID && !person.IsAdmin {
			continue
		}
		views = append(views, viewCatalogueEdit(lang, e, person.ID))
	}
	if len(views) > 0 {
		data["SongSuggestions"] = views
		data["IsStaff"] = person.IsAdmin
	}
}

// suggestedMessage is what a mixed save says: the chart live, the rest
// suggested.
func suggestedMessage(lang string, held []string) string {
	var what []string
	for _, r := range held {
		switch r {
		case "key":
			what = append(what, i18n.T(lang, "the key’s name"))
		case "lead":
			what = append(what, i18n.T(lang, "the lead-in"))
		case "gp":
			what = append(what, i18n.T(lang, "the Guitar Pro sync"))
		case "title":
			what = append(what, i18n.T(lang, "the title or the artist"))
		case "reference":
			what = append(what, i18n.T(lang, "removing the recording"))
		}
	}
	return i18n.T(lang, "Saved for everyone who plays it; %s suggested: you see it now, a reviewer accepts it.", strings.Join(what, ", "))
}

// chartReachView is who plays the chart, for the editor's note: the
// counts, and the viewer's places with their keys ("A", or the song's own
// key).
type chartReachView struct {
	Songbooks, Setlists, Arrangements int
	Mine                              []chartPlaceView
}

type chartPlaceView struct {
	Name, Owner, Key string
	Link             string
}

func (s *Server) chartReachView(r *http.Request, songID, personID int64, songKey string) *chartReachView {
	reach, err := s.store.ChartReachOf(r.Context(), songID, personID)
	if err != nil {
		return nil
	}
	v := &chartReachView{Songbooks: reach.Songbooks, Setlists: reach.Setlists, Arrangements: reach.Arrangements}
	for _, p := range reach.Mine {
		key := chordsym.KeyIn(songKey, p.Semitones)
		if key == "" && p.Semitones != 0 {
			key = strings.Replace(fmt.Sprintf("%+d", p.Semitones), "-", "−", 1)
		}
		v.Mine = append(v.Mine, chartPlaceView{Name: p.Name, Owner: p.Owner, Key: key, Link: fmt.Sprintf("/songbooks/%d", p.SongbookID)})
	}
	return v
}
