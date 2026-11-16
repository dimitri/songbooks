package tab

import (
	"encoding/json"
	"errors"
	"strings"
)

// ErrRiffJSON is what a "score[...]:" voice still holding notes copied
// out of a Guitar Pro file as JSON gets: that format is retired, songs
// point into their Guitar Pro file with "gp[Track]: bars" lines instead
// (docs/gp-file-model.md). Only an old revision brought back from
// History still has one.
var ErrRiffJSON = errors.New(`score: notes copied from a Guitar Pro file are no longer read here; point at the file with a "gp[Track]: bars" line`)

// isRiffJSON: a body is the retired JSON format when it starts with "{"
// AND is valid JSON. LilyPond's own braced form — "{ c'4 d' }", the most
// natural way to write a short riff — also starts with "{" but is never
// valid JSON.
func isRiffJSON(src string) bool {
	t := strings.TrimSpace(src)
	return strings.HasPrefix(t, "{") && json.Valid([]byte(t))
}
