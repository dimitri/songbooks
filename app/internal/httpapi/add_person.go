package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"app/internal/store"
)

// Adding an existing account to a band or a community (docs/accounts.md):
// there are no usernames, so the form names the person by their email —
// any account — or picks them from the people the adder already plays
// or learns with (store.ListPeopleKnownTo). An email nobody uses is
// refused, not invited: the same form leaves it blank to get a link to
// send them, and a link is what makes a new account.

// addTarget resolves who an add-member request names: personID (one of
// the adder's known people — a site admin may name anyone) or else an
// email. It writes the refusal and returns nil when there's no one: 400
// for neither, 404 for an email no account has or a person the adder
// doesn't know.
func (s *Server) addTarget(w http.ResponseWriter, r *http.Request, email string, personID int64) *store.Person {
	actor := personFromContext(r)
	email = strings.ToLower(strings.TrimSpace(email))
	var target *store.Person
	var err error
	switch {
	case personID != 0:
		known := actor.IsAdmin
		if !known {
			if known, err = s.store.KnowsPerson(r.Context(), actor.ID, personID); err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return nil
			}
		}
		if !known {
			httpError(w, http.StatusNotFound, "no such person")
			return nil
		}
		target, err = s.store.GetPersonByID(r.Context(), personID)
	case strings.Contains(email, "@"):
		target, err = s.store.GetPersonByEmail(r.Context(), email)
	default:
		httpError(w, http.StatusBadRequest, "missing email")
		return nil
	}
	if errors.Is(err, store.ErrNotFound) {
		if personID != 0 {
			httpError(w, http.StatusNotFound, "no such person")
		} else {
			httpError(w, http.StatusNotFound, "no account with that email")
		}
		return nil
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return nil
	}
	return target
}

// knownPeopleExcept is the "people you already play with" a form offers:
// store.ListPeopleKnownTo(personID) minus the ones in skip (the group's
// current members).
func (s *Server) knownPeopleExcept(ctx context.Context, personID int64, skip map[int64]bool) ([]store.KnownPerson, error) {
	known, err := s.store.ListPeopleKnownTo(ctx, personID)
	if err != nil {
		return nil, err
	}
	out := known[:0]
	for _, k := range known {
		if !skip[k.ID] {
			out = append(out, k)
		}
	}
	return out, nil
}
