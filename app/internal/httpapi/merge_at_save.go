package httpapi

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"app/internal/i18n"
	"app/internal/songdiff"
)

// Merge at save (the edit model, docs/catalogue-edit-review.md §4.6). A
// save carries the version the editor opened; when the song moved on
// since (someone else saved meanwhile), the save is merged with what
// changed — the version opened (store.SongAtVersion, migration 0142) the
// base, songdiff's three-way merge by the song's parts (each header
// field, each section, their order) — instead of refused:
//
//   - changes in different parts merge silently, and the answer says who
//     saved meanwhile (X-Merged-With: their names, URL-encoded): the
//     editor reloads the song with both;
//   - a part both changed, differently, is asked once (409, "conflicts":
//     each part's name, theirs and yours); the editor sends the choice
//     back (X-Merge-Resolve: "key=mine;key=theirs", URL-encoded);
//   - a version that kept no revision to merge against (before migration
//     0142): 409 as before, the editor's "copy your changes, reload".
//
// Every song's chart saves go through it (PUT /api/songs/{id}, /raw,
// /api/variants/{id}): the catalogue's as a band's or one's own.

const (
	mergedWithHeader   = "X-Merged-With"
	mergeResolveHeader = "X-Merge-Resolve"
)

// mergeConflict is a part both changed, as the editor asks about it.
type mergeConflict struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Theirs string `json:"theirs"`
	Mine   string `json:"mine"`
}

// mergeAtSave merges body, edited from version expect of song id, with
// what changed since: the body to save and the version to save it at.
// false when it answered already (409: conflicts to resolve, or nothing
// to merge against). A save at the current version, or with no version,
// is body as it is.
func (s *Server) mergeAtSave(w http.ResponseWriter, r *http.Request, id int64, body, expect string) (string, string, bool) {
	if expect == "" {
		return body, expect, true
	}
	ctx := r.Context()
	cur, err := s.store.SongChartOf(ctx, id)
	if err != nil {
		return body, expect, true // the save says what's wrong
	}
	if strconv.Itoa(cur.Version) == expect {
		return body, expect, true
	}
	v, err := strconv.Atoi(expect)
	if err != nil {
		return body, expect, true
	}
	base, ok, err := s.store.SongAtVersion(ctx, id, v)
	if err != nil || !ok {
		return body, expect, true // refused as before (409)
	}
	resolve := parseMergeResolve(r.Header.Get(mergeResolveHeader))
	var conflicts []mergeConflict
	for _, p := range songdiff.Plan(base.Body, cur.Body, body) {
		if p.Conflict {
			conflicts = append(conflicts, mergeConflict{Key: p.Key, Name: partName(s.langOfRequest(r), p)})
		}
	}
	for _, k := range songdiff.MetaConflicts(base.Body, cur.Body, body) {
		conflicts = append(conflicts, mergeConflict{Key: k, Name: k[2:]})
	}
	var open []mergeConflict
	for _, c := range conflicts {
		if _, ok := resolve[c.Key]; !ok {
			c.Theirs, c.Mine = songdiff.PartText(cur.Body, c.Key), songdiff.PartText(body, c.Key)
			open = append(open, c)
		}
	}
	person := personFromContext(r)
	var me int64
	if person != nil {
		me = person.ID
	}
	who, _ := s.store.SongEditorsSince(ctx, id, v, me)
	if len(open) > 0 {
		lang := s.langOfRequest(r)
		names := strings.Join(who, ", ")
		if names == "" {
			names = i18n.T(lang, "Someone")
		}
		writeJSON(w, http.StatusConflict, map[string]any{
			"conflicts": open, "with": who,
			"error": i18n.T(lang, "%s saved while you were editing, in the same place: choose which to keep.", names),
		})
		return "", "", false
	}
	merged := songdiff.MergeResolved(base.Body, cur.Body, body, func(k string) bool { return resolve[k] == "mine" })
	if len(who) > 0 {
		w.Header().Set(mergedWithHeader, url.QueryEscape(strings.Join(who, ", ")))
	} else {
		w.Header().Set(mergedWithHeader, "1")
	}
	return merged, strconv.Itoa(cur.Version), true
}

// parseMergeResolve reads "key=mine;key=theirs".
func parseMergeResolve(h string) map[string]string {
	out := map[string]string{}
	if s, err := url.QueryUnescape(h); err == nil {
		h = s
	}
	for _, kv := range strings.Split(h, ";") {
		k, v, ok := strings.Cut(kv, "=")
		if ok && (v == "mine" || v == "theirs") {
			out[strings.TrimSpace(k)] = v
		}
	}
	return out
}

// partName names a part for the editor: "Verse 2", "tempo", "the order
// of the sections".
func partName(lang string, p songdiff.Part) string {
	if p.Kind == "order" {
		return i18n.T(lang, "the order of the sections")
	}
	return p.Name
}
