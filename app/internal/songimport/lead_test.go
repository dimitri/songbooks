package songimport

import (
	"testing"

	"app/internal/chartalign"
)

// The recording's bars before the first section begin with the song's
// "lead:" bars: those aren't an intro missing from the chart.
func TestIntroWithoutLead(t *testing.T) {
	in := &chartalign.IntroProposal{Bars: 3, Grid: chartalign.Fold([]string{"N.C.", "G", "C"})}
	if got := withoutLead(in, 1); got == nil || got.Bars != 2 || got.Grid != chartalign.Fold([]string{"G", "C"}) {
		t.Errorf("3 bars less a lead of 1: %+v", got)
	}
	if got := withoutLead(in, 3); got != nil {
		t.Errorf("all lead: %+v", got)
	}
	rep := &chartalign.IntroProposal{Bars: 4, Grid: chartalign.Fold([]string{"N.C.", "G", "G", "G"})}
	if got := withoutLead(rep, 1); got == nil || got.Grid != chartalign.Fold([]string{"G", "G", "G"}) {
		t.Errorf("a folded intro: %+v (from %q)", got, rep.Grid)
	}
}
