package main

import (
	"context"
	"fmt"
	"log"

	"app/internal/setlist"
	"app/internal/store"
)

func main() {
	ctx := context.Background()
	st, err := store.New(ctx, "postgres://localhost/songbooks_dev")
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	mercySets := []setlist.Set{
		{Label: "Set 1", Entries: []setlist.Entry{
			{Book: "Mercy", Stem: "mercy"},
			{Book: "Mercy", Stem: "id-rather-go-blind"},
			{Book: "Mercy", Stem: "the-thrill-is-gone"},
			{Book: "Mercy", Stem: "ill-play-the-blues-for-you"},
			{Book: "Mercy", Stem: "falling-in-love-again"},
			{Book: "Mercy", Stem: "colors"},
			{Book: "Mercy", Stem: "wake-up-alone"},
			{Book: "Mercy", Stem: "its-too-late"},
			{Book: "Mercy", Stem: "worried-life-blues"},
			{Book: "Mercy", Stem: "the-sky-is-crying"},
			{Book: "Mercy", Stem: "whos-been-talking"},
		}},
		{Label: "Set 2", Entries: []setlist.Entry{
			{Book: "Mercy", Stem: "back-to-black"},
			{Book: "Mercy", Stem: "you-were-mine"},
			{Book: "Mercy", Stem: "shine-a-light"},
			{Book: "Mercy", Stem: "too-much-of-a-woman"},
			{Book: "Mercy", Stem: "travelin-blues"},
			{Book: "Mercy", Stem: "you-know-im-no-good"},
			{Book: "Mercy", Stem: "kill-or-be-kind"},
			{Book: "Mercy", Stem: "soulful-dress"},
			{Book: "Mercy", Stem: "valerie"},
			{Book: "Mercy", Stem: "the-best-youve-had"},
			{Book: "Mercy", Stem: "whisper-in-a-hurricane"},
			{Book: "Mercy", Stem: "black-magic-woman"},
			{Book: "Mercy", Stem: "aint-no-love"},
		}},
		{Label: "Encore", Entries: []setlist.Entry{
			{Book: "Mercy", Stem: "my-yard"},
			{Book: "Mercy", Stem: "give-me-one-reason"},
			{Book: "Mercy", Stem: "gone"},
		}},
	}
	createBandSetlist(ctx, st, 17, "Mercy — full set (from the legacy titlepage)", mercySets)

	olSets := []setlist.Set{
		{Label: "Set 1", Entries: []setlist.Entry{
			{Book: "OtherLies", Stem: "Belle-of-the-Ball"},
			{Book: "OtherLies", Stem: "In-Candesenza"},
			{Book: "OtherLies", Stem: "The-Whirlwind-of-Lovers"},
			{Book: "OtherLies", Stem: "Stars-On-Black"},
			{Book: "OtherLies", Stem: "My-Favourite-Scarlet"},
			{Book: "OtherLies", Stem: "Mottainai"},
			{Book: "OtherLies", Stem: "Bones"},
			{Book: "OtherLies", Stem: "GinLane"},
			{Book: "OtherLies", Stem: "Kawasaki"},
			{Book: "OtherLies", Stem: "The-Obsession-With-Control"},
			{Book: "OtherLies", Stem: "Vanity-Rewards"},
			{Book: "OtherLies", Stem: "Vibrator"},
			{Book: "OtherLies", Stem: "Voiles"},
			{Book: "OtherLies", Stem: "Of-Human-Bondage"},
			{Book: "OtherLies", Stem: "Teens4Today"},
			{Book: "OtherLies", Stem: "The-Last-Embrace"},
		}},
	}
	createBandSetlist(ctx, st, 18, "Set List 2025 (from the legacy titlepage)", olSets)

	fmt.Println("done")
}

func createBandSetlist(ctx context.Context, st *store.Store, bandID int64, title string, sets []setlist.Set) {
	id, err := st.CreateSetlist(ctx, "band", title, "imported from the legacy LaTeX titlepage", nil, &bandID, 1)
	if err != nil {
		log.Fatalf("create setlist for band %d: %v", bandID, err)
	}
	l := &setlist.Setlist{Sets: sets}
	l.Set("title", title)
	if _, err := st.SaveSetlistByID(ctx, id, l, title, "imported from the legacy LaTeX titlepage", "", 1); err != nil {
		log.Fatalf("save setlist %d: %v", id, err)
	}
	fmt.Printf("created setlist %d %q for band %d with %d sets\n", id, title, bandID, len(sets))
}
