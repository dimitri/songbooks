package tab

import (
	"fmt"

	"app/internal/song"
)

// LoadScores resolves every section's "score[label]:" voices
// (song.Section.Scores — plain text in the .song body itself, see that
// field's own doc comment) into real Riffs via ParseVoice, keyed by
// section index. A single voice that fails to parse (most likely a typo
// in hand-written LilyPond content) is skipped rather than blanking out
// every other section's notation — real notation on an otherwise-fine
// page is more useful degraded than entirely gone over one bad bar. errs
// carries one error per skipped voice, for the caller to log; it is not
// itself a reason to treat the call as failed.
func LoadScores(sg *song.Song) (scores map[int][]NamedRiff, errs []error) {
	for i, sec := range sg.Sections {
		for _, sv := range sec.Scores {
			riff, err := ParseVoice(sv.Label, sv.Body)
			if err != nil {
				errs = append(errs, fmt.Errorf("section %d (%s) score[%s]: %w", i, sec.Label, sv.Label, err))
				continue
			}
			if scores == nil {
				scores = make(map[int][]NamedRiff)
			}
			scores[i] = append(scores[i], NamedRiff{Name: sv.Label, Riff: riff})
		}
	}
	return scores, errs
}
