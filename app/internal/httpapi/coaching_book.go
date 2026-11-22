package httpapi

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"app/internal/chordsym"
	"app/internal/i18n"
	"app/internal/store"
)

// The coach's Songbooks tab (coaching_book.html, docs/coaching.md "The
// songbooks"): one songbook, full width, like a band's: the repertoire's
// songs as rows, each with the students and groups it's given to, in
// their keys (a chip each: its key changed, or taken out, in place), a
// "+ Assign" for the others; a search and a filter by student; the
// students in a compact strip above, the requests waiting above them.
// Assigning is adding the song to the student's (or group's) songbook,
// in a key: the song page's own coach-keys endpoint.

// bookAssignment is a song given to a student or a group, in their key.
type bookAssignment struct {
	Kind       string // "student" or "group"
	PersonID   int64
	GroupID    int64
	Name       string // the chip's: the student's first name (the full one when two share it), the group's
	FullName   string
	SongbookID int64
	Semitones  int
	KeyText    string // "G −2", "G", "−2", "" (original, no key known)
}

// Who is the filter's value for the assignment: "s:56", "g:3".
func (a bookAssignment) Who() string {
	if a.Kind == "group" {
		return fmt.Sprintf("g:%d", a.GroupID)
	}
	return fmt.Sprintf("s:%d", a.PersonID)
}

// bookRow is a song of the coach's songbook.
type bookRow struct {
	store.StudentSong
	// InRepertoire false: a song only students or groups have (from
	// another of the community's songbooks).
	InRepertoire bool
	Assigned     []bookAssignment
}

// WhoList is the row's assignments for the filter: "s:56 g:3".
func (r bookRow) WhoList() string {
	var out []string
	for _, a := range r.Assigned {
		out = append(out, a.Who())
	}
	return strings.Join(out, " ")
}

// bookFilter is one choice of the filter by student.
type bookFilter struct {
	Value, Label string
}

// chipKey is a key as a chip says it: the key's name and how far it moved,
// "G −2"; the name alone in the song's own key; "" when there's neither.
func chipKey(key string, n int) string {
	name := chordsym.KeyIn(key, n)
	off := ""
	switch {
	case n > 0:
		off = fmt.Sprintf("+%d", n)
	case n < 0:
		off = fmt.Sprintf("−%d", -n)
	}
	return strings.TrimSpace(name + " " + off)
}

// coachBookData is the songbook's rows, its filter and its people, for
// the coach's view (data, after coachBooksData).
func (s *Server) coachBookData(r *http.Request, community *store.Community, repID int64, data map[string]any) error {
	ctx := r.Context()
	lang := s.langOfRequest(r)
	rows := map[int64]*bookRow{}
	var order []int64
	add := func(sg store.StudentSong, inRep bool) *bookRow {
		row, ok := rows[sg.VariantID]
		if !ok {
			sg.Semitones = 0
			row = &bookRow{StudentSong: sg}
			rows[sg.VariantID] = row
			order = append(order, sg.VariantID)
		}
		row.InRepertoire = row.InRepertoire || inRep
		return row
	}
	if repID != 0 {
		songs, err := s.store.ListStudentSongs(ctx, repID)
		if err != nil {
			return err
		}
		for _, sg := range songs {
			add(sg, true)
		}
	}
	// Who has which song: each student's songbook, each group's.
	students, err := s.communityStudents(r, community.ID)
	if err != nil {
		return err
	}
	firsts := map[string]int{}
	for _, m := range students {
		firsts[firstWord(m.DisplayName)]++
	}
	var filters []bookFilter
	assign := func(sbID int64, a bookAssignment) error {
		songs, err := s.store.ListStudentSongs(ctx, sbID)
		if err != nil {
			return err
		}
		for _, sg := range songs {
			row := add(sg, false)
			a.Semitones = sg.Semitones
			row.Assigned = append(row.Assigned, a)
		}
		return nil
	}
	for _, m := range students {
		name := firstWord(m.DisplayName)
		if firsts[name] > 1 {
			name = m.DisplayName
		}
		a := bookAssignment{Kind: "student", PersonID: m.PersonID, Name: name, FullName: m.DisplayName}
		filters = append(filters, bookFilter{a.Who(), i18n.T(lang, "%s’s songs", m.DisplayName)})
		sbID, err := s.store.FindStudentSongbook(ctx, community.ID, m.PersonID)
		if err != nil {
			return err
		}
		if sbID == 0 {
			continue
		}
		a.SongbookID = sbID
		if err := assign(sbID, a); err != nil {
			return err
		}
	}
	groups, err := s.store.ListCoachingGroups(ctx, community.ID, 0)
	if err != nil {
		return err
	}
	for _, g := range groups {
		a := bookAssignment{Kind: "group", GroupID: g.ID, Name: g.Name, FullName: g.Name, SongbookID: g.SongbookID}
		filters = append(filters, bookFilter{a.Who(), i18n.T(lang, "%s’s songs", g.Name)})
		if err := assign(g.SongbookID, a); err != nil {
			return err
		}
	}
	out := make([]bookRow, 0, len(order))
	ids := make([]int64, 0, len(order))
	for _, id := range order {
		row := rows[id]
		out = append(out, *row)
		ids = append(ids, id)
	}
	// Each song's own key (its key: header, or its chords'), then each
	// chip's key from it.
	keyed := make([]store.StudentSong, len(out))
	for i := range out {
		keyed[i] = out[i].StudentSong
	}
	keyed = s.withKeys(ctx, keyed)
	for i := range out {
		out[i].StudentSong = keyed[i]
		for j := range out[i].Assigned {
			a := &out[i].Assigned[j]
			a.KeyText = chipKey(out[i].Key, a.Semitones)
		}
		sort.SliceStable(out[i].Assigned, func(x, y int) bool {
			ax, ay := out[i].Assigned[x], out[i].Assigned[y]
			if ax.Kind != ay.Kind {
				return ax.Kind == "student"
			}
			return store.FoldName(ax.Name) < store.FoldName(ay.Name)
		})
	}
	// The repertoire first, by title; then the songs only students have.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].InRepertoire != out[j].InRepertoire {
			return out[i].InRepertoire
		}
		return store.FoldName(out[i].Title) < store.FoldName(out[j].Title)
	})
	data["BookRows"] = out
	data["BookFilters"] = filters
	data["Badges"] = s.songBadgesFor(ctx, ids)
	return nil
}
