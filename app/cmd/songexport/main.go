// songexport writes a complete, human-readable, git-friendly backup of
// every song, songbook and membership to plain files on disk — a
// second, file-based backup alongside the regular pg_dump (see
// deploy/backup-db.sh), restorable with cmd/songimport even without
// Postgres access to the original database.
//
// Layout:
//
//	songbooks/<owner>/<songbook>/<title>.song   one real .song source file per song
//	songbooks/<band>/members.txt                band roster (see membersfmt.go)
//	songbooks/<community>/members.txt           community roster + role definitions
//	songbooks/_catalog/...                      public-catalogue songs (no person/band/community owner)
//	people.json                                 every person referenced anywhere (for username resolution on import)
//	variants.json                               per-song metadata a directory tree alone can't carry:
//	                                             visibility, true owner, work (title/artist/genre/album),
//	                                             fork lineage, every songbook it's actually filed in (a song
//	                                             can be in more than one), and song_order position.
//
// One song gets exactly one .song file (dedup by variant id) even if it
// belongs to several songbooks — variants.json's songbookMemberships
// list is what records every one of those memberships; the file itself
// lives under what this tool calls its "primary" songbook (the song's
// own owner's songbook if it has one there, else the lowest-id songbook
// that contains it, else "_unfiled").
//
// Usage:
//
//	songexport --db "$DATABASE_URL" --out ./songbooks-export
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"app/internal/membersfmt"
)

type person struct {
	ID          int64    `json:"id"`
	Username    string   `json:"username"`
	DisplayName string   `json:"displayName"`
	Email       string   `json:"email,omitempty"`
	IsAdmin     bool     `json:"isAdmin,omitempty"`
	Instruments []string `json:"instruments,omitempty"`
}

type band struct {
	ID        int64
	Name      string
	MusicType string
}

type community struct {
	ID              int64
	Name            string
	DefaultJoinRole string
}

// songGroup mirrors internal/store.SongGroup exactly — song_order is
// stored as a JSON array of these (label + the variant ids in that
// group), NOT a flat id list (confirmed against internal/store/order.go
// — an earlier version of this tool assumed []int64 and silently
// unmarshaled every songbook's order to empty, since the shapes don't
// match and json.Unmarshal's error was being ignored).
type songGroup struct {
	Label      string  `json:"Label"`
	VariantIDs []int64 `json:"VariantIDs"`
}

type songbook struct {
	ID        int64
	OwnerKind string // person, band, community
	OwnerID   int64
	Name      string
	IsDefault bool
	SongOrder []songGroup
}

// songbookRecord is this songbook's entry in songbooks.json — enough for
// songimport to recreate it (if CreateVariant's own default-songbook
// side effect didn't already) and, once every variant has a new id,
// reconstruct its song_order exactly via SaveOrder.
type songbookRecord struct {
	OwnerKind string      `json:"ownerKind"`
	OwnerName string      `json:"ownerName"`
	Name      string      `json:"name"`
	IsDefault bool        `json:"isDefault"`
	SongOrder []songGroup `json:"songOrder,omitempty"` // VariantIDs are OLD (pre-import) song ids — see variants.json to map them
}

type work struct {
	Title, Artist, Genre, Album string
}

type songRow struct {
	ID                int64
	Title, Artist     string
	Body              string
	Visibility        string
	OwnerPersonID     *int64
	OwnerBandID       *int64
	OwnerCommunityID  *int64
	ForkedFromID      *int64
	CreatedByPersonID *int64
	WorkID            int64
	CreatedAt         time.Time
}

type songbookMembership struct {
	OwnerKind    string `json:"ownerKind"`
	OwnerName    string `json:"ownerName"`
	SongbookName string `json:"songbookName"`
}

type variantRecord struct {
	ID                 int64                `json:"id"`
	File               string               `json:"file"`
	Title              string               `json:"title"`
	Artist             string               `json:"artist"`
	Visibility         string               `json:"visibility"`
	OwnerKind          string               `json:"ownerKind"` // catalog, person, band, community
	OwnerName          string               `json:"ownerName,omitempty"`
	WorkTitle          string               `json:"workTitle"`
	WorkArtist         string               `json:"workArtist"`
	WorkGenre          string               `json:"workGenre,omitempty"`
	WorkAlbum          string               `json:"workAlbum,omitempty"`
	CreatedByUsername  string               `json:"createdByUsername,omitempty"`
	CreatedAt          string               `json:"createdAt"`
	ForkedFromID       int64                `json:"forkedFromId,omitempty"`
	ForkedFromFile     string               `json:"forkedFromFile,omitempty"`
	SongbookMembership []songbookMembership `json:"songbookMemberships,omitempty"`
}

// rfc3339 formats a timestamp the way every date in this export is
// written — real RFC3339, not Postgres's own "::text" cast (a space
// instead of "T", no colon in the UTC offset — not what members.txt's
// own doc comment, or a JSON consumer expecting real RFC3339, wants).
func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func rfc3339Null(t *time.Time) string {
	if t == nil {
		return ""
	}
	return rfc3339(*t)
}

var slugRE = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func slugify(s string) string {
	s = strings.TrimSpace(s)
	s = slugRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "untitled"
	}
	return s
}

// uniquePath appends "-2", "-3", ... to base (before its extension) the
// first time base collides with something already written in the same
// directory — titles aren't unique, filenames must be.
func uniquePath(used map[string]bool, dir, base, ext string) string {
	candidate := filepath.Join(dir, base+ext)
	if !used[candidate] {
		used[candidate] = true
		return candidate
	}
	for i := 2; ; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, i, ext))
		if !used[candidate] {
			used[candidate] = true
			return candidate
		}
	}
}

func main() {
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "Postgres connection string")
	outDir := flag.String("out", "./songbooks-export", "output directory (created if missing)")
	flag.Parse()
	if *dbURL == "" {
		log.Fatal("usage: songexport --db <url> --out <dir>")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	must := func(err error, what string) {
		if err != nil {
			log.Fatalf("%s: %v", what, err)
		}
	}

	// ---- Load every reference table up front; small, fits in memory. ----

	people := map[int64]person{}
	rows, err := pool.Query(ctx, `SELECT id, username, display_name, coalesce(email,''), is_admin, instruments FROM people`)
	must(err, "query people")
	for rows.Next() {
		var p person
		must(rows.Scan(&p.ID, &p.Username, &p.DisplayName, &p.Email, &p.IsAdmin, &p.Instruments), "scan person")
		people[p.ID] = p
	}
	rows.Close()

	bands := map[int64]band{}
	rows, err = pool.Query(ctx, `SELECT id, name, coalesce(music_type,'') FROM bands`)
	must(err, "query bands")
	for rows.Next() {
		var b band
		must(rows.Scan(&b.ID, &b.Name, &b.MusicType), "scan band")
		bands[b.ID] = b
	}
	rows.Close()

	communities := map[int64]community{}
	rows, err = pool.Query(ctx, `SELECT id, name, default_join_role FROM communities`)
	must(err, "query communities")
	for rows.Next() {
		var c community
		must(rows.Scan(&c.ID, &c.Name, &c.DefaultJoinRole), "scan community")
		communities[c.ID] = c
	}
	rows.Close()

	works := map[int64]work{}
	rows, err = pool.Query(ctx, `SELECT id, title, artist, coalesce(genre,''), coalesce(album,'') FROM works`)
	must(err, "query works")
	for rows.Next() {
		var id int64
		var w work
		must(rows.Scan(&id, &w.Title, &w.Artist, &w.Genre, &w.Album), "scan work")
		works[id] = w
	}
	rows.Close()

	songbooks := map[int64]songbook{}
	rows, err = pool.Query(ctx, `SELECT id, owner_kind, coalesce(owner_person_id,0)+coalesce(owner_band_id,0)+coalesce(owner_community_id,0), name, is_default, song_order FROM songbooks`)
	must(err, "query songbooks")
	for rows.Next() {
		var sb songbook
		var orderJSON []byte
		must(rows.Scan(&sb.ID, &sb.OwnerKind, &sb.OwnerID, &sb.Name, &sb.IsDefault, &orderJSON), "scan songbook")
		json.Unmarshal(orderJSON, &sb.SongOrder)
		songbooks[sb.ID] = sb
	}
	rows.Close()

	// songbook_id -> ordered []variant_id actually present (songbook_entries), for membership + position
	entriesBySongbook := map[int64][]int64{}
	rows, err = pool.Query(ctx, `SELECT songbook_id, variant_id FROM songbook_entries ORDER BY songbook_id, id`)
	must(err, "query songbook_entries")
	for rows.Next() {
		var sbID, vID int64
		must(rows.Scan(&sbID, &vID), "scan songbook_entry")
		entriesBySongbook[sbID] = append(entriesBySongbook[sbID], vID)
	}
	rows.Close()

	songbooksByVariant := map[int64][]int64{} // variant_id -> []songbook_id (every songbook that lists it)
	for sbID, variantIDs := range entriesBySongbook {
		for _, vID := range variantIDs {
			songbooksByVariant[vID] = append(songbooksByVariant[vID], sbID)
		}
	}

	// ---- Directory-naming helpers ----

	ownerDirName := func(kind string, id int64) string {
		switch kind {
		case "person":
			if p, ok := people[id]; ok {
				return slugify(p.Username)
			}
		case "band":
			if b, ok := bands[id]; ok {
				return slugify(b.Name)
			}
		case "community":
			if c, ok := communities[id]; ok {
				return slugify(c.Name)
			}
		}
		return "_unknown"
	}

	// ---- Songs ----

	rows, err = pool.Query(ctx, `
		SELECT id, title, artist, body, visibility, owner_person_id, owner_band_id, owner_community_id,
		       forked_from_id, created_by_person_id, work_id, created_at
		FROM songs ORDER BY id`)
	must(err, "query songs")
	var songRows []songRow
	for rows.Next() {
		var s songRow
		must(rows.Scan(&s.ID, &s.Title, &s.Artist, &s.Body, &s.Visibility, &s.OwnerPersonID, &s.OwnerBandID,
			&s.OwnerCommunityID, &s.ForkedFromID, &s.CreatedByPersonID, &s.WorkID, &s.CreatedAt), "scan song")
		songRows = append(songRows, s)
	}
	rows.Close()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatalf("mkdir %s: %v", *outDir, err)
	}
	usedPaths := map[string]bool{}
	var variants []variantRecord
	fileByVariant := map[int64]string{}

	for _, s := range songRows {
		// The song's own owner_kind (catalog/person/band/community) is
		// real DB metadata (kept in variants.json below), but it's the
		// wrong thing to file this FILE under: a community-songbook song
		// created via handleCreateSongInSongbook is owner_kind=catalog
		// (songs.owner_* has no community column path through
		// CreateVariant — see that handler's own comment), yet it very
		// much belongs, for a human looking at this backup, under the
		// community whose songbook actually contains it — confirmed live
		// against a real export: Florent's whole 32-song "Jouez le
		// Blues" songbook landed under "_catalog/" instead of
		// "Florent-Passamonti/" before this fix. File placement follows
		// the song's PRIMARY SONGBOOK's owner; the song's own raw
		// owner_kind is only the fallback for a song that isn't in any
		// songbook at all.
		var trueOwnerKind, trueOwnerName string
		switch {
		case s.OwnerPersonID != nil:
			trueOwnerKind, trueOwnerName = "person", slugify(people[*s.OwnerPersonID].Username)
		case s.OwnerBandID != nil:
			trueOwnerKind, trueOwnerName = "band", slugify(bands[*s.OwnerBandID].Name)
		case s.OwnerCommunityID != nil:
			trueOwnerKind, trueOwnerName = "community", slugify(communities[*s.OwnerCommunityID].Name)
		default:
			trueOwnerKind, trueOwnerName = "catalog", "_catalog"
		}

		memberSongbooks := songbooksByVariant[s.ID]
		sort.Slice(memberSongbooks, func(i, j int) bool { return memberSongbooks[i] < memberSongbooks[j] })
		var ownerSongbookID int64
		for _, sbID := range memberSongbooks {
			sb := songbooks[sbID]
			if sb.OwnerKind == trueOwnerKind && ownerDirName(sb.OwnerKind, sb.OwnerID) == trueOwnerName {
				ownerSongbookID = sbID
				break
			}
		}
		if ownerSongbookID == 0 && len(memberSongbooks) > 0 {
			ownerSongbookID = memberSongbooks[0]
		}

		ownerName := trueOwnerName
		songbookDirName := "_unfiled"
		if ownerSongbookID != 0 {
			sb := songbooks[ownerSongbookID]
			ownerName = ownerDirName(sb.OwnerKind, sb.OwnerID)
			songbookDirName = slugify(sb.Name)
		}
		dir := filepath.Join(*outDir, "songbooks", ownerName, songbookDirName)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("mkdir %s: %v", dir, err)
		}
		path := uniquePath(usedPaths, dir, slugify(s.Title), ".song")
		if err := os.WriteFile(path, []byte(s.Body), 0o644); err != nil {
			log.Fatalf("write %s: %v", path, err)
		}
		relPath, _ := filepath.Rel(*outDir, path)
		fileByVariant[s.ID] = relPath

		w := works[s.WorkID]
		var createdByUsername string
		if s.CreatedByPersonID != nil {
			createdByUsername = people[*s.CreatedByPersonID].Username
		}
		var memberships []songbookMembership
		for _, sbID := range memberSongbooks {
			sb := songbooks[sbID]
			memberships = append(memberships, songbookMembership{
				OwnerKind: sb.OwnerKind, OwnerName: ownerDirName(sb.OwnerKind, sb.OwnerID),
				SongbookName: sb.Name,
			})
		}
		variants = append(variants, variantRecord{
			ID: s.ID, File: relPath, Title: s.Title, Artist: s.Artist, Visibility: s.Visibility,
			OwnerKind: trueOwnerKind, OwnerName: trueOwnerName,
			WorkTitle: w.Title, WorkArtist: w.Artist, WorkGenre: w.Genre, WorkAlbum: w.Album,
			CreatedByUsername: createdByUsername, CreatedAt: rfc3339(s.CreatedAt),
			SongbookMembership: memberships,
		})
	}
	// Second pass: resolve forkedFromFile now that every file path is known.
	for i, s := range songRows {
		if s.ForkedFromID != nil {
			variants[i].ForkedFromID = *s.ForkedFromID
			variants[i].ForkedFromFile = fileByVariant[*s.ForkedFromID]
		}
	}

	// ---- members.txt for every band and community ----

	for id, b := range bands {
		rows, err := pool.Query(ctx, `
			SELECT p.username, bm.status, bm.joined_at, bm.left_at,
			       coalesce(ip.username,'')
			FROM band_members bm
			JOIN people p ON p.id = bm.person_id
			LEFT JOIN people ip ON ip.id = bm.invited_by_person_id
			WHERE bm.band_id = $1 ORDER BY bm.joined_at`, id)
		must(err, "query band_members")
		var roster []membersfmt.Member
		for rows.Next() {
			var m membersfmt.Member
			var joinedAt time.Time
			var leftAt *time.Time
			must(rows.Scan(&m.Username, &m.RoleOrStatus, &joinedAt, &leftAt, &m.InvitedBy), "scan band_member")
			m.JoinedAt, m.LeftAt = rfc3339(joinedAt), rfc3339Null(leftAt)
			roster = append(roster, m)
		}
		rows.Close()
		dir := filepath.Join(*outDir, "songbooks", slugify(b.Name))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("mkdir %s: %v", dir, err)
		}
		must(membersfmt.Write(filepath.Join(dir, "members.txt"), membersfmt.File{
			Header:  map[string]string{"name": b.Name, "music_type": b.MusicType},
			Members: roster,
		}), "write band members.txt")
	}

	for id, c := range communities {
		roleRows, err := pool.Query(ctx, `SELECT id, name, coalesce(builtin_key,'') FROM community_roles WHERE community_id = $1`, id)
		must(err, "query community_roles")
		var roleDefs []membersfmt.Role
		for roleRows.Next() {
			var rid int64
			var rd membersfmt.Role
			must(roleRows.Scan(&rid, &rd.Name, &rd.Builtin), "scan community_role")
			roleDefs = append(roleDefs, rd)
		}
		roleRows.Close()
		for i := range roleDefs {
			privRows, err := pool.Query(ctx, `SELECT privilege FROM community_role_privileges crp JOIN community_roles cr ON cr.id = crp.role_id WHERE cr.community_id = $1 AND cr.name = $2`, id, roleDefs[i].Name)
			must(err, "query community_role_privileges")
			for privRows.Next() {
				var priv string
				must(privRows.Scan(&priv), "scan privilege")
				roleDefs[i].Privileges = append(roleDefs[i].Privileges, priv)
			}
			privRows.Close()
		}

		rows, err := pool.Query(ctx, `
			SELECT p.username, cr.name, cm.joined_at, cm.left_at,
			       coalesce(ip.username,'')
			FROM community_members cm
			JOIN people p ON p.id = cm.person_id
			JOIN community_roles cr ON cr.id = cm.role_id
			LEFT JOIN people ip ON ip.id = cm.invited_by_person_id
			WHERE cm.community_id = $1 ORDER BY cm.joined_at`, id)
		must(err, "query community_members")
		var roster []membersfmt.Member
		for rows.Next() {
			var m membersfmt.Member
			var joinedAt time.Time
			var leftAt *time.Time
			must(rows.Scan(&m.Username, &m.RoleOrStatus, &joinedAt, &leftAt, &m.InvitedBy), "scan community_member")
			m.JoinedAt, m.LeftAt = rfc3339(joinedAt), rfc3339Null(leftAt)
			roster = append(roster, m)
		}
		rows.Close()
		dir := filepath.Join(*outDir, "songbooks", slugify(c.Name))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("mkdir %s: %v", dir, err)
		}
		must(membersfmt.Write(filepath.Join(dir, "members.txt"), membersfmt.File{
			Header:  map[string]string{"name": c.Name, "default_join_role": c.DefaultJoinRole},
			Roles:   roleDefs,
			Members: roster,
		}), "write community members.txt")
	}

	// ---- people.json / variants.json / songbooks.json ----

	var peopleList []person
	for _, p := range people {
		peopleList = append(peopleList, p)
	}
	sort.Slice(peopleList, func(i, j int) bool { return peopleList[i].ID < peopleList[j].ID })
	writeJSON(filepath.Join(*outDir, "people.json"), peopleList)

	sort.Slice(variants, func(i, j int) bool { return variants[i].ID < variants[j].ID })
	writeJSON(filepath.Join(*outDir, "variants.json"), variants)

	var songbookList []songbookRecord
	for _, sbID := range sortedKeys(songbooks) {
		sb := songbooks[sbID]
		songbookList = append(songbookList, songbookRecord{
			OwnerKind: sb.OwnerKind, OwnerName: ownerDirName(sb.OwnerKind, sb.OwnerID),
			Name: sb.Name, IsDefault: sb.IsDefault, SongOrder: sb.SongOrder,
		})
	}
	writeJSON(filepath.Join(*outDir, "songbooks.json"), songbookList)

	fmt.Printf("Exported %d songs, %d people, %d bands, %d communities, %d songbooks to %s\n",
		len(variants), len(people), len(bands), len(communities), len(songbooks), *outDir)
}

func sortedKeys(m map[int64]songbook) []int64 {
	keys := make([]int64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func writeJSON(path string, v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Fatalf("write %s: %v", path, err)
	}
}
