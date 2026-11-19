package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // Europe/Paris, in an image with no zoneinfo

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"app/internal/setlist"
	"app/internal/store"
)

// fixtures makes the coaching and calendar features look alive, on an
// anonymized copy: students, voices, groups, lessons, jams, rehearsals,
// gigs, all invented. Everything is found again before being made (a
// person by username, a group by name, an event by owner, kind, name and
// start, a set list by band and title, a note by its text), so a second
// run adds only what the first didn't (new weeks, say).

// The names fixtures gives events and set lists: anonymize leaves them
// alone when it runs again.
const (
	evMardi       = "Cours collectif · Mardi soir"
	evJeudi       = "Cours collectif · Jeudi midi"
	evPrivate     = "Cours particulier · "
	evMasterclass = "Master class : souffle et résonance"
	evSpring      = "Master class de printemps"
	evOpenMic     = "Scène ouverte du Canal"
	evJam         = "Jam du vendredi"
	evRehearsal   = "Répétition"
	evQA          = "Questions-réponses en direct"
	evGig         = "Concert · "
)

var fixtureSetlists = []string{"Concert · La Péniche Rouge", "Concert · Le Quai 9"}

func isFixtureEvent(name string) bool {
	switch name {
	case evMardi, evJeudi, evMasterclass, evSpring, evOpenMic, evJam, evRehearsal, evQA:
		return true
	}
	return strings.HasPrefix(name, evPrivate) || strings.HasPrefix(name, evGig)
}

func fixtureEventNames(ctx context.Context, tx pgx.Tx) (map[int64]bool, error) {
	rows, err := tx.Query(ctx, `SELECT id, name FROM events`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		if isFixtureEvent(name) {
			out[id] = true
		}
	}
	return out, rows.Err()
}

// A voice type's ranges, MIDI notes, chest, mixed and head.
type voiceType struct {
	name   string
	ranges [3][2]int
	key    int // how far from the original key its songs usually go
}

var voiceTypes = []voiceType{
	{"soprano", [3][2]int{{57, 67}, {62, 76}, {69, 84}}, 3},
	{"alto", [3][2]int{{53, 64}, {58, 71}, {65, 77}}, -1},
	{"tenor", [3][2]int{{45, 60}, {55, 67}, {62, 74}}, -3},
	{"baryton", [3][2]int{{41, 55}, {50, 62}, {57, 69}}, -5},
	{"mezzo", [3][2]int{{55, 65}, {60, 72}, {67, 79}}, 1},
}

var rangeNotes = [3][]string{
	{"Solide et chaleureuse", "Tendance à appuyer en fin de phrase", "Bien ancrée, timbre rond"},
	{"Le passage se stabilise", "À travailler sur les voyelles fermées", "Mixte encore fragile vers le haut"},
	{"Légère, à soutenir", "Belle couleur en pianissimo", "S'ouvre bien après l'échauffement"},
}

type fixer struct {
	db  *pgxpool.Pool
	st  *store.Store
	c   counts
	loc *time.Location
	now time.Time
	end time.Time
}

func fixtures(ctx context.Context, db *pgxpool.Pool, st *store.Store, now time.Time) (counts, error) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return nil, err
	}
	f := &fixer{db: db, st: st, c: counts{}, loc: loc, now: now.In(loc)}
	f.end = f.now.AddDate(0, 0, 56)
	return f.c, f.run(ctx)
}

func (f *fixer) communityByName(ctx context.Context, name string) (int64, error) {
	var id int64
	err := f.db.QueryRow(ctx, `SELECT id FROM communities WHERE name = $1 ORDER BY id LIMIT 1`, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("no community %q: run sbdemo anonymize first", name)
	}
	return id, err
}

func (f *fixer) run(ctx context.Context) error {
	lucas, err := f.st.GetPersonByUsername(ctx, keptUsername)
	if err != nil {
		return fmt.Errorf("%s: %w", keptUsername, err)
	}
	lumen, err := f.communityByName(ctx, "Studio Lumen")
	if err != nil {
		return err
	}
	jam, err := f.communityByName(ctx, "Jam du Canal")
	if err != nil {
		return err
	}
	blues, err := f.communityByName(ctx, "Atelier Blues Rive Gauche")
	if err != nil {
		return err
	}
	if err := f.st.SetCommunityKind(ctx, lumen, "coaching"); err != nil {
		return err
	}
	coach, err := f.staff(ctx, lumen)
	if err != nil {
		return err
	}
	for _, c := range []int64{lumen, jam, blues} {
		if err := f.member(ctx, c, lucas.ID, "member"); err != nil {
			return err
		}
	}
	jamHost, err := f.staff(ctx, jam)
	if err != nil {
		jamHost = lucas.ID
	}
	bluesHost, err := f.staff(ctx, blues)
	if err != nil {
		bluesHost = lucas.ID
	}

	repertoire, err := f.repertoire(ctx, lumen)
	if err != nil {
		return err
	}
	if len(repertoire) == 0 {
		return fmt.Errorf("Studio Lumen has no songs to give its students")
	}

	// Students.
	students := make([]int64, 13)
	for k := range students {
		if students[k], err = f.student(ctx, lumen, coach, k, repertoire); err != nil {
			return err
		}
	}
	// Lucas is one too, a baritone in the Tuesday group.
	if err := f.voice(ctx, lumen, lucas.ID, coach, 3, 1); err != nil {
		return err
	}
	if err := f.studentSongs(ctx, lumen, lucas.ID, lucasName, coach, 3, 2, 4, repertoire); err != nil {
		return err
	}
	if err := f.note(ctx, lumen, lucas.ID, coach, TeachingNote(7)); err != nil {
		return err
	}

	// Groups: two year-long classes and a spring master class.
	sy := f.now.Year()
	if f.now.Month() < time.September {
		sy--
	}
	seasonStart := time.Date(sy, time.September, 1, 0, 0, 0, 0, time.UTC)
	seasonEnd := time.Date(sy+1, time.June, 30, 0, 0, 0, 0, time.UTC)
	mardi, err := f.group(ctx, lumen, "Mardi soir", "year", &seasonStart, &seasonEnd, nil,
		append(append([]int64{}, students[0:6]...), lucas.ID), coach, repertoire, 0)
	if err != nil {
		return err
	}
	jeudi, err := f.group(ctx, lumen, "Jeudi midi", "year", &seasonStart, &seasonEnd, nil, students[6:10], coach, repertoire, 4)
	if err != nil {
		return err
	}
	spring := thirdSaturday(sy+1, time.March, f.loc).Add(10 * time.Hour)
	springEv, err := f.event(ctx, "community", lumen, "masterclass", evSpring,
		"Une journée pour préparer les auditions de juin : interprétation, présence scénique, travail en petits groupes.",
		"Studio Mistral", spring, spring.Add(7*time.Hour), "members", coach)
	if err != nil {
		return err
	}
	springGroup := []int64{students[0], students[2], students[4], students[6], students[8], students[10], students[11], students[12]}
	if _, err := f.group(ctx, lumen, "Master class de printemps", "oneoff", nil, nil, &springEv, springGroup, coach, repertoire, 8); err != nil {
		return err
	}
	for _, p := range springGroup {
		if err := f.st.RegisterForEvent(ctx, springEv, p, 0); err != nil {
			return err
		}
	}

	// Lessons, the next eight weeks.
	for _, t := range f.weekly(time.Tuesday, 19, 0, 1, 0) {
		if err := f.lesson(ctx, lumen, coach, evMardi, "Échauffement, travail de groupe sur le répertoire, puis passages individuels.",
			"Studio Mistral, salle 2", t, 90, "in_person", &mardi, nil); err != nil {
			return err
		}
	}
	for _, t := range f.weekly(time.Thursday, 12, 30, 1, 0) {
		if err := f.lesson(ctx, lumen, coach, evJeudi, "Une heure à la pause de midi : respiration, vocalises, un morceau du répertoire.",
			"En ligne", t, 60, "online", &jeudi, nil); err != nil {
			return err
		}
	}
	privates := []struct {
		student  int64
		day      time.Weekday
		h, m     int
		every    int
		modes    []string
		lengthMn int
	}{
		{students[10], time.Monday, 18, 0, 2, []string{"in_person"}, 45},
		{students[11], time.Wednesday, 17, 30, 1, []string{"online"}, 45},
		{students[12], time.Saturday, 10, 0, 2, []string{"in_person", "online"}, 60},
	}
	for _, pv := range privates {
		p, err := f.st.GetPersonByID(ctx, pv.student)
		if err != nil {
			return err
		}
		first := strings.Fields(p.DisplayName)[0]
		for i, t := range f.weekly(pv.day, pv.h, pv.m, pv.every, 0) {
			mode := pv.modes[i%len(pv.modes)]
			place := "Studio Mistral, salle 1"
			if mode == "online" {
				place = "En ligne"
			}
			if err := f.lesson(ctx, lumen, coach, evPrivate+first, "Cours individuel : technique vocale et travail du répertoire personnel.",
				place, t, pv.lengthMn, mode, nil, []int64{pv.student}); err != nil {
				return err
			}
		}
	}
	if sats := f.weekly(time.Saturday, 14, 0, 1, 0); len(sats) > 4 {
		t := sats[4]
		mc, err := f.event(ctx, "community", lumen, "masterclass", evMasterclass,
			"Un après-midi sur le souffle et la résonance : exercices en groupe, puis chacun chante un morceau devant les autres.",
			"Studio Mistral", t, t.Add(4*time.Hour), "members", coach)
		if err != nil {
			return err
		}
		for _, p := range append(append([]int64{}, students[:8]...), lucas.ID) {
			if err := f.st.RegisterForEvent(ctx, mc, p, 0); err != nil {
				return err
			}
		}
	}

	// Jam du Canal: an open mic every other Friday, a jam the Fridays
	// between.
	for _, t := range f.weekly(time.Friday, 20, 30, 2, 0) {
		if _, err := f.event(ctx, "community", jam, "open_mic", evOpenMic,
			"Trois morceaux chacun, backline sur place (batterie, ampli basse, deux amplis guitare). Inscription au bar.",
			"La Péniche Rouge", t, t.Add(3*time.Hour), "members", jamHost); err != nil {
			return err
		}
	}
	for _, t := range f.weekly(time.Friday, 20, 30, 2, 1) {
		if _, err := f.event(ctx, "community", jam, "jam", evJam,
			"Jam ouverte : on choisit les morceaux dans le répertoire de la communauté, tous niveaux bienvenus.",
			"Le Quai 9", t, t.Add(3*time.Hour), "both", jamHost); err != nil {
			return err
		}
	}
	// Atelier Blues: a live Q&A.
	if weds := f.weekly(time.Wednesday, 19, 0, 1, 0); len(weds) > 1 {
		t := weds[1]
		if _, err := f.event(ctx, "community", blues, "meetup", evQA,
			"Une heure en direct pour vos questions sur les leçons du moment : shuffle, bends, mise en place.",
			"En ligne", t, t.Add(time.Hour), "members", bluesHost); err != nil {
			return err
		}
	}

	// Two of Lucas's bands: weekly rehearsals and a gig each.
	bands, err := f.lucasBands(ctx, lucas.ID)
	if err != nil {
		return err
	}
	plans := []struct {
		day     time.Weekday
		h, m    int
		studio  string
		gigWeek int
		venue   string
	}{
		{time.Monday, 20, 0, "Studio Mistral", 2, "La Péniche Rouge"},
		{time.Wednesday, 20, 30, "Studio Bleu Nuit", 5, "Le Quai 9"},
	}
	for i, b := range bands {
		if i >= len(plans) {
			break
		}
		pl := plans[i]
		for _, t := range f.weekly(pl.day, pl.h, pl.m, 1, 0) {
			if _, err := f.event(ctx, "band", b, "rehearsal", evRehearsal,
				"On cale le set du prochain concert, puis deux nouveaux morceaux.",
				pl.studio, t, t.Add(150*time.Minute), "members", lucas.ID); err != nil {
				return err
			}
		}
		sats := f.weekly(time.Saturday, 21, 0, 1, 0)
		if len(sats) <= pl.gigWeek {
			continue
		}
		t := sats[pl.gigWeek]
		ev, err := f.event(ctx, "band", b, "concert", evGig+pl.venue,
			"Deux sets, une pause entre les deux. Balances à 18 h.", pl.venue, t, t.Add(150*time.Minute), "members", lucas.ID)
		if err != nil {
			return err
		}
		sl, err := f.setlist(ctx, b, fixtureSetlists[i], pl.venue, t, lucas.ID)
		if err != nil {
			return err
		}
		if sl != 0 {
			if err := f.st.SetEventSetlist(ctx, ev, sl); err != nil {
				return err
			}
		}
	}
	return nil
}

// staff is a community's first current staff member.
func (f *fixer) staff(ctx context.Context, community int64) (int64, error) {
	var id int64
	err := f.db.QueryRow(ctx, `
		SELECT m.person_id FROM community_members m JOIN community_roles r ON r.id = m.role_id
		WHERE m.community_id = $1 AND upper_inf(m.validity) AND r.builtin_key = 'staff'
		ORDER BY m.id LIMIT 1`, community).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("community %d has no staff", community)
	}
	return id, err
}

// member makes person a current member of community, when they aren't.
func (f *fixer) member(ctx context.Context, community, person int64, role string) error {
	ok, err := f.st.IsCurrentCommunityMember(ctx, community, person)
	if err != nil || ok {
		return err
	}
	if _, err := f.st.AddCommunityMember(ctx, community, person, role, 0); err != nil {
		return err
	}
	f.c.add("community_members", 1)
	return f.track(ctx, "membership", community, person)
}

// repertoire is a community's main songbook's songs, by id.
func (f *fixer) repertoire(ctx context.Context, community int64) ([]int64, error) {
	rows, err := f.db.Query(ctx, `
		SELECT e.variant_id FROM songbook_entries e
		JOIN songbooks b ON b.id = e.songbook_id
		WHERE b.owner_community_id = $1 AND b.is_default
		ORDER BY e.variant_id`, community)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

// student makes the k-th invented student of community: a person (no
// email, no password), a member, a voice, a songbook, a note or two.
func (f *fixer) student(ctx context.Context, community, coach int64, k int, repertoire []int64) (int64, error) {
	username := fmt.Sprintf("demo-student-%02d", k+1)
	name := PersonName(500 + k)
	var id int64
	if p, err := f.st.GetPersonByUsername(ctx, username); err == nil {
		id = p.ID
	} else if id, err = f.st.CreatePerson(ctx, username, name, ""); err != nil {
		return 0, err
	} else {
		f.c.add("people (students)", 1)
		if err := f.track(ctx, "person", id, 0); err != nil {
			return 0, err
		}
	}
	if err := f.member(ctx, community, id, "member"); err != nil {
		return 0, err
	}
	vt := k % len(voiceTypes)
	if err := f.voice(ctx, community, id, coach, vt, k); err != nil {
		return 0, err
	}
	if err := f.studentSongs(ctx, community, id, name, coach, vt, k, 3+k%3, repertoire); err != nil {
		return 0, err
	}
	had, err := f.st.StudentNotes(ctx, community, id)
	if err != nil {
		return 0, err
	}
	if err := f.st.SetStudentNotes(ctx, community, id, StudentProfile(k), coach); err != nil {
		return 0, err
	}
	if had == "" { // the copy's own notes stay when the fixtures go
		if err := f.track(ctx, "student_notes", community, id); err != nil {
			return 0, err
		}
	}
	for j := 0; j <= k%2; j++ {
		if err := f.note(ctx, community, id, coach, TeachingNote(k+j*5)); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// voice notes a student's three registers, a voice type's ranges moved a
// little (k), the registers overlapping.
func (f *fixer) voice(ctx context.Context, community, person, coach int64, vt, k int) error {
	had, err := f.st.StudentRanges(ctx, community, person)
	if err != nil {
		return err
	}
	shift := k%3 - 1
	for i, reg := range store.Registers {
		lo, hi := voiceTypes[vt].ranges[i][0]+shift, voiceTypes[vt].ranges[i][1]+shift+(k%2)
		r := store.VoiceRange{Register: reg, Low: &lo, High: &hi, Note: rangeNotes[i][(k+i)%len(rangeNotes[i])]}
		if err := f.st.SetStudentRange(ctx, community, person, r, coach); err != nil {
			return err
		}
		f.c.add("student_voice_ranges", 1)
	}
	if len(had) > 0 { // the copy's own ranges: not the fixtures' to remove
		return nil
	}
	return f.track(ctx, "ranges", community, person)
}

// studentSongs fills a student's songbook: n songs of the repertoire, in
// keys that suit the voice.
func (f *fixer) studentSongs(ctx context.Context, community, person int64, name string, coach int64, vt, k, n int, repertoire []int64) error {
	had, err := f.st.FindStudentSongbook(ctx, community, person)
	if err != nil {
		return err
	}
	sb, err := f.st.StudentSongbook(ctx, community, person, name+"’s songs", coach)
	if err != nil {
		return err
	}
	if had == 0 { // the copy's own songbook stays when the fixtures go
		if err := f.track(ctx, "songbook", sb, 0); err != nil {
			return err
		}
	}
	return f.fill(ctx, sb, coach, repertoire, k*2, n, voiceTypes[vt].key, "songbook_entries (students)")
}

// fill puts n songs of repertoire (from the start-th) in songbook sb,
// each in a key near key.
func (f *fixer) fill(ctx context.Context, sb, by int64, repertoire []int64, start, n, key int, what string) error {
	seen := map[int64]bool{}
	for j := 0; j < n && j < len(repertoire); j++ {
		v := repertoire[(start+j*3)%len(repertoire)]
		if seen[v] {
			continue
		}
		seen[v] = true
		if err := f.st.AddToSongbook(ctx, sb, v, by); err != nil {
			return err
		}
		semi := key + (j%3 - 1)
		semi = max(-12, min(12, semi))
		if err := f.st.SetEntrySemitones(ctx, sb, v, semi); err != nil {
			return err
		}
		f.c.add(what, 1)
	}
	return nil
}

// note adds a teaching note, unless the student has it already.
func (f *fixer) note(ctx context.Context, community, person, coach int64, body string) error {
	var n int
	if err := f.db.QueryRow(ctx, `SELECT count(*) FROM teaching_notes WHERE community_id = $1 AND person_id = $2 AND body = $3`,
		community, person, body).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	id, err := f.st.AddTeachingNote(ctx, community, person, nil, body, coach)
	if err != nil {
		return err
	}
	f.c.add("teaching_notes", 1)
	return f.track(ctx, "teaching_note", id, 0)
}

// group finds or makes a coaching group, sets its members and puts four
// songs in its songbook.
func (f *fixer) group(ctx context.Context, community int64, name, kind string, starts, ends *time.Time, event *int64,
	members []int64, coach int64, repertoire []int64, start int) (int64, error) {
	var id int64
	err := f.db.QueryRow(ctx, `SELECT id FROM coaching_groups WHERE community_id = $1 AND name = $2`, community, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		if id, err = f.st.CreateCoachingGroup(ctx, community, name, kind, starts, ends, event); err != nil {
			return 0, err
		}
		f.c.add("coaching_groups", 1)
		if err := f.track(ctx, "group", id, 0); err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	}
	if err := f.st.SetGroupMembers(ctx, id, members, coach); err != nil {
		return 0, err
	}
	g, err := f.st.GetCoachingGroup(ctx, id)
	if err != nil {
		return 0, err
	}
	if g.SongbookID != 0 {
		if err := f.fill(ctx, g.SongbookID, coach, repertoire, start, 4, -2+start%3, "songbook_entries (groups)"); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// weekly is the times on day at h:m in the next eight weeks, every
// `every` weeks: the weeks whose number (weekNumber) is parity modulo
// every, so that another run picks the same ones.
func (f *fixer) weekly(day time.Weekday, h, m, every, parity int) []time.Time {
	var out []time.Time
	d := time.Date(f.now.Year(), f.now.Month(), f.now.Day(), h, m, 0, 0, f.loc)
	for ; d.Before(f.end); d = time.Date(d.Year(), d.Month(), d.Day()+1, h, m, 0, 0, f.loc) {
		if d.Weekday() != day || !d.After(f.now) || holiday(d) {
			continue
		}
		if every > 1 && weekNumber(d)%every != parity {
			continue
		}
		out = append(out, d)
	}
	return out
}

// weekNumber counts weeks from Monday 1 January 2024: unlike ISO week
// numbers, it never repeats a parity at a year's end.
func weekNumber(d time.Time) int {
	day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	return int(day.Sub(time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)).Hours()) / 24 / 7
}

// holiday: Christmas Eve and Day, New Year's Eve and Day; nobody
// rehearses then.
func holiday(d time.Time) bool {
	m, n := d.Month(), d.Day()
	return (m == time.December && (n == 24 || n == 25 || n == 31)) || (m == time.January && n == 1)
}

func thirdSaturday(year int, month time.Month, loc *time.Location) time.Time {
	d := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	for d.Weekday() != time.Saturday {
		d = d.AddDate(0, 0, 1)
	}
	return d.AddDate(0, 0, 14)
}

// event finds or makes an event (owner, kind, name and start say which).
func (f *fixer) event(ctx context.Context, ownerKind string, owner int64, kind, name, desc, place string,
	starts, ends time.Time, scope string, creator int64) (int64, error) {
	var id int64
	err := f.db.QueryRow(ctx, `
		SELECT id FROM events WHERE owner_kind = $1::event_owner_kind AND coalesce(owner_community_id, owner_band_id) = $2
		  AND event_type = $3::event_type AND name = $4 AND starts_at = $5`,
		ownerKind, owner, kind, name, starts).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	if id, err = f.st.CreateEvent(ctx, ownerKind, owner, kind, name, desc, place, starts, &ends, scope, creator); err != nil {
		return 0, err
	}
	f.c.add("events ("+kind+")", 1)
	return id, f.track(ctx, "event", id, 0)
}

// lesson finds or makes a lesson: a group's (its members registered) or
// for students one by one.
func (f *fixer) lesson(ctx context.Context, community, coach int64, name, desc, place string, t time.Time, minutes int,
	mode string, group *int64, students []int64) error {
	id, err := f.event(ctx, "community", community, "lesson", name, desc, place, t, t.Add(time.Duration(minutes)*time.Minute), "members", coach)
	if err != nil {
		return err
	}
	if err := f.st.SetLessonDetails(ctx, id, mode, group); err != nil {
		return err
	}
	if group != nil {
		g, err := f.st.GetCoachingGroup(ctx, *group)
		if err != nil {
			return err
		}
		for _, m := range g.Members {
			students = append(students, m.PersonID)
		}
	}
	for _, p := range students {
		if err := f.st.RegisterForEvent(ctx, id, p, 0); err != nil {
			return err
		}
	}
	return nil
}

// lucasBands are the two bands of Lucas's with the most songs.
func (f *fixer) lucasBands(ctx context.Context, person int64) ([]int64, error) {
	rows, err := f.db.Query(ctx, `
		SELECT m.band_id FROM band_members m
		LEFT JOIN songbooks b ON b.owner_band_id = m.band_id AND b.is_default
		LEFT JOIN songbook_entries e ON e.songbook_id = b.id
		WHERE m.person_id = $1 AND upper_inf(m.validity)
		GROUP BY m.band_id ORDER BY count(e.id) DESC, m.band_id LIMIT 2`, person)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

// setlist finds or makes a band's set list for a gig: two sets of its
// songs and an encore.
func (f *fixer) setlist(ctx context.Context, band int64, title, venue string, date time.Time, by int64) (int64, error) {
	var id int64
	err := f.db.QueryRow(ctx, `SELECT id FROM setlists WHERE owner_band_id = $1 AND title = $2`, band, title).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	rows, err := f.db.Query(ctx, `
		SELECT e.variant_id FROM songbook_entries e JOIN songbooks b ON b.id = e.songbook_id
		WHERE b.owner_band_id = $1 AND b.is_default ORDER BY e.variant_id LIMIT 13`, band)
	if err != nil {
		return 0, err
	}
	songs, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil || len(songs) == 0 {
		return 0, err
	}
	l := &setlist.Setlist{}
	l.Set("title", title)
	l.Set("date", date.Format("2006-01-02"))
	l.Set("venue", venue)
	half := (len(songs) - 1) / 2
	sets := []struct {
		label string
		songs []int64
	}{{"Set 1", songs[:half]}, {"Set 2", songs[half : len(songs)-1]}, {"Rappel", songs[len(songs)-1:]}}
	for _, s := range sets {
		if len(s.songs) == 0 {
			continue
		}
		set := setlist.Set{Label: s.label}
		for _, v := range s.songs {
			set.Entries = append(set.Entries, setlist.Entry{VariantID: v})
		}
		l.Sets = append(l.Sets, set)
	}
	b := band
	if id, err = f.st.CreateSetlist(ctx, "band", title, "", nil, &b, nil, by); err != nil {
		return 0, err
	}
	if _, err := f.st.SaveSetlistByID(ctx, id, l, title, "", "", by); err != nil {
		return 0, err
	}
	f.c.add("setlists", 1)
	return id, f.track(ctx, "setlist", id, 0)
}

// track notes a row the fixtures made, for `sbdemo remove`.
func (f *fixer) track(ctx context.Context, kind string, id, ref int64) error {
	_, err := f.db.Exec(ctx, `INSERT INTO demo_fixture_rows (kind, id, ref) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, kind, id, ref)
	return err
}

// removeFixtures takes away every row the fixtures made (demo_fixture_rows),
// the latest kinds first: events, set lists, groups, notes, ranges,
// songbooks, memberships, then the invented people.
func removeFixtures(ctx context.Context, db *pgxpool.Pool) (counts, error) {
	c := counts{}
	steps := []struct{ kind, sql string }{
		{"event", `DELETE FROM events WHERE id IN (SELECT id FROM demo_fixture_rows WHERE kind = 'event')`},
		{"setlist", `DELETE FROM setlists WHERE id IN (SELECT id FROM demo_fixture_rows WHERE kind = 'setlist')`},
		{"group", `DELETE FROM songbooks WHERE id IN (SELECT songbook_id FROM coaching_groups WHERE id IN (SELECT id FROM demo_fixture_rows WHERE kind = 'group'))`},
		{"group", `DELETE FROM coaching_groups WHERE id IN (SELECT id FROM demo_fixture_rows WHERE kind = 'group')`},
		{"teaching_note", `DELETE FROM teaching_notes WHERE id IN (SELECT id FROM demo_fixture_rows WHERE kind = 'teaching_note')`},
		{"student_notes", `DELETE FROM student_notes n USING demo_fixture_rows d WHERE d.kind = 'student_notes' AND n.community_id = d.id AND n.person_id = d.ref`},
		{"ranges", `DELETE FROM student_voice_ranges r USING demo_fixture_rows d WHERE d.kind = 'ranges' AND r.community_id = d.id AND r.person_id = d.ref`},
		{"songbook", `DELETE FROM songbooks WHERE id IN (SELECT id FROM demo_fixture_rows WHERE kind = 'songbook')`},
		{"membership", `DELETE FROM community_members m USING demo_fixture_rows d WHERE d.kind = 'membership' AND m.community_id = d.id AND m.person_id = d.ref`},
		{"person", `DELETE FROM people WHERE id IN (SELECT id FROM demo_fixture_rows WHERE kind = 'person')`},
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	for _, st := range steps {
		tag, err := tx.Exec(ctx, st.sql)
		if err != nil {
			return nil, fmt.Errorf("removing %s: %w", st.kind, err)
		}
		c.add(st.kind, tag.RowsAffected())
	}
	if _, err := tx.Exec(ctx, `DELETE FROM demo_fixture_rows`); err != nil {
		return nil, err
	}
	return c, tx.Commit(ctx)
}
