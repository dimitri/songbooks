// Package songimport is what the import wizard and the command line
// share for bringing a song in: its reference recording, tempo, metre
// and key from that recording, its chords from iReal Pro, its lyrics
// placed in its sections, and its sections synced to the recording.
// The HTTP handlers (internal/httpapi) and cmd/sbcoach are thin callers.
package songimport

import (
	"context"
	"fmt"
	"strings"

	"app/internal/irealpro"
	"app/internal/store"
)

// IRealInfo is what a found iReal Pro chart says about the song.
type IRealInfo struct {
	Title, Key, Style string
	TimeSig           string // "4/4", "" when unknown
	SourceURL         string
	Cached            bool
}

// FindIReal looks up a bar-accurate chord chart for title and artist:
// the cache first (store.FindIRealSong), then a best-effort scan of the
// iReal Pro forum (irealpro.FindOnForum). It returns the chart's grid
// segments, what MergePreviewSegments needs.
func FindIReal(ctx context.Context, st *store.Store, title, artist string) ([]string, IRealInfo, error) {
	if cached, err := st.FindIRealSong(ctx, title); err == nil {
		return []string{cached.GridText}, IRealInfo{
			Title: cached.Title, Key: cached.Key, Style: cached.Style,
			TimeSig:   timeSig(cached.TimeSigNum, cached.TimeSigDen),
			SourceURL: cached.SourceURL, Cached: true,
		}, nil
	}
	tunes, threadURL, err := irealpro.FindOnForum(title, artist)
	if err != nil {
		return nil, IRealInfo{}, err
	}
	if len(tunes) == 0 {
		return nil, IRealInfo{}, fmt.Errorf("no iReal Pro chart for %q", title)
	}
	// A thread can carry several songs: the one whose title shares the
	// most words with the one asked for.
	want := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(title)) {
		want[w] = true
	}
	best, bestScore := tunes[0], -1
	for _, t := range tunes {
		score := 0
		for _, w := range strings.Fields(strings.ToLower(t.Title)) {
			if want[w] {
				score++
			}
		}
		if score > bestScore {
			best, bestScore = t, score
		}
	}
	return best.Segments(), IRealInfo{
		Title: best.Title, Key: best.Key, Style: best.Style,
		TimeSig:   timeSig(best.TimeSigNum, best.TimeSigDen),
		SourceURL: threadURL,
	}, nil
}

func timeSig(num, den int) string {
	if num <= 0 || den <= 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", num, den)
}

// IRealFix applies the iReal Pro chart's chords to body's sections, the
// ones MergePreviewSegments marks safe (same key, a strong match) unless
// all is set; the others are left as they are and counted in the notes.
func IRealFix(ctx context.Context, st *store.Store, body, title, artist string, all bool) (string, []irealpro.SectionMergeCandidate, IRealInfo, error) {
	segments, info, err := FindIReal(ctx, st, title, artist)
	if err != nil {
		return body, nil, info, err
	}
	cands := irealpro.MergePreviewSegments(body, segments)
	accept := map[string]bool{}
	for _, c := range cands {
		if c.Safe || all {
			accept[c.Label] = true
		}
	}
	if len(accept) == 0 {
		return body, cands, info, nil
	}
	return irealpro.ApplyMerge(body, cands, accept), cands, info, nil
}
