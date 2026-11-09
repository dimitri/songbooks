package httpapi

import (
	"encoding/json"
	"net/http"

	"app/internal/song"
)

// jsonSong/jsonSection/jsonMeasure/jsonLyricLine/jsonLyricSeg are the wire
// shapes for internal/song's types — kept separate from song.Song itself so
// the parser package has no JSON-tag concerns of its own.

type jsonMetaField struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type jsonMeasure struct {
	Tokens []string `json:"tokens"`
	Open   bool     `json:"open"`
	Close  bool     `json:"close"`
	Volta  string   `json:"volta"`
	Meter  string   `json:"meter"`
	// RepeatCount mirrors song.Measure.RepeatCount ("|:4 ... :|") — exposed
	// so a round-trip through this JSON API (the Chart editor's load/save
	// and its /api/preview fetch) doesn't silently drop an explicit repeat
	// count down to the implicit, unlabeled "x2" every "|: :|" means.
	RepeatCount int `json:"repeatCount,omitempty"`
}

type jsonLyricSeg struct {
	HasChord bool   `json:"hasChord"`
	Chord    string `json:"chord"`
	Text     string `json:"text"`
}

type jsonLyricLine struct {
	Segments []jsonLyricSeg `json:"segments"`
}

// jsonScoreVoice mirrors song.ScoreVoice — one "score[label]: ..." block.
type jsonScoreVoice struct {
	Label string `json:"label"`
	Body  string `json:"body"`
}

type jsonSection struct {
	Label       string           `json:"label"`
	Repeat      bool             `json:"repeat"`
	Time        string           `json:"time"`
	Scores      []jsonScoreVoice `json:"scores,omitempty"`
	Times       int              `json:"times,omitempty"`
	BarsPerLine []int            `json:"barsPerLine,omitempty"`
	Measures    []jsonMeasure    `json:"measures"`
	Lyrics      []jsonLyricLine  `json:"lyrics"`
	Notes       []string         `json:"notes,omitempty"`
}

type jsonSong struct {
	Meta     []jsonMetaField `json:"meta"`
	Sections []jsonSection   `json:"sections"`
}

func toJSON(s *song.Song) jsonSong {
	out := jsonSong{}
	for _, f := range s.Meta {
		out.Meta = append(out.Meta, jsonMetaField{Key: f.Key, Value: f.Value})
	}
	for _, sec := range s.Sections {
		js := jsonSection{
			Label:       sec.Label,
			Repeat:      sec.Repeat,
			Time:        sec.Time,
			Times:       sec.Times,
			BarsPerLine: sec.BarsPerLine,
			Notes:       sec.Notes,
		}
		for _, sv := range sec.Scores {
			js.Scores = append(js.Scores, jsonScoreVoice{Label: sv.Label, Body: sv.Body})
		}
		for _, m := range sec.Measures {
			js.Measures = append(js.Measures, jsonMeasure{
				Tokens: m.Tokens, Open: m.Open, Close: m.Close, Volta: m.Volta, Meter: m.Meter,
				RepeatCount: m.RepeatCount,
			})
		}
		for _, l := range sec.Lyrics {
			jl := jsonLyricLine{}
			for _, seg := range l.Segments {
				jl.Segments = append(jl.Segments, jsonLyricSeg{HasChord: seg.HasChord, Chord: seg.Chord, Text: seg.Text})
			}
			js.Lyrics = append(js.Lyrics, jl)
		}
		out.Sections = append(out.Sections, js)
	}
	return out
}

func fromJSON(in jsonSong) *song.Song {
	out := &song.Song{}
	for _, f := range in.Meta {
		out.Meta = append(out.Meta, song.MetaField{Key: f.Key, Value: f.Value})
	}
	for _, js := range in.Sections {
		sec := song.Section{
			Label:       js.Label,
			Repeat:      js.Repeat,
			Time:        js.Time,
			Times:       js.Times,
			BarsPerLine: js.BarsPerLine,
			Notes:       js.Notes,
		}
		for _, sv := range js.Scores {
			sec.Scores = append(sec.Scores, song.ScoreVoice{Label: sv.Label, Body: sv.Body})
		}
		for _, m := range js.Measures {
			sec.Measures = append(sec.Measures, song.Measure{
				Tokens: m.Tokens, Open: m.Open, Close: m.Close, Volta: m.Volta, Meter: m.Meter,
				RepeatCount: m.RepeatCount,
			})
		}
		for _, jl := range js.Lyrics {
			l := song.LyricLine{}
			for _, seg := range jl.Segments {
				l.Segments = append(l.Segments, song.LyricSeg{HasChord: seg.HasChord, Chord: seg.Chord, Text: seg.Text})
			}
			sec.Lyrics = append(sec.Lyrics, l)
		}
		out.Sections = append(out.Sections, sec)
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
