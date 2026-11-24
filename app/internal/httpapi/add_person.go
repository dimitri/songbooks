package httpapi

import (
	"context"
	"errors"
	"net/http"

	"app/internal/store"
)

// Adding someone to a band or a community (docs/accounts.md) is by name:
// a placeholder with a personal link to send them (the personal-invites
// routes), who joins with their email — a new account or one they have.
// Or, for someone with an account, by picking them among the people the
// adder already plays or learns with (store.ListPeopleKnownTo): the add
// routes take that person's id, never an email, so nothing tells whether
// an email has an account.

// addTarget resolves who an add-member request names: personID, one of
// the adder's known people (a site admin may name anyone). It writes the
// refusal and returns nil when there's no one: 400 without an id, 404
// for a person the adder doesn't know.
func (s *Server) addTarget(w http.ResponseWriter, r *http.Request, personID int64) *store.Person {
	if personID == 0 {
		httpError(w, http.StatusBadRequest, "missing personId")
		return nil
	}
	actor := personFromContext(r)
	known := actor.IsAdmin
	if !known {
		var err error
		if known, err = s.store.KnowsPerson(r.Context(), actor.ID, personID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return nil
		}
	}
	if !known {
		httpError(w, http.StatusNotFound, "no such person")
		return nil
	}
	target, err := s.store.GetPersonByID(r.Context(), personID)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such person")
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
