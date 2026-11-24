package httpapi

import (
	"net/http/httptest"
	"testing"

	"app/internal/chartalign"
	"app/internal/songimport"
	"app/internal/store"
)

func autoOf(markers map[int]float64, bars int) *songimport.AutoSync {
	a := &songimport.AutoSync{Result: &chartalign.Result{State: "good"}}
	for i := 0; i < 8; i++ {
		if t, ok := markers[i]; ok {
			a.Markers = append(a.Markers, songimport.Marker{SectionIndex: i, TimeSeconds: t})
		}
	}
	for b := 0; b < bars; b++ {
		a.BarPoints = append(a.BarPoints, chartalign.BarPoint{Bar: b, Time: float64(b) * 2})
	}
	return a
}

// "Use this sync" places the sections a person hasn't set: the starts
// set by ear stay, estimates out of order with them go, and the
// analysis' every bar is written only where it agrees with them.
func TestMergeSyncKeepsSetStarts(t *testing.T) {
	p := autoOf(map[int]float64{0: 1, 1: 20, 2: 40, 3: 60}, 40)
	old := []store.AudioMarker{{SectionIndex: 1, TimeSeconds: 20.02, Origin: store.SyncSet}, {SectionIndex: 2, TimeSeconds: 41, Origin: store.SyncAuto}}
	m, pts, kept := mergeSync(old, nil, p)
	if kept != 1 || len(m) != 4 || m[1].Origin != store.SyncSet || m[1].TimeSeconds != 20.02 || m[2].TimeSeconds != 40 || m[2].Origin != store.SyncAuto {
		t.Fatalf("markers %+v, kept %d", m, kept)
	}
	if pts == nil || len(*pts) != 40 {
		t.Fatalf("points %v: the analysis agrees with the start set by ear, its bars are written", pts)
	}
	// A start set by ear the analysis disagrees with: the starts alone
	// place the bars.
	old[0].TimeSeconds = 25
	m, pts, _ = mergeSync(old, nil, p)
	if pts == nil || len(*pts) != 0 {
		t.Errorf("points %v, want dropped", pts)
	}
	// An estimate that would land after a start set by ear goes.
	old2 := []store.AudioMarker{{SectionIndex: 2, TimeSeconds: 10, Origin: store.SyncSet}}
	m, _, _ = mergeSync(old2, nil, p)
	for _, x := range m {
		if x.SectionIndex == 1 {
			t.Errorf("section 1 at %.2f kept, after section 2's 10 s set by ear", x.TimeSeconds)
		}
	}
	if !markersInOrder(m) {
		t.Errorf("out of order: %+v", m)
	}
	// Per-bar points checked by ear stay as they are.
	if _, pts, _ := mergeSync(nil, []store.GPBarSyncPoint{{BarIndex: 0, TimeSeconds: 1, Origin: store.SyncSet}}, p); pts != nil {
		t.Errorf("points set by ear replaced: %v", pts)
	}
	// replace: nothing kept.
	if m, _, kept := mergeSync(nil, nil, p); kept != 0 || len(m) != 4 {
		t.Errorf("replace: %+v %d", m, kept)
	}
}

// A Guitar Pro file's own per-bar sync is never touched by a markers save.
func TestReconcileLeavesGuitarPro(t *testing.T) {
	s := &Server{}
	pts, err := s.reconcileBarPoints(httptest.NewRequest("PUT", "/", nil), &store.SongAudio{Category: store.AudioGP}, nil)
	if err != nil || pts != nil {
		t.Errorf("gp: %v %v, want untouched", pts, err)
	}
}

// Saving the sync with every start cleared drops the estimated per-bar
// points: it doesn't confirm them ("Sync set") as an all-set save would.
func TestReconcileEmptySync(t *testing.T) {
	pts := []store.GPBarSyncPoint{{BarIndex: 0, TimeSeconds: 1, Origin: store.SyncAuto}, {BarIndex: 4, TimeSeconds: 9, Origin: store.SyncAuto}}
	if got := reconcile(pts, nil, nil, nil); got == nil || len(*got) != 0 {
		t.Errorf("empty sync: %v, want the points dropped", got)
	}
	// All set, where the points put them: confirmed.
	got := reconcile(pts, []store.AudioMarker{{SectionIndex: 0, TimeSeconds: 1, Origin: store.SyncSet}, {SectionIndex: 1, TimeSeconds: 9.02, Origin: store.SyncSet}}, map[int]int{0: 0, 1: 4}, nil)
	if got == nil || len(*got) != 2 || (*got)[0].Origin != store.SyncSet || pts[0].Origin != store.SyncAuto {
		t.Errorf("confirmed: %v", got)
	}
	// A start moved: the markers win.
	if got := reconcile(pts, []store.AudioMarker{{SectionIndex: 1, TimeSeconds: 10, Origin: store.SyncSet}}, map[int]int{1: 4}, nil); got == nil || len(*got) != 0 {
		t.Errorf("moved: %v", got)
	}
}

func TestMarkersInOrder(t *testing.T) {
	ok := []store.AudioMarker{{SectionIndex: 2, TimeSeconds: 30}, {SectionIndex: 0, TimeSeconds: 1}, {SectionIndex: 1, TimeSeconds: 1}}
	if !markersInOrder(ok) {
		t.Error("in order (a section with no bars shares its start)")
	}
	bad := []store.AudioMarker{{SectionIndex: 0, TimeSeconds: 60}, {SectionIndex: 1, TimeSeconds: 75}, {SectionIndex: 2, TimeSeconds: 70}}
	if markersInOrder(bad) {
		t.Error("section 2 before section 1 accepted")
	}
}
