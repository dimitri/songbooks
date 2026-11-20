package httpapi

import (
	"reflect"
	"testing"
)

func TestClickMeterFor(t *testing.T) {
	q := func(n int) clickMeter { return clickMeter{Beats: n, PerBpm: 1, Unit: "quarter", Known: true} }
	h := func(n int) clickMeter { return clickMeter{Beats: n, PerBpm: 1, Unit: "half", Known: true} }
	fallback := clickMeter{Beats: 4, PerBpm: 1, Unit: "quarter"}
	cases := []struct {
		sig  string
		want clickMeter
	}{
		{"4/4", q(4)},
		{"3/4", q(3)},
		{"2/4", q(2)},
		{"5/4", q(5)},
		{"6/4", q(6)},
		{"7/4", q(7)},
		{"2/2", h(2)},
		{"3/2", h(3)},
		{"C", q(4)},
		{"C|", h(2)},
		// Compound: every eighth; 1 strong, the middle of the bar lighter
		// (4 in 6/8, 7 in 12/8), or every dotted quarter in 9/8; the bpm
		// in dotted quarters.
		{"6/8", clickMeter{Beats: 6, Groups: []int{3, 3}, Accents: []int{3}, PerBpm: 3, Unit: "dotted-quarter", Known: true}},
		{"9/8", clickMeter{Beats: 9, Groups: []int{3, 3, 3}, Accents: []int{3, 6}, PerBpm: 3, Unit: "dotted-quarter", Known: true}},
		{"12/8", clickMeter{Beats: 12, Groups: []int{3, 3, 3, 3}, Accents: []int{6}, PerBpm: 3, Unit: "dotted-quarter", Known: true}},
		{" 12 / 8 ", clickMeter{Beats: 12, Groups: []int{3, 3, 3, 3}, Accents: []int{6}, PerBpm: 3, Unit: "dotted-quarter", Known: true}},
		// Odd: every eighth, a lighter accent on each group's start.
		{"5/8", clickMeter{Beats: 5, Groups: []int{3, 2}, Accents: []int{3}, PerBpm: 1, Unit: "eighth", Known: true}},
		{"7/8", clickMeter{Beats: 7, Groups: []int{2, 2, 3}, Accents: []int{2, 4}, PerBpm: 1, Unit: "eighth", Known: true}},
		{"11/8", clickMeter{Beats: 11, Groups: []int{3, 3, 3, 2}, Accents: []int{3, 6, 9}, PerBpm: 1, Unit: "eighth", Known: true}},
		{"13/8", clickMeter{Beats: 13, Groups: []int{3, 3, 3, 2, 2}, Accents: []int{3, 6, 9, 11}, PerBpm: 1, Unit: "eighth", Known: true}},
		{"17/8", clickMeter{Beats: 17, Groups: []int{2, 2, 2, 2, 2, 2, 2, 3}, Accents: []int{2, 4, 6, 8, 10, 12, 14}, PerBpm: 1, Unit: "eighth", Known: true}},
		{"3/8", clickMeter{Beats: 3, PerBpm: 1, Unit: "eighth", Known: true}},
		{"", fallback},
		{"waltz", fallback},
		{"7/16", fallback},
		{"0/4", fallback},
	}
	for _, c := range cases {
		got := clickMeterFor(c.sig)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("clickMeterFor(%q) = %+v, want %+v", c.sig, got, c.want)
		}
		sum := 0
		for _, g := range got.Groups {
			sum += g
		}
		if got.Groups != nil && sum != got.Beats {
			t.Errorf("clickMeterFor(%q): groups %v don't add up to %d beats", c.sig, got.Groups, got.Beats)
		}
		for _, a := range got.Accents {
			if a <= 0 || a >= got.Beats {
				t.Errorf("clickMeterFor(%q): accent %d outside the bar", c.sig, a)
			}
		}
	}
}
