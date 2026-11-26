package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"app/internal/chordsym"
	"app/internal/i18n"
	"app/internal/reftitle"
	"app/internal/song"
	"app/internal/songdiff"
	"app/internal/songimport"
	"app/internal/store"
)

// The catalogue: open edits (docs/ownership-model.md, "The catalogue:
// open edits"; store/catalogue_edits.go). Anyone signed in edits The Big
// Songbook's songs (SongRights.Contribute); the song's chart saves
// (PUT /api/songs/{id}, /raw, /meta, PUT /api/variants/{id}) go through
// catalogueSave, which weighs each edit against the song's reference
// recording:
//
//   - a new reference (another recording) makes a new catalogue song for
//     it, the edited chart its chart; the song edited stays as it was
//     (201, {"newSongId"});
//   - a chart transposed as a whole is refused (422): a catalogue song is
//     in its recording's key; keys are set per songbook, student, band;
//   - its staff's edits are applied at once;
//   - anyone else's is applied at once when it keeps or improves how the
//     chart follows the recording (songimport.RecordingFit, before and
//     after) and touches none of the lead-in, the Guitar Pro sync, the
//     key, the title or artist, or adding/removing the recording; else
//     it waits for review (202, {"pending"}): the song stays as it was.
//     Without an analysis of the recording to judge by, a change of the
//     sections or their bars waits too. A newcomer's first edits wait
//     whatever they change (store.CatalogueNewcomerEdits).
//
// The edit's one-line summary comes in the X-Edit-Summary header (URL-
// encoded; optional), and goes on its revision.

// editSummaryHeader carries the edit's one-line summary.
const editSummaryHeader = "X-Edit-Summary"

// The choice made for an edit that changes the reference, the title or
// the artist (X-Edit-Kind): a fix of this song, or another recording; for
// another recording, its reference label (X-Reference-Label) and, when
// the edit kept the reference, the new recording's link
// (X-New-Reference). URL-encoded.
const (
	// structureConfirmedHeader: the author saw the band hint (a change of
	// the sections by a member of a band that plays the song) and saves
	// for everyone all the same.
	structureConfirmedHeader = "X-Structure-Confirmed"
	editKindHeader           = "X-Edit-Kind"
	editKindFix              = "fix"
	editKindRecording        = "recording"
	referenceLabelHeader     = "X-Reference-Label"
	newReferenceHeader       = "X-New-Reference"
)

// headerText is a URL-encoded header's text on one line, at most max
// characters.
func headerText(r *http.Request, name string, max int) string {
	raw := r.Header.Get(name)
	if s, err := url.QueryUnescape(raw); err == nil {
		raw = s
	}
	raw = strings.Join(strings.Fields(raw), " ")
	if utf8.RuneCountInString(raw) > max {
		raw = string([]rune(raw)[:max])
	}
	return raw
}

// setHeaderLine sets a chart's "key: value" header line in place, or
// adds it after the first header line.
func setHeaderLine(body, key, value string) string {
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		if k, _, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == key && !strings.HasPrefix(l, " ") {
			lines[i] = key + ": " + value
			return strings.Join(lines, "\n")
		}
		if strings.HasPrefix(strings.TrimSpace(l), "[") {
			break
		}
	}
	at := 0
	if len(lines) > 0 && strings.Contains(lines[0], ":") && !strings.HasPrefix(strings.TrimSpace(lines[0]), "[") {
		at = 1
	}
	lines = append(lines[:at], append([]string{key + ": " + value}, lines[at:]...)...)
	return strings.Join(lines, "\n")
}

// refTitleNow is the title of reference link ref, fetched now (a few
// seconds at most; "" without a fetcher or when none is found): a
// reference label's suggestion.
func (s *Server) refTitleNow(ctx context.Context, ref string) string {
	if RefTitles == nil || strings.TrimSpace(ref) == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, reftitle.Timeout+time.Second)
	defer cancel()
	title, _ := RefTitles.Title(ctx, ref)
	return strings.TrimSpace(title)
}

// maxEditSummary is the longest summary kept, in characters.
const maxEditSummary = 200

// editSummary is the request's one-line summary, trimmed to one line of
// at most maxEditSummary characters.
func editSummary(r *http.Request) string {
	raw := r.Header.Get(editSummaryHeader)
	if raw == "" {
		return ""
	}
	if s, err := url.QueryUnescape(raw); err == nil {
		raw = s
	}
	raw = strings.Join(strings.Fields(raw), " ")
	if utf8.RuneCountInString(raw) > maxEditSummary {
		raw = string([]rune(raw)[:maxEditSummary])
	}
	return raw
}

// chartAnchors is what of a chart its reference recording anchors.
type chartAnchors struct {
	Sections string // each section's label, repeat, bars, passes, time
	Lead     string // the "lead:" line
	GP       string // "gp_start:" and the sections' "gp[...]:" lines
	Key      string
}

func anchorsOf(body string) chartAnchors {
	sg := song.Parse(body)
	var a chartAnchors
	var b strings.Builder
	b.WriteString("time=" + strings.TrimSpace(sg.Get("time")) + ";")
	var gp strings.Builder
	gp.WriteString("start=" + strings.TrimSpace(sg.Get("gp_start")) + ";")
	for _, sec := range sg.Sections {
		times := sec.Times
		if times < 1 {
			times = 1
		}
		fmt.Fprintf(&b, "%s|%v|%d|%d|%s;", strings.ToLower(strings.TrimSpace(sec.Label)), sec.Repeat, len(sec.Measures), times, sec.Time)
		for _, g := range sec.GP {
			fmt.Fprintf(&gp, "%s:%s=%s;", sec.Label, g.Track, strings.TrimSpace(g.Spec))
		}
	}
	a.Sections = b.String()
	a.Lead = sg.LeadLine()
	a.GP = gp.String()
	a.Key = strings.TrimSpace(sg.Get("key"))
	return a
}

// fitRank orders verdicts: following the recording is best.
func fitRank(f string) int {
	switch f {
	case songimport.FitFollows:
		return 2
	case songimport.FitUnknown:
		return 1
	}
	return 0
}

// fitWorse reports whether after follows the recording less well than
// before: a lower verdict, or more ways to differ.
func fitWorse(before, after songimport.Fit) bool {
	rb, ra := fitRank(before.Fit), fitRank(after.Fit)
	if ra != rb {
		return ra < rb
	}
	return after.Fit == songimport.FitDiffers && len(after.Reasons) > len(before.Reasons)
}

// catalogueJudgement is why an edit waits for review (none: applied at
// once), and the recording's verdicts before and after it (nil without
// an analysis).
type catalogueJudgement struct {
	Reasons             []string
	FitBefore, FitAfter *songimport.Fit
}

// judgeCatalogueEdit weighs the edit from before to after: fit reads a
// chart's verdict against the recording (nil: no analysis); newcomer,
// the author's edits all wait.
func judgeCatalogueEdit(before, after string, fit func(string) *songimport.Fit, newcomer bool) catalogueJudgement {
	var j catalogueJudgement
	a, b := anchorsOf(before), anchorsOf(after)
	if fit != nil {
		j.FitBefore, j.FitAfter = fit(before), fit(after)
	}
	// The recording judges the structure, when it has a verdict to give.
	judged := j.FitBefore != nil && j.FitAfter != nil &&
		!(j.FitBefore.Fit == songimport.FitUnknown && j.FitAfter.Fit == songimport.FitUnknown)
	add := func(why string) { j.Reasons = append(j.Reasons, why) }
	if judged && fitWorse(*j.FitBefore, *j.FitAfter) {
		add("fit")
	}
	if a.Sections != b.Sections && !judged {
		add("structure")
	}
	if a.Lead != b.Lead {
		add("lead")
	}
	if a.GP != b.GP {
		add("gp")
	}
	if a.Key != b.Key {
		add("key")
	}
	if newcomer {
		add("newcomer")
	}
	return j
}

// chartTransposed reports whether after is before transposed as a whole:
// the same chords, in the same order, each moved by the same interval.
func chartTransposed(before, after string) bool {
	cb, ca := song.UsedChords(song.Parse(before)), song.UsedChords(song.Parse(after))
	if len(cb) < 2 || len(cb) != len(ca) {
		return false
	}
	shift := -1
	for i := range cb {
		pb, pa := chordsym.PitchClass(chordsym.Split(cb[i]).Root), chordsym.PitchClass(chordsym.Split(ca[i]).Root)
		if pb < 0 || pa < 0 {
			return false
		}
		d := (pa - pb + 12) % 12
		if shift >= 0 && d != shift {
			return false
		}
		shift = d
	}
	return shift > 0
}

// sameReference reports whether two reference links name the same
// recording.
func sameReference(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == b {
		return true
	}
	if va, vb := store.YouTubeID(a), store.YouTubeID(b); va != "" && va == vb {
		return true
	}
	return false
}

// suggestionMessage says what a held edit is, once saved: a suggestion
// its author sees at once, or a whole edit waiting.
func suggestionMessage(lang string, held []string) string {
	if suggestionPart(held) {
		return i18n.T(lang, "Suggested: you see it now on the song; everyone else does once a reviewer accepts it.")
	}
	return i18n.T(lang, "Your edit waits for review: the song stays as it is for everyone until it’s approved; you see it now on the song.")
}

// heldReasons are the reasons an edit waits for a reviewer: what
// identifies the song or lines it up with its recording (the key's name,
// the lead-in, the Guitar Pro sync, removing the recording), a
// passer-by's first edit, the daily limit. The others ("fit",
// "structure", "newcomer") flag an applied edit for the reviewers.
var heldReasons = map[string]bool{"lead": true, "gp": true, "key": true, "reference": true, "title": true, "passerby": true, "rate": true}

// splitCatalogueReasons splits an edit's reasons into why it waits and
// why an applied one is worth a look.
func splitCatalogueReasons(reasons []string) (held, flags []string) {
	for _, r := range reasons {
		if heldReasons[r] {
			held = append(held, r)
		} else {
			flags = append(flags, r)
		}
	}
	return held, flags
}

// catalogueReasonText says why an edit waits.
func catalogueReasonText(lang, why string) string {
	switch why {
	case "fit":
		return i18n.T(lang, "it makes the chart follow its recording less well")
	case "structure":
		return i18n.T(lang, "it changes the sections or their bars, and the recording has no analysis to check them against")
	case "lead":
		return i18n.T(lang, "it changes the lead-in")
	case "gp":
		return i18n.T(lang, "it changes how the chart lines up with the Guitar Pro file")
	case "key":
		// The header alone: a label fix (song.KeyShift), no chord or
		// recording moves, but every songbook names its key from it.
		return i18n.T(lang, "it renames the key: no chord moves, but every songbook names its key for this song from it")
	case "title":
		return i18n.T(lang, "it changes the title or the artist")
	case "reference":
		return i18n.T(lang, "it removes the reference recording")
	case "newcomer":
		return i18n.T(lang, "it is one of its author’s first edits")
	case "passerby":
		return i18n.T(lang, "it is its author’s first edit, of a song none of their songbooks holds")
	case "rate":
		return i18n.T(lang, "its author made many edits today: the next ones wait until they’re trusted")
	}
	return why
}

func catalogueReasonsText(lang string, reasons []string) []string {
	out := make([]string, 0, len(reasons))
	for _, r := range reasons {
		out = append(out, catalogueReasonText(lang, r))
	}
	return out
}

// recordingFitFunc is the "follows the recording" check of songID's
// charts against its reference recording's analysis; nil when it has
// none.
func (s *Server) recordingFitFunc(ctx context.Context, songID int64) func(string) *songimport.Fit {
	in, err := s.store.FitInputOf(ctx, songID)
	if err != nil || in == nil {
		return nil
	}
	return func(body string) *songimport.Fit {
		f := songimport.RecordingFit(in.Analysis, body, SyncSections(song.Parse(body)))
		return &f
	}
}

// catalogueSave saves song id's chart (title, artist, reference, body, at
// expectVersion) when it is the catalogue's, as the package comment
// says, and answers; false (nothing written) when it isn't the
// catalogue's: the caller saves as for any song.
func (s *Server) catalogueSave(w http.ResponseWriter, r *http.Request, id int64, title, artist, ref, body, expectVersion string) bool {
	person := personFromContext(r)
	if person == nil {
		return false
	}
	ctx := r.Context()
	rights, err := s.store.SongRightsFor(ctx, person.ID, id)
	if err != nil || !rights.Catalogue {
		return false
	}
	lang := s.langOfRequest(r)
	if !rights.Contribute {
		httpError(w, http.StatusForbidden, i18n.T(lang, "You can’t edit The Big Songbook’s songs."))
		return true
	}
	cur, err := s.store.SongChartOf(ctx, id)
	if err != nil {
		writeSaveErr(w, err)
		return true
	}
	curBody, curVersion := cur.Body, strconv.Itoa(cur.Version)
	if expectVersion != "" && expectVersion != curVersion {
		writeSaveErr(w, store.ErrConflict)
		return true
	}
	summary := editSummary(r)
	// Nothing changed: nothing to keep.
	if body == curBody && title == cur.Title && artist == cur.Artist && strings.TrimSpace(ref) == strings.TrimSpace(cur.ReferenceURL) {
		writeJSON(w, http.StatusOK, map[string]any{"version": curVersion})
		return true
	}
	// The key is the recording's.
	if chartTransposed(curBody, body) {
		httpError(w, http.StatusUnprocessableEntity, i18n.T(lang, "The Big Songbook’s songs stay in their recording’s key: transpose it in a songbook, for a student or for a band instead."))
		return true
	}
	// A new reference, or a new title or artist: a fix of this song (the
	// same recording: a dead link, a typo), or another recording of it
	// (album and live, original and cover), which is another catalogue
	// song. The editor asks (428) and says it again (X-Edit-Kind).
	oldRef, newRef := strings.TrimSpace(cur.ReferenceURL), strings.TrimSpace(ref)
	refChanged := oldRef != "" && newRef != "" && !sameReference(oldRef, newRef)
	identityChanged := strings.TrimSpace(title) != strings.TrimSpace(cur.Title) || strings.TrimSpace(artist) != strings.TrimSpace(cur.Artist)
	kind := r.Header.Get(editKindHeader)
	if (refChanged || identityChanged) && kind != editKindFix && kind != editKindRecording {
		// The new link's title, a label's suggestion, comes after
		// (GET /api/reference-title): the question doesn't wait for it.
		out := map[string]any{"choose": true, "referenceChanged": refChanged, "identityChanged": identityChanged,
			"title": strings.TrimSpace(title), "artist": strings.TrimSpace(artist)}
		if refChanged {
			out["newReference"] = newRef
		}
		writeJSON(w, http.StatusPreconditionRequired, out)
		return true
	}
	if kind == editKindRecording && (refChanged || identityChanged) {
		rec := newRef
		if !refChanged {
			rec = headerText(r, newReferenceHeader, 500)
			if rec == "" || sameReference(rec, oldRef) {
				httpError(w, http.StatusUnprocessableEntity, i18n.T(lang, "Another recording needs its own reference: give the link to its video."))
				return true
			}
			body = setHeaderLine(body, "reference", rec)
		}
		label := headerText(r, referenceLabelHeader, reftitle.MaxLabel)
		if label == "" {
			label = reftitle.Label(s.refTitleNow(ctx, rec), title, artist)
		}
		newID, err := s.store.CreateRecordingVersion(ctx, id, person.ID, title, artist, rec, label, body, summary)
		if err != nil {
			writeSaveErr(w, err)
			return true
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"version": curVersion, "newSongId": newID, "url": fmt.Sprintf("/song/%d", newID),
			"message": i18n.T(lang, "Another recording is another song of The Big Songbook, with your chart: OK opens it. The song you edited stays as it was."),
		})
		return true
	}
	baseVersion := cur.Version
	// A new reference (a fix: the link put right, or one added): the
	// song's empty reference label from its title, once saved.
	newReference := newRef != "" && !sameReference(oldRef, newRef)
	// A band member changing the sections or their bars: the song
	// changes for everyone, the band included; to play it differently,
	// the band's arrangement. Said once before saving (428), never a
	// gate (X-Structure-Confirmed).
	if r.Header.Get(structureConfirmedHeader) == "" && anchorsOf(curBody).Sections != anchorsOf(body).Sections {
		if holds, err := s.store.BandsHolding(ctx, person.ID, id); err == nil && len(holds) > 0 {
			names := make([]string, 0, len(holds))
			for _, h := range holds {
				names = append(names, h.BandName)
			}
			writeJSON(w, http.StatusPreconditionRequired, map[string]any{
				"structureHint": true, "bands": names,
				"message": i18n.T(lang, "This changes the song’s sections for everyone who plays it, %s included. To play it differently, make the band’s arrangement instead.", strings.Join(names, ", ")),
			})
			return true
		}
	}
	// Its staff: at once.
	if rights.Edit {
		version, revID, err := s.store.SaveSongEditRevision(ctx, store.SongSave{
			SongID: id, Title: title, Artist: artist, ReferenceURL: ref, Body: body,
			ExpectVersion: curVersion, ActorID: person.ID, Summary: summary,
		})
		if err != nil {
			writeSaveErr(w, err)
			return true
		}
		out := map[string]any{"version": version, "revisionId": revID, "saved": i18n.T(lang, "Saved for everyone who plays it.")}
		if newReference {
			s.newReferenceNames(ctx, id, newRef, out)
		}
		writeJSON(w, http.StatusOK, out)
		return true
	}
	standing, err := s.store.CatalogueStandingOf(ctx, person.ID)
	if err != nil {
		writeSaveErr(w, err)
		return true
	}
	j := judgeCatalogueEdit(curBody, body, s.recordingFitFunc(ctx, id), standing.Newcomer())
	// The recording taken away: the song follows none any more.
	if oldRef != "" && newRef == "" {
		j.Reasons = append(j.Reasons, "reference")
	}
	// A fix of the song's name (its title, its artist): what identifies
	// it, and finds its duplicates — a suggestion.
	if identityChanged {
		j.Reasons = append(j.Reasons, "title")
	}
	// Patrol instead of holding (docs/catalogue-edit-review.md §4.5): what
	// identifies the song or lines it up with its recording waits for a
	// reviewer; the rest is applied, and what's worth a look flagged.
	if !standing.Trusted {
		// A passer-by's very first edit, of a song none of their places
		// holds.
		if standing.Accepted == 0 {
			if holds, err := s.store.PersonHoldsSong(ctx, person.ID, id); err == nil && !holds {
				j.Reasons = append(j.Reasons, "passerby")
			}
		}
		// The daily limit, until trusted.
		if n, err := s.store.CatalogueAppliedSince(ctx, person.ID, time.Now().Add(-24*time.Hour)); err == nil && n >= store.CatalogueDailyLimit {
			j.Reasons = append(j.Reasons, "rate")
		}
	}
	held, flags := splitCatalogueReasons(j.Reasons)
	in := store.CatalogueEditInput{
		SongID: id, AuthorID: person.ID, BaseVersion: baseVersion, BaseBody: curBody,
		Title: title, Artist: artist, ReferenceURL: ref, Body: body, Summary: summary, Reasons: j.Reasons,
	}
	if j.FitBefore != nil {
		in.FitBefore, in.FitBeforeDetail = j.FitBefore.Fit, j.FitBefore.Reasons
	}
	if j.FitAfter != nil {
		in.FitAfter, in.FitAfterDetail = j.FitAfter.Fit, j.FitAfter.Reasons
	}
	if len(held) == 0 {
		in.Reasons = flags
		in.Patrolled = standing.Trusted && len(flags) == 0
		version, revID, err := s.store.ApplyCatalogueEditRevision(ctx, in)
		if err != nil {
			writeSaveErr(w, err)
			return true
		}
		out := map[string]any{"version": version, "revisionId": revID, "saved": i18n.T(lang, "Saved for everyone who plays it.")}
		if newReference {
			s.newReferenceNames(ctx, id, newRef, out)
		}
		writeJSON(w, http.StatusOK, out)
		return true
	}
	// A mixed save: the chart's changes go live at once, only the
	// protected part is suggested (catalogue_suggestions.go).
	if suggestionPart(held) {
		if live := withoutProtected(curBody, body, held); live != curBody {
			liveIn := in
			liveIn.Body, liveIn.Title, liveIn.Artist = live, cur.Title, cur.Artist
			if newRef == "" && oldRef != "" {
				liveIn.ReferenceURL = cur.ReferenceURL
			}
			liveIn.Reasons = flags
			liveIn.Patrolled = standing.Trusted && len(flags) == 0
			version, revID, err := s.store.ApplyCatalogueEditRevision(ctx, liveIn)
			if err != nil {
				writeSaveErr(w, err)
				return true
			}
			v, _ := strconv.Atoi(version)
			sugg := in
			sugg.BaseVersion, sugg.BaseBody, sugg.Reasons = v, live, held
			sugg.FitBefore, sugg.FitBeforeDetail = in.FitAfter, in.FitAfterDetail
			editID, err := s.store.HoldCatalogueEdit(ctx, sugg)
			if err != nil {
				writeSaveErr(w, err)
				return true
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"version": version, "revisionId": revID, "suggested": true, "editId": editID,
				"url": store.CatalogueEditLink(editID), "saved": suggestedMessage(lang, held),
			})
			return true
		}
	}
	in.Reasons = held
	editID, err := s.store.HoldCatalogueEdit(ctx, in)
	if err != nil {
		writeSaveErr(w, err)
		return true
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"version": curVersion, "pending": true, "editId": editID, "url": store.CatalogueEditLink(editID),
		"reasons": catalogueReasonsText(lang, held),
		"suggested": suggestionPart(held),
		"message": suggestionMessage(lang, held),
	})
	return true
}

// newReferenceNames: a catalogue song's new reference saved, its title
// (fetched now and kept, as handlePutMeta does) and the reference label
// filled from it while the song has none (refLabelNow), in the save's
// answer — the Reference view and the header show them at once.
func (s *Server) newReferenceNames(ctx context.Context, id int64, ref string, out map[string]any) {
	out["referenceTitle"] = s.fetchRefTitle(ctx, id, ref)
	if label := s.refLabelNow(ctx, id); label != "" {
		out["referenceLabel"] = label
	}
}

// ---- Pages -----------------------------------------------------------

// catalogueEditView is one edit as the review queue and its page show it.
type catalogueEditView struct {
	store.CatalogueEdit
	Reasons   []string // said
	FitBefore fitView
	FitAfter  fitView
	Changes   []songdiff.Change
	Lines     []songdiff.Line
	Stale     bool // the song changed since its author's base
	Mine      bool
	// ToCheck: applied, and no reviewer has seen it yet (patrol).
	ToCheck bool
}

func fitViewOf(lang, fit string, detail json.RawMessage) fitView {
	switch fit {
	case songimport.FitFollows:
		return fitView{Fit: fit, Label: i18n.T(lang, "follows the recording")}
	case songimport.FitDiffers:
		var reasons []songimport.FitReason
		_ = json.Unmarshal(detail, &reasons)
		return fitView{Fit: fit, Label: i18n.T(lang, "differs from the recording"), Why: fitWhy(lang, reasons)}
	case songimport.FitUnknown:
		return fitView{Fit: fit, Label: i18n.T(lang, "no verdict from the recording")}
	}
	return fitView{}
}

func viewCatalogueEdit(lang string, e store.CatalogueEdit, me int64) catalogueEditView {
	v := catalogueEditView{CatalogueEdit: e, Mine: e.AuthorID == me}
	v.CatalogueEdit.Reasons = nil
	v.Reasons = catalogueReasonsText(lang, e.Reasons)
	v.FitBefore = fitViewOf(lang, e.FitBefore, e.FitBeforeDetail)
	v.FitAfter = fitViewOf(lang, e.FitAfter, e.FitAfterDetail)
	v.Changes = songdiff.Describe(e.BaseBody, e.Body)
	v.Lines = songdiff.Lines(e.BaseBody, e.Body)
	v.Stale = e.Status == store.EditPending && e.SongVersion != e.BaseVersion
	v.ToCheck = e.Status == store.EditApplied && e.PatrolledAt == nil && e.RevertedAt == nil
	return v
}

// handleCatalogueReviewPage is the review queue (staff and trusted
// editors): the edits waiting, the open reports; for the staff, who is
// kept from editing.
func (s *Server) handleCatalogueReviewPage(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	ctx := r.Context()
	standing, err := s.store.CatalogueStandingOf(ctx, person.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !standing.Reviewer() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	lang := s.langOf(w)
	edits, err := s.store.PendingCatalogueEdits(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	toCheck, err := s.store.CatalogueEditsToCheck(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var checks []catalogueEditView
	for _, e := range toCheck {
		checks = append(checks, viewCatalogueEdit(lang, e, person.ID))
	}
	reports, err := s.store.OpenCatalogueReports(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var views []catalogueEditView
	for _, e := range edits {
		views = append(views, viewCatalogueEdit(lang, e, person.ID))
	}
	data := map[string]any{
		"Me": person, "Edits": views, "Reports": reports, "IsStaff": standing.Staff, "ToCheck": checks,
		"NewcomerEdits": store.CatalogueNewcomerEdits, "TrustedEdits": store.CatalogueTrustedEdits, "DailyLimit": store.CatalogueDailyLimit,
	}
	if standing.Staff {
		data["Banned"], _ = s.store.CatalogueBannedPeople(ctx)
	}
	_ = s.store.SeenCatalogueQueue(ctx, person.ID)
	s.render(w, "catalogue_review", data)
}

// handleCatalogueEditPage is one edit: its author's text and what it
// changes, for its author and the reviewers.
func (s *Server) handleCatalogueEditPage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	person := personFromContext(r)
	ctx := r.Context()
	e, err := s.store.CatalogueEditByID(ctx, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	standing, err := s.store.CatalogueStandingOf(ctx, person.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if e.AuthorID != person.ID && !standing.Reviewer() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	v := viewCatalogueEdit(s.langOf(w), *e, person.ID)
	s.render(w, "catalogue_edit", map[string]any{
		"Me": person, "Edit": v, "IsReviewer": standing.Reviewer() && (!v.Mine || standing.Staff), "IsStaff": standing.Staff,
		"ToCheck": v.ToCheck,
	})
}

// ---- API ---------------------------------------------------------------

func (s *Server) catalogueDecisionErr(w http.ResponseWriter, r *http.Request, err error) {
	lang := s.langOfRequest(r)
	switch {
	case errors.Is(err, store.ErrNotReviewer):
		httpError(w, http.StatusForbidden, i18n.T(lang, "Only The Big Songbook’s staff and trusted editors review its edits, never their own."))
	case errors.Is(err, store.ErrEditDecided):
		httpError(w, http.StatusConflict, i18n.T(lang, "This edit was decided already."))
	case errors.Is(err, store.ErrConflict):
		httpError(w, http.StatusConflict, i18n.T(lang, "The song changed since, in the same place as this edit: reject it with a note, so its author edits the song as it is now."))
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrForbidden):
		httpError(w, http.StatusForbidden, err.Error())
	default:
		httpError(w, http.StatusInternalServerError, err.Error())
	}
}

// handleDecideCatalogueEdit is POST /api/big-songbook/edits/{id}/approve
// and /reject, {"note"}; a reject by the staff may keep its author from
// editing ("ban": true).
func (s *Server) handleDecideCatalogueEdit(approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "id")
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid id")
			return
		}
		var req struct {
			Note string `json:"note"`
			Ban  bool   `json:"ban"`
		}
		if r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
				return
			}
		}
		person := personFromContext(r)
		ctx := r.Context()
		if approve {
			err = s.store.ApproveCatalogueEdit(ctx, id, person.ID, req.Note)
			// It brought a new reference: the song's empty reference
			// label from its title.
			if e, gerr := s.store.CatalogueEditByID(ctx, id); err == nil && gerr == nil &&
				strings.TrimSpace(e.ReferenceURL) != "" && !sameReference(chartHead(e.BaseBody, "reference"), e.ReferenceURL) {
				s.refLabelNow(ctx, e.SongID)
			}
		} else {
			err = s.store.RejectCatalogueEdit(ctx, id, person.ID, req.Note)
		}
		if err != nil {
			s.catalogueDecisionErr(w, r, err)
			return
		}
		if !approve && req.Ban {
			if e, err := s.store.CatalogueEditByID(ctx, id); err == nil && e.AuthorID != 0 {
				if err := s.store.SetCatalogueBanned(ctx, person.ID, e.AuthorID, true); err != nil {
					s.catalogueDecisionErr(w, r, err)
					return
				}
			}
		}
		status := store.EditRejected
		if approve {
			status = store.EditApproved
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": status})
	}
}

// handlePatrolCatalogueEdit is POST /api/big-songbook/edits/{id}/patrol:
// a reviewer saw an applied edit ("Looks good").
func (s *Server) handlePatrolCatalogueEdit(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.PatrolCatalogueEdit(r.Context(), id, personFromContext(r).ID); err != nil {
		s.catalogueDecisionErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"patrolled": true})
}

// handleRevertCatalogueEdit is POST /api/big-songbook/edits/{id}/revert,
// {"note"}: a reviewer undoes an applied edit, its author told why; only
// its changes are taken out when later edits came (409 when one changed
// the same place: History then).
func (s *Server) handleRevertCatalogueEdit(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}
	err = s.store.RevertCatalogueEdit(r.Context(), id, personFromContext(r).ID, req.Note)
	if errors.Is(err, store.ErrConflict) {
		httpError(w, http.StatusConflict, i18n.T(s.langOfRequest(r), "A later edit changed the same place: revert it from the song’s History."))
		return
	}
	if err != nil {
		s.catalogueDecisionErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"reverted": true})
}

// handleReportCatalogueSong is POST /api/big-songbook/songs/{id}/report,
// {"reason"}: anyone who sees a catalogue song reports it.
func (s *Server) handleReportCatalogueSong(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	lang := s.langOfRequest(r)
	if strings.TrimSpace(req.Reason) == "" {
		httpError(w, http.StatusBadRequest, i18n.T(lang, "Say what’s wrong with the song."))
		return
	}
	person := personFromContext(r)
	rights, err := s.store.SongRightsFor(r.Context(), person.ID, id)
	if err != nil || !rights.See || !rights.Catalogue {
		httpError(w, http.StatusForbidden, i18n.T(lang, "Only The Big Songbook’s songs are reported here."))
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if utf8.RuneCountInString(reason) > 2000 {
		reason = string([]rune(reason)[:2000])
	}
	if _, err := s.store.ReportCatalogueSong(r.Context(), id, person.ID, reason); err != nil {
		s.catalogueDecisionErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reported": true, "message": i18n.T(lang, "Thank you: The Big Songbook’s reviewers will look at it.")})
}

// handleHandleCatalogueReport is POST /api/big-songbook/reports/{id}/handled.
func (s *Server) handleHandleCatalogueReport(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.HandleCatalogueReport(r.Context(), id, personFromContext(r).ID); err != nil {
		s.catalogueDecisionErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"handled": true})
}

// handleCatalogueBan is POST /api/big-songbook/people/{id}/ban,
// {"banned"}: the staff keep someone from editing the catalogue, or let
// them again.
func (s *Server) handleCatalogueBan(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Banned bool `json:"banned"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetCatalogueBanned(r.Context(), personFromContext(r).ID, id, req.Banned); err != nil {
		s.catalogueDecisionErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"banned": req.Banned})
}

// ---- The song page and its history --------------------------------------

// addCatalogueEditing adds to the song page's data what editing The Big
// Songbook's song takes: Catalogue (its key locked, its Report),
// CatalogueGated (the viewer's edits are weighed: not its staff),
// CataloguePending (the viewer's edit of it waiting for review).
func (s *Server) addCatalogueEditing(r *http.Request, person *store.Person, songID int64, data map[string]any) {
	if person == nil || songID == 0 {
		return
	}
	rights, err := s.store.SongRightsFor(r.Context(), person.ID, songID)
	if err != nil || !rights.Catalogue {
		return
	}
	data["Catalogue"] = true
	data["CanReport"] = rights.See
	data["CatalogueGated"] = rights.Contribute && !rights.Edit
	// What a save of theirs does, for the editor's note: its staff's and
	// a trusted editor's are live at once; anyone else's too, checked by
	// a reviewer afterwards, unless the key's name, the lead-in or the
	// Guitar Pro sync is at stake; a passer-by's very first edit (of a
	// song none of their places holds) waits for a reviewer.
	switch {
	case rights.Edit:
		data["CatalogueSaveMode"] = "staff"
	case rights.Contribute:
		data["CatalogueSaveMode"] = "open"
		if standing, err := s.store.CatalogueStandingOf(r.Context(), person.ID); err == nil {
			if standing.Trusted {
				data["CatalogueSaveMode"] = "staff"
			} else if standing.Accepted == 0 {
				if holds, err := s.store.PersonHoldsSong(r.Context(), person.ID, songID); err == nil && !holds {
					data["CatalogueSaveMode"] = "passerby"
				}
			}
		}
	}
	if e, err := s.store.PendingCatalogueEditOf(r.Context(), person.ID, songID); err == nil && e != nil {
		data["CataloguePending"] = e
	}
}

// catalogueRevertable is, for the history page, which of revisions
// personID may revert: on a catalogue song, its staff and trusted
// editors any; an author their own edit while it is the latest.
func (s *Server) catalogueRevertable(ctx context.Context, personID int64, revisions []store.Revision) map[int64]bool {
	out := map[int64]bool{}
	for _, rv := range revisions {
		if ok, err := s.store.MayRevertCatalogue(ctx, personID, rv.ID); err == nil && ok {
			out[rv.ID] = true
		}
	}
	return out
}

// handlePutReferenceLabel is PUT /api/variants/{id}/reference-label,
// {"label"}: a catalogue song's reference label (the album, "Live at …",
// "Original", "Cover"…), shown after its artist. A fix like the title's:
// anyone who edits the catalogue, at once, a revision kept.
func (s *Server) handlePutReferenceLabel(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	person := personFromContext(r)
	lang := s.langOfRequest(r)
	rights, err := s.store.SongRightsFor(r.Context(), person.ID, id)
	if err != nil {
		writeSaveErr(w, err)
		return
	}
	if !rights.Catalogue || !rights.Contribute {
		httpError(w, http.StatusForbidden, i18n.T(lang, "You can’t edit The Big Songbook’s songs."))
		return
	}
	label := strings.Join(strings.Fields(req.Label), " ")
	if utf8.RuneCountInString(label) > 80 {
		label = string([]rune(label)[:80])
	}
	summary := editSummary(r)
	if summary == "" {
		summary = i18n.T(lang, "Reference label: %s", label)
	}
	if err := s.store.SetReferenceLabel(r.Context(), id, person.ID, label, summary); err != nil {
		writeSaveErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"label": label})
}

// handleReferenceTitle is GET /api/reference-title?url=[&title=&artist=]:
// a reference link's title (a video's, a page's), "" when none is found,
// and the reference label made from it for the song's title and artist
// (reftitle.Label) — the label's suggestion when an edit makes another
// recording.
func (s *Server) handleReferenceTitle(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ref := strings.TrimSpace(q.Get("url"))
	if !strings.HasPrefix(ref, "https://") && !strings.HasPrefix(ref, "http://") {
		writeJSON(w, http.StatusOK, map[string]string{"title": "", "label": ""})
		return
	}
	title := s.refTitleNow(r.Context(), ref)
	writeJSON(w, http.StatusOK, map[string]string{"title": title, "label": reftitle.Label(title, q.Get("title"), q.Get("artist"))})
}
