package httpapi

import (
	"context"
	"strings"

	"app/internal/song"
)

// songBadges is the row of small icons a song listing shows next to each
// title: what the song holds (chords, lyrics, one icon per score track),
// whether its Guitar Pro file is attached, and its reference recordings —
// a backing track or the full song, when someone has said which.
type songBadges struct {
	Chords bool
	Lyrics bool
	Tracks []string // score track names, in order of first appearance
	GPFile bool
	Audio  []audioBadge
}

type audioBadge struct {
	Category string // gp, youtube, band
	Kind     string // full, backing, "" unknown
	Aligned  bool
}

// Label is the recording's tooltip, e.g. "Guitar Pro recording — backing
// track, aligned to the bars".
func (a audioBadge) Label() string {
	name := map[string]string{"gp": "Guitar Pro recording", "youtube": "YouTube recording", "band": "Band recording"}[a.Category]
	if name == "" {
		name = "Recording"
	}
	switch a.Kind {
	case "backing":
		name += " — backing track"
	case "full":
		name += " — full performance"
	}
	if a.Aligned {
		name += ", aligned to the bars"
	}
	return name
}

func badgesFromSong(sg *song.Song) songBadges {
	var b songBadges
	seen := map[string]bool{}
	for _, sec := range sg.Sections {
		if len(sec.Lyrics) > 0 {
			b.Lyrics = true
		}
		for _, m := range sec.Measures {
			if !b.Chords && len(m.Tokens) > 0 && !isSilentBar(m.Tokens) && !allSimile(m.Tokens) {
				b.Chords = true
			}
		}
		for _, v := range sec.Scores {
			name := strings.TrimSpace(v.Label)
			if name == "" || seen[strings.ToLower(name)] {
				continue
			}
			seen[strings.ToLower(name)] = true
			b.Tracks = append(b.Tracks, name)
		}
	}
	return b
}

// allSimile: a bar of only repeat marks ("%") says nothing about chords.
func allSimile(tokens []string) bool {
	for _, t := range tokens {
		if strings.TrimSpace(t) != "%" {
			return false
		}
	}
	return true
}

// songBadgesFor builds the listing badges for every variant in ids, in two
// queries (see store.SongMediaFor). Best effort: a failure shows no badges.
func (s *Server) songBadgesFor(ctx context.Context, ids []int64) map[int64]songBadges {
	out := map[int64]songBadges{}
	media, err := s.store.SongMediaFor(ctx, ids)
	if err != nil {
		return out
	}
	for id, m := range media {
		b := badgesFromSong(song.Parse(m.Body))
		b.GPFile = m.GPFile
		for _, a := range m.Audio {
			b.Audio = append(b.Audio, audioBadge{Category: string(a.Category), Kind: a.Kind, Aligned: a.Aligned})
		}
		out[id] = b
	}
	return out
}
