package store

import (
	"context"
	"strings"
	"testing"
)

// A band's votes with stars (band_proposals.go, docs/band-votes.md): the
// tally member by member, what the votes decide under each rule, and
// migration 0127's move of the old yes/veto votes.

func votesMembers() []BandMember {
	return []BandMember{
		{PersonID: 1, DisplayName: "Ann", Status: "core"},
		{PersonID: 2, DisplayName: "Bob", Status: "core"},
		{PersonID: 3, DisplayName: "Cid", Status: "core"},
		{PersonID: 4, DisplayName: "Gus", Status: "guest"},
	}
}

func stars(person int64, n int) ProposalBallot {
	return ProposalBallot{PersonID: person, Name: map[int64]string{1: "Ann", 2: "Bob", 3: "Cid", 4: "Gus", 9: "Old"}[person], Stars: n}
}

func noThanks(person int64) ProposalBallot {
	b := stars(person, 0)
	b.NoThanks = true
	return b
}

func TestTallyCoreMembersOnly(t *testing.T) {
	// Gus is a guest and Old has left: neither counts anywhere.
	ballots := []ProposalBallot{stars(1, 4), stars(2, 1), stars(4, 1), noThanks(9)}
	tl := TallyOf(ballots, votesMembers(), NoThanksBlock, 2)
	if tl.Ratings != 2 || tl.AvgText != "2.5" || tl.Positive != 1 || tl.Negative != 1 {
		t.Errorf("ratings %d avg %q for %d against %d, want 2, 2.5, 1, 1", tl.Ratings, tl.AvgText, tl.Positive, tl.Negative)
	}
	if tl.Voted != 2 || tl.Members != 3 {
		t.Errorf("voted %d/%d, want 2/3 (the core members)", tl.Voted, tl.Members)
	}
	if strings.Join(tl.NotVoted, ",") != "Cid" || tl.Waiting != 1 {
		t.Errorf("not voted %v (waiting %d), want Cid alone: a guest is never waited for", tl.NotVoted, tl.Waiting)
	}
	if len(tl.NoThanks) != 0 || tl.Blocked {
		t.Errorf("a former member's no thanks counts: %+v", tl.NoThanks)
	}
	if tl.Mine == nil || tl.Mine.Stars != 1 {
		t.Errorf("the viewer's own vote: %+v", tl.Mine)
	}
	if tl.Hist != [MaxStars]int{1, 0, 0, 1} {
		t.Errorf("histogram %v", tl.Hist)
	}
	// Not voted is never 0 stars: no ballot, no rating.
	if none := TallyOf(nil, votesMembers(), NoThanksBlock, 1); none.Ratings != 0 || none.Avg != 0 || none.AvgText != "" || none.Mine != nil || none.Voted != 0 || none.Members != 3 {
		t.Errorf("no votes: %+v", none)
	}
	// A core member's no thanks blocks, apart from the stars.
	b := TallyOf([]ProposalBallot{stars(1, 4), noThanks(2)}, votesMembers(), NoThanksBlock, 0)
	if !b.Blocked || strings.Join(b.BlockedBy, ",") != "Bob" || b.Ratings != 1 || b.Avg != 4 || b.Voted != 2 {
		t.Errorf("Bob's no thanks: %+v", b)
	}
	if c := TallyOf([]ProposalBallot{noThanks(2)}, votesMembers(), NoThanksCount, 0); c.Blocked {
		t.Error("under the count rule a no thanks doesn't block")
	}
}

func TestProposalOutcome(t *testing.T) {
	m := votesMembers()
	for _, c := range []struct {
		name    string
		rule    string
		ballots []ProposalBallot
		want    string
	}{
		{"one core member still to vote", NoThanksBlock, []ProposalBallot{stars(1, 4), stars(2, 4)}, ""},
		{"a guest isn't waited for", NoThanksBlock, []ProposalBallot{stars(1, 4), stars(2, 3), stars(3, 3)}, "accepted"},
		{"all for it", NoThanksBlock, []ProposalBallot{stars(1, 3), stars(2, 3), stars(3, 3)}, "accepted"},
		{"against it", NoThanksBlock, []ProposalBallot{stars(1, 2), stars(2, 2), stars(3, 3)}, "rejected"},
		{"average 2.5 but more against than for", NoThanksBlock, []ProposalBallot{stars(1, 4), stars(2, 2), stars(3, 2)}, "rejected"},
		{"average 2.67, more for", NoThanksBlock, []ProposalBallot{stars(1, 3), stars(2, 3), stars(3, 2)}, "accepted"},
		{"a core no thanks holds it back", NoThanksBlock, []ProposalBallot{stars(1, 4), stars(2, 4), noThanks(3)}, ""},
		{"a core no thanks vetoes", NoThanksVeto, []ProposalBallot{stars(1, 4), noThanks(3)}, "rejected"},
		{"a no thanks counts against", NoThanksCount, []ProposalBallot{stars(1, 4), stars(2, 3), noThanks(3)}, "accepted"},
		{"no thanks against two for", NoThanksCount, []ProposalBallot{stars(1, 4), noThanks(2), noThanks(3)}, "rejected"},
		{"most core members say no thanks", NoThanksCount, []ProposalBallot{noThanks(1), noThanks(2)}, "rejected"},
		{"a guest's vote decides nothing", NoThanksVeto, []ProposalBallot{noThanks(4), stars(1, 3)}, ""},
	} {
		if got := proposalOutcome(c.rule, m, c.ballots); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// TestMigration0127 runs the migration on votes cast as yes/veto: yes is
// 3 stars, veto no thanks, the old votes kept in the history, a guest's
// out of the votes standing but in the history.
func TestMigration0127(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := NewBandFixture(t, st)
	second, err := st.CreatePerson(ctx, "second", "Second", "second@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBandMember(ctx, f.Band, second, "core", f.Core); err != nil {
		t.Fatal(err)
	}
	pid, err := st.CreateProposal(ctx, f.Band, f.Core, "new_song", f.CatalogSong, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := migrationsFS.ReadFile("migrations/0127_band_votes.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// Back to before 0127, in a transaction rolled back after.
	for _, q := range []string{
		`DROP TABLE listening_party_now`,
		`DROP TABLE proposal_vote_log`,
		`ALTER TABLE bands DROP COLUMN proposal_no_thanks`,
		`ALTER TABLE proposal_votes DROP CONSTRAINT proposal_votes_stars_or_no_thanks`,
		`ALTER TABLE proposal_votes DROP COLUMN stars, DROP COLUMN no_thanks, DROP COLUMN legacy_vote`,
		`ALTER TABLE proposal_votes ADD COLUMN vote proposal_vote_kind NOT NULL`,
	} {
		if _, err := tx.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO proposal_votes (proposal_id, person_id, vote, cast_at) VALUES
		($1, $2, 'yes', now() - interval '2 days'), ($1, $3, 'veto', now() - interval '1 day'), ($1, $4, 'yes', now())`,
		pid, f.Core, second, f.Guest); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(raw)); err != nil {
		t.Fatalf("migration 0127: %v", err)
	}
	type vote struct {
		stars    int
		noThanks bool
		legacy   string
	}
	got := map[int64]vote{}
	rows, err := tx.Query(ctx, `SELECT person_id, coalesce(stars, 0), no_thanks, legacy_vote::text FROM proposal_votes WHERE proposal_id = $1`, pid)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var p int64
		var v vote
		if err := rows.Scan(&p, &v.stars, &v.noThanks, &v.legacy); err != nil {
			t.Fatal(err)
		}
		got[p] = v
	}
	rows.Close()
	if got[f.Core] != (vote{3, false, "yes"}) || got[second] != (vote{0, true, "veto"}) {
		t.Errorf("the votes standing: %+v, want yes → 3 stars, veto → no thanks", got)
	}
	if _, ok := got[f.Guest]; ok || len(got) != 2 {
		t.Errorf("a guest's vote still stands: %+v", got)
	}
	var logged, guestLogged int
	if err := tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE person_id = $2) FROM proposal_vote_log WHERE proposal_id = $1 AND legacy_vote IS NOT NULL`, pid, f.Guest).Scan(&logged, &guestLogged); err != nil {
		t.Fatal(err)
	}
	if logged != 3 || guestLogged != 1 {
		t.Errorf("history: %d old votes (%d the guest's), want 3 (1)", logged, guestLogged)
	}
	var rule string
	if err := tx.QueryRow(ctx, `SELECT proposal_no_thanks FROM bands WHERE id = $1`, f.Band).Scan(&rule); err != nil || rule != NoThanksBlock {
		t.Errorf("the band's rule: %q %v, want block", rule, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO proposal_votes (proposal_id, person_id, stars, no_thanks) VALUES ($1, $2, 5, false)`, pid, f.Guest); err == nil {
		t.Error("5 stars taken: the scale is 1 to 4")
	}
}
