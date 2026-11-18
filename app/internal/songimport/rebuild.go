package songimport

import (
	"context"

	"app/internal/importer"
	"app/internal/song"
	"app/internal/store"
)

// MapDraft is a band's song map for a song, built from scratch: the best
// Ultimate Guitar chart's sections and grids, cleaned up and without its
// lyrics (importer.SongMap), iReal Pro's grids where its chart is a safe
// match, the song's own title and artist, the chart's capo, the key its
// chords are in, and the "map:" line. Lyrics are added one song at a
// time with the wizard's lyrics step.
func MapDraft(ctx context.Context, st *store.Store, title, artist string) (*Draft, error) {
	res, err := importer.FindBestUGTab(title, artist)
	if err != nil && artist != "" {
		// A band or title spelled differently on the site (missing
		// accents, "The"): the title alone.
		res, err = importer.FindBestUGTab(title, "")
	}
	if err != nil {
		return nil, err
	}
	all := make([]int, len(res.Song.Sections))
	for i := range all {
		all[i] = i
	}
	d := &Draft{URL: res.URL, Title: title, Artist: artist, Capo: res.Song.Capo}
	text, notes := importer.CleanSongText(res.Song.ToSongText(all), importer.CleanOptions{StripInlineChords: true})
	d.Notes = append(d.Notes, notes...)
	d.Guide = text
	text = importer.SongMap(text)
	text = withIReal(ctx, st, d, text, title, artist)
	text = SetHeader(text, "title", title, true)
	if artist != "" {
		text = SetHeader(text, "artist", artist, true)
	}
	if d.Capo != "" {
		text = SetHeader(text, "capo", d.Capo, false)
	}
	if k, _ := ChordKey(song.UsedChords(song.Parse(text))); k != "" {
		text = SetHeader(text, "key", k, false)
	}
	d.Text = importer.SongMap(text) // the map: line last, after the headers
	return d, nil
}
