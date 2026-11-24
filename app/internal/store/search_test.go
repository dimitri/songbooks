package store

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestSearch: the palette finds only what the person may see, by the
// same rules as the pages it opens.
func TestSearch(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	person := func(handle, name string) int64 {
		t.Helper()
		id, err := st.CreatePerson(ctx, name, handle+"@example.com")
		must(err)
		return id
	}
	me := person("me", "Mia Me")
	other := person("other", "Otto Other")
	coach := person("coach", "Cora Coach")

	// Songs: the library's, mine, not someone else's private one.
	_, err := st.CreateVariant(ctx, OwnerCatalog, 0, "Velvet Global", "Somebody", me)
	must(err)
	_, err = st.CreateVariant(ctx, OwnerPerson, me, "Velvet Mine", "", me)
	must(err)
	_, err = st.CreateVariant(ctx, OwnerPerson, other, "Velvet Theirs", "", other)
	must(err)

	// Bands: mine, and one I'm not in.
	mine, err := st.CreateBand(ctx, "Velvet Hour", me)
	must(err)
	theirs, err := st.CreateBand(ctx, "Velvet Underground", other)
	must(err)
	_, err = st.CreateVariant(ctx, OwnerBand, mine, "Velvet Band Song", "", me)
	must(err)
	_, err = st.CreateVariant(ctx, OwnerBand, theirs, "Velvet Their Band Song", "", other)
	must(err)
	_, err = st.CreateSetlist(ctx, "band", "Velvet Set", "", nil, &mine, nil, me)
	must(err)
	_, err = st.CreateSetlist(ctx, "band", "Velvet Their Set", "", nil, &theirs, nil, other)
	must(err)
	_, err = st.CreateSetlist(ctx, "person", "Velvet Private Set", "", &other, nil, nil, other)
	must(err)
	soon := time.Now().Add(48 * time.Hour)
	_, err = st.CreateEvent(ctx, "band", mine, "rehearsal", "Velvet Rehearsal", "", "", soon, nil, "members", me)
	must(err)
	_, err = st.CreateEvent(ctx, "band", theirs, "rehearsal", "Velvet Their Rehearsal", "", "", soon, nil, "members", other)
	must(err)

	// A community I'm in: an open songbook's song, a song only in a
	// paid-for songbook, a jam; one I'm not in.
	jam, err := st.CreateCommunity(ctx, "Velvet Jam", other)
	must(err)
	_, err = st.AddCommunityMember(ctx, jam, me, "member", other)
	must(err)
	_, err = st.CreateVariant(ctx, OwnerCommunity, jam, "Velvet Community Song", "", other)
	must(err)
	paid, err := st.CreateVariant(ctx, OwnerCommunity, jam, "Velvet Paid Song", "", other)
	must(err)
	def, err := st.GetDefaultSongbook(ctx, "community", jam)
	must(err)
	must(st.RemoveFromSongbook(ctx, def, paid))
	course, err := st.CreateSongbook(ctx, "community", jam, "Velvet Course", false)
	must(err)
	must(st.AddToSongbook(ctx, course, paid, other))
	must(st.SetSongbookRequiresGrant(ctx, course, true))
	_, err = st.CreateEvent(ctx, "community", jam, "jam", "Velvet Jam Night", "", "", soon, nil, "members", other)
	must(err)
	elsewhere, err := st.CreateCommunity(ctx, "Elsewhere", other)
	must(err)
	_, err = st.CreateVariant(ctx, OwnerCommunity, elsewhere, "Velvet Elsewhere Song", "", other)
	must(err)

	// A coaching studio: I'm a student there; Vera is another student.
	studio, err := st.CreateCommunity(ctx, "Studio", coach)
	must(err)
	must(st.SetCommunityKind(ctx, studio, "coaching"))
	vera := person("vera", "Vera Velvet")
	for _, p := range []int64{me, vera} {
		_, err = st.AddCommunityMember(ctx, studio, p, "member", coach)
		must(err)
	}
	_, err = st.StudentSongbook(ctx, studio, vera, "Velvet’s songs", coach)
	must(err)
	_, err = st.StudentSongbook(ctx, studio, me, "Velvet songs of mine", coach)
	must(err)
	lessonAt := soon
	_, err = st.CreateEvent(ctx, "community", studio, "lesson", "Velvet Lesson", "", "", lessonAt, nil, "members", coach)
	must(err)

	titles := func(who int64, q string) []string {
		t.Helper()
		hits, err := st.Search(ctx, who, q, 20)
		must(err)
		var out []string
		for _, h := range hits {
			out = append(out, h.Kind+":"+h.Title)
		}
		sort.Strings(out)
		return out
	}
	got := strings.Join(titles(me, "velvet"), ", ")
	for _, want := range []string{
		"song:Velvet Global", "song:Velvet Mine", "song:Velvet Band Song", "song:Velvet Community Song",
		"band:Velvet Hour", "community:Velvet Jam", "setlist:Velvet Set", "event:Velvet Rehearsal", "event:Velvet Jam Night",
		"songbook:Velvet songs of mine",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	for _, nope := range []string{
		"Velvet Theirs", "Velvet Their Band Song", "Velvet Underground", "Velvet Their Set", "Velvet Private Set",
		"Velvet Their Rehearsal", "Velvet Paid Song", "songbook:Velvet Course", "Velvet Elsewhere Song", "Velvet’s songs",
		"Velvet Lesson", "student:",
	} {
		if strings.Contains(got, nope) {
			t.Errorf("%s shouldn't be found: %s", nope, got)
		}
	}

	// Paid for: the course and its song.
	must(st.GrantSongbookAccess(ctx, course, me, other))
	got = strings.Join(titles(me, "velvet"), ", ")
	for _, want := range []string{"song:Velvet Paid Song", "songbook:Velvet Course"} {
		if !strings.Contains(got, want) {
			t.Errorf("granted: missing %s in %s", want, got)
		}
	}

	// The coach finds their students and their lessons; accents and case
	// don't matter.
	got = strings.Join(titles(coach, "VÉLVET"), ", ")
	for _, want := range []string{"student:Vera Velvet", "event:Velvet Lesson"} {
		if !strings.Contains(got, want) {
			t.Errorf("coach: missing %s in %s", want, got)
		}
	}
	if strings.Contains(got, "student:Mia") || strings.Contains(got, "Velvet Hour") {
		t.Errorf("coach: %s", got)
	}

	// Too short to search.
	if got := titles(me, "v"); len(got) != 0 {
		t.Errorf("one letter: %v", got)
	}
}
