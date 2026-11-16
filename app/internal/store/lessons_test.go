package store

import (
	"bytes"
	"context"
	"testing"
)

func TestLessonTitles(t *testing.T) {
	for in, want := range map[string]string{
		"07_Plan Chicago Blues PB.mp3":     "Plan Chicago Blues",
		"01_Mon premier Blues 1 et 2":      "Mon premier Blues 1 et 2",
		"10_ Trois II-V-I":                 "Trois II-V-I",
		"22_Les ingredients d_un bon solo": "Les ingredients d un bon solo",
		"30_ES-335 Blues .gp":              "ES-335 Blues",
	} {
		if got := lessonTitle(in); got != want {
			t.Errorf("lessonTitle(%q) = %q, want %q", in, got, want)
		}
	}
	if got := commonTitle([]string{"Plan psychédélique 1", "Plan psychédélique 2"}); got != "Plan psychédélique" {
		t.Errorf("commonTitle: %q", got)
	}
}

// A course's lessons come from its files, numbered as the author numbers
// them, by folder or by file name; running it again adds only what's new.
func TestLessonsFromFiles(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, err := st.CreatePerson(ctx, "author", "Author", "author@example.com")
	if err != nil {
		t.Fatal(err)
	}
	sb, err := st.GetDefaultSongbook(ctx, "person", me)
	if err != nil {
		t.Fatal(err)
	}
	song := func(title string) int64 {
		id, err := st.CreateVariant(ctx, OwnerPerson, me, title, "", me)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	blues1, blues2, psy1, psy2, chicago, extra := song("Mon premier Blues 1"), song("Mon premier Blues 2"),
		song("Who Knows 1"), song("Who Knows 2"), song("Plan Chicago Blues"), song("Not in the files")
	n := 0
	add := func(folder, name string, v *int64) int64 {
		n++
		id, _, err := st.AddSongbookFile(ctx, NewSongbookFile{SongbookID: sb, VariantID: v, Path: folder, Filename: name}, bytes.NewReader([]byte{byte(n)}))
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	add("", "La méthode.pdf", nil)
	add("01_Mon premier Blues 1 et 2", "01_Mon premier Blues 1.gp", &blues1)
	add("01_Mon premier Blues 1 et 2", "01_ Mon premier Blues 2.gp", &blues2)
	add("", "07_Plan Chicago Blues.gp", &chicago)
	add("", "07_Plan Chicago Blues PB.mp3", &chicago)
	looseMP3 := add("", "20_Same old Blues PB.mp3", nil)
	add("", "32_ Plan psychédélique 1.gp", &psy1)
	add("", "32_ Plan psychédélique 2.gp", &psy2)

	if err := st.SetSongbookKind(ctx, sb, SongbookKindCourse); err != nil {
		t.Fatal(err)
	}
	created, placed, err := st.LessonsFromFiles(ctx, sb)
	if err != nil {
		t.Fatal(err)
	}
	if created != 4 || placed != 6 {
		t.Errorf("created %d lessons, placed %d songs and files; want 4, 6", created, placed)
	}
	lessons, loose, err := st.ListLessons(ctx, sb)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range lessons {
		got = append(got, l.Label())
	}
	want := []string{"Lesson 1 · Mon premier Blues 1 et 2", "Lesson 7 · Plan Chicago Blues", "Lesson 20 · Same old Blues", "Lesson 32 · Plan psychédélique"}
	if len(got) != len(want) {
		t.Fatalf("lessons %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("lesson %d: %q, want %q", i, got[i], want[i])
		}
	}
	if len(lessons[0].Songs) != 2 || lessons[0].Songs[0].VariantID != blues1 || len(lessons[2].Songs) != 0 || len(lessons[3].Songs) != 2 {
		t.Errorf("lesson songs: %+v", lessons)
	}
	if len(loose) != 1 || loose[0].VariantID != extra {
		t.Errorf("songs in no lesson: %+v", loose)
	}
	f, err := st.GetSongbookFile(ctx, looseMP3)
	if err != nil || f.LessonID == nil || *f.LessonID != lessons[2].ID {
		t.Errorf("a file with no song goes in its lesson: %v %v", f.LessonID, err)
	}

	// The course's order is its lessons'.
	groups, err := st.ListSongsOrdered(ctx, sb)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 4 || groups[0].Label != want[0] || groups[3].Label != "Not in a lesson" {
		t.Errorf("course order: %+v", groups)
	}

	// Moved by hand, renamed: running it again keeps that, adds nothing.
	if err := st.SetSongLesson(ctx, sb, extra, &lessons[1].ID); err != nil {
		t.Fatal(err)
	}
	num := 7
	if err := st.UpdateLesson(ctx, lessons[1].ID, &num, "Chicago", "Écoutez d'abord.\n\nPuis jouez."); err != nil {
		t.Fatal(err)
	}
	if created, placed, err := st.LessonsFromFiles(ctx, sb); err != nil || created != 0 || placed != 0 {
		t.Errorf("again: %d, %d, %v", created, placed, err)
	}
	l, err := st.LessonOfSong(ctx, sb, extra)
	if err != nil || l == nil || l.Title != "Chicago" || len(l.Songs) != 2 || l.Songs[1].VariantID != extra || len(l.Paragraphs()) != 2 {
		t.Errorf("lesson of the moved song: %+v %v", l, err)
	}
	if err := st.MoveLessonSong(ctx, l.ID, extra, -1); err != nil {
		t.Fatal(err)
	}
	if l, _ = st.GetLesson(ctx, l.ID); l.Songs[0].VariantID != extra {
		t.Errorf("moved up: %+v", l.Songs)
	}

	// Progress: started by opening a song, done when said.
	if err := st.RecordSongVisit(ctx, me, blues2, ""); err != nil {
		t.Fatal(err)
	}
	if err := st.SetLessonDone(ctx, lessons[3].ID, me, true); err != nil {
		t.Fatal(err)
	}
	p, err := st.CourseProgress(ctx, sb, me)
	if err != nil {
		t.Fatal(err)
	}
	if !p[lessons[0].ID].Started || p[lessons[0].ID].Done || !p[lessons[3].ID].Done || p[lessons[1].ID].Started {
		t.Errorf("progress: %+v", p)
	}
}
