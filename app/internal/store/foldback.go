package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"app/internal/song"
	"app/internal/songdiff"
)

// Fold back (the edit model, docs/catalogue-edit-review.md §4.7, migration
// 0143). Some arrangements of The Big Songbook's songs are fixes in
// disguise: the copy a band or a person was made to make before Edit
// fixed the shared song (#141, the forced band offer), or a silent copy
// from before. Each arrangement of a catalogue song is classed by what it
// changed itself — its chart against its original as it was when it was
// made (base_body), and against the original now:
//
//   - identical: it changed nothing (or the original has it all now): an
//     accidental copy; going back to the link loses nothing;
//   - fix: a few changes (FoldFixChanges at most), the same sections and
//     bars, no arrangement name: probably a fix, to send to the shared
//     song;
//   - arrangement: anything else — another form, a name: left alone.
//
// Folding back moves everything that pointed at the arrangement (songbook
// entries and their keys, set list items, notes, practice: repointSong)
// to the original, records the arrangement's last state as a revision
// under its id and a song_merges row (reason 'foldback': its history is
// listed on the original's, an old link opens the original), and deletes
// it. Never an arrangement with media of its own (recordings, a Guitar
// Pro file the original hasn't): those stay.

// FoldFixChanges is how many changes (songdiff.Describe) a "fix" has at
// most.
const FoldFixChanges = 3

// Fold kinds.
const (
	FoldIdentical   = "identical"
	FoldFix         = "fix"
	FoldArrangement = "arrangement"
)

// FoldCandidate is an arrangement of a catalogue song, classed.
type FoldCandidate struct {
	ForkID, OriginalID         int64
	Title, Artist, VersionName string
	OriginalTitle              string
	OwnerKind, OwnerName       string // 'band', 'user', 'community'
	OwnerID                    int64
	Kind                       string
	// Changes: what the arrangement changed itself (since its base); for
	// a fix, what it would send.
	Changes []songdiff.Change
	// OwnMedia: recordings or a Guitar Pro file the original hasn't — not
	// folded.
	OwnMedia bool
	// Kept: its owner said it's their arrangement.
	Kept      bool
	CreatedAt time.Time
}

// Foldable: offered to fold back.
func (c FoldCandidate) Foldable() bool {
	return (c.Kind == FoldIdentical || c.Kind == FoldFix) && !c.OwnMedia && !c.Kept
}

// FoldCandidates classes the arrangements of catalogue songs: forkID's
// alone (0: all of them), by owner and title.
func (st *Store) FoldCandidates(ctx context.Context, forkID int64) ([]FoldCandidate, error) {
	rows, err := st.db.Query(ctx, `
		SELECT f.id, o.id, f.title, f.artist, f.version_name, o.title,
		       CASE WHEN f.owner_band_id IS NOT NULL THEN 'band' WHEN f.owner_person_id IS NOT NULL THEN 'user' ELSE 'community' END,
		       coalesce(b.name, p.display_name, c.name, ''), coalesce(f.owner_band_id, f.owner_person_id, f.owner_community_id, 0),
		       f.body, coalesce(f.base_body, ''), f.base_body IS NOT NULL, o.body, f.foldback_kept_at IS NOT NULL, f.created_at,
		       EXISTS (SELECT 1 FROM song_audio a WHERE a.variant_id = f.id
		               AND NOT EXISTS (SELECT 1 FROM song_audio x WHERE x.variant_id = o.id AND x.sha256 = a.sha256)
		               -- the same reference's audio, fetched again for the copy: the original's to fetch
		               AND NOT (a.category = 'youtube' AND f.reference_url = o.reference_url))
		       OR EXISTS (SELECT 1 FROM song_gp_source_file g WHERE g.variant_id = f.id
		               AND NOT EXISTS (SELECT 1 FROM song_gp_source_file x WHERE x.variant_id = o.id AND x.sha256 = g.sha256))
		       OR EXISTS (SELECT 1 FROM band_recordings r WHERE r.variant_id = f.id)
		FROM songs f
		JOIN songs o ON o.id = f.forked_from_id
		LEFT JOIN bands b ON b.id = f.owner_band_id
		LEFT JOIN people p ON p.id = f.owner_person_id
		LEFT JOIN communities c ON c.id = f.owner_community_id
		WHERE `+catalogueSQL("o")+` AND NOT coalesce(`+catalogueSQL("f")+`, false) AND f.frozen_at IS NULL
		  AND ($1 = 0 OR f.id = $1)
		ORDER BY 7, 8, f.title, f.id`, forkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FoldCandidate
	for rows.Next() {
		var c FoldCandidate
		var body, base, orig string
		var hasBase bool
		if err := rows.Scan(&c.ForkID, &c.OriginalID, &c.Title, &c.Artist, &c.VersionName, &c.OriginalTitle,
			&c.OwnerKind, &c.OwnerName, &c.OwnerID, &body, &base, &hasBase, &orig, &c.Kept, &c.CreatedAt, &c.OwnMedia); err != nil {
			return nil, err
		}
		if !hasBase {
			base = orig
		}
		c.Kind, c.Changes = classifyFork(base, orig, body, c.VersionName)
		out = append(out, c)
	}
	return out, rows.Err()
}

// classifyFork classes an arrangement (body) of a song: base, the
// original when it was made; orig, the original now.
func classifyFork(base, orig, body, name string) (string, []songdiff.Change) {
	// A name: someone made it their arrangement on purpose.
	if strings.TrimSpace(name) != "" {
		return FoldArrangement, songdiff.Describe(base, body)
	}
	if !songdiff.ChartChanged(base, body) || !songdiff.ChartChanged(orig, body) {
		return FoldIdentical, nil
	}
	changes := songdiff.Describe(base, body)
	if len(changes) > FoldFixChanges || sectionShape(base) != sectionShape(body) {
		return FoldArrangement, changes
	}
	return FoldFix, changes
}

// sectionShape is a chart's sections and their bars ("verse:4;chorus:8").
func sectionShape(body string) string {
	var b strings.Builder
	for _, s := range song.Parse(body).Sections {
		fmt.Fprintf(&b, "%s:%d:%v;", strings.ToLower(strings.TrimSpace(s.Label)), len(s.Measures), s.Repeat)
	}
	return b.String()
}

// FoldFixBody is the original with the arrangement's own changes (since
// its base) taken into it — what "Send the fix" saves on the shared song —
// and false when the original changed the same parts since (nothing to
// send cleanly).
func (st *Store) FoldFixBody(ctx context.Context, forkID int64) (int64, string, bool, error) {
	var origID int64
	var body, orig string
	var base *string
	err := st.db.QueryRow(ctx, `
		SELECT o.id, f.body, f.base_body, o.body FROM songs f JOIN songs o ON o.id = f.forked_from_id WHERE f.id = $1`, forkID).
		Scan(&origID, &body, &base, &orig)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", false, fmt.Errorf("no arrangement %d: %w", forkID, ErrNotFound)
	}
	if err != nil {
		return 0, "", false, err
	}
	b := orig
	if base != nil {
		b = *base
	}
	for _, p := range songdiff.Plan(b, orig, body) {
		if p.Conflict {
			return origID, "", false, nil
		}
	}
	// The arrangement's metadata stays the original's (its title, its
	// recording): only the chart's changes go.
	merged := songdiff.Merge(b, orig, body, func(string) bool { return true })
	return origID, merged, true, nil
}

// ErrNotFoldable: the arrangement isn't one to fold back (a real
// arrangement, media of its own, kept by its owner).
var ErrNotFoldable = errors.New("this arrangement isn't one to fold back")

// FoldReport is what folding an arrangement back moved (or would, DryRun).
type FoldReport struct {
	Candidate FoldCandidate
	Moved     RepointCounts
	DryRun    bool
}

// FoldBack folds arrangement forkID back into its original (the package
// comment), by actorID (0: a tool). kinds: the kinds it may fold
// (FoldIdentical, FoldFix): ErrNotFoldable otherwise. With dryRun it does
// it all and rolls back.
func (st *Store) FoldBack(ctx context.Context, actorID, forkID int64, dryRun bool, kinds ...string) (*FoldReport, error) {
	cs, err := st.FoldCandidates(ctx, forkID)
	if err != nil {
		return nil, err
	}
	if len(cs) != 1 {
		return nil, fmt.Errorf("song %d isn't an arrangement of a catalogue song: %w", forkID, ErrNotFoldable)
	}
	c := cs[0]
	ok := false
	for _, k := range kinds {
		ok = ok || k == c.Kind
	}
	if !ok || c.OwnMedia {
		return nil, ErrNotFoldable
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var actor *int64
	if actorID != 0 {
		actor = &actorID
	}
	var snap VariantSnapshot
	if err := tx.QueryRow(ctx, `SELECT title, artist, reference_url, body FROM songs WHERE id = $1 FOR UPDATE`, forkID).
		Scan(&snap.Title, &snap.Artist, &snap.ReferenceURL, &snap.Body); err != nil {
		return nil, err
	}
	if _, err := recordRevision(ctx, tx, "variant", strconv.FormatInt(forkID, 10), actor, snap, "folded back into the shared chart"); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM song_recording_fit WHERE song_id = $1`, forkID); err != nil {
		return nil, err
	}
	moved, err := st.repointSong(ctx, tx, forkID, c.OriginalID, actor)
	if err != nil {
		return nil, err
	}
	movedJSON, err := json.Marshal(moved)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO song_merges (merged_song_id, into_song_id, title, artist, version_name, owner_name, reason, moved, merged_by)
		VALUES ($1, $2, $3, $4, $5, $6, 'foldback', $7, $8)`,
		forkID, c.OriginalID, c.Title, c.Artist, c.VersionName, c.OwnerName, movedJSON, actor); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE song_merges SET into_song_id = $2 WHERE into_song_id = $1`, forkID, c.OriginalID); err != nil {
		return nil, err
	}
	if err := deleteSong(ctx, tx, forkID); err != nil {
		return nil, err
	}
	r := &FoldReport{Candidate: c, Moved: moved, DryRun: dryRun}
	if dryRun {
		return r, nil // rolled back
	}
	return r, tx.Commit(ctx)
}

// KeepArrangement records that forkID is its owner's arrangement: the
// fold-back offer isn't made again.
func (st *Store) KeepArrangement(ctx context.Context, actorID, forkID int64) error {
	_, err := st.db.Exec(ctx, `
		UPDATE songs SET foldback_kept_at = now(), foldback_kept_by = $2 WHERE id = $1 AND foldback_kept_at IS NULL`,
		forkID, actorID)
	return err
}

// FoldOffersFor is the arrangements personID may change (their own,
// their bands', their communities' they edit) that are offered to fold
// back, for their home page.
func (st *Store) FoldOffersFor(ctx context.Context, personID int64) ([]FoldCandidate, error) {
	cs, err := st.FoldCandidates(ctx, 0)
	if err != nil {
		return nil, err
	}
	var out []FoldCandidate
	for _, c := range cs {
		if !c.Foldable() {
			continue
		}
		if r, err := st.SongRightsFor(ctx, personID, c.ForkID); err == nil && r.Edit {
			out = append(out, c)
		}
	}
	return out, nil
}
