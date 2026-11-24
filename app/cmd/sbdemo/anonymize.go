package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"app/internal/importer"
	"app/internal/store"
)

// counts is what a run changed, table by table, for its report.
type counts map[string]int64

func (c counts) add(table string, n int64) { c[table] += n }

func (c counts) print(title string) {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Println(title)
	for _, k := range keys {
		fmt.Printf("  %-34s %d\n", k, c[k])
	}
}

// exec runs one statement and counts the rows it touched under table.
func exec(ctx context.Context, tx pgx.Tx, c counts, table, sql string, args ...any) error {
	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", table, err)
	}
	c.add(table, tag.RowsAffected())
	return nil
}

// The account kept (its password, its sessions): the first one, the
// bootstrap account, which the local stack logs in as
// (SONGBOOKS_DEV_AUTO_LOGIN=1) and its owner signs in to staging with,
// by keptEmail and their password. There are no usernames to find it
// by (migrations 0135, 0136).
const (
	keptPersonID = 1
	keptEmail    = "tapoueh@example.invalid"
)

// studentEmailLike matches the fixtures' invented students' emails
// (fixer.student): invented already, anonymize leaves them be.
const studentEmailLike = "demo-student-%@example.invalid"

// lyricSongs is how many songs get invented lyrics.
const lyricSongs = 8

type anonymizer struct {
	db *pgxpool.Pool
	st *store.Store
	c  counts

	communityNames map[int64]string // id → new name
	bandNames      map[int64]string
	bandRank       map[int64]int
	oldBandNames   map[string]string // old name, lowercased → new
	songTitles     map[int64]string  // song id → new title
	songArtists    map[int64]string
}

func anonymize(ctx context.Context, db *pgxpool.Pool, st *store.Store) (counts, error) {
	a := &anonymizer{db: db, st: st, c: counts{},
		communityNames: map[int64]string{}, bandNames: map[int64]string{}, bandRank: map[int64]int{},
		oldBandNames: map[string]string{}, songTitles: map[int64]string{}, songArtists: map[int64]string{}}
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	for _, step := range []func(context.Context, pgx.Tx) error{
		a.people, a.private, a.communities, a.bands, a.coaching, a.songbooks, a.lessons,
		a.songs, a.gpFiles, a.setlists, a.events,
	} {
		if err := step(ctx, tx); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	// Outside the transaction, with the store's own functions.
	if err := a.looks(ctx); err != nil {
		return nil, err
	}
	if err := a.songbookFiles(ctx); err != nil {
		return nil, err
	}
	return a.c, nil
}

// people: invented names, no contact details, no photos, no passwords
// (but the kept account's, so its owner can still sign in on staging).
func (a *anonymizer) people(ctx context.Context, tx pgx.Tx) error {
	// The coach of the coaching community that becomes Studio Lumen.
	coaches := map[int64]bool{}
	rows, err := tx.Query(ctx, `
		SELECT c.name, c.id, m.person_id FROM communities c
		JOIN community_members m ON m.community_id = c.id AND upper_inf(m.validity)
		JOIN community_roles r ON r.id = m.role_id AND r.builtin_key = 'staff'
		ORDER BY m.id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		var cid, pid int64
		if err := rows.Scan(&name, &cid, &pid); err != nil {
			return err
		}
		if CommunityName(name, int(cid)) == "Studio Lumen" && len(coaches) == 0 {
			coaches[pid] = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	type person struct {
		id       int64
		hasEmail bool
		isShell  bool
	}
	var people []person
	// fixtures' students are invented already.
	rows, err = tx.Query(ctx, `SELECT id, email IS NOT NULL, is_shell FROM people
		WHERE coalesce(email, '') NOT LIKE $1 ORDER BY id`, studentEmailLike)
	if err != nil {
		return err
	}
	for rows.Next() {
		var p person
		if err := rows.Scan(&p.id, &p.hasEmail, &p.isShell); err != nil {
			return err
		}
		people = append(people, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	// Emails move out of the way first: a demo-N's may be someone
	// else's until everyone's renamed. (The old usernames, real
	// handles, are gone with their column: store.New migrated past
	// 0136 before this runs.)
	if _, err := tx.Exec(ctx, `UPDATE people SET email = CASE WHEN email IS NOT NULL THEN 'sbdemo-tmp-' || id END
		WHERE coalesce(email, '') NOT LIKE $1`, studentEmailLike); err != nil {
		return err
	}
	for _, p := range people {
		email := fmt.Sprintf("demo-%d@example.invalid", p.id)
		name := PersonName(int(p.id))
		if p.id == keptPersonID {
			email, name = keptEmail, lucasName
		} else if coaches[p.id] {
			name = coachName
		}
		var emailArg *string
		if p.hasEmail || p.id == keptPersonID {
			emailArg = &email
		}
		if err := exec(ctx, tx, a.c, "people", `
			UPDATE people SET display_name = $2, email = $3, nickname = '', picture_url = '',
				phone = '', share_email = false, share_phone = false, gravatar_opt_out = true,
				password_hash = CASE WHEN id = $4 THEN password_hash END
			WHERE id = $1`, p.id, name, emailArg, keptPersonID); err != nil {
			return err
		}
	}
	// A group invite's places are named after who they're for.
	return exec(ctx, tx, a.c, "group_invite_slots", `
		UPDATE group_invite_slots s SET display_name = coalesce(
			(SELECT display_name FROM people WHERE id = s.claimed_by_person_id),
			split_part($1, ' ', 1))
		WHERE display_name <> ''`, PersonName(1000))
}

// private: what nobody but its owner should have in a copy: sessions,
// sign-in tokens, passkeys, calendar feed secrets, mail, notifications,
// history (revisions hold every old body, lyrics included), searches.
func (a *anonymizer) private(ctx context.Context, tx pgx.Tx) error {
	for _, s := range []struct{ table, sql string }{
		{"sessions", `DELETE FROM sessions WHERE person_id <> ` + strconv.Itoa(keptPersonID)},
		{"login_tokens", `DELETE FROM login_tokens`},
		{"webauthn_credentials", `DELETE FROM webauthn_credentials`},
		{"calendar_feeds", `DELETE FROM calendar_feeds`},
		{"outbox", `DELETE FROM outbox`},
		{"notifications", `DELETE FROM notifications`},
		{"revisions", `DELETE FROM revisions`},
		{"song_visits", `UPDATE song_visits SET query = '' WHERE query <> ''`},
		{"community_submissions", `UPDATE community_submissions SET note = '' WHERE note <> ''`},
		{"variant_annotations", `UPDATE variant_annotations SET body = 'Attention aux nuances sur ce passage.' WHERE body <> 'Attention aux nuances sur ce passage.'`},
		{"section_annotations", `UPDATE section_annotations SET body = 'Reprendre ce passage plus lentement.' WHERE body <> 'Reprendre ce passage plus lentement.'`},
	} {
		if err := exec(ctx, tx, a.c, s.table, s.sql); err != nil {
			return err
		}
	}
	return nil
}

// communities: the four known ones by their new names, any other
// invented; custom role names too.
func (a *anonymizer) communities(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `SELECT id, name FROM communities ORDER BY id`)
	if err != nil {
		return err
	}
	old := map[int64]string{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return err
		}
		old[id] = name
		a.communityNames[id] = CommunityName(name, int(id))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for id, name := range a.communityNames {
		if err := exec(ctx, tx, a.c, "communities", `UPDATE communities SET name = $2 WHERE id = $1`, id, name); err != nil {
			return err
		}
	}
	return exec(ctx, tx, a.c, "community_roles", `
		UPDATE community_roles SET name = 'Rôle ' || id WHERE builtin_key IS NULL AND name <> 'Rôle ' || id`)
}

// bands: invented names (their looks come after, with SetBrand), and
// nothing that points at their real recordings, albums or proposals.
func (a *anonymizer) bands(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `SELECT id, name FROM bands ORDER BY id`)
	if err != nil {
		return err
	}
	type band struct {
		id   int64
		name string
	}
	var bands []band
	for rows.Next() {
		var b band
		if err := rows.Scan(&b.id, &b.name); err != nil {
			return err
		}
		bands = append(bands, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for i, b := range bands {
		name := BandName(i)
		a.bandNames[b.id], a.bandRank[b.id] = name, i
		a.oldBandNames[strings.ToLower(strings.TrimSpace(b.name))] = name
		if err := exec(ctx, tx, a.c, "bands", `UPDATE bands SET name = $2 WHERE id = $1`, b.id, name); err != nil {
			return err
		}
	}
	for _, s := range []struct{ table, sql string }{
		// Its recordings (band_recordings) go with it: they're files in
		// the band's own cloud folder, named after their songs.
		{"band_recording_sources", `DELETE FROM band_recording_sources`},
		{"band_recording_fixes", `DELETE FROM band_recording_fixes`},
		{"band_albums", `DELETE FROM band_albums`},
		{"proposal_votes", `DELETE FROM proposal_votes`},
		{"band_proposals", `DELETE FROM band_proposals`},
	} {
		if err := exec(ctx, tx, a.c, s.table, s.sql); err != nil {
			return err
		}
	}
	return nil
}

// coaching: groups, notes and ranges' words of a coaching community.
func (a *anonymizer) coaching(ctx context.Context, tx pgx.Tx) error {
	for _, s := range []struct {
		table, sql string
		args       []any
	}{
		{"coaching_groups", `UPDATE coaching_groups SET name = 'Groupe ' || id
			WHERE name NOT IN ('Mardi soir', 'Jeudi midi', 'Master class de printemps') AND name <> 'Groupe ' || id`, nil},
		{"teaching_notes", `UPDATE teaching_notes SET body = (ARRAY[` + sqlStrings(teachingNotes) + `])[1 + id % $1]
			WHERE body <> ALL(ARRAY[` + sqlStrings(teachingNotes) + `])`, []any{len(teachingNotes)}},
		{"student_notes", `UPDATE student_notes SET notes = (ARRAY[` + sqlStrings(studentProfiles) + `])[1 + person_id % $1]
			WHERE notes <> '' AND notes <> ALL(ARRAY[` + sqlStrings(studentProfiles) + `])`, []any{len(studentProfiles)}},
		{"student_voice_ranges", `UPDATE student_voice_ranges SET note = ''
			WHERE note <> '' AND note <> ALL(ARRAY[` + sqlStrings(append(append(append([]string{}, rangeNotes[0]...), rangeNotes[1]...), rangeNotes[2]...)) + `])`, nil},
	} {
		if err := exec(ctx, tx, a.c, s.table, s.sql, s.args...); err != nil {
			return err
		}
	}
	return nil
}

// sqlStrings is a list as SQL string literals.
func sqlStrings(ss []string) string {
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	return strings.Join(q, ", ")
}

// songbooks: a community's main songbook takes its new name, a course a
// volume of the method, a student's and a group's their owner's; any
// other with a name that says more than "Songbook", an invented one.
func (a *anonymizer) songbooks(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `
		SELECT s.id, s.name, s.kind, s.is_default, s.owner_kind, coalesce(s.owner_community_id, 0),
		       coalesce(p.display_name, ''), coalesce(g.name, '')
		FROM songbooks s
		LEFT JOIN people p ON p.id = s.student_person_id
		LEFT JOIN coaching_groups g ON g.songbook_id = s.id
		ORDER BY s.id`)
	if err != nil {
		return err
	}
	type sb struct {
		id                    int64
		name, kind, ownerKind string
		isDefault             bool
		community             int64
		student, group        string
	}
	var sbs []sb
	for rows.Next() {
		var s sb
		if err := rows.Scan(&s.id, &s.name, &s.kind, &s.isDefault, &s.ownerKind, &s.community, &s.student, &s.group); err != nil {
			return err
		}
		sbs = append(sbs, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	courses := map[int64]int{}
	for _, s := range sbs {
		name := s.name
		switch {
		case s.kind == "student" && s.student != "":
			name = s.student + "’s songs"
		case s.kind == "group" && s.group != "":
			name = s.group
		case s.kind == "course":
			name = MethodName(courses[s.community])
			courses[s.community]++
		case s.isDefault && s.ownerKind == "community" && !genericSongbooks[s.name]:
			name = a.communityNames[s.community]
		case !genericSongbooks[s.name]:
			name = SongbookName(int(s.id))
		}
		if err := exec(ctx, tx, a.c, "songbooks", `UPDATE songbooks SET name = $2, playlist_url = '' WHERE id = $1`, s.id, name); err != nil {
			return err
		}
	}
	return exec(ctx, tx, a.c, "songbook_access_grants", `UPDATE songbook_access_grants SET reference = '' WHERE reference <> ''`)
}

// lessons: a course's lessons get technique titles, no text of the
// author's, no video.
func (a *anonymizer) lessons(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `SELECT id, coalesce(number, 0), body <> '' FROM lessons ORDER BY songbook_id, number, id`)
	if err != nil {
		return err
	}
	type lesson struct {
		id      int64
		number  int
		hasBody bool
	}
	var ls []lesson
	for rows.Next() {
		var l lesson
		if err := rows.Scan(&l.id, &l.number, &l.hasBody); err != nil {
			return err
		}
		ls = append(ls, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, l := range ls {
		body := ""
		if l.hasBody {
			body = "Écoutez l'exemple, puis jouez-le lentement avec le métronome avant de monter le tempo."
		}
		if err := exec(ctx, tx, a.c, "lessons", `UPDATE lessons SET title = $2, body = $3, video_url = '' WHERE id = $1`,
			l.id, LessonTitle(l.number), body); err != nil {
			return err
		}
	}
	return nil
}

// artistFor is an invented artist for a real one: a band of the app by
// its new name, any other the same invented name wherever it appears.
type artistMap struct {
	bands map[string]string
	seen  map[string]string
	next  int
}

func (m *artistMap) get(real string) string {
	k := strings.ToLower(strings.TrimSpace(real))
	if n, ok := m.bands[k]; ok {
		return n
	}
	if n, ok := m.seen[k]; ok {
		return n
	}
	n := BandName(m.next)
	m.next++
	m.seen[k] = n
	return n
}

// songs: every work and every variant under an invented title and
// artist; bodies made demo bodies (DemoBody); a few songs given invented
// lyrics; no reference video, no reference recording's address, no
// album, no cover.
func (a *anonymizer) songs(ctx context.Context, tx pgx.Tx) error {
	// Invented artists skip the names the app's bands now have.
	am := &artistMap{bands: a.oldBandNames, seen: map[string]string{}, next: len(a.bandNames)}
	// Already renamed bands, on a second run, map to themselves.
	for _, n := range a.bandNames {
		am.bands[strings.ToLower(n)] = n
	}
	// Works, in id order.
	type work struct {
		id            int64
		title, artist string
	}
	var works []work
	rows, err := tx.Query(ctx, `SELECT id, artist FROM works ORDER BY id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var w work
		if err := rows.Scan(&w.id, &w.artist); err != nil {
			return err
		}
		works = append(works, w)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	workTitle := map[int64]string{}
	workArtist := map[int64]string{}
	if _, err := tx.Exec(ctx, `UPDATE works SET title = 'sbdemo-tmp-' || id`); err != nil {
		return err
	}
	for i, w := range works {
		workTitle[w.id], workArtist[w.id] = SongTitle(i), am.get(w.artist)
		if err := exec(ctx, tx, a.c, "works", `UPDATE works SET title = $2, artist = $3, album = NULL WHERE id = $1`,
			w.id, workTitle[w.id], workArtist[w.id]); err != nil {
			return err
		}
	}
	if err := exec(ctx, tx, a.c, "work_covers", `DELETE FROM work_covers`); err != nil {
		return err
	}

	// Which songs get lyrics: a coaching community's songs first, then
	// a jam's songbook's.
	lyricIDs := map[int64]int{}
	rows, err = tx.Query(ctx, `
		SELECT s.id, s.body, CASE WHEN c.kind = 'coaching' THEN 0 ELSE 1 END
		FROM songs s JOIN communities c ON c.id = s.owner_community_id
		WHERE c.name IN ('Studio Lumen')
		UNION ALL
		SELECT s.id, s.body, 1 FROM songbook_entries e
		JOIN songbooks b ON b.id = e.songbook_id AND b.is_default AND b.owner_kind = 'community'
		JOIN communities c ON c.id = b.owner_community_id AND c.name = 'Jam du Canal'
		JOIN songs s ON s.id = e.variant_id
		ORDER BY 3, 1`)
	if err != nil {
		return err
	}
	perGroup := map[int]int{}
	for rows.Next() {
		var id int64
		var body string
		var group int
		if err := rows.Scan(&id, &body, &group); err != nil {
			return err
		}
		want := map[int]int{0: 5, 1: lyricSongs - 5}[group]
		if _, ok := lyricIDs[id]; ok || perGroup[group] >= want || sungSections(body) < 2 {
			continue
		}
		lyricIDs[id] = len(lyricIDs)
		perGroup[group]++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	type song struct {
		id       int64
		workID   *int64
		artist   string
		body     string
		gpSource string
	}
	var songs []song
	rows, err = tx.Query(ctx, `
		SELECT s.id, s.work_id, s.artist, s.body, coalesce(g.filename, '')
		FROM songs s LEFT JOIN song_gp_source_file g ON g.variant_id = s.id
		ORDER BY s.id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var s song
		if err := rows.Scan(&s.id, &s.workID, &s.artist, &s.body, &s.gpSource); err != nil {
			return err
		}
		songs = append(songs, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	loose := len(works)
	for _, s := range songs {
		var title, artist string
		if s.workID != nil && workTitle[*s.workID] != "" {
			title, artist = workTitle[*s.workID], workArtist[*s.workID]
		} else {
			title, artist = SongTitle(loose), am.get(s.artist)
			loose++
		}
		gp := ""
		if s.gpSource != "" {
			gp = gpFilename(title)
		}
		var lyr func(string, int) []string
		if n, ok := lyricIDs[s.id]; ok {
			lyr = demoLyrics(n)
			a.c.add("songs (invented lyrics)", 1)
		}
		body := DemoBody(s.body, title, artist, gp, lyr)
		a.songTitles[s.id], a.songArtists[s.id] = title, artist
		if err := exec(ctx, tx, a.c, "songs", `
			UPDATE songs SET title = $2, artist = $3, reference_url = '', body = $4,
				version = version + CASE WHEN body = $4 THEN 0 ELSE 1 END
			WHERE id = $1`, s.id, title, artist, body); err != nil {
			return err
		}
	}
	return exec(ctx, tx, a.c, "song_audio", `UPDATE song_audio SET source_url = '' WHERE source_url <> ''`)
}

// sungSections counts a body's verse, chorus and bridge sections.
func sungSections(body string) int {
	n := 0
	for _, l := range strings.Split(body, "\n") {
		if m := sectionHeaderRE.FindStringSubmatch(strings.TrimSpace(l)); m != nil && m[2] == "" && sungKinds[importer.SectionKind(m[1])] {
			n++
		}
	}
	return n
}

func gpFilename(title string) string {
	return strings.NewReplacer("/", " ", "\\", " ").Replace(title) + ".gp"
}

// gpFiles: each song's Guitar Pro file under its new title (DemoGP), its
// facts following; its older versions dropped.
func (a *anonymizer) gpFiles(ctx context.Context, tx pgx.Tx) error {
	if err := exec(ctx, tx, a.c, "song_gp_source_file_version", `DELETE FROM song_gp_source_file_version`); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT variant_id FROM song_gp_source_file ORDER BY variant_id`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		var data, facts []byte
		if err := tx.QueryRow(ctx, `SELECT data, facts FROM song_gp_source_file WHERE variant_id = $1`, id).Scan(&data, &facts); err != nil {
			return err
		}
		title, artist := a.songTitles[id], a.songArtists[id]
		out, err := DemoGP(data, title, artist)
		if errors.Is(err, errNotGP) {
			a.c.add("song_gp_source_file (not a .gp, kept as is)", 1)
			out = data
		} else if err != nil {
			return fmt.Errorf("song %d's Guitar Pro file: %w", id, err)
		}
		facts = withFacts(facts, title, artist)
		sum := sha256.Sum256(out)
		if err := exec(ctx, tx, a.c, "song_gp_source_file", `
			UPDATE song_gp_source_file SET data = $2, byte_size = $3, sha256 = $4, filename = $5, facts = $6
			WHERE variant_id = $1`, id, out, len(out), hex.EncodeToString(sum[:]), gpFilename(title), nullBytes(facts)); err != nil {
			return err
		}
	}
	return nil
}

func nullBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// withFacts sets a facts document's title and artist, when it has them.
func withFacts(raw []byte, title, artist string) []byte {
	if len(raw) == 0 {
		return raw
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	for k, v := range map[string]string{"title": title, "artist": artist} {
		if _, ok := m[k]; ok {
			b, _ := json.Marshal(v)
			m[k] = b
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

// setlists: invented titles and venues, no notes; a gig's venue too.
func (a *anonymizer) setlists(ctx context.Context, tx pgx.Tx) error {
	// fixtures' set lists are invented already.
	rows, err := tx.Query(ctx, `SELECT id FROM setlists WHERE title <> ALL($1) ORDER BY id`, fixtureSetlists)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for i, id := range ids {
		if err := exec(ctx, tx, a.c, "setlists", `
			UPDATE setlists SET title = $2, notes = '', venue = CASE WHEN venue = '' THEN '' ELSE $3 END WHERE id = $1`,
			id, SetlistTitle(i), Venue(int(id))); err != nil {
			return err
		}
		if err := exec(ctx, tx, a.c, "setlist_items", `UPDATE setlist_items SET note = '' WHERE setlist_id = $1 AND note <> ''`, id); err != nil {
			return err
		}
	}
	return exec(ctx, tx, a.c, "setlist_gigs", `
		UPDATE setlist_gigs SET venue = CASE WHEN venue = '' THEN '' ELSE (ARRAY[`+sqlStrings(venues)+`])[1 + id % $1] END, notes = ''`, len(venues))
}

// Event names by type, invented.
var eventNames = map[string][]string{
	"jam":             {"Jam du Canal", "Jam du jeudi", "Grande jam de saison"},
	"concert":         {"Concert", "Concert de fin d'année", "Soirée concert"},
	"meetup":          {"Rencontre", "Apéro des musiciens", "Questions-réponses"},
	"listening_party": {"Écoute collective"},
	"dinner":          {"Dîner de l'association"},
	"show_together":   {"Sortie concert"},
	"rehearsal":       {"Répétition"},
	"lesson":          {"Cours"},
	"open_mic":        {"Scène ouverte"},
	"masterclass":     {"Master class"},
}

// events: invented names, places, no descriptions (the fixtures' own,
// already invented, stay as they are).
func (a *anonymizer) events(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `SELECT id, event_type::text, location <> '' FROM events ORDER BY id`)
	if err != nil {
		return err
	}
	type ev struct {
		id       int64
		kind     string
		hasPlace bool
	}
	var evs []ev
	for rows.Next() {
		var e ev
		if err := rows.Scan(&e.id, &e.kind, &e.hasPlace); err != nil {
			return err
		}
		evs = append(evs, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	fixtures, err := fixtureEventNames(ctx, tx)
	if err != nil {
		return err
	}
	for _, e := range evs {
		if fixtures[e.id] {
			continue
		}
		names := eventNames[e.kind]
		if len(names) == 0 {
			names = []string{"Événement"}
		}
		place := ""
		if e.hasPlace {
			place = Venue(int(e.id))
		}
		if err := exec(ctx, tx, a.c, "events", `UPDATE events SET name = $2, description = '', location = $3 WHERE id = $1`,
			e.id, names[int(e.id)%len(names)], place); err != nil {
			return err
		}
	}
	return nil
}

// looks: every community and band a new look (SetBrand), no logo, no
// website.
func (a *anonymizer) looks(ctx context.Context) error {
	for id, name := range a.communityNames {
		if err := a.st.SetBrand(ctx, "community", id, Look(name, int(id))); err != nil {
			return err
		}
		a.c.add("communities (looks)", 1)
	}
	for id, name := range a.bandNames {
		if err := a.st.SetBrand(ctx, "band", id, Look(name, a.bandRank[id]+3)); err != nil {
			return err
		}
		a.c.add("bands (looks)", 1)
	}
	return nil
}

// songbookFiles: a songbook's download area. Its PDFs (a course's book,
// its author's name on every page) go; its Guitar Pro files are
// rewritten (DemoGP); every file and folder is renamed after its song or
// lesson.
func (a *anonymizer) songbookFiles(ctx context.Context) error {
	files, err := a.allFiles(ctx)
	if err != nil {
		return err
	}
	lessonTitles := map[int64]string{}
	rows, err := a.db.Query(ctx, `SELECT id, title FROM lessons`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var t string
		if err := rows.Scan(&id, &t); err != nil {
			return err
		}
		lessonTitles[id] = t
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	owner := map[int64]string{} // songbook → its community's or band's new name
	rows, err = a.db.Query(ctx, `SELECT id, coalesce(owner_community_id, 0), coalesce(owner_band_id, 0) FROM songbooks`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, c, b int64
		if err := rows.Scan(&id, &c, &b); err != nil {
			return err
		}
		owner[id] = a.communityNames[c] + a.bandNames[b]
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	used := map[string]bool{}
	type rename struct {
		id             int64
		path, filename string
	}
	var renames []rename
	for _, f := range files {
		if strings.EqualFold(path.Ext(f.Filename), ".pdf") || f.Kind == "book" {
			if err := a.st.DeleteSongbookFile(ctx, f.ID); err != nil {
				return err
			}
			a.c.add("songbook_files (PDFs removed)", 1)
			continue
		}
		title := ""
		switch {
		case f.VariantID != nil && a.songTitles[*f.VariantID] != "":
			title = a.songTitles[*f.VariantID]
		case f.LessonID != nil && lessonTitles[*f.LessonID] != "":
			title = lessonTitles[*f.LessonID]
		default:
			title = fmt.Sprintf("Fichier %d", f.ID)
		}
		if f.Kind == "gp" || strings.EqualFold(path.Ext(f.Filename), ".gp") {
			if err := a.rewriteFileGP(ctx, f, title, owner[f.SongbookID]); err != nil {
				return err
			}
		}
		dir := ""
		if f.Path != "" {
			if n := lessonNumber(path.Base(f.Path)); n > 0 {
				dir = fmt.Sprintf("%02d_%s", n, LessonTitle(n))
			} else {
				dir = "Fichiers"
			}
		}
		stem := title
		if n := lessonNumber(f.Filename); n > 0 {
			stem = fmt.Sprintf("%02d_%s", n, title)
		}
		stem = strings.NewReplacer("/", " ", "\\", " ").Replace(stem)
		ext := path.Ext(f.Filename)
		name := stem + ext
		for k := 2; used[fmt.Sprint(f.SongbookID, "/", dir, "/", name)]; k++ {
			name = fmt.Sprintf("%s (%d)%s", stem, k, ext)
		}
		used[fmt.Sprint(f.SongbookID, "/", dir, "/", name)] = true
		renames = append(renames, rename{f.ID, dir, name})
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE songbook_files SET filename = 'sbdemo-tmp-' || id`); err != nil {
		return err
	}
	for _, r := range renames {
		if err := exec(ctx, tx, a.c, "songbook_files", `UPDATE songbook_files SET path = $2, filename = $3, title = '' WHERE id = $1`,
			r.id, r.path, r.filename); err != nil {
			return err
		}
	}
	if err := exec(ctx, tx, a.c, "songbook_downloads", `
		UPDATE songbook_downloads d SET name = coalesce((SELECT filename FROM songbook_files f WHERE f.id = d.file_id), '')`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// lessonNumber is the number a name starts with ("07_…"), 0 for none.
func lessonNumber(name string) int {
	n, i := 0, 0
	for i < len(name) && i < 3 && name[i] >= '0' && name[i] <= '9' {
		n = n*10 + int(name[i]-'0')
		i++
	}
	if i == 0 || i == len(name) || (name[i] != '_' && name[i] != ' ') {
		return 0
	}
	return n
}

func (a *anonymizer) allFiles(ctx context.Context) ([]store.SongbookFile, error) {
	rows, err := a.db.Query(ctx, `SELECT DISTINCT songbook_id FROM songbook_files ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return a.st.ListSongbookFiles(ctx, ids)
}

// rewriteFileGP stores a download area's .gp file rewritten (DemoGP) as
// a new blob, the old one dropped when nothing else uses it.
func (a *anonymizer) rewriteFileGP(ctx context.Context, f store.SongbookFile, title, artist string) error {
	data, err := io.ReadAll(a.st.OpenSongbookFile(ctx, f))
	if err != nil {
		return err
	}
	out, err := DemoGP(data, title, artist)
	if errors.Is(err, errNotGP) {
		a.c.add("songbook_files (not a .gp, kept as is)", 1)
		return nil
	}
	if err != nil {
		return fmt.Errorf("file %d: %w", f.ID, err)
	}
	if bytes.Equal(out, data) {
		return nil
	}
	blob, _, err := a.st.PutFileBlob(ctx, bytes.NewReader(out))
	if err != nil {
		return err
	}
	if blob == f.BlobID {
		return nil
	}
	if _, err := a.db.Exec(ctx, `UPDATE songbook_files SET blob_id = $2, modified_at = now() WHERE id = $1`, f.ID, blob); err != nil {
		return err
	}
	if _, err := a.db.Exec(ctx, `
		DELETE FROM file_blobs b WHERE b.id = $1
		  AND NOT EXISTS (SELECT 1 FROM songbook_files f WHERE f.blob_id = b.id)`, f.BlobID); err != nil {
		return err
	}
	a.c.add("songbook_files (Guitar Pro rewritten)", 1)
	return nil
}
