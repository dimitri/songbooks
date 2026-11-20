package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// A space's sections were consolidated (menu PR 5, docs: the menu
// proposal's §3.3): a band's Proposals and Discover became views of its
// Repertoire, its Rehearsals and Concerts its Recordings; a community's
// Downloads its Resources, and so on. Every old address still works: it
// redirects, for good, to the page that holds it now, with the right
// view.

// movedInBand redirects a band page's old address to where it lives now
// in the same band (to is the rest of the path, with any query the new
// page needs: "/catalog/vote"); the request's own query is kept.
func (s *Server) movedInBand(to string) http.HandlerFunc {
	return movedInSpace("/bands/%d", to)
}

// movedInCommunity is movedInBand for a community's pages.
func (s *Server) movedInCommunity(to string) http.HandlerFunc {
	return movedInSpace("/communities/%d", to)
}

func movedInSpace(home, to string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, movedTarget(fmt.Sprintf(home, id)+to, r.URL.Query()), http.StatusMovedPermanently)
	}
}

// movedTarget is target with the old request's query added to its own
// (the target's wins: it names the view).
func movedTarget(target string, old url.Values) string {
	path, query, _ := strings.Cut(target, "?")
	q, _ := url.ParseQuery(query)
	for k, vs := range old {
		if _, set := q[k]; !set {
			q[k] = vs
		}
	}
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

// movedResources is GET /communities/{id}/downloads/songbooks/{sbID}: one
// songbook's files, now at /resources/songbooks/{sbID}.
func (s *Server) movedResources(w http.ResponseWriter, r *http.Request) {
	id, err1 := strconv.ParseInt(r.PathValue("id"), 10, 64)
	sb, err2 := strconv.ParseInt(r.PathValue("sbID"), 10, 64)
	if err1 != nil || err2 != nil {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, movedTarget(fmt.Sprintf("/communities/%d/resources/songbooks/%d", id, sb), r.URL.Query()), http.StatusMovedPermanently)
}
