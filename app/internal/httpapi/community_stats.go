package httpapi

import (
	"errors"
	"net/http"
	"sort"
	"strconv"

	"app/internal/store"
)

// A community's stats, for its staff: who viewed which song and
// downloaded which file, per member, per file, per song, and for a course
// per lesson.

// statsSongbook is one songbook's part of the stats: its lessons (a
// course's), its songs and its files.
type statsSongbook struct {
	store.Songbook
	Lessons []store.CourseLessonStats
	Songs   []store.SongStats
	Files   []store.FileStats
	Zips    []store.ZipStats
}

// handleCommunityStatsPage is GET /communities/{id}/admin/stats.
func (s *Server) handleCommunityStatsPage(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	community, err := s.store.GetCommunity(r.Context(), communityID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	stats, err := s.store.GetCommunityStats(r.Context(), communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	songbooks, err := s.store.ListSongbooks(r.Context(), "community", communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var parts []statsSongbook
	at := map[int64]int{}
	for _, sb := range songbooks {
		at[sb.ID] = len(parts)
		parts = append(parts, statsSongbook{Songbook: sb})
	}
	for _, l := range stats.Songs {
		if i, ok := at[l.SongbookID]; ok {
			parts[i].Songs = append(parts[i].Songs, l)
		}
	}
	for _, l := range stats.Lessons {
		if i, ok := at[l.SongbookID]; ok {
			parts[i].Lessons = append(parts[i].Lessons, l)
		}
	}
	for _, f := range stats.Files {
		if i, ok := at[f.SongbookID]; ok {
			parts[i].Files = append(parts[i].Files, f)
		}
	}
	for _, z := range stats.Zips {
		if i, ok := at[z.SongbookID]; ok {
			parts[i].Zips = append(parts[i].Zips, z)
		}
	}
	for _, p := range parts {
		// The most viewed songs first; lessons stay in their course's order.
		sort.SliceStable(p.Songs, func(i, j int) bool { return p.Songs[i].Views > p.Songs[j].Views })
	}
	s.render(w, "community_stats", map[string]any{
		"Me": personFromContext(r), "Community": community, "Stats": stats, "Songbooks": parts,
		"HasCourses": len(stats.Lessons) > 0,
	})
}

// handleCommunityMemberStatsPage is GET /communities/{id}/admin/stats/members/{personID}:
// what one member viewed and downloaded.
func (s *Server) handleCommunityMemberStatsPage(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	personID, err := pathID(r, "personID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	community, err := s.store.GetCommunity(r.Context(), communityID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	stats, err := s.store.GetCommunityStats(r.Context(), communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var member *store.MemberStats
	for i := range stats.Members {
		if stats.Members[i].PersonID == personID {
			member = &stats.Members[i]
		}
	}
	if member == nil {
		http.NotFound(w, r)
		return
	}
	views, downloads, err := s.store.GetMemberActivity(r.Context(), communityID, personID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	done, err := s.store.GetMemberLessonsDone(r.Context(), communityID, personID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	songbooks, err := s.store.ListSongbooks(r.Context(), "community", communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	names := map[int64]string{}
	for _, sb := range songbooks {
		names[sb.ID] = sb.Name
	}
	s.render(w, "community_stats_member", map[string]any{
		"Me": personFromContext(r), "Community": community, "Member": member,
		"Views": views, "Downloads": downloads, "LessonsDone": done, "SongbookNames": names,
	})
}
