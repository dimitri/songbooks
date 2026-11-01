package httpapi

import (
	"net/http"
	"strconv"
)

// handleNotificationsPage lists the logged-in person's own notification
// inbox, newest first — mark-read/mark-all-read actions live client-side
// against the routes below.
func (s *Server) handleNotificationsPage(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	notifs, err := s.store.ListNotifications(r.Context(), person.ID, false)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "notifications", map[string]any{"Me": person, "Notifications": notifs})
}

// handleUnreadNotificationCount backs the nav bell's badge.
func (s *Server) handleUnreadNotificationCount(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	n, err := s.store.UnreadCount(r.Context(), person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": n})
}

// handleMarkNotificationRead marks one of the logged-in person's own
// notifications read — store.MarkRead itself scopes by person_id, so
// there's no cross-account leak here even without an extra ownership
// check.
func (s *Server) handleMarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid notification id")
		return
	}
	person := personFromContext(r)
	if err := s.store.MarkRead(r.Context(), id, person.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"read": true})
}

// handleMarkAllNotificationsRead marks every one of the logged-in
// person's unread notifications read.
func (s *Server) handleMarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	if err := s.store.MarkAllRead(r.Context(), person.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"read": true})
}
