package tab

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DecodeRiffJSON decodes a Riff encoded as plain JSON (Go's default
// struct encoding — field names unchanged, e.g. {"Tuning":[...],
// "MinimumFret":0,"Bars":[{"Beats":[...]}]}) — the alternate score:
// source format a Guitar Pro import writes instead of LilyPond-subset
// text (see ParseScore). A GP file already carries the exact fret/string
// a human player chose for every note; re-deriving it through
// ParseScore's own assignFrets (which only ever sees a bare pitch, and
// picks its own lowest-available-fret voicing) could silently pick a
// DIFFERENT fingering than the one actually tabbed out — so an imported
// riff is stored as this package's own Riff shape directly, verbatim,
// never round-tripped through LilyPond-subset text at all.
func DecodeRiffJSON(data []byte) (*Riff, error) {
	var riff Riff
	if err := json.Unmarshal(data, &riff); err != nil {
		return nil, err
	}
	if len(riff.Tuning) == 0 && !riff.Drums {
		return nil, fmt.Errorf("score: imported riff has no Tuning")
	}
	if len(riff.Bars) == 0 {
		return nil, fmt.Errorf("score: imported riff has no Bars")
	}
	return &riff, nil
}

// DecodeScoreSource is the one entry point everything that reads a
// score: field's stored source should call instead of ParseScore
// directly — a leading "{" means a GP-imported riff (DecodeRiffJSON),
// anything else is LilyPond-subset text (ParseScore). The two formats
// can never collide: real LilyPond-subset source never starts with "{"
// (every real riff opens with "\new TabStaff" or a bare note), and valid
// JSON never starts with "\".
func DecodeScoreSource(src string) (*Riff, error) {
	if isRiffJSON(src) {
		return DecodeRiffJSON([]byte(src))
	}
	return ParseScore(src)
}

// isRiffJSON: a body is a GP-imported riff's JSON when it starts with "{"
// AND is valid JSON. LilyPond's own braced form — "{ c'4 d' }", the most
// natural way to write a short riff — also starts with "{" but is never
// valid JSON, so it goes on to the LilyPond-subset parser instead of
// failing as broken JSON.
func isRiffJSON(src string) bool {
	t := strings.TrimSpace(src)
	return strings.HasPrefix(t, "{") && json.Valid([]byte(t))
}
