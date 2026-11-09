package tab

import (
	"reflect"
	"testing"
)

func TestComputeBeamSegments_MixedDuration(t *testing.T) {
	// A dotted-8th + 16th beam group (the real Bad Spell case): level 0
	// (eighth-beam) spans both notes; level 1 (sixteenth-beam) only
	// actually applies to the second note, so it's a partial stub
	// pointing back toward its only beamed neighbor (index 0).
	beats := []Beat{
		{Duration: "8.", Notes: []Note{{Fret: 1, String: 1}}},
		{Duration: "16", Notes: []Note{{Fret: 1, String: 1}}},
	}
	grp := BeamGroup{StartIdx: 0, EndIdx: 1, MinFlags: 1}
	got := ComputeBeamSegments(beats, grp)
	want := []BeamSegment{
		{Level: 0, StartIdx: 0, EndIdx: 1},
		{Level: 1, StartIdx: 1, EndIdx: 1, PartialToward: -1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ComputeBeamSegments = %+v, want %+v", got, want)
	}
}

func TestComputeBeamSegments_AllSixteenths(t *testing.T) {
	beats := []Beat{
		{Duration: "16", Notes: []Note{{Fret: 1, String: 1}}},
		{Duration: "16", Notes: []Note{{Fret: 1, String: 1}}},
	}
	grp := BeamGroup{StartIdx: 0, EndIdx: 1, MinFlags: 2}
	got := ComputeBeamSegments(beats, grp)
	want := []BeamSegment{
		{Level: 0, StartIdx: 0, EndIdx: 1},
		{Level: 1, StartIdx: 0, EndIdx: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ComputeBeamSegments = %+v, want %+v", got, want)
	}
}

func TestComputeBeamSegments_LeadingPartial(t *testing.T) {
	// 16th, 8th, 8th — the lone 16th leads the group, so its level-1
	// partial has no PREVIOUS beamed neighbor and must point forward.
	beats := []Beat{
		{Duration: "16", Notes: []Note{{Fret: 1, String: 1}}},
		{Duration: "8", Notes: []Note{{Fret: 1, String: 1}}},
		{Duration: "8", Notes: []Note{{Fret: 1, String: 1}}},
	}
	grp := BeamGroup{StartIdx: 0, EndIdx: 2, MinFlags: 1}
	got := ComputeBeamSegments(beats, grp)
	want := []BeamSegment{
		{Level: 0, StartIdx: 0, EndIdx: 2},
		{Level: 1, StartIdx: 0, EndIdx: 0, PartialToward: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ComputeBeamSegments = %+v, want %+v", got, want)
	}
}
