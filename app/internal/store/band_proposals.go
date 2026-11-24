package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"app/internal/setlist"
)

// A band's proposals and their votes (docs/band-votes.md).
//
// A member proposes a song for the band's songbook (kind 'new_song': the
// song found in the catalogue, or made for the band from a YouTube link,
// its lyrics and grids fetched) or another recording of a song the band
// plays ('new_reference': the link becomes the song's reference). Each
// core member then rates it 1 to 4 stars — an even scale, so a vote is never
// right in the middle: 3 and 4 are for it, 1 and 2 against —, or says
// "no thanks" (a refusal, not a star), or hasn't voted yet — never shown
// as 0 stars. They change their vote, or take it back, until the
// proposal is decided.
//
// The tool helps the band decide; it never decides nor enforces: the
// votes never accept or turn down a proposal by themselves. Any current
// member (core or guest) decides by hand — adds the song (to the
// songbook, and its set list when that was the target) or turns it
// down — whatever the votes, a no thanks standing included. A no thanks
// is a veto in what it says — the strongest no, shown first, with its
// sign, the proposal flagged — not a lock. The tally draws no verdict:
// the average, the counts, the no thanks and who hasn't voted, sorted;
// the band decides. Only the members whose role grants vote (the core
// members, by default) are counted — the average, for and against, "n
// of m voted", the order; anyone else in the band (a guest) may vote
// too, their votes listed apart, never counted (ProposalTally.Guests).
//
// No one sees how the others voted before voting (ProposalTally.BlindFor,
// ProposalFilter.Viewer): until then only how many voting members voted,
// of how many. Taking one's vote back hides them again; a decided
// proposal shows everything to everyone.

// ErrAlreadyDecided is returned by AcceptProposal/RejectProposal and
// CastVote when the proposal is no longer 'open'.
var ErrAlreadyDecided = fmt.Errorf("proposal is already decided")

// ErrArchivedNoVote is CastVote's refusal in an archived band (read
// only: docs/band-roles.md).
var ErrArchivedNoVote = fmt.Errorf("the band is archived: %w", ErrForbidden)

// Who may do what with a band's proposals and pools: today's rules, each
// in one place, to switch to the band's roles (HasBandPrivilege) without
// touching their callers.

// MayVote says whether personID's votes on bandID's proposals count —
// in their average, counts, "n of m voted" and order: their role grants
// vote (docs/band-roles.md; by default, the core members). Any other
// member votes too (MayCastVote), listed apart.
func (st *Store) MayVote(ctx context.Context, bandID, personID int64) (bool, error) {
	return st.HasBandPrivilege(ctx, bandID, personID, BandPrivVote)
}

// MayCastVote says whether personID rates bandID's proposals at all: any
// current member of a band that isn't archived. Whether it counts is
// MayVote's.
func (st *Store) MayCastVote(ctx context.Context, bandID, personID int64) (bool, error) {
	member, err := st.IsCurrentBandMember(ctx, bandID, personID)
	if err != nil || !member {
		return false, err
	}
	archived, err := st.BandArchived(ctx, bandID)
	return !archived, err
}

// MayDecideProposal says whether personID adds or turns down bandID's
// proposals by hand: their role grants decide_proposals (by default, any
// current member; a band may keep it to a staff).
func (st *Store) MayDecideProposal(ctx context.Context, bandID, personID int64) (bool, error) {
	return st.HasBandPrivilege(ctx, bandID, personID, BandPrivDecideProposals)
}

// MayPropose says whether personID proposes songs to bandID: their role
// grants propose (by default, any current member).
func (st *Store) MayPropose(ctx context.Context, bandID, personID int64) (bool, error) {
	return st.HasBandPrivilege(ctx, bandID, personID, BandPrivPropose)
}

// MayShareListen says whether personID adds songs to bandID's Discover
// and Influences: proposing's privilege, propose.
func (st *Store) MayShareListen(ctx context.Context, bandID, personID int64) (bool, error) {
	return st.MayPropose(ctx, bandID, personID)
}

// MaxStars is the top of the scale: 1 and 2 are against, 3 and 4 for.
const MaxStars = 4

// BandProposal is one row of band_proposals, with its song, its
// proposer's and decider's names, what its payload says, and its votes'
// totals.
type BandProposal struct {
	ID                int64
	BandID            int64
	ProposerPersonID  int64
	ProposerUsername  string
	ProposerName      string
	Kind              string // "new_song" or "new_reference"
	TargetVariantID   *int64
	TargetTitle       string
	TargetArtist      string
	TargetVersionName string
	TargetReference   string
	Payload           json.RawMessage
	Status            string // "open", "accepted", "rejected"
	CreatedAt         time.Time
	DecidedAt         *time.Time
	DecidedByPersonID *int64
	DecidedByUsername string

	// The votes standing: how many rated it, their stars' sum, how many
	// gave 3 or 4 stars (Positive) and 1 or 2 (Negative), how many said
	// no thanks.
	Ratings  int
	StarSum  int
	Positive int
	Negative int
	NoThanks int
	// Hidden: the viewer hasn't voted on this open proposal (or doesn't
	// vote), so the votes' totals above are blanked (BlindFor): no one
	// sees how the others voted before voting.
	Hidden bool

	// From the payload.
	Note      string
	Target    string // "repertoire" or "setlist"
	SetlistID int64
	URL       string // a new_reference's recording
}

// AvgStars is the average of the stars given, 0 when no one rated it.
func (p BandProposal) AvgStars() float64 {
	if p.Ratings == 0 {
		return 0
	}
	return float64(p.StarSum) / float64(p.Ratings)
}

// AvgText is the average with one decimal ("4.2"), "" when no one rated.
func (p BandProposal) AvgText() string {
	if p.Ratings == 0 {
		return ""
	}
	return fmt.Sprintf("%.1f", math.Round(p.AvgStars()*10)/10)
}

// VideoID is the YouTube id of what the proposal plays: a new
// reference's link, else its song's reference.
func (p BandProposal) VideoID() string {
	if p.URL != "" {
		if id := YouTubeID(p.URL); id != "" {
			return id
		}
	}
	return YouTubeID(p.TargetReference)
}

// ProposalPayload is band_proposals.payload: the note "why this song",
// where it should go once accepted, and a new reference's link.
type ProposalPayload struct {
	URL       string `json:"url,omitempty"`
	Note      string `json:"note,omitempty"`
	Target    string `json:"target,omitempty"` // "repertoire" (default) or "setlist"
	SetlistID int64  `json:"setlistId,omitempty"`
}

// NewReferencePayload is the payload of a 'new_reference' proposal (its
// link and a note): ProposalPayload's older name.
type NewReferencePayload = ProposalPayload

// BandProposalSnapshot is the entity_type 'band_proposal' snapshot shape
// recorded by RecordRevision — same Exists convention as
// BandMemberSnapshot/BandCatalogEntrySnapshot.
type BandProposalSnapshot struct {
	Exists            bool            `json:"exists"`
	BandID            int64           `json:"band_id"`
	ProposerPersonID  int64           `json:"proposer_person_id"`
	Kind              string          `json:"kind"`
	TargetVariantID   *int64          `json:"target_variant_id"`
	Payload           json.RawMessage `json:"payload"`
	Status            string          `json:"status"`
	CreatedAt         time.Time       `json:"created_at"`
	DecidedAt         *time.Time      `json:"decided_at"`
	DecidedByPersonID *int64          `json:"decided_by_person_id"`
}

// CreateProposal records a new 'new_song' or 'new_reference' proposal of
// targetVariantID: the song for the songbook, or the band's song the
// recording is for. A member whose role grants propose (MayPropose; by
// default, any current member). A set list target must be the band's.
//
// It notifies no one (band_proposals_notify.go): a new proposal shows on
// the band's To vote chip and in its "what's new".
func (st *Store) CreateProposal(ctx context.Context, bandID, proposerID int64, kind string, targetVariantID int64, payload json.RawMessage) (int64, error) {
	if kind != "new_song" && kind != "new_reference" {
		return 0, fmt.Errorf("invalid proposal kind %q: %w", kind, ErrInvalid)
	}
	if targetVariantID == 0 {
		return 0, fmt.Errorf("a proposal must reference an existing variant id: %w", ErrInvalid)
	}
	if may, err := st.MayPropose(ctx, bandID, proposerID); err != nil {
		return 0, err
	} else if !may {
		return 0, fmt.Errorf("person %d may not propose songs to band %d: %w", proposerID, bandID, ErrForbidden)
	}
	if len(payload) == 0 || string(payload) == "null" {
		payload = json.RawMessage("{}")
	}
	var pl ProposalPayload
	if err := json.Unmarshal(payload, &pl); err != nil {
		return 0, fmt.Errorf("invalid payload: %v: %w", err, ErrInvalid)
	}
	pl.Note = strings.TrimSpace(pl.Note)
	if len(pl.Note) > 500 {
		return 0, fmt.Errorf("a note is 500 characters at most: %w", ErrInvalid)
	}
	pl.URL = strings.TrimSpace(pl.URL)
	if kind == "new_reference" && pl.URL == "" {
		return 0, fmt.Errorf("a new recording needs its link: %w", ErrInvalid)
	}
	switch pl.Target {
	case "", "repertoire":
		pl.Target, pl.SetlistID = "repertoire", 0
	case "setlist":
		if kind != "new_song" || pl.SetlistID == 0 {
			return 0, fmt.Errorf("a set list target needs a new song and its set list: %w", ErrInvalid)
		}
		meta, err := st.GetSetlistMeta(ctx, pl.SetlistID)
		if err != nil {
			return 0, err
		}
		if meta.OwnerBandID == nil || *meta.OwnerBandID != bandID {
			return 0, fmt.Errorf("set list %d isn't the band's: %w", pl.SetlistID, ErrForbidden)
		}
	default:
		return 0, fmt.Errorf("invalid target %q: %w", pl.Target, ErrInvalid)
	}
	payload, _ = json.Marshal(pl)

	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO band_proposals (band_id, proposer_person_id, kind, target_variant_id, payload)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		bandID, proposerID, kind, targetVariantID, payload).Scan(&id); err != nil {
		return 0, err
	}

	actor := &proposerID
	if err := st.RecordRevision(ctx, tx, "band_proposal", fmt.Sprint(id), actor, BandProposalSnapshot{Exists: false}); err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return id, nil
}

// CreateProposedSong makes a song for a proposal to the band: the band's,
// private, in none of its songbooks until the proposal is accepted (its
// members see and fix it meanwhile: song_visible, SongRightsFor). Its
// chart is the title, artist and reference; the lyrics and grids come
// after (the HTTP layer's draft).
func (st *Store) CreateProposedSong(ctx context.Context, personID, bandID int64, title, artist, referenceURL string) (int64, error) {
	title, artist, referenceURL = strings.TrimSpace(title), strings.TrimSpace(artist), strings.TrimSpace(referenceURL)
	if title == "" {
		return 0, fmt.Errorf("a song needs a title: %w", ErrInvalid)
	}
	ok, err := st.MayPropose(ctx, bandID, personID)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrForbidden
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	workID, err := st.findOrCreateWork(ctx, tx, title, artist)
	if err != nil {
		return 0, err
	}
	var b strings.Builder
	b.WriteString("title: " + title + "\n")
	if artist != "" {
		b.WriteString("artist: " + artist + "\n")
	}
	if referenceURL != "" {
		b.WriteString("reference: " + referenceURL + "\n")
	}
	b.WriteString("\n[Verse]\n")
	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO songs (title, artist, reference_url, body, work_id, owner_band_id, created_by_person_id, public)
		VALUES ($1, $2, $3, $4, $5, $6, $7, false) RETURNING id`,
		title, artist, referenceURL, b.String(), workID, bandID, personID).Scan(&id); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// ProposalFilter narrows and orders ListProposals. Zero values mean "no
// filter", newest first.
type ProposalFilter struct {
	Status string // "open", "accepted", "rejected", or "decided" (either)
	Kind   string // "new_song", "new_reference"
	Sort   string // "stars" (average, best first; vetoed ones after), "no_thanks" (most first), "newest" or ""; turned-down ones last in each
	// Viewer: the person the list is for. An open proposal they haven't
	// voted on comes with its totals blanked (Hidden), and sorts apart:
	// first, newest first, before the ones sorted by their votes. 0: an
	// internal list (counts), nothing blanked.
	Viewer int64
}

const proposalCols = `
	bp.id, bp.band_id, bp.proposer_person_id, p.username, coalesce(nullif(p.display_name, ''), p.username),
	bp.kind::text, bp.target_variant_id,
	coalesce(s.title, ''), coalesce(s.artist, ''), coalesce(s.version_name, ''), coalesce(s.reference_url, ''),
	bp.payload, bp.status::text, bp.created_at, bp.decided_at, bp.decided_by_person_id, coalesce(dp.username, ''),
	coalesce(v.ratings, 0), coalesce(v.star_sum, 0), coalesce(v.positive, 0), coalesce(v.negative, 0), coalesce(v.no_thanks, 0)`

// seenSQL is whether person (the query's parameter $n) sees a proposal's
// votes: it's decided, or they voted on it — counted or listed apart —
// and are still in the band.
func seenSQL(n int) string {
	return fmt.Sprintf(`(bp.status <> 'open' OR EXISTS (SELECT 1 FROM proposal_votes mv
		JOIN band_members mbm ON mbm.band_id = bp.band_id AND mbm.person_id = mv.person_id AND upper_inf(mbm.validity)
		WHERE mv.proposal_id = bp.id AND mv.person_id = $%d))`, n)
}

const proposalFrom = `
	FROM band_proposals bp
	JOIN people p ON p.id = bp.proposer_person_id
	LEFT JOIN songs s ON s.id = bp.target_variant_id
	LEFT JOIN people dp ON dp.id = bp.decided_by_person_id
	LEFT JOIN LATERAL (
		SELECT count(stars)::int AS ratings, coalesce(sum(stars), 0)::int AS star_sum,
		       count(*) FILTER (WHERE stars >= 3)::int AS positive, count(*) FILTER (WHERE stars <= 2)::int AS negative,
		       count(*) FILTER (WHERE no_thanks)::int AS no_thanks
		FROM proposal_votes pv WHERE pv.proposal_id = bp.id
		  AND has_band_privilege(bp.band_id, pv.person_id, 'vote')) v ON true`

func scanProposal(row pgx.Row) (*BandProposal, error) {
	var p BandProposal
	if err := row.Scan(&p.ID, &p.BandID, &p.ProposerPersonID, &p.ProposerUsername, &p.ProposerName,
		&p.Kind, &p.TargetVariantID,
		&p.TargetTitle, &p.TargetArtist, &p.TargetVersionName, &p.TargetReference,
		&p.Payload, &p.Status, &p.CreatedAt, &p.DecidedAt, &p.DecidedByPersonID, &p.DecidedByUsername,
		&p.Ratings, &p.StarSum, &p.Positive, &p.Negative, &p.NoThanks, &p.Hidden); err != nil {
		return nil, err
	}
	p.Hidden = !p.Hidden // scanned as "seen"
	if p.Hidden {
		p.Ratings, p.StarSum, p.Positive, p.Negative, p.NoThanks = 0, 0, 0, 0, 0
	}
	var pl ProposalPayload
	_ = json.Unmarshal(p.Payload, &pl)
	p.Note, p.Target, p.SetlistID, p.URL = pl.Note, pl.Target, pl.SetlistID, pl.URL
	if p.Target == "" {
		p.Target = "repertoire"
	}
	return &p, nil
}

// ListProposals lists bandID's proposals matching filter, in its order,
// each with its votes' totals.
func (st *Store) ListProposals(ctx context.Context, bandID int64, filter ProposalFilter) ([]BandProposal, error) {
	args := []any{bandID}
	seen := "true"
	if filter.Viewer != 0 {
		args = append(args, filter.Viewer)
		seen = seenSQL(len(args))
	}
	q := `SELECT ` + proposalCols + `, ` + seen + ` AS seen` + proposalFrom + ` WHERE bp.band_id = $1`
	switch filter.Status {
	case "":
	case "decided":
		q += " AND bp.status <> 'open'"
	default:
		args = append(args, filter.Status)
		q += fmt.Sprintf(" AND bp.status = $%d::band_proposal_status", len(args))
	}
	if filter.Kind != "" {
		args = append(args, filter.Kind)
		q += fmt.Sprintf(" AND bp.kind = $%d::band_proposal_kind", len(args))
	}
	// The ones the viewer hasn't voted on first, newest first: their
	// place among the others would tell how those were voted.
	first := ""
	if filter.Viewer != 0 {
		blind := "NOT " + seen
		first = blind + ` DESC, CASE WHEN ` + blind + ` THEN bp.created_at END DESC, `
	}
	// A turned-down proposal last, whatever the order.
	const rejectedLast = `(bp.status = 'rejected'), `
	first = rejectedLast + first
	switch filter.Sort {
	case "stars":
		q += ` ORDER BY ` + first + `(coalesce(v.no_thanks, 0) > 0), (coalesce(v.ratings, 0) > 0) DESC, v.star_sum::float / nullif(v.ratings, 0) DESC NULLS LAST,
		              v.ratings DESC, coalesce(v.no_thanks, 0), bp.created_at DESC, bp.id DESC`
	case "no_thanks":
		q += ` ORDER BY ` + first + `coalesce(v.no_thanks, 0) DESC, bp.created_at DESC, bp.id DESC`
	default:
		q += ` ORDER BY ` + rejectedLast + `bp.created_at DESC, bp.id DESC`
	}

	rows, err := st.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BandProposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// GetProposal loads one proposal by id, same shape ListProposals rows
// have.
func (st *Store) GetProposal(ctx context.Context, proposalID int64) (*BandProposal, error) {
	p, err := scanProposal(st.db.QueryRow(ctx, `SELECT `+proposalCols+`, true`+proposalFrom+` WHERE bp.id = $1`, proposalID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such proposal %d: %w", proposalID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// ProposalBallot is one member's standing vote on a proposal: their
// stars, or their no thanks.
type ProposalBallot struct {
	ProposalID int64     `json:"-"`
	PersonID   int64     `json:"personId"`
	Username   string    `json:"-"`
	Name       string    `json:"name"`
	Stars      int       `json:"stars,omitempty"`
	NoThanks   bool      `json:"noThanks,omitempty"`
	CastAt     time.Time `json:"castAt"`
}

// ProposalBallots are the standing votes on each of proposalIDs.
func (st *Store) ProposalBallots(ctx context.Context, proposalIDs []int64) (map[int64][]ProposalBallot, error) {
	out := map[int64][]ProposalBallot{}
	if len(proposalIDs) == 0 {
		return out, nil
	}
	rows, err := st.db.Query(ctx, `
		SELECT v.proposal_id, v.person_id, p.username, coalesce(nullif(p.display_name, ''), p.username),
		       coalesce(v.stars, 0), v.no_thanks, v.cast_at
		FROM proposal_votes v JOIN people p ON p.id = v.person_id
		WHERE v.proposal_id = ANY($1)
		ORDER BY v.cast_at`, proposalIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var b ProposalBallot
		if err := rows.Scan(&b.ProposalID, &b.PersonID, &b.Username, &b.Name, &b.Stars, &b.NoThanks, &b.CastAt); err != nil {
			return nil, err
		}
		out[b.ProposalID] = append(out[b.ProposalID], b)
	}
	return out, rows.Err()
}

// ProposalVoteEntry is one line of a proposal's vote history.
type ProposalVoteEntry struct {
	PersonID   int64
	Name       string
	Stars      int
	NoThanks   bool
	LegacyVote string // "yes" or "veto": a vote cast before stars (migration 0127)
	CastAt     time.Time
}

// ProposalVoteLog is every vote cast on proposalID, changed or taken
// back, newest first.
func (st *Store) ProposalVoteLog(ctx context.Context, proposalID int64) ([]ProposalVoteEntry, error) {
	rows, err := st.db.Query(ctx, `
		SELECT l.person_id, coalesce(nullif(p.display_name, ''), p.username), coalesce(l.stars, 0), l.no_thanks,
		       coalesce(l.legacy_vote::text, ''), l.cast_at
		FROM proposal_vote_log l JOIN people p ON p.id = l.person_id
		WHERE l.proposal_id = $1
		ORDER BY l.cast_at DESC, l.id DESC`, proposalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProposalVoteEntry
	for rows.Next() {
		var e ProposalVoteEntry
		if err := rows.Scan(&e.PersonID, &e.Name, &e.Stars, &e.NoThanks, &e.LegacyVote, &e.CastAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ProposalTally is where a proposal's vote stands, member by member.
type ProposalTally struct {
	Ratings  int              `json:"ratings"`
	Avg      float64          `json:"avg"`
	AvgText  string           `json:"avgText"`
	Hist     [MaxStars]int    `json:"hist"`     // how many gave 1…4 stars
	Positive int              `json:"positive"` // 3 or 4 stars
	Negative int              `json:"negative"` // 1 or 2 stars
	Rated    []ProposalBallot `json:"rated"`
	NoThanks []ProposalBallot `json:"noThanks"`
	NotVoted []string         `json:"notVoted"`
	// Guests: the votes of the members whose role doesn't count (by
	// default, the guests), stars first, then no thanks — listed apart,
	// in nothing above nor below.
	Guests []ProposalBallot `json:"guests"`
	// Vetoed: a voting member said no thanks — the strongest no, shown
	// first; it decides nothing by itself.
	Vetoed bool `json:"vetoed"`
	// Voted of Members: how many of the band's voting members (their
	// role grants vote) have voted — stars or no thanks; information
	// only, no quorum.
	Voted   int `json:"voted"`
	Members int `json:"members"`
	// Mine: the viewer's own vote, nil when they haven't voted.
	Mine *ProposalBallot `json:"mine"`
	// Hidden: the votes are blanked for the viewer (BlindFor).
	Hidden bool `json:"hidden,omitempty"`
}

// BlindFor is t as its viewer may see it: everything once the proposal
// is decided or once they've voted on it (stars or no thanks, counted
// or a guest's); before that only how many voting members voted, of how
// many — no average, no split, no names, no one's no thanks, no guest's
// vote. Taking one's vote back hides them again.
func (t ProposalTally) BlindFor(status string) ProposalTally {
	if status != "open" || t.Mine != nil {
		return t
	}
	return ProposalTally{Voted: t.Voted, Members: t.Members, Hidden: true,
		Rated: []ProposalBallot{}, NoThanks: []ProposalBallot{}, NotVoted: []string{}, Guests: []ProposalBallot{}}
}

func memberName(m BandMember) string {
	if m.DisplayName != "" {
		return m.DisplayName
	}
	return m.Username
}

// TallyOf is where a proposal's vote stands with ballots, among the
// band's current members, as viewer sees it. Only the members whose
// role votes (BandMember.Votes; by default the core members) count, and
// "n of m voted" counts them only; another current member's ballot (a
// guest's) is listed apart (Guests), in nothing else; a former member's
// is left out.
func TallyOf(ballots []ProposalBallot, members []BandMember, viewer int64) ProposalTally {
	t := ProposalTally{Rated: []ProposalBallot{}, NoThanks: []ProposalBallot{}, NotVoted: []string{}, Guests: []ProposalBallot{}}
	core := map[int64]bool{}
	current := map[int64]bool{}
	for _, m := range members {
		current[m.PersonID] = true
		if m.Votes {
			core[m.PersonID] = true
		}
	}
	voted := map[int64]bool{}
	sum := 0
	for _, b := range ballots {
		if !core[b.PersonID] {
			if current[b.PersonID] && (b.NoThanks || b.Stars >= 1 && b.Stars <= MaxStars) {
				t.Guests = append(t.Guests, b)
				if b.PersonID == viewer {
					mine := b
					t.Mine = &mine
				}
			}
			continue
		}
		voted[b.PersonID] = true
		if b.PersonID == viewer {
			mine := b
			t.Mine = &mine
		}
		if b.NoThanks {
			t.NoThanks = append(t.NoThanks, b)
			t.Vetoed = true
			continue
		}
		if b.Stars >= 1 && b.Stars <= MaxStars {
			t.Rated = append(t.Rated, b)
			t.Hist[b.Stars-1]++
			sum += b.Stars
			if b.Stars*2 > MaxStars {
				t.Positive++
			} else {
				t.Negative++
			}
		}
	}
	sort.SliceStable(t.Rated, func(i, j int) bool { return t.Rated[i].Stars > t.Rated[j].Stars })
	// The guests' stars, best first, then their no thanks.
	sort.SliceStable(t.Guests, func(i, j int) bool {
		if t.Guests[i].NoThanks != t.Guests[j].NoThanks {
			return !t.Guests[i].NoThanks
		}
		return t.Guests[i].Stars > t.Guests[j].Stars
	})
	t.Ratings = len(t.Rated)
	if t.Ratings > 0 {
		t.Avg = float64(sum) / float64(t.Ratings)
		t.AvgText = fmt.Sprintf("%.1f", math.Round(t.Avg*10)/10)
	}
	for _, m := range members {
		if !m.Votes {
			continue
		}
		t.Members++
		if voted[m.PersonID] {
			t.Voted++
		} else {
			t.NotVoted = append(t.NotVoted, memberName(m))
		}
	}
	return t
}

// ProposalTallyFor is p's tally as viewer sees it (one proposal: the
// votes, the members and the rule loaded), blanked until they vote
// (BlindFor).
func (st *Store) ProposalTallyFor(ctx context.Context, p *BandProposal, viewer int64) (ProposalTally, error) {
	t, err := st.proposalTally(ctx, p, viewer)
	return t.BlindFor(p.Status), err
}

// proposalTally is p's whole tally, nothing blanked: the rules' own.
func (st *Store) proposalTally(ctx context.Context, p *BandProposal, viewer int64) (ProposalTally, error) {
	ballots, err := st.ProposalBallots(ctx, []int64{p.ID})
	if err != nil {
		return ProposalTally{}, err
	}
	members, err := st.ListBandMembers(ctx, p.BandID, false)
	if err != nil {
		return ProposalTally{}, err
	}
	return TallyOf(ballots[p.ID], members, viewer), nil
}

// decideProposal locks proposalID, checks it's still 'open', records the
// pre-decide revision, and flips it to newStatus ("accepted"/"rejected").
// decidedBy is nil for a decision of the votes and non-nil for one by
// hand. It runs after the decision's side effect (the song added), so a
// failed one leaves the proposal open, to try again.
func (st *Store) decideProposal(ctx context.Context, proposalID int64, newStatus string, decidedBy *int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var snap BandProposalSnapshot
	var kind string
	err = tx.QueryRow(ctx, `
		SELECT band_id, proposer_person_id, kind::text, target_variant_id, payload, status::text, created_at, decided_at, decided_by_person_id
		FROM band_proposals WHERE id = $1 FOR UPDATE`, proposalID).
		Scan(&snap.BandID, &snap.ProposerPersonID, &kind, &snap.TargetVariantID, &snap.Payload, &snap.Status, &snap.CreatedAt, &snap.DecidedAt, &snap.DecidedByPersonID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("no such proposal %d: %w", proposalID, ErrNotFound)
	}
	if err != nil {
		return err
	}
	if snap.Status != "open" {
		return ErrAlreadyDecided
	}
	snap.Exists = true
	snap.Kind = kind

	if err := st.RecordRevision(ctx, tx, "band_proposal", fmt.Sprint(proposalID), decidedBy, snap); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE band_proposals SET status = $1, decided_at = now(), decided_by_person_id = $2 WHERE id = $3`,
		newStatus, decidedBy, proposalID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// applyAcceptEffect does what accepting p means: a new song goes in the
// band's songbook — a link when it's a public song, the band's own
// otherwise (LinkCheck: another band's private song is refused) — and in
// its set list when that was its target; a new recording becomes the
// song's reference, when the one accepting may change that song.
func (st *Store) applyAcceptEffect(ctx context.Context, p *BandProposal, actorPersonID int64) error {
	if actorPersonID == 0 {
		// Accepted by the votes: on the proposer's behalf.
		actorPersonID = p.ProposerPersonID
	}
	switch p.Kind {
	case "new_song":
		if p.TargetVariantID == nil {
			return fmt.Errorf("new_song proposal %d has no target variant", p.ID)
		}
		sbID, err := st.GetDefaultSongbook(ctx, "band", p.BandID)
		if err != nil {
			return err
		}
		sb, err := st.GetSongbook(ctx, sbID)
		if err != nil {
			return err
		}
		if err := st.LinkCheck(ctx, actorPersonID, *p.TargetVariantID, sb); err != nil {
			return err
		}
		if err := st.AddToSongbook(ctx, sbID, *p.TargetVariantID, actorPersonID); err != nil {
			return err
		}
		if p.Target == "setlist" && p.SetlistID != 0 {
			return st.appendToBandSetlist(ctx, p.BandID, p.SetlistID, *p.TargetVariantID, actorPersonID)
		}
		return nil

	case "new_reference":
		if p.TargetVariantID == nil {
			return fmt.Errorf("new_reference proposal %d has no target variant", p.ID)
		}
		if p.URL == "" {
			return fmt.Errorf("new_reference proposal %d has no url in its payload", p.ID)
		}
		if ok, err := st.CanEditVariant(ctx, actorPersonID, *p.TargetVariantID); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("person %d may not change song %d: %w", actorPersonID, *p.TargetVariantID, ErrForbidden)
		}
		sg, version, err := st.LoadVariant(*p.TargetVariantID)
		if err != nil {
			return err
		}
		sg.Set("reference", p.URL)
		if _, err := st.SaveVariant(*p.TargetVariantID, sg, version, actorPersonID); err != nil {
			return err
		}
		return nil

	default:
		return fmt.Errorf("unknown proposal kind %q", p.Kind)
	}
}

// appendToBandSetlist puts songID at the end of bandID's set list
// setlistID (its last set), unless it's in it already.
func (st *Store) appendToBandSetlist(ctx context.Context, bandID, setlistID, songID, actor int64) error {
	l, meta, version, err := st.LoadSetlistByID(ctx, setlistID)
	if err != nil {
		return err
	}
	if meta.OwnerBandID == nil || *meta.OwnerBandID != bandID {
		return fmt.Errorf("set list %d isn't band %d's: %w", setlistID, bandID, ErrForbidden)
	}
	for _, set := range l.Sets {
		for _, e := range set.Entries {
			if e.VariantID == songID {
				return nil
			}
		}
	}
	if len(l.Sets) == 0 {
		l.Sets = []setlist.Set{{Label: "Set 1"}}
	}
	last := &l.Sets[len(l.Sets)-1]
	last.Entries = append(last.Entries, setlist.Entry{VariantID: songID})
	_, err = st.SaveSetlistByID(ctx, setlistID, l, meta.Title, meta.Notes, version, actor)
	return err
}

// AcceptProposal adds proposalID's song by hand: any current member of
// its band, whatever the votes — a no thanks standing included: the
// votes are information. Its effect is applied, then the proposer
// notified.
func (st *Store) AcceptProposal(ctx context.Context, proposalID, actorPersonID int64) error {
	return st.decideAndNotify(ctx, proposalID, "accepted", &actorPersonID)
}

// RejectProposal turns proposalID down by hand (any current member of
// its band): no effect, the proposer notified.
func (st *Store) RejectProposal(ctx context.Context, proposalID, actorPersonID int64) error {
	return st.decideAndNotify(ctx, proposalID, "rejected", &actorPersonID)
}

// decideAndNotify is AcceptProposal/RejectProposal's shared body
// (decidedBy nil: a decision with no one's name on it, which nothing
// makes since the votes stopped deciding).
func (st *Store) decideAndNotify(ctx context.Context, proposalID int64, newStatus string, decidedBy *int64) error {
	p, err := st.GetProposal(ctx, proposalID)
	if err != nil {
		return err
	}
	if p.Status != "open" {
		return ErrAlreadyDecided
	}

	// By hand: a current member of the PROPOSAL's own band (not
	// whatever band the HTTP path carried).
	if decidedBy != nil {
		may, err := st.MayDecideProposal(ctx, p.BandID, *decidedBy)
		if err != nil {
			return err
		}
		if !may {
			return fmt.Errorf("person %d is not a current member of band %d: %w", *decidedBy, p.BandID, ErrForbidden)
		}
	}

	if newStatus == "accepted" {
		var actor int64
		if decidedBy != nil {
			actor = *decidedBy
		}
		if err := st.applyAcceptEffect(ctx, p, actor); err != nil {
			return err
		}
	}

	if err := st.decideProposal(ctx, proposalID, newStatus, decidedBy); err != nil {
		return err
	}

	return st.notifyProposalDecided(ctx, p, newStatus)
}

// Vote is a member's vote: Stars (1–4), or NoThanks, or Clear (taken
// back: not voted).
type Vote struct {
	Stars    int  `json:"stars"`
	NoThanks bool `json:"noThanks"`
	Clear    bool `json:"clear"`
}

func (v Vote) valid() bool {
	n := 0
	if v.Stars != 0 {
		if v.Stars < 1 || v.Stars > MaxStars {
			return false
		}
		n++
	}
	if v.NoThanks {
		n++
	}
	if v.Clear {
		n++
	}
	return n == 1
}

// CastVote records personID's vote on proposalID — stars, no thanks, or
// taking it back — in place of their previous one (and in the vote log),
// while the proposal is open. The votes decide nothing by themselves: it
// stays open, for a core member to decide by hand. It returns the
// proposal's status.
func (st *Store) CastVote(ctx context.Context, proposalID, personID int64, v Vote) (string, error) {
	if !v.valid() {
		return "", fmt.Errorf("a vote is 1 to 4 stars, no thanks, or taken back: %w", ErrInvalid)
	}
	p, err := st.GetProposal(ctx, proposalID)
	if err != nil {
		return "", err
	}
	if p.Status != "open" {
		return p.Status, ErrAlreadyDecided
	}
	may, err := st.MayCastVote(ctx, p.BandID, personID)
	if err != nil {
		return "", err
	}
	if !may {
		if isMember, err := st.IsCurrentBandMember(ctx, p.BandID, personID); err == nil && isMember {
			return "", ErrArchivedNoVote
		}
		return "", fmt.Errorf("person %d is not a current member of band %d: %w", personID, p.BandID, ErrForbidden)
	}

	var stars *int
	if v.Stars != 0 {
		stars = &v.Stars
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if v.Clear {
		if _, err := tx.Exec(ctx, `DELETE FROM proposal_votes WHERE proposal_id = $1 AND person_id = $2`, proposalID, personID); err != nil {
			return "", err
		}
	} else if _, err := tx.Exec(ctx, `
		INSERT INTO proposal_votes (proposal_id, person_id, stars, no_thanks) VALUES ($1, $2, $3, $4)
		ON CONFLICT (proposal_id, person_id) DO UPDATE
		SET stars = excluded.stars, no_thanks = excluded.no_thanks, cast_at = now()`,
		proposalID, personID, stars, v.NoThanks); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO proposal_vote_log (proposal_id, person_id, stars, no_thanks) VALUES ($1, $2, $3, $4)`,
		proposalID, personID, stars, v.NoThanks); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return p.Status, nil
}

// SongCoverImages is the album cover found for each of songIDs (its
// work's, docs/covers.md), by song; none for a song without one.
func (st *Store) SongCoverImages(ctx context.Context, songIDs []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(songIDs) == 0 {
		return out, nil
	}
	rows, err := st.db.Query(ctx, `
		SELECT s.id, c.cover->>'image'
		FROM songs s JOIN work_covers c ON c.work_id = s.work_id
		WHERE s.id = ANY($1) AND c.found AND coalesce(c.cover->>'source', '') <> 'youtube'
		  AND coalesce(c.cover->>'image', '') <> ''`, songIDs)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var img string
		if err := rows.Scan(&id, &img); err != nil {
			return out, err
		}
		out[id] = img
	}
	return out, rows.Err()
}

// MayFixProposedSong says whether personID fixes songID, a song made for
// a proposal to bandID, in the propose flow's check step: they made it,
// it's the band's, not sent yet (in no songbook, proposal or set list),
// and their role still grants propose — whatever their song rights (a
// guest proposes, without editing the band's songs).
func (st *Store) MayFixProposedSong(ctx context.Context, bandID, songID, personID int64) (bool, error) {
	var ok bool
	err := st.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM songs s
		  WHERE s.id = $1 AND s.owner_band_id = $2 AND s.created_by_person_id = $3
		    AND NOT EXISTS (SELECT 1 FROM songbook_entries se WHERE se.variant_id = s.id)
		    AND NOT EXISTS (SELECT 1 FROM band_proposals bp WHERE bp.target_variant_id = s.id)
		    AND NOT EXISTS (SELECT 1 FROM setlist_items si WHERE si.variant_id = s.id))
		  AND has_band_privilege($2, $3, 'propose')`, songID, bandID, personID).Scan(&ok)
	return ok, err
}

// DeleteUnsentProposedSong drops a song made for a proposal that was
// never sent: bandID's, made by personID, in no songbook and no
// proposal. Anything else is left alone (ErrForbidden).
func (st *Store) DeleteUnsentProposedSong(ctx context.Context, bandID, songID, personID int64) error {
	tag, err := st.db.Exec(ctx, `
		DELETE FROM songs s
		WHERE s.id = $1 AND s.owner_band_id = $2 AND s.created_by_person_id = $3
		  AND NOT EXISTS (SELECT 1 FROM songbook_entries se WHERE se.variant_id = s.id)
		  AND NOT EXISTS (SELECT 1 FROM band_proposals bp WHERE bp.target_variant_id = s.id)
		  AND NOT EXISTS (SELECT 1 FROM setlist_items si WHERE si.variant_id = s.id)`, songID, bandID, personID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("song %d isn't an unsent proposal's: %w", songID, ErrForbidden)
	}
	return nil
}
