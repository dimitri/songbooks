package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"app/internal/chordsym"
	"app/internal/i18n"
	"app/internal/store"
)

// A coaching community's Songbooks section (docs/coaching.md, "The
// songbooks"), seen from both sides:
//
//   - the student: first their songbook with the coach (its songs, each in
//     their key, the next lesson, the PDF in their keys), then the coach's
//     repertoire, each song either in their songbook already or one to
//     ask for ("Ask for it": the coach is told, the row says "Asked");
//   - the coach: the students' requests waiting, a card per student and
//     per group (their songbook, their voice, where they stand), ordered
//     by the next lesson, and the repertoire, each song to add to a
//     student's or a group's songbook in their key.

// pickerText is a key picker's text: "A (+2)", "G (original)", "+2",
// "original".
func pickerText(lang, key string, n int) string {
	if n != 0 {
		return keyPill(key, n)
	}
	if name := chordsym.KeyIn(key, 0); name != "" {
		return name + " (" + i18n.T(lang, "original") + ")"
	}
	return i18n.T(lang, "original")
}

// keyPill is a song's key for a student, as its pill says it: "C (−3)",
// "−3" for a song with no key known, "" in the original key (the
// template says "original").
func keyPill(key string, semitones int) string {
	if semitones == 0 {
		return ""
	}
	off := fmt.Sprintf("+%d", semitones)
	if semitones < 0 {
		off = fmt.Sprintf("−%d", -semitones)
	}
	if name := chordsym.KeyIn(key, semitones); name != "" {
		return name + " (" + off + ")"
	}
	return off
}

// bookSong is a song of a student's songbook, with its key's pill.
type bookSong struct {
	store.StudentSong
	Pill string // "" in the original key
	// Voice: the song's melody at their key, how it sits in their voice
	// (row_voice.go).
	Voice rowVoice
}

func bookSongs(songs []store.StudentSong) []bookSong {
	out := make([]bookSong, len(songs))
	for i, sg := range songs {
		out[i] = bookSong{StudentSong: sg, Pill: keyPill(sg.Key, sg.Semitones)}
	}
	return out
}

// askRow is a song of the repertoire as a student sees it: in
// their songbook (InBook), asked for (Asked), or neither.
type askRow struct {
	VariantID int64
	Title     string
	Artist    string
	InBook    bool
	Asked     bool
}

// requestView is a request waiting on the coach, with the song's key (the
// key picker's start). A proposal (coach_proposals.go) says more: the key
// the student would like, where the picker starts (that key), which song
// enters the repertoire and how.
type requestView struct {
	store.SongRequest
	Key string
	// Start: the picker's start, semitones from Key; WishedKey: the key
	// the student would like, named ("A", "+2"), "" when none said.
	Start     int
	WishedKey string
	// StripVariant: the song whose melody the voice strip draws.
	StripVariant int64
	// StudentFirst: the student's first name; PickerText: the picker's
	// key at Start ("A (+2)", "G (original)").
	StudentFirst string
	PickerText   string
	// Accept says what accepting does: "link" (a public song, linked),
	// "copy" (the student's private song: the community's arrangement made
	// from it), "own" (the community's already, or its arrangement of it).
	Accept string
}

// coachBookCard is the coach's card of one student's or one group's
// songbook.
type coachBookCard struct {
	Kind       string // "student" or "group"
	PersonID   int64
	GroupID    int64
	Name       string
	SongbookID int64
	SongCount  int
	Href       string // where its songbook is kept (the student's or the group's page)
	// A student's voice, lowest to highest noted ("do3–la4"), "" when
	// none is.
	Voice string
	// Comfort: the comfortable zone ("sol2–la3"); Label: the coach's
	// working label in words ("leaning baritone"), "" when not classified.
	Comfort string
	Label   string
	// A group's: a year's class or a one-off, and how many students.
	IsYear  bool
	Members int
	// Where they stand: the last song worked on, the takes since the last
	// lesson, the next lesson (a group's session).
	LastSong   *store.WorkedOn
	Takes      int
	Next       *time.Time
	NextOnline bool
	Requests   int // their requests waiting
}

// handleCoachingSongbooksPage is /communities/{id}/songbooks for a
// coaching community: the student's view or the coach's.
func (s *Server) handleCoachingSongbooksPage(w http.ResponseWriter, r *http.Request, community *store.Community) {
	ctx := r.Context()
	v, err := s.coachingViewerOf(r, community.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	coach := v.Coach
	data := s.coachingPageData(r, community, coach, "songbooks")
	data["ReadOnly"] = v.ReadOnly
	coachName := s.store.CoachName(ctx, community.ID)
	data["CoachName"] = coachName
	data["CoachFirstName"] = firstWord(coachName)
	repID, err := s.store.RepertoireSongbook(ctx, community.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data["RepertoireID"] = repID
	var repertoire []store.SongbookEntry
	if repID != 0 {
		sb, err := s.store.GetSongbook(ctx, repID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data["RepertoireName"] = sb.Name
		if repertoire, err = s.store.ListSongbookEntries(ctx, repID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		sort.SliceStable(repertoire, func(i, j int) bool { return store.FoldName(repertoire[i].Title) < store.FoldName(repertoire[j].Title) })
	}
	// The coach's: one songbook, full width (coaching_book.go).
	page := "coaching_songbooks"
	if coach {
		page = "coaching_book"
		if err = s.coachBooksData(r, community, repertoire, data); err == nil {
			err = s.coachBookData(r, community, repID, data)
		}
	} else {
		err = s.studentBooksData(r, community, v.PersonID, repertoire, data)
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, page, data)
}

// studentBookRows is how many of their songs the student's card lists
// before "All n songs".
const studentBookRows = 8

// studentBooksData is the student's view: their songbook, the
// repertoire, their groups' songbooks and the community's others they
// may open.
func (s *Server) studentBooksData(r *http.Request, community *store.Community, personID int64, repertoire []store.SongbookEntry, data map[string]any) error {
	ctx := r.Context()
	sbID, err := s.store.FindStudentSongbook(ctx, community.ID, personID)
	if err != nil {
		return err
	}
	var songs []store.StudentSong
	if sbID != 0 {
		if songs, err = s.store.ListStudentSongs(ctx, sbID); err != nil {
			return err
		}
		sb, err := s.store.GetSongbook(ctx, sbID)
		if err != nil {
			return err
		}
		data["BookName"] = sb.Name
	}
	all := bookSongs(s.withKeys(ctx, songs))
	if len(all) > 0 {
		ids := make([]int64, len(all))
		for i := range all {
			ids[i] = all[i].VariantID
		}
		ranges := s.melodyRanges(ctx, ids)
		zone, _ := s.store.StudentVoiceZone(ctx, personID, community.ID)
		lang := s.langOfRequest(r)
		for i := range all {
			m, known := ranges[all[i].VariantID]
			all[i].Voice = rowVoiceOf(lang, m, known, all[i].Semitones, zone)
		}
	}
	data["BookID"] = sbID
	data["BookCount"] = len(all)
	shown := all
	if len(shown) > studentBookRows {
		shown = shown[:studentBookRows]
	}
	data["BookSongs"] = shown
	moved := 0
	for _, sg := range all {
		if sg.Semitones != 0 {
			moved++
		}
	}
	data["BookMoved"] = moved
	var lessons []store.CoachingLesson
	if personID != 0 { // 0: a member with nothing yet (an admin's preview)
		if lessons, err = s.store.ListCoachingLessons(ctx, community.ID, time.Now(), personID); err != nil {
			return err
		}
	}
	if len(lessons) > 0 {
		ev := lessons[0].Event
		data["NextLesson"] = &ev
	}
	in := map[int64]bool{}
	for _, sg := range songs {
		in[sg.VariantID] = true
	}
	asked, err := s.store.StudentSongRequests(ctx, community.ID, personID)
	if err != nil {
		return err
	}
	rows := make([]askRow, 0, len(repertoire))
	for _, e := range repertoire {
		rows = append(rows, askRow{VariantID: e.VariantID, Title: e.Title, Artist: e.Artist,
			InBook: in[e.VariantID], Asked: !in[e.VariantID] && asked[e.VariantID] == store.SongRequestPending})
	}
	data["Repertoire"] = rows
	if err := s.studentProposalsData(r, community, personID, sbID, data); err != nil {
		return err
	}
	// Their groups' songbooks, and the community's other songbooks they
	// may open (never another student's, nor a group they aren't in).
	var groups []store.CoachingGroup
	if personID != 0 {
		if groups, err = s.store.ListCoachingGroups(ctx, community.ID, personID); err != nil {
			return err
		}
	}
	var groupCards []coachBookCard
	for _, g := range groups {
		gs, err := s.store.ListStudentSongs(ctx, g.SongbookID)
		if err != nil {
			return err
		}
		groupCards = append(groupCards, coachBookCard{Kind: "group", GroupID: g.ID, Name: g.Name, SongbookID: g.SongbookID,
			SongCount: len(gs), IsYear: g.IsYear(), Href: fmt.Sprintf("/songbooks/%d", g.SongbookID)})
	}
	data["GroupBooks"] = groupCards
	others, err := s.otherCommunityBooks(r, community.ID, personID, false)
	if err != nil {
		return err
	}
	data["OtherBooks"] = others
	return nil
}

// otherCommunityBooks are a coaching community's songbooks besides the
// repertoire and the students' and groups' (the coach's reference
// material, a concert's book): all of them for the coach, those the
// student may open for a student.
func (s *Server) otherCommunityBooks(r *http.Request, communityID, personID int64, coach bool) ([]communitySongbookRow, error) {
	ctx := r.Context()
	sbs, err := s.store.ListSongbooks(ctx, "community", communityID)
	if err != nil {
		return nil, err
	}
	var out []communitySongbookRow
	for _, sb := range sbs {
		if sb.IsDefault || sb.IsStudent() || sb.Kind == store.SongbookKindGroup {
			continue
		}
		if !coach {
			if ok, err := s.store.CanAccessSongbook(ctx, personID, sb.ID); err != nil || !ok {
				continue
			}
		}
		entries, err := s.store.ListSongbookEntries(ctx, sb.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, communitySongbookRow{Songbook: sb, SongCount: len(entries), LessonCount: s.lessonCount(ctx, sb)})
	}
	return out, nil
}

// coachBooksData is the coach's view: the requests waiting, a card per
// student and per group, the repertoire, the community's other songbooks.
func (s *Server) coachBooksData(r *http.Request, community *store.Community, repertoire []store.SongbookEntry, data map[string]any) error {
	ctx := r.Context()
	person := personFromContext(r)
	requests, err := s.store.PendingSongRequests(ctx, community.ID)
	if err != nil {
		return err
	}
	views := make([]requestView, len(requests))
	for i, q := range requests {
		views[i] = requestView{SongRequest: q, Key: s.songKeyOf(ctx, q.VariantID), StripVariant: q.VariantID}
		if q.IsProposal() {
			s.proposalView(r, &views[i])
		}
		views[i].StudentFirst = firstWord(q.StudentName)
		views[i].PickerText = pickerText(s.langOfRequest(r), views[i].Key, views[i].Start)
	}
	data["Requests"] = views
	data["OpenRequest"] = int64(0)
	if id, err := strconv.ParseInt(r.URL.Query().Get("request"), 10, 64); err == nil {
		data["OpenRequest"] = id
	}
	opts, err := s.store.CommunityPlayerOptions(ctx, community.ID)
	if err != nil {
		return err
	}
	data["KeyPicker"] = opts.CoachStudentKeys
	cards, err := s.coachBookCards(r, community, requests)
	if err != nil {
		return err
	}
	data["Cards"] = cards
	data["Repertoire"] = repertoire
	others, err := s.otherCommunityBooks(r, community.ID, person.ID, true)
	if err != nil {
		return err
	}
	data["OtherBooks"] = others
	canEdit, err := s.store.HasCommunityPrivilege(ctx, community.ID, person.ID, store.PrivEditRepertoire)
	if err != nil {
		return err
	}
	data["CanEdit"] = canEdit || person.IsAdmin
	return nil
}

// coachBookCards is a card per student and per group, the soonest lesson
// first, then those with none planned, by name.
func (s *Server) coachBookCards(r *http.Request, community *store.Community, requests []store.SongRequest) ([]coachBookCard, error) {
	ctx := r.Context()
	lang := s.langOfRequest(r)
	now := time.Now()
	members, err := s.store.ListCommunityMembers(ctx, community.ID, false)
	if err != nil {
		return nil, err
	}
	lessons, err := s.store.ListCoachingLessons(ctx, community.ID, time.Time{}, 0)
	if err != nil {
		return nil, err
	}
	worked, err := s.store.LastSongsWorkedOn(ctx, community.ID)
	if err != nil {
		return nil, err
	}
	profiles, err := s.store.CommunityVoiceProfiles(ctx, community.ID)
	if err != nil {
		return nil, err
	}
	asked := map[int64]int{}
	for _, q := range requests {
		asked[q.PersonID]++
	}
	// Each student's and group's last lesson so far and next one.
	type when struct {
		last, next *store.Event
	}
	people, groups := map[int64]*when{}, map[int64]*when{}
	at := func(m map[int64]*when, id int64) *when {
		if m[id] == nil {
			m[id] = &when{}
		}
		return m[id]
	}
	for _, l := range lessons { // soonest first
		ev := l.Event
		past := ev.IsPast(now)
		mark := func(w *when) {
			switch {
			case past:
				w.last = &ev
			case w.next == nil:
				w.next = &ev
			}
		}
		for _, st := range l.Students {
			mark(at(people, st.PersonID))
		}
		if ev.CoachingGroupID != nil {
			mark(at(groups, *ev.CoachingGroupID))
		}
	}
	var cards []coachBookCard
	for _, m := range members {
		if m.IsStaff() {
			continue
		}
		c := coachBookCard{Kind: "student", PersonID: m.PersonID, Name: m.DisplayName, Requests: asked[m.PersonID],
			Href: fmt.Sprintf("/communities/%d/students/%d#songs", community.ID, m.PersonID)}
		if c.SongbookID, err = s.store.FindStudentSongbook(ctx, community.ID, m.PersonID); err != nil {
			return nil, err
		}
		if c.SongbookID != 0 {
			songs, err := s.store.ListStudentSongs(ctx, c.SongbookID)
			if err != nil {
				return nil, err
			}
			c.SongCount = len(songs)
		}
		rs, err := s.store.StudentRanges(ctx, community.ID, m.PersonID)
		if err != nil {
			return nil, err
		}
		c.Voice = voiceSpan(rs, lang)
		p := profiles[m.PersonID]
		c.Comfort = spanName(p.TessituraLow, p.TessituraHigh, lang)
		c.Label = voiceLabelText(lang, p.VoiceType, p.Leaning)
		if w, ok := worked[m.PersonID]; ok {
			c.LastSong = &w
		}
		since := now.AddDate(0, 0, -7)
		w := people[m.PersonID]
		if w != nil && w.last != nil {
			since = w.last.StartsAt
		}
		if c.Takes, err = s.store.TakesSince(ctx, community.ID, m.PersonID, since); err != nil {
			return nil, err
		}
		if w != nil && w.next != nil {
			c.Next, c.NextOnline = &w.next.StartsAt, w.next.LessonMode == "online"
		}
		cards = append(cards, c)
	}
	gs, err := s.store.ListCoachingGroups(ctx, community.ID, 0)
	if err != nil {
		return nil, err
	}
	for _, g := range gs {
		c := coachBookCard{Kind: "group", GroupID: g.ID, Name: g.Name, SongbookID: g.SongbookID, IsYear: g.IsYear(), Members: len(g.Members),
			Href: fmt.Sprintf("/communities/%d/groups/%d", community.ID, g.ID)}
		songs, err := s.store.ListStudentSongs(ctx, g.SongbookID)
		if err != nil {
			return nil, err
		}
		c.SongCount = len(songs)
		if w := groups[g.ID]; w != nil && w.next != nil {
			c.Next, c.NextOnline = &w.next.StartsAt, w.next.LessonMode == "online"
		}
		cards = append(cards, c)
	}
	sortBookCards(cards)
	return cards, nil
}

// sortBookCards orders the coach's cards by the next lesson, the soonest
// first; those with none after, students before groups, by name.
func sortBookCards(cards []coachBookCard) {
	sort.SliceStable(cards, func(i, j int) bool {
		a, b := cards[i], cards[j]
		switch {
		case a.Next != nil && b.Next != nil:
			if !a.Next.Equal(*b.Next) {
				return a.Next.Before(*b.Next)
			}
		case a.Next != nil:
			return true
		case b.Next != nil:
			return false
		}
		if a.Kind != b.Kind {
			return a.Kind == "student"
		}
		return store.FoldName(a.Name) < store.FoldName(b.Name)
	})
}

// voiceSpan is a voice from its lowest noted note to its highest, named
// in lang ("do3–la4"); "" when nothing is noted.
func voiceSpan(rs []store.VoiceRange, lang string) string {
	lo, hi := 0, 0
	for _, r := range rs {
		if r.Low != nil && (lo == 0 || *r.Low < lo) {
			lo = *r.Low
		}
		if r.High != nil && *r.High > hi {
			hi = *r.High
		}
	}
	if lo == 0 || hi == 0 {
		return ""
	}
	return i18n.NoteName(lang, lo) + "–" + i18n.NoteName(lang, hi)
}

// Requests -----------------------------------------------------------------

// songRequestLink is where a request's notification takes the coach: the
// Songbooks section, the request open.
func songRequestLink(communityID, requestID int64) string {
	return fmt.Sprintf("/communities/%d/songbooks?request=%d#request-%d", communityID, requestID, requestID)
}

// recipientLang is the language a notification for personID is written
// in: their own choice, else the community's, else English.
func (s *Server) recipientLang(r *http.Request, personID, communityID int64) string {
	ctx := r.Context()
	if l, _ := s.store.PersonLocale(ctx, personID); l != "" {
		return l
	}
	if l, _ := s.store.CommunityLocale(ctx, communityID); l != "" {
		return l
	}
	return "en"
}

// handlePostSongRequest is POST /api/communities/{id}/song-requests
// {variantId}: a student asks for a song of the repertoire, for their own
// songbook (never anyone else's: the student is who asks). The coach (each
// of the community's staff) is told, once while the request waits.
func (s *Server) handlePostSongRequest(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	person := personFromContext(r)
	if coach {
		httpError(w, http.StatusForbidden, "the coach adds songs; students ask for them")
		return
	}
	if _, ok := s.currentStudent(r, community.ID, person.ID); !ok {
		httpError(w, http.StatusForbidden, "only a student of this community can ask")
		return
	}
	var req struct {
		VariantID int64 `json:"variantId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.VariantID == 0 {
		httpError(w, http.StatusBadRequest, "which song?")
		return
	}
	ctx := r.Context()
	if ok, err := s.store.RepertoireHas(ctx, community.ID, req.VariantID); err != nil || !ok {
		httpError(w, http.StatusNotFound, "not a song of the repertoire")
		return
	}
	sbID, err := s.store.FindStudentSongbook(ctx, community.ID, person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sbID != 0 {
		songs, err := s.store.ListStudentSongs(ctx, sbID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, sg := range songs {
			if sg.VariantID == req.VariantID {
				httpError(w, http.StatusConflict, "already in your songbook")
				return
			}
		}
	}
	id, created, err := s.store.RequestSong(ctx, community.ID, person.ID, req.VariantID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if created {
		q, err := s.store.GetSongRequest(ctx, id)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := s.notifyCoaches(r, community, q); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": store.SongRequestPending})
}

// notifyCoaches tells each of the community's staff a student asked for a
// song, in the staff member's language, the request one tap away.
func (s *Server) notifyCoaches(r *http.Request, community *store.Community, q *store.SongRequest) error {
	members, err := s.store.ListCommunityMembers(r.Context(), community.ID, false)
	if err != nil {
		return err
	}
	for _, m := range members {
		if ok, err := s.store.HasAnyCommunityManagePrivilege(r.Context(), community.ID, m.PersonID); err != nil || !ok {
			continue
		}
		lang := s.recipientLang(r, m.PersonID, community.ID)
		body := i18n.T(lang, "%s asks for “%s” in their songbook", q.StudentName, q.Title)
		if _, err := s.store.CreateNotification(r.Context(), m.PersonID, "song_request", body, songRequestLink(community.ID, q.ID)); err != nil {
			return err
		}
	}
	return nil
}

// coachSongRequest checks a call on a request: the coach, a request of
// this community still waiting.
func (s *Server) coachSongRequest(w http.ResponseWriter, r *http.Request) (*store.Community, *store.SongRequest, bool) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return nil, nil, false
	}
	if !coach {
		httpError(w, http.StatusForbidden, "only the coach can do that")
		return nil, nil, false
	}
	id, err := pathID(r, "requestID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid request")
		return nil, nil, false
	}
	q, err := s.store.GetSongRequest(r.Context(), id)
	if err != nil || q.CommunityID != community.ID {
		httpError(w, http.StatusNotFound, "no such request")
		return nil, nil, false
	}
	if q.Status != store.SongRequestPending {
		httpError(w, http.StatusConflict, "already answered")
		return nil, nil, false
	}
	return community, q, true
}

// handlePostSongRequestAdd is POST
// /api/communities/{id}/song-requests/{requestID}/add {semitones}: the
// coach puts the song in the student's songbook (made the first time), in
// that key; the student is told.
func (s *Server) handlePostSongRequestAdd(w http.ResponseWriter, r *http.Request) {
	community, q, ok := s.coachSongRequest(w, r)
	if !ok {
		return
	}
	var req struct {
		Semitones int    `json:"semitones"`
		Reply     string `json:"reply"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}
	if req.Semitones < -12 || req.Semitones > 12 {
		httpError(w, http.StatusBadRequest, "key out of range")
		return
	}
	// A song the student proposed: accepted (coach_proposals.go).
	if q.IsProposal() {
		reply, ok := s.replyOf(w, r, req.Reply)
		if ok {
			s.acceptProposal(w, r, community, q, req.Semitones, reply)
		}
		return
	}
	ctx := r.Context()
	me := personFromContext(r)
	student, ok := s.currentStudent(r, community.ID, q.PersonID)
	if !ok {
		httpError(w, http.StatusConflict, "no longer a student here")
		return
	}
	studentLang := s.recipientLang(r, q.PersonID, community.ID)
	sbID, err := s.store.StudentSongbook(ctx, community.ID, q.PersonID, i18n.T(studentLang, "%s’s songs", student.DisplayName), me.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !s.writeLinkErr(w, r, s.store.LinkCheckFor(ctx, me.ID, q.VariantID, store.CommunityOwner(community.ID))) {
		return
	}
	if err := s.store.AddToSongbook(ctx, sbID, q.VariantID, me.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.SetEntrySemitones(ctx, sbID, q.VariantID, req.Semitones); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.DecideSongRequest(ctx, q.ID, store.SongRequestAdded, me.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.store.MarkNotificationsReadByLink(ctx, "song_request", songRequestLink(community.ID, q.ID))
	body := i18n.T(studentLang, "%s added “%s” to your songbook", me.DisplayName, q.Title)
	if _, err := s.store.CreateNotification(ctx, q.PersonID, "song_request_added", body, fmt.Sprintf("/song/%d?songbook=%d", q.VariantID, sbID)); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	key := s.songKeyOf(ctx, q.VariantID)
	writeJSON(w, http.StatusOK, map[string]any{"songbookId": sbID, "semitones": req.Semitones, "keyName": chordsym.KeyIn(key, req.Semitones)})
}

// handlePostSongRequestDecline is POST
// /api/communities/{id}/song-requests/{requestID}/decline {reply}: not
// now. The student may ask again. A proposal declined: the student is
// told, with the coach's note (reply, optional).
func (s *Server) handlePostSongRequestDecline(w http.ResponseWriter, r *http.Request) {
	community, q, ok := s.coachSongRequest(w, r)
	if !ok {
		return
	}
	var req struct {
		Reply string `json:"reply"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}
	reply, ok := s.replyOf(w, r, req.Reply)
	if !ok {
		return
	}
	if err := s.store.AnswerSongRequest(r.Context(), q.ID, store.SongRequestDeclined, personFromContext(r).ID, reply); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusConflict, "already answered")
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.store.MarkNotificationsReadByLink(r.Context(), "song_request", songRequestLink(community.ID, q.ID))
	if q.IsProposal() {
		if err := s.declineProposal(r, community, q, reply); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// replyOf is the coach's note to the student, trimmed: false (answered)
// when it's too long.
func (s *Server) replyOf(w http.ResponseWriter, r *http.Request, reply string) (string, bool) {
	reply = strings.TrimSpace(reply)
	if len([]rune(reply)) > store.MaxProposalNote {
		httpError(w, http.StatusBadRequest, i18n.T(s.langOfRequest(r), "A note is %d characters at most.", store.MaxProposalNote))
		return "", false
	}
	return reply, true
}
