package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	"app/internal/store"
)

// An invite or join link that no longer works gets a page of its own,
// not a bare error: most often it's someone who joined through it
// opening it again, to come back in, and the way back in is to log in.

// deadLink is such a link, as its page tells it.
type deadLink struct {
	// Reason is "used" (a single-use link taken), "closed" (a group
	// link closed), "expired", or "unknown" (no such link: never was,
	// revoked, replaced by a newer one).
	Reason string
	// TargetKind ("band", "community" or "") and TargetID are where the
	// link takes (took) its holder; TargetName that group's name.
	TargetKind string
	TargetID   int64
	TargetName string
}

// deadLinkNotice is the /home query that shows the notice saying a link
// someone signed in followed no longer works.
const deadLinkNotice = "link=dead"

// handleDeadInvite answers an /invite/{token} link that GetInvite (or
// taking it) refused: why, from the tombstone a used link leaves
// (store.DeadInviteFor), else unknown.
func (s *Server) handleDeadInvite(w http.ResponseWriter, r *http.Request, token string) {
	d := deadLink{Reason: "unknown"}
	dead, err := s.store.DeadInviteFor(r.Context(), token)
	switch {
	case err == nil:
		d = deadLink{Reason: "expired", TargetKind: dead.TargetKind, TargetID: dead.TargetID, TargetName: dead.TargetName}
		if dead.Used {
			d.Reason = "used"
		}
	case !errors.Is(err, store.ErrNotFound):
		log.Printf("dead invite %s: %v", token, err)
	}
	s.showDeadLink(w, r, d)
}

// handleDeadJoinLink answers a /join/{token} link that is unknown (gi
// nil) or closed.
func (s *Server) handleDeadJoinLink(w http.ResponseWriter, r *http.Request, gi *store.GroupInviteView) {
	d := deadLink{Reason: "unknown"}
	if gi != nil {
		d = deadLink{Reason: "closed", TargetKind: gi.TargetKind, TargetID: gi.BandID, TargetName: gi.TargetName}
		if gi.TargetKind == "community" {
			d.TargetID = gi.CommunityID
		}
	}
	s.showDeadLink(w, r, d)
}

// showDeadLink sends someone signed in where the link would have taken
// them, when they're in that band or community already, else home, with
// a notice; anyone else gets the page: log in, or ask for a new link.
// 410 Gone for a link that was (used, closed, expired), 404 for one that
// never was, as far as anyone can tell; never cached.
func (s *Server) showDeadLink(w http.ResponseWriter, r *http.Request, d deadLink) {
	// What it answers depends on who asks, and a browser keeps a 404 or
	// a 410 it got: without this, the page seen signed out comes back
	// from the cache once they've logged in, instead of the way in.
	w.Header().Set("Cache-Control", "no-store")
	if p := s.sessionPerson(r); p != nil {
		http.Redirect(w, r, s.deadLinkDest(r.Context(), d, p.ID), http.StatusSeeOther)
		return
	}
	status := http.StatusGone
	if d.Reason == "unknown" {
		status = http.StatusNotFound
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	s.render(w, "link_dead", map[string]any{
		"Reason": d.Reason, "TargetKind": d.TargetKind, "TargetName": d.TargetName,
	})
}

// deadLinkDest is where a dead link sends personID, signed in: the
// band or community it was for when they're in it, else home with
// deadLinkNotice.
func (s *Server) deadLinkDest(ctx context.Context, d deadLink, personID int64) string {
	switch d.TargetKind {
	case "band":
		if ok, err := s.store.IsCurrentBandMember(ctx, d.TargetID, personID); err == nil && ok {
			return fmt.Sprintf("/bands/%d", d.TargetID)
		}
	case "community":
		if ok, err := s.store.IsCurrentCommunityMember(ctx, d.TargetID, personID); err == nil && ok {
			return fmt.Sprintf("/communities/%d", d.TargetID)
		}
	}
	return "/home?" + deadLinkNotice
}
