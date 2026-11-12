package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"app/internal/chordsym"
	"app/internal/song"
)

// transposeResponse is POST /api/transpose's answer. Text is only set when
// a transpose was asked for; the rest describes the song as it stands
// after it (or as sent, for a plain analysis).
type transposeResponse struct {
	Text        string   `json:"text,omitempty"`
	From        string   `json:"from,omitempty"`
	To          string   `json:"to,omitempty"`
	Semitones   int      `json:"semitones"`
	Kept        int      `json:"kept"`
	Restored    int      `json:"restored"`
	Key         string   `json:"key"`
	KeyGuessed  bool     `json:"keyGuessed"`
	Chords      []string `json:"chords"`
	Missing     []string `json:"missing"`
	DiagramKeys []string `json:"diagramKeys"`
	// Targets are the keys the song can go to from Key (same mode, named
	// the usual way), for the form's "New key" list; AllKeys is every
	// key, offered only when Key was guessed and may need correcting.
	Targets []transposeTarget `json:"targets"`
	AllKeys []string          `json:"allKeys,omitempty"`
}

type transposeTarget struct {
	Key       string `json:"key"`
	Semitones int    `json:"semitones"`
}

// handleTranspose is POST /api/transpose, body {text, from?, to?,
// semitones?}. It works on the editor's unsaved source text and saves
// nothing: the editor puts the rewritten text in Source and the person
// saves it like any other edit, so it lands in the song's history.
//
// With neither "to" nor "semitones" it only analyzes the text (its key,
// the chords it uses, which have no diagram) — what the transpose form
// and the chord-diagram builder's "still to enter" list need. The
// rewrite itself lives in song.TransposeText, next to the parser whose
// line rules it has to follow.
func (s *Server) handleTranspose(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text      string `json:"text"`
		From      string `json:"from"`
		To        string `json:"to"`
		Semitones int    `json:"semitones"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	var resp transposeResponse
	text := req.Text
	if req.To != "" || req.Semitones != 0 {
		res, err := song.TransposeText(req.Text, song.TransposeOptions{From: req.From, To: req.To, Semitones: req.Semitones})
		switch {
		case errors.Is(err, song.ErrModeChange):
			httpError(w, http.StatusBadRequest, "A major key can only go to another major key, and a minor key to another minor key.")
			return
		case errors.Is(err, song.ErrUnknownKey):
			httpError(w, http.StatusBadRequest, "Choose the key the song is written in first.")
			return
		case err != nil:
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
		text = res.Text
		resp.Text, resp.From, resp.To = res.Text, res.From, res.To
		resp.Semitones, resp.Kept, resp.Restored = res.Semitones, res.Kept, res.Restored
	}
	a := song.Analyze(text)
	resp.Key, resp.KeyGuessed = a.Key, a.KeyGuessed
	resp.Chords, resp.Missing, resp.DiagramKeys = nonNil(a.Chords), nonNil(a.Missing), nonNil(a.DiagramKeys)
	keyName := resp.Key
	if resp.Text == "" && req.From != "" {
		// Analysis for a corrected guess: targets from the key chosen.
		keyName = req.From
	}
	resp.Targets = transposeTargets(keyName, song.Parse(text).Get("flats") == "yes")
	if resp.KeyGuessed {
		for _, minor := range []bool{false, true} {
			for pc := range 12 {
				resp.AllKeys = append(resp.AllKeys, chordsym.KeyName(pc, minor, false))
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// transposeTargets lists the 11 other keys of key's mode, from 5
// semitones down to 6 up, named as chordsym.KeyName does.
func transposeTargets(key string, flats bool) []transposeTarget {
	k, ok := chordsym.ParseKey(key)
	if !ok {
		return []transposeTarget{}
	}
	out := make([]transposeTarget, 0, 11)
	for n := -5; n <= 6; n++ {
		if n != 0 {
			out = append(out, transposeTarget{Key: chordsym.KeyName(k.PitchClass()+n, k.Minor, flats), Semitones: n})
		}
	}
	return out
}

// nonNil keeps empty lists as [] in JSON, so the page can loop over them.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
