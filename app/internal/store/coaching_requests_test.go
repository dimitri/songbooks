package store

import (
	"context"
	"testing"
)

// TestSongRequests: a student's "Ask for it" waits once, is added or let
// go, and may be asked again after a "not now"; only the repertoire's
// songs count; a songbook's keys are its moved songs.
func TestSongRequests(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	coach, err := st.CreatePerson(ctx, "coach", "Coach", "coach@example.com")
	must(err)
	student, err := st.CreatePerson(ctx, "student", "Stan Student", "student@example.com")
	must(err)
	c, err := st.CreateCommunity(ctx, "Voice", coach)
	must(err)
	must(st.SetCommunityKind(ctx, c, "coaching"))
	_, err = st.AddCommunityMember(ctx, c, student, "member", coach)
	must(err)
	rep, err := st.GetDefaultSongbook(ctx, "community", c)
	must(err)
	if got, _ := st.RepertoireSongbook(ctx, c); got != rep {
		t.Fatalf("repertoire: %d, want %d", got, rep)
	}
	song, err := st.CreateVariant(ctx, OwnerPerson, coach, "Song", "", coach)
	must(err)
	other, err := st.CreateVariant(ctx, OwnerPerson, coach, "Other", "", coach)
	must(err)
	must(st.AddToSongbook(ctx, rep, song, coach))
	if ok, _ := st.RepertoireHas(ctx, c, song); !ok {
		t.Fatal("the repertoire's song isn't in it")
	}
	if ok, _ := st.RepertoireHas(ctx, c, other); ok {
		t.Fatal("a song outside the repertoire is in it")
	}

	id, created, err := st.RequestSong(ctx, c, student, song)
	must(err)
	if !created {
		t.Fatal("a first request wasn't created")
	}
	again, created, err := st.RequestSong(ctx, c, student, song)
	must(err)
	if created || again != id {
		t.Fatalf("asking again while it waits: %d %v", again, created)
	}
	pending, err := st.PendingSongRequests(ctx, c)
	must(err)
	if len(pending) != 1 || pending[0].StudentName != "Stan Student" || pending[0].Title != "Song" {
		t.Fatalf("pending: %+v", pending)
	}
	mine, err := st.StudentSongRequests(ctx, c, student)
	must(err)
	if mine[song] != SongRequestPending {
		t.Fatalf("the student's requests: %v", mine)
	}

	must(st.DecideSongRequest(ctx, id, SongRequestDeclined, coach))
	if err := st.DecideSongRequest(ctx, id, SongRequestAdded, coach); err != ErrNotFound {
		t.Fatalf("deciding twice: %v", err)
	}
	if pending, _ := st.PendingSongRequests(ctx, c); len(pending) != 0 {
		t.Fatalf("still pending: %+v", pending)
	}
	// Not now, then asked again: waiting again, the same request.
	again, created, err = st.RequestSong(ctx, c, student, song)
	must(err)
	if !created || again != id {
		t.Fatalf("asking after a not now: %d %v", again, created)
	}
	must(st.DecideSongRequest(ctx, id, SongRequestAdded, coach))
	if q, _ := st.GetSongRequest(ctx, id); q.Status != SongRequestAdded {
		t.Fatalf("status: %q", q.Status)
	}
	if err := st.DecideSongRequest(ctx, id, "maybe", coach); err == nil {
		t.Fatal("an unknown status was taken")
	}

	// The keys of a songbook: only its moved songs.
	sb, err := st.StudentSongbook(ctx, c, student, "Stan’s songs", coach)
	must(err)
	must(st.AddToSongbook(ctx, sb, song, coach))
	must(st.AddToSongbook(ctx, sb, other, coach))
	must(st.SetEntrySemitones(ctx, sb, song, -3))
	keys, err := st.SongbookKeys(ctx, sb)
	must(err)
	if len(keys) != 1 || keys[song] != -3 {
		t.Fatalf("keys: %v", keys)
	}
}

// TestVoiceProfile: the coach's assessment of a student's voice, each
// change kept with its note; a register's range changed is kept too.
func TestVoiceProfile(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	coach, err := st.CreatePerson(ctx, "coach", "Cora Coach", "coach@example.com")
	must(err)
	student, err := st.CreatePerson(ctx, "student", "Stan Student", "student@example.com")
	must(err)
	c, err := st.CreateCommunity(ctx, "Voice", coach)
	must(err)
	must(st.SetCommunityKind(ctx, c, "coaching"))

	if p, err := st.StudentVoiceProfile(ctx, c, student); err != nil || p.VoiceType != "" {
		t.Fatalf("no profile yet: %+v %v", p, err)
	}
	if err := st.SetStudentVoiceAssessment(ctx, c, student, "falsetto", nil, nil, "", coach); err == nil {
		t.Fatal("an unknown voice type was taken")
	}
	hi, lo := 67, 55
	must(st.SetStudentVoiceAssessment(ctx, c, student, "baritone", &hi, &lo, "warm, light top", coach))
	p, err := st.StudentVoiceProfile(ctx, c, student)
	must(err)
	if p.VoiceType != "baritone" || *p.TessituraLow != 55 || *p.TessituraHigh != 67 || p.Assessment != "warm, light top" {
		t.Fatalf("profile: %+v", p)
	}
	// The same again: nothing new. Only the words: one "assessment" line.
	must(st.SetStudentVoiceAssessment(ctx, c, student, "baritone", &lo, &hi, "warm, light top", coach))
	must(st.SetStudentVoiceAssessment(ctx, c, student, "baritone", &lo, &hi, "warm; the top opens up", coach))
	// The type moves: its old and new values, the note with it.
	must(st.SetStudentVoiceAssessment(ctx, c, student, "tenor", &lo, &hi, "more tenor than we thought", coach))
	cl, ch := 48, 64
	must(st.SetStudentRange(ctx, c, student, VoiceRange{Register: "chest", Low: &cl, High: &ch}, coach))
	must(st.SetStudentRange(ctx, c, student, VoiceRange{Register: "chest", Low: &cl, High: &ch, Note: "same"}, coach))
	h, err := st.StudentVoiceHistory(ctx, c, student)
	must(err)
	var got []string
	for _, x := range h {
		got = append(got, x.Field+":"+x.Old+">"+x.New+"/"+x.Note+"/"+x.ChangedBy)
	}
	want := []string{
		"chest:>48-64//Cora Coach",
		"voice_type:baritone>tenor/more tenor than we thought/Cora Coach",
		"assessment:warm, light top>warm; the top opens up/warm; the top opens up/Cora Coach",
		"tessitura:>55-67/warm, light top/Cora Coach",
		"voice_type:>baritone/warm, light top/Cora Coach",
	}
	if len(got) != len(want) {
		t.Fatalf("history: %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("history[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	types, err := st.CommunityVoiceTypes(ctx, c)
	must(err)
	if types[student] != "tenor" {
		t.Fatalf("types: %v", types)
	}
}

// TestStudentNotes: a student's notes are private or shared; the
// student's list has the shared ones only; a private note becomes shared,
// never back, and only once.
func TestStudentNotes(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	coach, err := st.CreatePerson(ctx, "coach", "Cora Coach", "coach@example.com")
	must(err)
	student, err := st.CreatePerson(ctx, "student", "Stan Student", "student@example.com")
	must(err)
	c, err := st.CreateCommunity(ctx, "Voice", coach)
	must(err)
	song, err := st.CreateVariant(ctx, OwnerPerson, coach, "Song", "", coach)
	must(err)
	private, err := st.AddTeachingNote(ctx, c, student, nil, "PRIVATE", coach)
	must(err)
	shared, err := st.AddStudentNote(ctx, c, student, nil, &song, "SHARED", true, coach)
	must(err)
	all, err := st.ListTeachingNotes(ctx, c, student)
	must(err)
	if len(all) != 2 {
		t.Fatalf("the coach's notes: %+v", all)
	}
	theirs, err := st.ListSharedTeachingNotes(ctx, c, student)
	must(err)
	if len(theirs) != 1 || theirs[0].ID != shared || theirs[0].SongTitle != "Song" || theirs[0].SharedAt == nil || theirs[0].AuthorName != "Cora Coach" {
		t.Fatalf("the student's notes: %+v", theirs)
	}
	must(st.ShareTeachingNote(ctx, c, student, private))
	if err := st.ShareTeachingNote(ctx, c, student, private); err != ErrNotFound {
		t.Fatalf("sharing twice: %v", err)
	}
	if err := st.ShareTeachingNote(ctx, c, coach, shared); err != ErrNotFound {
		t.Fatalf("sharing another student's note: %v", err)
	}
	must(st.UpdateTeachingNote(ctx, c, student, private, "NOW SHARED"))
	theirs, _ = st.ListSharedTeachingNotes(ctx, c, student)
	if len(theirs) != 2 {
		t.Fatalf("after sharing: %+v", theirs)
	}
}
