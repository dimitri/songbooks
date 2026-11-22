package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"app/internal/chordsym"
	"app/internal/i18n"
	"app/internal/store"
)

// A song in another key, kept in a songbook (store/songbook_keys.go): the
// player's Key panel saves the key it plays in into My songbook, or a
// songbook the person can change that holds the song; the songbook page
// sets it per row. The song opened from there opens in that key
// (handleSong's songbookKey); the song itself never changes.

type jsonSongbookKey struct {
	SongbookID int64  `json:"songbookId"`
	Name       string `json:"name"`
	Mine       bool   `json:"mine"`
	InSongbook bool   `json:"inSongbook"`
	Semitones  int    `json:"semitones"`
	KeyName    string `json:"keyName"`
}

// handleGetSongbookKeys is GET /api/songs/{id}/songbook-keys: the song's
// own key, and the songbooks the caller can keep a key for it in.
func (s *Server) handleGetSongbookKeys(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	keys, err := s.store.SongbookKeysFor(r.Context(), personFromContext(r).ID, variantID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	key := s.songKeyOf(r.Context(), variantID)
	lang := s.langOfRequest(r)
	out := make([]jsonSongbookKey, 0, len(keys))
	for _, k := range keys {
		name := k.Name
		if k.Mine {
			name = i18n.T(lang, "My songbook")
		}
		out = append(out, jsonSongbookKey{
			SongbookID: k.SongbookID, Name: name, Mine: k.Mine, InSongbook: k.InSongbook,
			Semitones: k.Semitones, KeyName: chordsym.KeyIn(key, k.Semitones),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "songbooks": out})
}

// handlePutSongbookKey is PUT /api/songbooks/{id}/entries/{variantID}/key
// {semitones}: the song in that key in that songbook, for whoever opens it
// from there. It takes the right to change the songbook; the person's own
// songbook gets the song when it isn't there yet. A coaching student's or
// group's songbook is the coach's to key (coach_keys.go).
func (s *Server) handlePutSongbookKey(w http.ResponseWriter, r *http.Request) {
	songbookID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	variantID, err := pathID(r, "variantID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	var req struct {
		Semitones int `json:"semitones"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Semitones < -12 || req.Semitones > 12 {
		httpError(w, http.StatusBadRequest, "key out of range")
		return
	}
	ctx := r.Context()
	p := personFromContext(r)
	sb, err := s.store.GetSongbook(ctx, songbookID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if ok, err := s.store.CanEditSongbook(ctx, p.ID, songbookID); err != nil || !ok || sb.IsStudent() || sb.Kind == store.SongbookKindGroup {
		httpError(w, http.StatusForbidden, "no edit access to this songbook")
		return
	}
	if _, in, err := s.store.SongbookEntryKey(ctx, songbookID, variantID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	} else if !in {
		mine, err := s.store.GetDefaultSongbook(ctx, "person", p.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if songbookID != mine {
			httpError(w, http.StatusNotFound, "the song isn't in this songbook")
			return
		}
		// A key is an entry setting (docs/ownership-model.md §10.4):
		// any song the person sees — a closed songbook's too, which
		// can't be linked anywhere else — gets one in their own
		// songbook; nothing is copied.
		if ok, err := s.store.CanViewVariant(ctx, p.ID, variantID); err != nil || !ok {
			httpError(w, http.StatusForbidden, "can't see this song")
			return
		}
		if err := s.store.AddToSongbook(ctx, songbookID, variantID, p.ID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := s.store.SetEntrySemitones(ctx, songbookID, variantID, req.Semitones); errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	} else if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"songbookId": songbookID, "semitones": req.Semitones,
		"keyName": chordsym.KeyIn(s.songKeyOf(ctx, variantID), req.Semitones),
	})
}

// songbookKeyLine is the line under a song's title when it opens in a
// songbook's key (play.html), and the editor's note: whose version it is,
// the key, and the link back to the song's own key.
type songbookKeyLine struct {
	Whose, WhoseLower string // "Mercy’s version"; "the catalogue’s version" mid-sentence
	Mine              bool   // the key of the person's own songbook
	Songbook          string // the songbook's name, when not theirs
	Label             string // "A (−2)", or "−2" for a song with no key
	OwnKey            string // the song's own key, "" when none
	Original          string // this page in the song's own key
}

// newSongbookKeyLine makes that line for variant opened in semitones.
func (s *Server) newSongbookKeyLine(r *http.Request, lang string, variant *store.CatalogVariant, personID int64, ownKey string, semitones int, mine bool, songbook string) *songbookKeyLine {
	l := &songbookKeyLine{Mine: mine, Songbook: songbook, OwnKey: ownKey}
	l.Whose, l.WhoseLower = s.whoseVersion(r, lang, variant, personID)
	off := strings.Replace(fmt.Sprintf("%+d", semitones), "-", "−", 1)
	if name := chordsym.KeyIn(ownKey, semitones); name != "" {
		l.Label = name + " (" + off + ")"
	} else {
		l.Label = off
	}
	q := r.URL.Query()
	q.Set("key", "original")
	q.Del("edit")
	l.Original = r.URL.Path + "?" + q.Encode()
	return l
}

// whoseVersion names whose song a song is, at the start of a line and
// mid-sentence: "Mercy’s version", "The catalogue’s version" / "the
// catalogue’s version", "Your version" / "your version".
func (s *Server) whoseVersion(r *http.Request, lang string, variant *store.CatalogVariant, personID int64) (string, string) {
	if variant == nil {
		return "", ""
	}
	if variant.Visibility == "user" && variant.OwnerID != nil && *variant.OwnerID == personID {
		return i18n.T(lang, "Your version"), i18n.T(lang, "your version")
	}
	o, err := s.store.VariantOwnerOf(r.Context(), variant.VariantID)
	if err != nil {
		return "", ""
	}
	switch {
	case o.Kind == "global":
		return i18n.T(lang, "The catalogue’s version"), i18n.T(lang, "the catalogue’s version")
	case o.Name != "":
		return i18n.T(lang, "%s’s version", o.Name), i18n.T(lang, "sentence|%s’s version", o.Name)
	}
	return "", ""
}
