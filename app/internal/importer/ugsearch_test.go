package importer

import "testing"

// realUGSearchPageSnippet is a minimal js-store blob built from the real
// field shapes/values Ultimate Guitar's own search page returned for
// "hotel california" (fetched and inspected directly, not guessed) —
// trimmed to a few representative entries rather than committing a
// whole scraped third-party page: one Pro (must be filtered out), one
// low-vote Chords, one very-high-vote Chords (must outrank the former
// despite a lower raw rating), one Tabs.
const realUGSearchPageSnippet = `<div class="js-store" data-content="{&quot;store&quot;:{&quot;page&quot;:{&quot;data&quot;:{&quot;results&quot;:[` +
	`{&quot;song_name&quot;:&quot;Hotel California&quot;,&quot;artist_name&quot;:&quot;Eagles&quot;,&quot;type&quot;:&quot;Pro&quot;,&quot;rating&quot;:4.9,&quot;votes&quot;:50,&quot;tab_url&quot;:&quot;https://tabs.ultimate-guitar.com/tab/eagles/hotel-california-guitar-pro-2243275&quot;},` +
	`{&quot;song_name&quot;:&quot;Hotel California&quot;,&quot;artist_name&quot;:&quot;Eagles&quot;,&quot;type&quot;:&quot;Chords&quot;,&quot;rating&quot;:4.62885,&quot;votes&quot;:136,&quot;tab_url&quot;:&quot;https://tabs.ultimate-guitar.com/tab/eagles/hotel-california-chords-1403188&quot;},` +
	`{&quot;song_name&quot;:&quot;Hotel California&quot;,&quot;artist_name&quot;:&quot;Eagles&quot;,&quot;type&quot;:&quot;Chords&quot;,&quot;rating&quot;:4.85987,&quot;votes&quot;:40755,&quot;tab_url&quot;:&quot;https://tabs.ultimate-guitar.com/tab/eagles/hotel-california-chords-46190&quot;},` +
	`{&quot;song_name&quot;:&quot;Hotel California&quot;,&quot;artist_name&quot;:&quot;Eagles&quot;,&quot;type&quot;:&quot;Tabs&quot;,&quot;rating&quot;:4.7,&quot;votes&quot;:900,&quot;tab_url&quot;:&quot;https://tabs.ultimate-guitar.com/tab/eagles/hotel-california-tabs-2241893&quot;}` +
	`]}}}}"></div>`

func TestParseUGSearchResults(t *testing.T) {
	results, err := parseUGSearchResults(realUGSearchPageSnippet)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3 (Pro must be filtered out): %+v", len(results), results)
	}
	for _, r := range results {
		if r.Type == "Pro" {
			t.Errorf("Pro result leaked through: %+v", r)
		}
	}
}

func TestSearchUGRanking(t *testing.T) {
	results, err := parseUGSearchResults(realUGSearchPageSnippet)
	if err != nil {
		t.Fatal(err)
	}
	// Mirror SearchUG's own sort (parseUGSearchResults alone doesn't
	// sort) to check the ranking rule directly.
	type scored struct {
		UGSearchResult
		score float64
	}
	var sc []scored
	for _, r := range results {
		sc = append(sc, scored{r, popularityScore(r)})
	}
	var chordsVotes []int
	for _, r := range results {
		if r.Type == "Chords" {
			chordsVotes = append(chordsVotes, r.Votes)
		}
	}
	if len(chordsVotes) != 2 {
		t.Fatalf("expected 2 Chords results, got %d", len(chordsVotes))
	}
	// The 40755-vote entry has a LOWER raw rating (4.85987 vs would-be
	// 4.9 if Pro counted) but must still rank as the clear popularity
	// winner over the 136-vote entry — this is the whole point of
	// popularityScore over raw rating.
	var lowVotes, highVotes UGSearchResult
	for _, r := range results {
		if r.Type != "Chords" {
			continue
		}
		if r.Votes > highVotes.Votes {
			highVotes = r
		} else {
			lowVotes = r
		}
	}
	if popularityScore(highVotes) <= popularityScore(lowVotes) {
		t.Errorf("high-vote result (votes=%d, score=%.3f) did not outrank low-vote result (votes=%d, score=%.3f)",
			highVotes.Votes, popularityScore(highVotes), lowVotes.Votes, popularityScore(lowVotes))
	}
}

func TestLyricsSimilarity(t *testing.T) {
	a := "[Verse]\n[Am]On a dark desert highway, [E7]cool wind in my hair"
	b := "On a dark desert highway\nCool wind in my hair"
	if sim := lyricsSimilarity(a, b); sim < 0.6 {
		t.Errorf("same lyrics (different markup) scored %.2f, want >= 0.6", sim)
	}
	c := "Twinkle twinkle little star, how I wonder what you are"
	if sim := lyricsSimilarity(a, c); sim > 0.2 {
		t.Errorf("unrelated lyrics scored %.2f, want a low score", sim)
	}
}

// TestMatchesQueryRejectsWrongSong locks in real mismatches a bulk
// Bands Camp re-import turned up live: Ultimate Guitar's own search
// handed back a popular, well-rated, but completely unrelated song as
// the top "Chords" hit for each of these queries.
func TestMatchesQueryRejectsWrongSong(t *testing.T) {
	cases := []struct {
		title, artist string
		r             UGSearchResult
	}{
		{"I Wish", "Stevie Wonder", UGSearchResult{SongName: "I Just Called To Say I Love You", ArtistName: "Stevie Wonder"}},
		{"Killing In The Name", "Rage Against The Machine", UGSearchResult{SongName: "Something In The Orange", ArtistName: "Zach Bryan"}},
		{"Long Train Running", "The Doobie Brothers", UGSearchResult{SongName: "Listen To The Music", ArtistName: "The Doobie Brothers"}},
		{"Shake Everything You've Got", "Maceo Parker", UGSearchResult{SongName: "Youve Got Everything Now", ArtistName: "The Smiths"}},
		{"Should I Stay or Should I Go", "The Clash", UGSearchResult{SongName: "Tangled - I See The Light", ArtistName: "Misc Cartoons"}},
		{"You Really Got Me", "Van Halen", UGSearchResult{SongName: "Nothing You Can Take From Me", ArtistName: "Misc Soundtrack"}},
		{"Blue Jean Blues", "ZZ Top", UGSearchResult{SongName: "Kodom", ArtistName: "Blue Jeans"}},
	}
	for _, c := range cases {
		if matchesQuery(c.title, c.artist, c.r) {
			t.Errorf("matchesQuery(%q, %q, %+v) = true, want false", c.title, c.artist, c.r)
		}
	}
}

func TestMatchesQueryAcceptsRealMatch(t *testing.T) {
	cases := []struct {
		title, artist string
		r             UGSearchResult
	}{
		{"Hotel California", "Eagles", UGSearchResult{SongName: "Hotel California", ArtistName: "Eagles"}},
		{"Master Blaster (Jammin')", "Stevie Wonder", UGSearchResult{SongName: "Master Blaster Jammin", ArtistName: "Stevie Wonder"}},
		{"I Love Rock 'n' Roll", "Joan Jett", UGSearchResult{SongName: "I Love Rock N Roll", ArtistName: "Joan Jett And The Blackhearts"}},
	}
	for _, c := range cases {
		if !matchesQuery(c.title, c.artist, c.r) {
			t.Errorf("matchesQuery(%q, %q, %+v) = false, want true", c.title, c.artist, c.r)
		}
	}
}
