package httpapi

import (
	"strings"
	"testing"

	"app/internal/song"
)

func TestPickupRows(t *testing.T) {
	bars16 := strings.Repeat(" B |", 16)
	cases := []struct {
		name, src string
		pickup    bool
		rows      []int
	}{
		{"count-in bar", "time: 4/4\n\n[Song]\ngrid: |? |" + bars16 + "\n", true, []int{9, 8}},
		{"anacrusis in the score", "time: 4/4\n\n[Song]\nscore[Solo]: {\"Tuning\":[64,59,55,50,45,40],\"Bars\":[{\"Beats\":[{\"Duration\":\"4\",\"Notes\":[{\"MIDI\":64,\"String\":1,\"Fret\":0}]}]}]}\ngrid: | B |" + bars16 + "\n", true, []int{9, 8}},
		{"plain start", "time: 4/4\n\n[Song]\ngrid: | E |" + bars16 + "\n", false, []int{8, 8, 1}},
		{"score plays from beat 1 over a silent grid bar", "time: 4/4\n\n[Song]\nscore[Gtr]: {\"Tuning\":[64,59,55,50,45,40],\"Bars\":[{\"Beats\":[{\"Duration\":\"1\",\"Notes\":[{\"MIDI\":64,\"String\":1,\"Fret\":0}]}]}]}\ngrid: |? |" + bars16 + "\n", false, []int{8, 8, 1}},
		{"anacrusis padded to a full bar", "time: 4/4\n\n[Song]\nscore[Gtr]: {\"Tuning\":[64,59,55,50,45,40],\"Bars\":[{\"Beats\":[{\"Rest\":true,\"Duration\":\"2.\"},{\"Duration\":\"4\",\"Notes\":[{\"MIDI\":64,\"String\":1,\"Fret\":0}]}]}]}\ngrid: | R |" + bars16 + "\n", true, []int{9, 8}},
		{"a grid naming no chord says nothing", "time: 4/4\n\n[Song]\ngrid: | R |" + strings.Repeat(" R |", 16) + "\n", false, []int{8, 8, 1}},
		{"explicit bars: wins", "time: 4/4\n\n[Song]\nbars: 4\ngrid: |? |" + bars16 + "\n", true, []int{4, 4, 4, 4, 1}},
	}
	for _, c := range cases {
		secs := buildSections(song.Parse(c.src))
		if got := secs[0].Measures[0].Pickup; got != c.pickup {
			t.Errorf("%s: pickup = %v, want %v", c.name, got, c.pickup)
		}
		var rows []int
		for _, l := range secs[0].Lines {
			rows = append(rows, len(l.Measures))
		}
		if len(rows) != len(c.rows) {
			t.Errorf("%s: rows %v, want %v", c.name, rows, c.rows)
			continue
		}
		for i := range rows {
			if rows[i] != c.rows[i] {
				t.Errorf("%s: rows %v, want %v", c.name, rows, c.rows)
				break
			}
		}
	}
}
