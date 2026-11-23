package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// The one access rule (docs/ownership-model.md §2, §4, §5): what a person
// may do with a song — see it, edit it, link it from a songbook, make a
// version of it, publish it — and with a songbook. A song has one owner
// (a person, a band, a community; the catalogue's is The Big Songbook,
// the global community everyone belongs to) and is public (everyone signed
// in) or private (its owner's members). Every page, API, search, PDF and
// download asks SongRightsFor / SongbookRightsFor; song_visible (migration
// 0114) is the same "see" for the queries that list songs.

// Owner is whose a song or a songbook is.
type Owner struct {
	Kind string // "person", "band" or "community"
	ID   int64
}

// PersonOwner, BandOwner and CommunityOwner name an owner.
func PersonOwner(id int64) Owner    { return Owner{Kind: "person", ID: id} }
func BandOwner(id int64) Owner      { return Owner{Kind: "band", ID: id} }
func CommunityOwner(id int64) Owner { return Owner{Kind: "community", ID: id} }

// SongbookOwner is the songbook's owner.
func (sb Songbook) Owner() Owner {
	switch sb.OwnerKind {
	case "band":
		return BandOwner(sb.OwnerBandID)
	case "community":
		return CommunityOwner(sb.OwnerCommunityID)
	}
	return PersonOwner(sb.OwnerPersonID)
}

// SongRights is what one person may do with one song.
type SongRights struct {
	SongID int64
	Owner  Owner
	// Catalogue: the song is The Big Songbook's.
	Catalogue bool
	Public    bool
	// Closed: the song is in a closed (paid) songbook of its owner's —
	// never public, never linked, copied or made a version of elsewhere.
	Closed bool
	// Member: the person is one of the owner's members (the person
	// themselves, a current member of the band or the community; everyone
	// for The Big Songbook).
	Member bool
	// Coaching: the owner is a coaching community (docs/coaching.md).
	Coaching bool
	// AssignedStudent: the person is a current student of that coaching
	// community and their own student songbook holds the song (the coach
	// gave it to them): they edit it in place, as the coach does — not
	// another student's song, not the repertoire's others.
	AssignedStudent bool

	See     bool // open it, play it, export it, download its files
	Edit    bool // change it in place
	Publish bool // make it public or private
}

// CanLinkFrom reports whether a songbook of owner o may hold the song as a
// link (§4): a band or a community links public songs and its own; a
// person, anything they see. A closed songbook's song goes nowhere else.
func (r *SongRights) CanLinkFrom(o Owner) bool {
	if r == nil || !r.See {
		return false
	}
	if r.Owner == o {
		return true
	}
	if r.Closed {
		return false
	}
	return r.Public || o.Kind == "person"
}

// CanVersionFor reports whether owner o may make its own version of the
// song (§5): what o may link, a closed songbook's song never — not even
// its owner's own, outside that songbook (New version from this is for
// whoever edits it).
func (r *SongRights) CanVersionFor(o Owner) bool {
	if r == nil || !r.See {
		return false
	}
	if r.Owner == o {
		return r.Edit
	}
	return !r.Closed && r.CanLinkFrom(o)
}

// LinkRefusal says why a songbook can't hold a song (CanLinkFrom false):
// ErrForbidden wrapped with whose the song is and why.
type LinkRefusal struct {
	Reason    string // "closed", "private", "unseen"
	OwnerName string
	// CatalogueSongID is the catalogue's song of the same work, which a
	// band may link instead of another band's private song; 0 when none.
	CatalogueSongID int64
}

func (e *LinkRefusal) Error() string {
	switch e.Reason {
	case "closed":
		return "this song is in a closed songbook: it can't go anywhere else"
	case "private":
		return fmt.Sprintf("this is %s's private song: only its own songbooks hold it", e.OwnerName)
	}
	return "you can't see this song"
}

func (e *LinkRefusal) Unwrap() error { return ErrForbidden }

// SongRightsFor is the rule for personID (0: no one signed in) and songID.
func (st *Store) SongRightsFor(ctx context.Context, personID, songID int64) (*SongRights, error) {
	return songRightsFor(ctx, st.db, personID, songID)
}

func songRightsFor(ctx context.Context, q dbtx, personID, songID int64) (*SongRights, error) {
	r := &SongRights{SongID: songID}
	var kind string
	var ownerID int64
	var admin, bandMember, bandCore, communityMember, editRepertoire, staff bool
	err := q.QueryRow(ctx, `
		SELECT s.owner_kind, coalesce(s.owner_person_id, s.owner_band_id, s.owner_community_id),
		       s.public, coalesce(c.kind = 'global', false), song_closed(s.id), song_visible(s.id, $2),
		       coalesce(c.kind = 'coaching', false), `+assignedStudentSQL("s.owner_community_id", "s.id", "$2")+`,
		       coalesce((SELECT is_admin FROM people WHERE id = $2), false),
		       EXISTS (SELECT 1 FROM band_members bm WHERE bm.band_id = s.owner_band_id AND bm.person_id = $2 AND upper_inf(bm.validity)),
		       EXISTS (SELECT 1 FROM band_members bm WHERE bm.band_id = s.owner_band_id AND bm.person_id = $2 AND upper_inf(bm.validity) AND bm.status = 'core'),
		       EXISTS (SELECT 1 FROM community_members cm WHERE cm.community_id = s.owner_community_id AND cm.person_id = $2 AND upper_inf(cm.validity)),
		       EXISTS (SELECT 1 FROM community_members cm JOIN community_role_privileges crp ON crp.role_id = cm.role_id
		               WHERE cm.community_id = s.owner_community_id AND cm.person_id = $2 AND upper_inf(cm.validity) AND crp.privilege = $3),
		       EXISTS (SELECT 1 FROM community_members cm JOIN community_role_privileges crp ON crp.role_id = cm.role_id
		               WHERE cm.community_id = s.owner_community_id AND cm.person_id = $2 AND upper_inf(cm.validity)
		                 AND crp.privilege IN ($4, $5, $6, $7, $8))
		FROM songs s LEFT JOIN communities c ON c.id = s.owner_community_id
		WHERE s.id = $1`, songID, personID, PrivEditRepertoire,
		PrivManageMembers, PrivManageEvents, PrivManageSubmissions, PrivManageSettings, PrivManageFiles).
		Scan(&kind, &ownerID, &r.Public, &r.Catalogue, &r.Closed, &r.See,
			&r.Coaching, &r.AssignedStudent,
			&admin, &bandMember, &bandCore, &communityMember, &editRepertoire, &staff)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such song %d: %w", songID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	r.Owner = Owner{Kind: kind, ID: ownerID}
	if personID <= 0 {
		r.See = false
		r.AssignedStudent = false
		return r, nil
	}
	switch {
	case r.Catalogue:
		r.Member = true
		r.Edit = admin
		r.Publish = admin
	case kind == "person":
		r.Member = ownerID == personID
		r.Edit = r.Member
		r.Publish = r.Member
	case kind == "band":
		r.Member = bandMember
		r.Edit = bandMember
		r.Publish = bandCore
	case kind == "community" && r.Coaching:
		// A coaching community's songs: the coach (its staff, who edit
		// its repertoire) and the students each song is given to (their
		// student songbook holds it); edit_repertoire alone, a student's
		// role's, doesn't open the others.
		r.Member = communityMember
		r.Edit = r.See && (editRepertoire && staff || r.AssignedStudent)
		r.Publish = staff
	case kind == "community":
		r.Member = communityMember
		r.Edit = editRepertoire && r.See
		r.Publish = staff
	}
	if r.Closed {
		r.Publish = false
	}
	return r, nil
}

// assignedStudentSQL is "person is a current student of the coaching
// community community and their own student songbook of it holds song"
// in a query (community, song and person are SQL expressions).
func assignedStudentSQL(community, song, person string) string {
	return `EXISTS (SELECT 1 FROM songbook_entries ase
	               JOIN songbooks asb ON asb.id = ase.songbook_id AND asb.kind = 'student'
	               JOIN communities ac ON ac.id = asb.owner_community_id AND ac.kind = 'coaching'
	               JOIN community_members acm ON acm.community_id = ac.id AND acm.person_id = asb.student_person_id
	                                         AND upper_inf(acm.validity)
	               WHERE ase.variant_id = ` + song + ` AND asb.owner_community_id = ` + community + `
	                 AND asb.student_person_id = ` + person + `)`
}

// IsAssignedStudent reports whether personID is a current student of the
// coaching community communityID whose own student songbook holds songID
// (a link or the community's own song).
func (st *Store) IsAssignedStudent(ctx context.Context, personID, communityID, songID int64) (bool, error) {
	var ok bool
	err := st.db.QueryRow(ctx, `SELECT `+assignedStudentSQL("$1::bigint", "$2::bigint", "$3::bigint"), communityID, songID, personID).Scan(&ok)
	return ok, err
}

// SongbookRights is what one person may do with one songbook.
type SongbookRights struct {
	Songbook *Songbook
	See      bool // open it (its owner's members, a grant for a sold one; everyone for a public one)
	Edit     bool // change it: rename, order, add and remove songs, keys
	Publish  bool // make it public, its owner's songs in it with it
}

// SongbookRightsFor is the rule for personID and songbookID.
func (st *Store) SongbookRightsFor(ctx context.Context, personID, songbookID int64) (*SongbookRights, error) {
	sb, err := st.GetSongbook(ctx, songbookID)
	if err != nil {
		return nil, err
	}
	r := &SongbookRights{Songbook: sb}
	if personID <= 0 {
		return r, nil
	}
	if r.See, err = st.CanAccessSongbook(ctx, personID, songbookID); err != nil {
		return nil, err
	}
	if r.Edit, err = st.CanEditSongbook(ctx, personID, songbookID); err != nil {
		return nil, err
	}
	if sb.Public {
		r.See = true
	}
	if sb.Closed {
		return r, nil
	}
	switch sb.OwnerKind {
	case "person":
		r.Publish = sb.OwnerPersonID == personID
	case "band":
		r.Publish, err = st.IsCurrentBandCore(ctx, sb.OwnerBandID, personID)
	case "community":
		r.Publish, err = st.HasAnyCommunityManagePrivilege(ctx, sb.OwnerCommunityID, personID)
	}
	return r, err
}

// LinkCheck is nil when songbookID may hold songID as a link added by
// personID, else why not (*LinkRefusal; ErrForbidden when personID can't
// change the songbook): the songbook's owner links what CanLinkFrom
// allows.
func (st *Store) LinkCheck(ctx context.Context, personID, songID int64, sb *Songbook) error {
	return st.LinkCheckFor(ctx, personID, songID, sb.Owner())
}

// LinkCheckFor is LinkCheck for any songbook of owner's (a set list's
// owner, say).
func (st *Store) LinkCheckFor(ctx context.Context, personID, songID int64, owner Owner) error {
	r, err := st.SongRightsFor(ctx, personID, songID)
	if err != nil {
		return err
	}
	if r.CanLinkFrom(owner) {
		return nil
	}
	return st.linkRefusal(ctx, r)
}

// InOwnerSongbooks reports whether one of owner's songbooks holds songID.
func (st *Store) InOwnerSongbooks(ctx context.Context, owner Owner, songID int64) (bool, error) {
	var ok bool
	err := st.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
		               WHERE se.variant_id = $1 AND sb.owner_kind = $2
		                 AND coalesce(sb.owner_person_id, sb.owner_band_id, sb.owner_community_id) = $3)`,
		songID, owner.Kind, owner.ID).Scan(&ok)
	return ok, err
}

func (st *Store) linkRefusal(ctx context.Context, r *SongRights) error {
	e := &LinkRefusal{Reason: "unseen"}
	switch {
	case !r.See:
	case r.Closed:
		e.Reason = "closed"
	default:
		e.Reason = "private"
	}
	if e.Reason != "unseen" {
		o, err := st.VariantOwnerOf(ctx, r.SongID)
		if err != nil {
			return err
		}
		e.OwnerName = o.Name
		if err := st.db.QueryRow(ctx, `
			SELECT coalesce((SELECT g.id FROM songs g
			                 WHERE g.public AND `+catalogueSQL("g")+`
			                   AND (g.id = s.forked_from_id OR g.work_id = s.work_id)
			                 ORDER BY (g.id = s.forked_from_id) DESC, g.id LIMIT 1), 0)
			FROM songs s WHERE s.id = $1`, r.SongID).Scan(&e.CatalogueSongID); err != nil {
			return err
		}
	}
	return e
}

// BigSongbookID is The Big Songbook's id (the global community; made
// when missing).
func (st *Store) BigSongbookID(ctx context.Context) (int64, error) {
	var id int64
	err := st.db.QueryRow(ctx, `SELECT big_songbook_id()`).Scan(&id)
	return id, err
}

// songKindSQL is a song's kind for pages and labels, from its owner:
// 'global' for The Big Songbook's (the catalogue), 'user' for a person's,
// 'band', 'community'. A label only: what anyone may do with the song is
// SongRightsFor's.
func songKindSQL(alias string) string {
	return `(CASE WHEN ` + alias + `.owner_person_id IS NOT NULL THEN 'user'
	              WHEN ` + alias + `.owner_band_id IS NOT NULL THEN 'band'
	              WHEN ` + alias + `.owner_community_id = ` + bigSongbookSQL + ` THEN 'global'
	              ELSE 'community' END)`
}

// bigSongbookSQL is The Big Songbook's id in a query (no row made: see
// big_songbook_id() for that).
const bigSongbookSQL = `(SELECT id FROM communities WHERE kind = 'global')`

// catalogueSQL is "alias is the catalogue's song" in a query.
func catalogueSQL(alias string) string {
	return alias + `.owner_community_id = ` + bigSongbookSQL
}
