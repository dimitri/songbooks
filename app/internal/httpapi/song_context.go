package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"app/internal/store"
)

// A song page's context, the place it was opened from (?setlist=, with
// &gig=1; ?songbook=; ?event=; ?band=; ?community=), names that place and
// shows what's in it: its name in the line above the header, the song's
// position there, the previous and next songs' titles, the key the song
// has there, a coaching community's keys and lesson. The right to the
// song says nothing of the right to that place: a public song opened with
// another person's songbook, another band's set list or an event of a
// community the viewer isn't in would show them. So each context counts
// only when the viewer may open its page; otherwise it is dropped, as if
// the address didn't have it, before anything reads it.

// openableSongContext is q without the contexts p may not open:
//   - setlist (and gig): its page's rule, store.CanViewSetlistByID (its
//     owner, its band's or community's members);
//   - songbook: mayOpenSongbook's (store.CanAccessSongbook — a public one
//     for everyone signed in, its owner's members, with the grant for a
//     gated one, a student's for the student and the coach's staff — or
//     a site admin);
//   - event: its program's page's (a coaching community's event: that
//     community's members, or a site admin);
//   - band, community: their repertoire's page's (current members, or a
//     site admin).
//
// A context that isn't a number is dropped too: no page has it. The
// other parameters stay as they are.
func (s *Server) openableSongContext(ctx context.Context, p *store.Person, q url.Values) url.Values {
	out := url.Values{}
	for k, v := range q {
		out[k] = v
	}
	id := func(k string) (int64, bool) {
		n, err := strconv.ParseInt(q.Get(k), 10, 64)
		return n, err == nil && n > 0
	}
	drop := func(keys ...string) {
		for _, k := range keys {
			out.Del(k)
		}
	}
	signedIn := p != nil && p.ID > 0
	if q.Get("setlist") != "" {
		n, ok := id("setlist")
		if ok && signedIn {
			ok, _ = s.store.CanViewSetlistByID(ctx, p.ID, n)
		}
		if !ok || !signedIn {
			drop("setlist", "gig")
		}
	} else if q.Has("gig") {
		drop("gig")
	}
	if q.Get("songbook") != "" {
		n, ok := id("songbook")
		if !ok || !s.mayOpenSongbookAs(ctx, p, n) {
			drop("songbook")
		}
	}
	if q.Get("event") != "" {
		n, ok := id("event")
		if !ok || !s.mayOpenEventProgram(ctx, p, n) {
			drop("event")
		}
	}
	if q.Get("band") != "" {
		n, ok := id("band")
		if ok && signedIn && !p.IsAdmin {
			ok, _ = s.store.IsCurrentBandMember(ctx, n, p.ID)
		}
		if !ok || !signedIn {
			drop("band")
		}
	}
	if q.Get("community") != "" {
		n, ok := id("community")
		if ok && signedIn && !p.IsAdmin {
			ok, _ = s.store.IsCurrentCommunityMember(ctx, n, p.ID)
		}
		if !ok || !signedIn {
			drop("community")
		}
	}
	return out
}

// withOpenableSongContext is r with its address's context checked
// (openableSongContext): r itself when it keeps all of it.
func (s *Server) withOpenableSongContext(r *http.Request) *http.Request {
	q := r.URL.Query()
	clean := s.openableSongContext(r.Context(), personFromContext(r), q)
	if len(clean) == len(q) {
		return r
	}
	r2 := r.Clone(r.Context())
	r2.URL.RawQuery = clean.Encode()
	return r2
}

// openableSongQuery is a song link's stored query (home's "Recently"
// list, store.RecentSong.Query) without the contexts p may no longer
// open.
func (s *Server) openableSongQuery(ctx context.Context, p *store.Person, query string) string {
	if query == "" {
		return ""
	}
	q, err := url.ParseQuery(query)
	if err != nil {
		return ""
	}
	clean := s.openableSongContext(ctx, p, q)
	if len(clean) == len(q) {
		return query
	}
	return clean.Encode()
}

// mayOpenSongbookAs is mayOpenSongbook for p (nil: signed out, who opens
// none).
func (s *Server) mayOpenSongbookAs(ctx context.Context, p *store.Person, songbookID int64) bool {
	if p == nil || p.ID <= 0 {
		return false
	}
	if p.IsAdmin {
		return true
	}
	ok, err := s.store.CanAccessSongbook(ctx, p.ID, songbookID)
	return err == nil && ok
}

// mayOpenEventProgram: p may open the event's program (coach_program.go):
// a community's event, p one of its current members or a site admin.
func (s *Server) mayOpenEventProgram(ctx context.Context, p *store.Person, eventID int64) bool {
	if p == nil || p.ID <= 0 {
		return false
	}
	ev, err := s.store.GetEvent(ctx, eventID)
	if err != nil || ev.OwnerKind != "community" {
		return false
	}
	if p.IsAdmin {
		return true
	}
	ok, err := s.store.IsCurrentCommunityMember(ctx, ev.OwnerCommunityID, p.ID)
	return err == nil && ok
}
