package store

import (
	"context"
	"testing"
	"time"
)

func TestEventProgram(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	coach, _ := st.CreatePerson(ctx, "coach", "Coach", "coach@example.com")
	emma, _ := st.CreatePerson(ctx, "emma", "Émma", "emma@example.com")
	leo, _ := st.CreatePerson(ctx, "leo", "Leo", "leo@example.com")
	c, err := st.CreateCommunity(ctx, "Voice", coach)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetCommunityKind(ctx, c, "coaching"); err != nil {
		t.Fatal(err)
	}
	var songs []int64
	for _, title := range []string{"B song", "A song", "C song"} {
		id, err := st.CreateVariant(ctx, OwnerPerson, coach, title, "", coach)
		if err != nil {
			t.Fatal(err)
		}
		songs = append(songs, id)
	}
	for _, p := range []int64{emma, leo} {
		if _, err := st.AddCommunityMember(ctx, c, p, "member", coach); err != nil {
			t.Fatal(err)
		}
		sb, err := st.StudentSongbook(ctx, c, p, "songs", coach)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AddToSongbook(ctx, sb, songs[0], coach); err != nil {
			t.Fatal(err)
		}
		if p == emma {
			if err := st.SetEntrySemitones(ctx, sb, songs[0], 2); err != nil {
				t.Fatal(err)
			}
			if err := st.AddToSongbook(ctx, sb, songs[1], coach); err != nil {
				t.Fatal(err)
			}
		}
	}

	// The repertoire: by title, its singers by name, accents aside.
	rep, err := st.CommunityRepertoire(ctx, c)
	if err != nil || len(rep) != 2 || rep[0].Title != "A song" || len(rep[1].Singers) != 2 || rep[1].Singers[0].Name != "Émma" || rep[1].Singers[0].Semitones != 2 {
		t.Fatalf("repertoire: %+v, %v", rep, err)
	}

	ev, err := st.CreateEvent(ctx, "community", c, "concert", "Concert", "", "", time.Now().Add(24*time.Hour), nil, "members", coach)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddProgramSongs(ctx, ev, []int64{songs[0], songs[1], songs[0]}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddProgramSongs(ctx, ev, []int64{songs[2], songs[1]}); err != nil {
		t.Fatal(err)
	}
	p, err := st.ListEventProgram(ctx, ev)
	if err != nil || len(p) != 3 || p[0].VariantID != songs[0] || p[2].VariantID != songs[2] {
		t.Fatalf("program: %+v, %v", p, err)
	}

	// Lead and backing: one lead, a backing singer made lead stops backing.
	if err := st.SetProgramBacking(ctx, p[0].ID, emma, true); err != nil {
		t.Fatal(err)
	}
	if err := st.SetProgramLead(ctx, p[0].ID, leo); err != nil {
		t.Fatal(err)
	}
	if err := st.SetProgramLead(ctx, p[0].ID, emma); err != nil {
		t.Fatal(err)
	}
	if err := st.SetProgramBacking(ctx, p[0].ID, leo, true); err != nil {
		t.Fatal(err)
	}
	if err := st.SetProgramKey(ctx, ev, p[0].ID, 2); err != nil {
		t.Fatal(err)
	}
	p, _ = st.ListEventProgram(ctx, ev)
	if p[0].Lead == nil || p[0].Lead.PersonID != emma || len(p[0].Backing) != 1 || p[0].Backing[0].PersonID != leo || p[0].Semitones != 2 {
		t.Fatalf("parts: %+v", p[0])
	}

	// Running order: moved down, the neighbours follow; the key from the
	// program.
	if err := st.MoveProgramSong(ctx, ev, p[0].ID, 1); err != nil {
		t.Fatal(err)
	}
	if prev, next, _ := st.ProgramNeighbors(ctx, ev, songs[0]); prev != songs[1] || next != songs[2] {
		t.Fatalf("neighbours: %d, %d", prev, next)
	}
	if n, ok, _ := st.ProgramKeyOf(ctx, ev, songs[0]); !ok || n != 2 {
		t.Fatalf("key: %d %v", n, ok)
	}
	if _, ok, _ := st.ProgramKeyOf(ctx, ev, -1); ok {
		t.Fatal("a song not in the program has a key")
	}
	if err := st.RemoveProgramSong(ctx, ev, p[0].ID); err != nil {
		t.Fatal(err)
	}
	if p, _ = st.ListEventProgram(ctx, ev); len(p) != 2 {
		t.Fatalf("after removing: %+v", p)
	}
}
