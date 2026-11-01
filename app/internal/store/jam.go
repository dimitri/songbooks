package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// JamEvent is one scheduled jam night: a short, human-typeable code tied to
// one project, open for signups until ExpiresAt.
type JamEvent struct {
	ID        int64
	ProjectID int64
	Code      string
	Name      string
	ExpiresAt time.Time
}

// JamPlayer is one signed-up nickname that picked a (song, instrument) —
// JamRoster groups these; the id (not just the display nickname) is what
// the organizer "call" control needs to target one specific participant.
type JamPlayer struct {
	ProfileID int64
	Nickname  string
}

// JamProfile is one no-login participant's nickname for a single jam event
// — not a Person, no password, scoped to that one event.
type JamProfile struct {
	ID         int64
	JamEventID int64
	Nickname   string
}

// RosterRow is one (song, instrument) group from JamRoster, with every
// nickname that picked it, plus its configured headcount caps (if any —
// see jam_song_instrument_limits).
type RosterRow struct {
	SongID     int64
	SongStem   string
	SongTitle  string
	PlayOrder  int
	Instrument string
	Players    []JamPlayer
	Count      int
	MinCount   int
	MaxCount   *int // nil = no cap set
	Unmet      bool // Count < MinCount — informational only, never enforced
	Full       bool // MaxCount set and Count >= *MaxCount
}

// InstrumentLimit is one organizer-set (or default) per-instrument
// headcount cap for a song in a jam's pickable list. MaxCount nil means no
// cap — only the four organizer-facing defaults (drummer/bassist/
// guitarist/singer) start with one; anything else the organizer adds
// starts at min 0, no max, unless they set one explicitly.
type InstrumentLimit struct {
	Instrument string
	MinCount   int
	MaxCount   *int
}

// DefaultInstrumentLimits is the pre-filled (editable, not hardcoded)
// starting point offered when an organizer adds a song to a jam's
// pickable list.
func DefaultInstrumentLimits() []InstrumentLimit {
	one, two, four := 1, 2, 4
	return []InstrumentLimit{
		{Instrument: "drummer", MinCount: 1, MaxCount: &one},
		{Instrument: "bassist", MinCount: 1, MaxCount: &one},
		{Instrument: "guitarist", MinCount: 1, MaxCount: &two},
		{Instrument: "singer", MinCount: 1, MaxCount: &four},
	}
}

// ErrInstrumentFull is returned by SetJamPick when picking would exceed a
// song's configured max_count for that instrument. MinCount is
// informational only (shown as "unmet" on the roster) and is never
// enforced here.
var ErrInstrumentFull = fmt.Errorf("instrument slots full for this song")

var jamCodeAlphabet = []byte("ABCDEFGHJKLMNPQRSTUVWXYZ23456789") // no O/0/I/1

func randomJamCode(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i, c := range b {
		b[i] = jamCodeAlphabet[int(c)%len(jamCodeAlphabet)]
	}
	return string(b), nil
}

// CreateJamEvent starts a new jam event for projectSlug, open for ttl, and
// makes creatorPersonID an organizer for it (see AddJamOrganizer) —
// "the person who creates the jam event is automatically an organizer for
// it." The caller is responsible for checking the requesting person's
// role (editor/owner) — this method trusts it, same convention as the
// rest of this package (see AddMember/CreateInvite).
func (st *Store) CreateJamEvent(ctx context.Context, projectSlug, name string, ttl time.Duration, creatorPersonID int64) (*JamEvent, error) {
	pid, err := st.projectID(ctx, projectSlug)
	if err != nil {
		return nil, err
	}
	// A handful of retries on a code collision is simpler and plenty
	// reliable at this scale than a longer code or a DB-side generator.
	for attempt := 0; attempt < 5; attempt++ {
		code, err := randomJamCode(6)
		if err != nil {
			return nil, err
		}
		var ev JamEvent
		err = st.db.QueryRow(ctx, `
			INSERT INTO jam_events (project_id, code, name, expires_at)
			VALUES ($1, $2, $3, $4)
			RETURNING id, project_id, code, name, expires_at`,
			pid, code, name, time.Now().Add(ttl)).
			Scan(&ev.ID, &ev.ProjectID, &ev.Code, &ev.Name, &ev.ExpiresAt)
		if err == nil {
			if err := st.AddJamOrganizer(ctx, ev.ID, creatorPersonID); err != nil {
				return nil, err
			}
			return &ev, nil
		}
		if isUniqueViolation(err) {
			continue
		}
		return nil, err
	}
	return nil, fmt.Errorf("could not generate a unique jam code after several attempts")
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23505"
}

// GetJamEvent resolves a code to its still-open event, or ErrNotFound if
// missing/expired.
func (st *Store) GetJamEvent(ctx context.Context, code string) (*JamEvent, error) {
	var ev JamEvent
	err := st.db.QueryRow(ctx, `
		SELECT id, project_id, code, name, expires_at FROM jam_events
		WHERE code = $1 AND expires_at > now()`, code).
		Scan(&ev.ID, &ev.ProjectID, &ev.Code, &ev.Name, &ev.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such jam event %q: %w", code, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &ev, nil
}

// CreateJamProfile signs a new nickname up for a jam event.
func (st *Store) CreateJamProfile(ctx context.Context, jamEventID int64, nickname string) (*JamProfile, error) {
	var p JamProfile
	err := st.db.QueryRow(ctx, `
		INSERT INTO jam_profiles (jam_event_id, nickname) VALUES ($1, $2)
		RETURNING id, jam_event_id, nickname`, jamEventID, nickname).
		Scan(&p.ID, &p.JamEventID, &p.Nickname)
	return &p, err
}

// GetJamProfile looks up a jam profile by id (from the jam cookie).
func (st *Store) GetJamProfile(ctx context.Context, id int64) (*JamProfile, error) {
	var p JamProfile
	err := st.db.QueryRow(ctx, `SELECT id, jam_event_id, nickname FROM jam_profiles WHERE id = $1`, id).
		Scan(&p.ID, &p.JamEventID, &p.Nickname)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such jam profile: %w", ErrNotFound)
	}
	return &p, err
}

// SetJamPick records that jamProfileID wants to play songID on instrument
// for jamEventID. Idempotent — picking the same (song, instrument) twice is
// a no-op, not an error. Rejects with ErrInstrumentFull if the song has a
// configured max_count for instrument and it's already met — min_count is
// informational only and is never enforced here (see RosterRow.Unmet).
func (st *Store) SetJamPick(ctx context.Context, jamEventID, jamProfileID, songID int64, instrument string) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var already bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM jam_picks WHERE jam_event_id = $1 AND jam_profile_id = $2 AND song_id = $3 AND instrument = $4)`,
		jamEventID, jamProfileID, songID, instrument).Scan(&already); err != nil {
		return err
	}
	if already {
		return tx.Commit(ctx)
	}

	var maxCount *int
	err = tx.QueryRow(ctx, `
		SELECT max_count FROM jam_song_instrument_limits
		WHERE jam_event_id = $1 AND song_id = $2 AND instrument = $3`,
		jamEventID, songID, instrument).Scan(&maxCount)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if maxCount != nil {
		var count int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM jam_picks WHERE jam_event_id = $1 AND song_id = $2 AND instrument = $3`,
			jamEventID, songID, instrument).Scan(&count); err != nil {
			return err
		}
		if count >= *maxCount {
			return fmt.Errorf("%s slots full for this song: %w", instrument, ErrInstrumentFull)
		}
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO jam_picks (jam_event_id, jam_profile_id, song_id, instrument)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING`, jamEventID, jamProfileID, songID, instrument); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RemoveJamPick withdraws a previously-made pick. Removing one that doesn't
// exist is a no-op, not an error.
func (st *Store) RemoveJamPick(ctx context.Context, jamEventID, jamProfileID, songID int64, instrument string) error {
	_, err := st.db.Exec(ctx, `
		DELETE FROM jam_picks
		WHERE jam_event_id = $1 AND jam_profile_id = $2 AND song_id = $3 AND instrument = $4`,
		jamEventID, jamProfileID, songID, instrument)
	return err
}

// JamSong is the lightweight shape ListJamSongs returns — just enough for
// the song-picker list, not a fully parsed song.Song. Instruments carries
// each configured instrument's current pick count against its cap (so the
// picker page can grey out a full instrument instead of only finding out
// on submit) and the nicknames who've already picked it — this song's own
// "who's in" roster, rendered as its own block on the picker page
// (jam_songs.html), isolated from any other song's picks.
type JamSong struct {
	ID          int64
	Book        string // project slug the variant lives under — needed to LoadSong/buildSections it for inline section-annotation rendering
	Stem        string
	Title       string
	PlayOrder   int
	Instruments []RosterRow // Instrument/Players/Count/MinCount/MaxCount/Unmet/Full populated; SongID/SongStem/SongTitle left zero here (redundant with the JamSong they're nested under)
}

// ListJamSongs lists jamCode's event's pickable song list — i.e. the
// songs an organizer has explicitly added via AddJamSong (jam_songs),
// ordered by play_order, NOT every song in the project. A plain
// project-scoped-through-the-event read, exactly like any other
// per-project query in this package. Previously this ran through a
// second, non-owning Postgres role plus a row-level-security policy
// (migrations/0003_jam_rls.sql); that was dropped
// (migrations/0004_drop_jam_rls.sql) as unneeded complexity — a jam code
// is already the access control here, same trust level as a project slug
// anywhere else in this file.
func (st *Store) ListJamSongs(ctx context.Context, jamCode string) ([]JamSong, error) {
	var eventID int64
	rows, err := st.db.Query(ctx, `
		SELECT s.id, p.slug, s.stem, s.title, js.play_order, je.id
		FROM jam_songs js
		JOIN songs s ON s.id = js.song_id
		JOIN projects p ON p.id = s.project_id
		JOIN jam_events je ON je.id = js.jam_event_id
		WHERE je.code = $1 AND je.expires_at > now()
		ORDER BY js.play_order`, jamCode)
	if err != nil {
		return nil, err
	}
	var out []JamSong
	for rows.Next() {
		var s JamSong
		if err := rows.Scan(&s.ID, &s.Book, &s.Stem, &s.Title, &s.PlayOrder, &eventID); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}

	limits, counts, err := st.jamLimitsAndCounts(ctx, eventID)
	if err != nil {
		return nil, err
	}
	players, err := st.jamPlayers(ctx, eventID)
	if err != nil {
		return nil, err
	}
	for i := range out {
		for _, lim := range limits[out[i].ID] {
			k := songInstrumentKey{out[i].ID, lim.Instrument}
			cnt := counts[k]
			out[i].Instruments = append(out[i].Instruments, RosterRow{
				Instrument: lim.Instrument, Players: players[k], Count: cnt, MinCount: lim.MinCount, MaxCount: lim.MaxCount,
				Unmet: cnt < lim.MinCount, Full: lim.MaxCount != nil && cnt >= *lim.MaxCount,
			})
		}
	}
	return out, nil
}

type songInstrumentKey struct {
	songID     int64
	instrument string
}

// jamLimitsAndCounts loads every configured instrument limit and current
// pick count for jamEventID, keyed by song id — shared by ListJamSongs
// (picker page) and JamRoster (organizer page) so both reflect the exact
// same counts.
func (st *Store) jamLimitsAndCounts(ctx context.Context, jamEventID int64) (limits map[int64][]InstrumentLimit, counts map[songInstrumentKey]int, err error) {
	limits = map[int64][]InstrumentLimit{}
	lrows, err := st.db.Query(ctx, `
		SELECT song_id, instrument, min_count, max_count
		FROM jam_song_instrument_limits WHERE jam_event_id = $1
		ORDER BY song_id, instrument`, jamEventID)
	if err != nil {
		return nil, nil, err
	}
	for lrows.Next() {
		var l InstrumentLimit
		var songID int64
		if err := lrows.Scan(&songID, &l.Instrument, &l.MinCount, &l.MaxCount); err != nil {
			lrows.Close()
			return nil, nil, err
		}
		limits[songID] = append(limits[songID], l)
	}
	lrows.Close()
	if err := lrows.Err(); err != nil {
		return nil, nil, err
	}

	counts = map[songInstrumentKey]int{}
	crows, err := st.db.Query(ctx, `
		SELECT song_id, instrument, count(*) FROM jam_picks WHERE jam_event_id = $1
		GROUP BY song_id, instrument`, jamEventID)
	if err != nil {
		return nil, nil, err
	}
	for crows.Next() {
		var k songInstrumentKey
		var n int
		if err := crows.Scan(&k.songID, &k.instrument, &n); err != nil {
			crows.Close()
			return nil, nil, err
		}
		counts[k] = n
	}
	crows.Close()
	return limits, counts, crows.Err()
}

// jamPlayers loads every (song, instrument) -> picked nicknames mapping
// for jamEventID — shared by ListJamSongs (so the participant-facing
// picker page can show each song's own little "who's in" roster, not
// just a bare count) and JamRoster (organizer page), so both ever only
// have one query that answers "who picked what for song X."
func (st *Store) jamPlayers(ctx context.Context, jamEventID int64) (map[songInstrumentKey][]JamPlayer, error) {
	rows, err := st.db.Query(ctx, `
		SELECT jp.song_id, jp.instrument, pr.id, pr.nickname
		FROM jam_picks jp JOIN jam_profiles pr ON pr.id = jp.jam_profile_id
		WHERE jp.jam_event_id = $1
		ORDER BY jp.song_id, jp.instrument, pr.nickname`, jamEventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	players := map[songInstrumentKey][]JamPlayer{}
	for rows.Next() {
		var k songInstrumentKey
		var jp JamPlayer
		if err := rows.Scan(&k.songID, &k.instrument, &jp.ProfileID, &jp.Nickname); err != nil {
			return nil, err
		}
		players[k] = append(players[k], jp)
	}
	return players, rows.Err()
}

// AddJamSong adds songID to jamEventID's pickable song list, at the end of
// the current play order, with the given per-instrument limits (typically
// DefaultInstrumentLimits plus whatever the organizer edited/added).
// Re-adding an already-added song replaces its limits (ON CONFLICT DO
// NOTHING on the jam_songs row itself, so its play_order/position doesn't
// move) rather than erroring.
func (st *Store) AddJamSong(ctx context.Context, jamEventID, songID int64, limits []InstrumentLimit) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := st.addJamSongTx(ctx, tx, jamEventID, songID, limits); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// addJamSongTx is AddJamSong's body, factored out so ForkVariant and
// CreateJamVariant (catalog.go) can add a freshly-created/forked
// jam-scoped variant to its event's pickable list inside their own
// already-open transaction, atomically with the variant's creation — "a
// jam-scoped variant that isn't on the jam's song list doesn't make
// sense" per the brief, so these two things should never happen one
// without the other.
func (st *Store) addJamSongTx(ctx context.Context, tx pgx.Tx, jamEventID, songID int64, limits []InstrumentLimit) error {
	var nextOrder int
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(play_order), 0) + 1 FROM jam_songs WHERE jam_event_id = $1`, jamEventID).Scan(&nextOrder); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO jam_songs (jam_event_id, song_id, play_order) VALUES ($1, $2, $3)
		ON CONFLICT (jam_event_id, song_id) DO NOTHING`, jamEventID, songID, nextOrder); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM jam_song_instrument_limits WHERE jam_event_id = $1 AND song_id = $2`, jamEventID, songID); err != nil {
		return err
	}
	for _, l := range limits {
		if l.Instrument == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO jam_song_instrument_limits (jam_event_id, song_id, instrument, min_count, max_count)
			VALUES ($1, $2, $3, $4, $5)`, jamEventID, songID, l.Instrument, l.MinCount, l.MaxCount); err != nil {
			return err
		}
	}
	return nil
}

// RemoveJamSong drops songID from jamEventID's pickable list entirely —
// cascades to its instrument limits and any picks already made for it.
func (st *Store) RemoveJamSong(ctx context.Context, jamEventID, songID int64) error {
	_, err := st.db.Exec(ctx, `DELETE FROM jam_songs WHERE jam_event_id = $1 AND song_id = $2`, jamEventID, songID)
	return err
}

// ListAddableJamSongs lists projectSlug's songs not yet on jamEventID's
// pickable list — the candidate list for the organizer's "add a song"
// control.
func (st *Store) ListAddableJamSongs(ctx context.Context, jamEventID int64, projectSlug string) ([]JamSong, error) {
	pid, err := st.projectID(ctx, projectSlug)
	if err != nil {
		return nil, err
	}
	rows, err := st.db.Query(ctx, `
		SELECT s.id, s.stem, s.title FROM songs s
		WHERE s.project_id = $1
		AND NOT EXISTS (SELECT 1 FROM jam_songs js WHERE js.jam_event_id = $2 AND js.song_id = s.id)
		ORDER BY s.stem`, pid, jamEventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JamSong
	for rows.Next() {
		var s JamSong
		if err := rows.Scan(&s.ID, &s.Stem, &s.Title); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ReorderJamSongs sets jamEventID's pickable-song play order to exactly
// songIDs' order (1-based) — same "send the whole current order" pattern
// as store.SaveOrder for a book's song order. Unknown song ids are
// ignored; song ids already on the list but omitted keep their previous
// play_order (so a partial reorder payload can't accidentally drop a song
// from the list).
func (st *Store) ReorderJamSongs(ctx context.Context, jamEventID int64, songIDs []int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for i, songID := range songIDs {
		if _, err := tx.Exec(ctx, `
			UPDATE jam_songs SET play_order = $1 WHERE jam_event_id = $2 AND song_id = $3`,
			i+1, jamEventID, songID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// JamRoster lists every song on jamEventID's pickable list (play-order),
// with every configured/picked instrument's players and caps — a
// song/instrument pair appears if it has a configured limit, at least one
// pick, or both.
func (st *Store) JamRoster(ctx context.Context, jamEventID int64) ([]RosterRow, error) {
	limits, _, err := st.jamLimitsAndCounts(ctx, jamEventID)
	if err != nil {
		return nil, err
	}

	players, err := st.jamPlayers(ctx, jamEventID)
	if err != nil {
		return nil, err
	}

	songRows, err := st.db.Query(ctx, `
		SELECT s.id, s.stem, s.title, js.play_order FROM jam_songs js JOIN songs s ON s.id = js.song_id
		WHERE js.jam_event_id = $1 ORDER BY js.play_order`, jamEventID)
	if err != nil {
		return nil, err
	}
	defer songRows.Close()

	// instruments seen per song from picks alone, for a song with picks on
	// an instrument that has no configured limit (e.g. the organizer never
	// set a cap for it) — it still needs to show up on the roster.
	pickedInstrumentsBySong := map[int64][]string{}
	for k := range players {
		pickedInstrumentsBySong[k.songID] = append(pickedInstrumentsBySong[k.songID], k.instrument)
	}

	var out []RosterRow
	for songRows.Next() {
		var songID int64
		var stem, title string
		var playOrder int
		if err := songRows.Scan(&songID, &stem, &title, &playOrder); err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		addRow := func(instrument string, min int, max *int) {
			if seen[instrument] {
				return
			}
			seen[instrument] = true
			ps := players[songInstrumentKey{songID, instrument}]
			cnt := len(ps)
			out = append(out, RosterRow{
				SongID: songID, SongStem: stem, SongTitle: title, PlayOrder: playOrder, Instrument: instrument, Players: ps,
				Count: cnt, MinCount: min, MaxCount: max, Unmet: cnt < min, Full: max != nil && cnt >= *max,
			})
		}
		for _, lim := range limits[songID] {
			addRow(lim.Instrument, lim.MinCount, lim.MaxCount)
		}
		for _, instrument := range pickedInstrumentsBySong[songID] {
			addRow(instrument, 0, nil)
		}
	}
	return out, songRows.Err()
}

// AddJamOrganizer designates personID an organizer for jamEventID —
// distinct from project-wide editor/owner (see
// httpapi.requireJamOrganizer). Idempotent.
func (st *Store) AddJamOrganizer(ctx context.Context, jamEventID, personID int64) error {
	_, err := st.db.Exec(ctx, `
		INSERT INTO jam_organizers (jam_event_id, person_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, jamEventID, personID)
	return err
}

// IsJamOrganizer reports whether personID is a designated organizer for
// jamEventID. Does NOT also check project editor/owner — callers wanting
// the full "organizer OR editor/owner" rule combine this with GetRole
// (see httpapi.requireJamOrganizer).
func (st *Store) IsJamOrganizer(ctx context.Context, jamEventID, personID int64) (bool, error) {
	var ok bool
	err := st.db.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM jam_organizers WHERE jam_event_id = $1 AND person_id = $2)`,
		jamEventID, personID).Scan(&ok)
	return ok, err
}

// JamOrganizer is one designated organizer, for display on the roster
// page's "organizers" list.
type JamOrganizer struct {
	PersonID int64
	Username string
}

// ListJamOrganizers lists jamEventID's designated organizers by username.
func (st *Store) ListJamOrganizers(ctx context.Context, jamEventID int64) ([]JamOrganizer, error) {
	rows, err := st.db.Query(ctx, `
		SELECT p.id, p.username FROM jam_organizers o JOIN people p ON p.id = o.person_id
		WHERE o.jam_event_id = $1 ORDER BY p.username`, jamEventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JamOrganizer
	for rows.Next() {
		var o JamOrganizer
		if err := rows.Scan(&o.PersonID, &o.Username); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// JamCallStatus is who's currently called up, for the roster page's "now
// calling" banner. nil (from GetJamCallStatus) means nobody is currently
// called.
type JamCallStatus struct {
	ProfileID  int64
	Nickname   string
	SongID     int64
	SongTitle  string
	Instrument string
	CalledAt   time.Time
}

// SetJamCall records that jamProfileID is currently called up to play
// songID on instrument for jamEventID — the organizer's "now calling"
// control. Overwrites any previous call for this event.
func (st *Store) SetJamCall(ctx context.Context, jamEventID, jamProfileID, songID int64, instrument string) error {
	_, err := st.db.Exec(ctx, `
		UPDATE jam_events SET called_profile_id = $1, called_song_id = $2, called_instrument = $3, called_at = now()
		WHERE id = $4`, jamProfileID, songID, instrument, jamEventID)
	return err
}

// ClearJamCall clears jamEventID's "now calling" state.
func (st *Store) ClearJamCall(ctx context.Context, jamEventID int64) error {
	_, err := st.db.Exec(ctx, `
		UPDATE jam_events SET called_profile_id = NULL, called_song_id = NULL, called_instrument = NULL, called_at = NULL
		WHERE id = $1`, jamEventID)
	return err
}

// GetJamCallStatus returns jamEventID's current "now calling" state, or
// nil if nobody is currently called.
func (st *Store) GetJamCallStatus(ctx context.Context, jamEventID int64) (*JamCallStatus, error) {
	var cs JamCallStatus
	err := st.db.QueryRow(ctx, `
		SELECT je.called_profile_id, pr.nickname, je.called_song_id, s.title, je.called_instrument, je.called_at
		FROM jam_events je
		LEFT JOIN jam_profiles pr ON pr.id = je.called_profile_id
		LEFT JOIN songs s ON s.id = je.called_song_id
		WHERE je.id = $1 AND je.called_profile_id IS NOT NULL`, jamEventID).
		Scan(&cs.ProfileID, &cs.Nickname, &cs.SongID, &cs.SongTitle, &cs.Instrument, &cs.CalledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cs, nil
}
