package store

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// TestPlayerParts: a person's parts for a song, from the song's choice,
// the context's, the profile, else the voice; each context its own; a
// cleared choice lets the one under it show again.
func TestPlayerParts(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, err := st.CreatePerson(ctx, "mia", "Mia", "mia@example.com")
	if err != nil {
		t.Fatal(err)
	}
	song, err := st.CreateVariant(ctx, OwnerPerson, me, "Song", "", me)
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateVariant(ctx, OwnerPerson, me, "Other song", "", me)
	if err != nil {
		t.Fatal(err)
	}
	band, err := st.CreateBand(ctx, "Mercy", me)
	if err != nil {
		t.Fatal(err)
	}
	jam, err := st.CreateCommunity(ctx, "Jam", me)
	if err != nil {
		t.Fatal(err)
	}
	inBand := PartsContext{Kind: "band", ID: band}
	inJam := PartsContext{Kind: "community", ID: jam}
	nowhere := PartsContext{}
	get := func(c PartsContext, variant int64) PlayerParts {
		t.Helper()
		p, err := st.ResolvePlayerParts(ctx, me, c, variant)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	want := func(what string, p PlayerParts, from string, ids ...string) {
		t.Helper()
		if p.From != from || !slices.Equal(p.Instruments, ids) {
			t.Fatalf("%s: %+v, want %s %v", what, p, from, ids)
		}
	}

	// Nothing said: the voice.
	want("no profile", get(inBand, song), PartsFromDefault, "voice")
	// The profile, messy as typed: normalised.
	if err := st.UpdateOwnInstruments(ctx, me, []string{"guitar & singer", "Basse"}, nil); err != nil {
		t.Fatal(err)
	}
	want("the profile", get(inBand, song), PartsFromProfile, "voice", "guitar", "bass")

	// Guitar with the band, all its songs; the jam keeps the profile.
	if err := st.SetPlayerParts(ctx, me, inBand, 0, []string{"guitar"}); err != nil {
		t.Fatal(err)
	}
	want("the band", get(inBand, song), PartsFromContext, "guitar")
	want("the band, another song", get(inBand, other), PartsFromContext, "guitar")
	want("the jam", get(inJam, song), PartsFromProfile, "voice", "guitar", "bass")
	// That one song with the band: voice and piano (ordered as the list is).
	if err := st.SetPlayerParts(ctx, me, inBand, song, []string{"keys", "voice", "keys"}); err != nil {
		t.Fatal(err)
	}
	want("the song in the band", get(inBand, song), PartsFromSong, "voice", "keys")
	want("another song in the band", get(inBand, other), PartsFromContext, "guitar")
	// Saved again: replaced, not added.
	if err := st.SetPlayerParts(ctx, me, inBand, song, []string{"sax"}); err != nil {
		t.Fatal(err)
	}
	want("the song in the band, again", get(inBand, song), PartsFromSong, "sax")
	// Elsewhere: its own; nothing at all is "I listen".
	if err := st.SetPlayerParts(ctx, me, nowhere, 0, nil); err != nil {
		t.Fatal(err)
	}
	want("elsewhere", get(nowhere, song), PartsFromContext)
	// Cleared: what's under it again.
	if err := st.ClearPlayerParts(ctx, me, inBand, song); err != nil {
		t.Fatal(err)
	}
	want("the song cleared", get(inBand, song), PartsFromContext, "guitar")
	if err := st.ClearPlayerParts(ctx, me, inBand, 0); err != nil {
		t.Fatal(err)
	}
	want("the band cleared", get(inBand, song), PartsFromProfile, "voice", "guitar", "bass")

	// Refused: an instrument that isn't one, too many, a context that isn't one.
	for what, try := range map[string]func() error{
		"an unknown instrument": func() error { return st.SetPlayerParts(ctx, me, inBand, 0, []string{"kazoo"}) },
		"seven instruments": func() error {
			return st.SetPlayerParts(ctx, me, inBand, 0, []string{"voice", "guitar", "bass", "drums", "keys", "sax", "cello"})
		},
		"a band without its id": func() error { return st.SetPlayerParts(ctx, me, PartsContext{Kind: "band"}, 0, []string{"voice"}) },
		"a school": func() error {
			return st.SetPlayerParts(ctx, me, PartsContext{Kind: "school", ID: 1}, 0, []string{"voice"})
		},
	} {
		if err := try(); !errors.Is(err, ErrBadParts) {
			t.Errorf("%s: %v", what, err)
		}
	}
}
