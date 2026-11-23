# A band's roles

Some bands have a staff that makes the decisions. A band's members each
hold a **role**: a name and a set of **privileges** from a fixed catalog,
the model a community's roles already use (`internal/store/roles.go`,
migration 0052), for bands. Code: `internal/store/band_roles.go` (the
catalog, the defaults, `HasBandPrivilege`), `internal/store/roles.go`
(the role operations, written once for both: `roleScope`, `GroupRoles`),
`internal/httpapi/roles.go` (the roles API, both kinds),
`internal/httpapi/band_roles.go` + `templates/band_roles.html` +
`static/js/band-roles.js` (the editor), migrations 0130 and 0131.

## The rule

Seeing the band — its pages, songbook, set lists, calendar, recordings,
PDF exports, its calendar in one's own feed — is every current member's,
as before. What a member may **change** is their role's:

- `store.HasBandPrivilege(ctx, bandID, personID, priv)` — the one rule
  (current member, role grants `priv`); `has_band_privilege(band,
  person, priv)` is the same in SQL, for queries.
- HTTP: `requireBandPrivilege(priv, h)` on a route,
  `s.bandPrivilege(r, bandID, priv)` in a handler, `s.bandPrivs(r,
  bandID)` for a page (`{{if .Privs.Can "edit_look"}}`). A site admin
  always passes, as before.

## Built-in roles: core and guest, as they were

Every band has two built-in roles, **core** and **guest** (seeded for
every band by migration 0130, and by a trigger for every new band). They
can't be renamed or deleted; what they grant can be changed. Their
defaults are exactly what core and guest members could do before roles
(the inventory below), so nothing changes for a band until it changes
its roles. `store.BandRoleDefaults` and the migration's
`seed_band_roles` say the same (a test checks it).

`band_members.role_id` is what a member may do. `band_members.status`
stays, kept in step by a trigger: **guest** for the guest role, **core**
for the core role and for any role of the band's own. Code that sets a
status (an invite's core/guest, the old core/guest switch, a revert)
points the row at that built-in role. Every membership row, past ones
included, got the built-in role of its status: no data lost.

A band's own role counts as **core** for code still reading the status
(the proposals' vote counts, `IsCurrentBandCore`); that code should ask
for a privilege instead (below).

## The privileges

| Privilege | What it allows | core | guest |
|---|---|---|---|
| `edit_songbook` | Add, remove, arrange songs in the band's songbooks; make and edit the band's songs, its arrangements ("Make the band's version"), its songbooks; undo a songbook change | ✓ | ✓ |
| `publish` | Make the band's songs, songbooks and recordings public | ✓ | |
| `manage_setlists` | Create, edit, delete set lists, their gigs, a line's version, the click track notes | ✓ | ✓ |
| `manage_events` | Create and change the band's events (rehearsals, gigs…); sign the band up for a community's event | ✓ | ✓ |
| `manage_recordings` | The recordings' folder and sync, name templates, fixes, a song's recording | ✓ | ✓ |
| `propose` | Propose songs; add to Discover and Influences | ✓ | ✓ |
| `vote` | Vote on proposals (counted in their tallies) | ✓ | |
| `decide_proposals` | Add a proposed song, or turn it down | ✓ | ✓ |
| `manage_members` | Invite people (every way: an account by username, one-off and personal links, the roster link and its names, the band's open link and QR code); change others' instruments | ✓ | |
| `remove_members` | Remove a member | ✓ | ✓ |
| `manage_roles` | Give members a role; create, rename, delete roles, change what they allow | ✓ | |
| `rename_archive` | Rename the band; archive and unarchive it (the one privilege an archived band still grants) | ✓ | |
| `edit_band` | Say what the band plays (covers or original music) | ✓ | ✓ |
| `edit_look` | The band's look (colours, fonts), logo, card picture, language | ✓ | |

Proposals (docs/band-votes.md): `store.MayPropose`, `MayVote`,
`MayDecideProposal` and `MayShareListen` ask `HasBandPrivilege` (propose,
vote, decide_proposals, propose); the accept and turn-down routes are
gated by `decide_proposals`, proposing and its drafts by `propose`; the
tallies count the members whose role grants `vote` (`BandMember.Votes`,
`has_band_privilege(…, 'vote')`): "n of m voted" is of them. The user's rule is "the tool helps, the band decides; any
member can add or turn down a proposal" — `propose` and
`decide_proposals` are every member's by default; a band that wants its
staff alone to decide makes a "Staff" role with `decide_proposals` and
takes it from core and guest.

## Inventory: who could do what before roles

Every band action and its gate before this change, and the privilege
that gates it now.

| Action | Where | Before | Now |
|---|---|---|---|
| See the band's pages (overview, songbook, To vote, Discover, set lists, calendar, recordings, members, settings) | `requireBandMember` on `GET /bands/{id}/…` | any member | any member |
| PDF exports (songbook, LilyPond) | `GET /bands/{id}/export-*.pdf` | any member | any member |
| The band's events in one's calendar feed, an event's page and .ics, register oneself | `calendar.go`, `events.go` | any member | any member |
| Rate a recording (stars) | `PUT …/recordings/{recID}/stars` | any member | any member (one's own rating) |
| Rename the band | `POST /api/bands/{id}/name` | any member | `rename_archive` (core by default: the user's decision) |
| Archive / unarchive the band | `POST /api/bands/{id}/archive` | — (new) | `rename_archive` |
| What it plays | `POST …/music-type` | any member | `edit_band` |
| Look (brand), logo | `POST …/brand`, `PUT/DELETE …/brand/logo` | core | `edit_look` |
| Card picture | `POST …/card-image` | core | `edit_look` |
| Language | `PUT …/locale` | core | `edit_look` |
| Delete the band | `DELETE /api/bands/{id}` | any member | site admins only (the user's decision; no privilege) |
| Add an account by username | `POST …/members` | core | `manage_members` |
| One-off link, personal invite, new link for a placeholder | `POST …/invites`, `…/personal-invites`, `…/members/{p}/invite-link`, `/api/people/{p}/personal-invites` (`CanInvitePerson`) | core | `manage_members` |
| Roster link and its names, close it | `POST …/group-invites…` | core | `manage_members` |
| The band's open link / QR code (#129) | `POST …/band-link`, close | core | `manage_members` |
| Someone else's instruments (a joined member) | `PUT …/members/{p}/instruments` (`MayEditMemberInstruments`) | core (anyone for a placeholder, oneself) | `manage_members` (same exceptions) |
| Core/guest switch | `POST …/members/{p}/status` | core | `manage_roles` |
| A member's role, the roles | `POST …/members/{p}/role`, `…/roles…` | — (new) | `manage_roles` |
| Remove a member | `DELETE …/members/{p}` | any member | `remove_members` |
| Undo a membership change (revisions) | `POST /api/revisions/{id}/revert` | any member | `manage_roles` (a guest undoing their own demotion went round the core-only switch) |
| Add a song, fork, new song, remove from the songbook | `POST/DELETE …/catalog…`, `POST …/variants`, `CreateBandVariant`, `CreateVariant` | any member | `edit_songbook` |
| Songbook entries: add, remove, order, keys, rename, delete a songbook, quick add, lessons | `CanEditSongbook` | any member | `edit_songbook` |
| Create a band songbook | `POST /api/songbooks` | any member | `edit_songbook` |
| Edit the band's song, make the band's arrangement | `SongRightsFor` (Edit), `OwnerMayChange`, `MayMakeVersion` | any member | `edit_songbook` |
| The songbooks offered to add a song to | `SongbookTargets` | the member's bands | the bands where `edit_songbook` |
| Who's told of a version's change | `versionEditors` | every member | those with `edit_songbook` |
| Make a song / songbook public | `SongRightsFor`, `SongbookRightsFor` (Publish) | core | `publish` |
| Make a recording public | `POST …/recordings/{recID}/public` | core | `publish` |
| Set lists: create, edit, delete, gigs, a line's version, click track notes; undo a set list change | `POST /api/setlists`, `CanEditSetlistByID` | any member | `manage_setlists` |
| Create an event; change one | `POST …/events`, `CanManageEvent` | any member | `manage_events` |
| Sign the band up for an event, claim a song of its set list as the band | `POST /events/{id}/register` (`band_id`), `POST /events/{id}/songs` (`bandId`) | any member (registering: anyone, unchecked) | `manage_events` |
| Recordings: folder, sync, song's recording, fixes, name templates | `…/recordings/…` | any member | `manage_recordings` |
| Propose a song, drafts | `POST …/proposals`, `…/proposal-drafts` (`CreateProposal`, `CreateProposedSong`) | any member | `propose` |
| Add to Discover / Influences | `POST …/listens` (`MayShareListen`) | any member | `propose` |
| Vote; be counted in a tally | `POST …/proposals/{id}/votes` (`MayVote`), `seenSQL`, `proposalFrom` | core | `vote` |
| Accept / turn down a proposal | `…/accept`, `…/reject` (`MayDecideProposal`) | any member | `decide_proposals` |

## The roles editor

On the band's **Settings**, "Roles": the band's roles as columns (core,
guest, then its own), what they may do as rows in four groups (Songs,
Proposals, Members, The band), a tick per cell that saves at once. Below,
for whoever may manage roles: the band's own roles (rename, delete — not
one a current member holds), a new role (empty, or starting with another
role's ticks), and who has which role (a select per member, saving as it
changes). The Members page's role select offers the same roles. Anyone in
the band sees the table (read only without `manage_roles`).

Someone must keep `manage_roles`: a change that would leave no current
member with it (a role's ticks, a member's role, the core/guest switch)
is refused (409), so a band can't lock itself out (a site admin can
always fix a band).

A deleted role's past holders keep their membership history, with the
core role.

## The API (both a band's roles and a community's)

`internal/httpapi/roles.go`, the community's routes now included (they
took any role id; they're held to the community in the path now):

- `GET /api/bands/{id}/roles` — any member
- `POST /api/bands/{id}/roles` `{name, privileges}` — `manage_roles`
- `POST /api/bands/{id}/roles/{roleID}/privileges` `{privileges}`
- `POST /api/bands/{id}/roles/{roleID}/rename` `{name}` (custom roles)
- `DELETE /api/bands/{id}/roles/{roleID}` (custom roles no one holds)
- `POST /api/bands/{id}/members/{personID}/role` `{roleId}`

## Deleting, renaming, archiving (migration 0131)

The user's decisions:

- **Deleting a band is for site admins only**, for now: no privilege,
  `requireAdmin` on `DELETE /api/bands/{id}`, the button on the band's
  Settings only for them. `delete_band` was dropped from every role.
- **Renaming and archiving** is `rename_archive`, the core role's by
  default (migration 0131 also gave it to every band's own role holding
  `manage_roles`, its staff). `edit_band` keeps what the band plays.
- **Archiving** (`bands.archived_at`; the Settings' "Archive the band",
  `POST /api/bands/{id}/archive {archived}`): an archived band is read
  only for everyone — `has_band_privilege` grants nothing but
  `rename_archive` while it is, site admins included (`bandPrivilege`,
  `bandPrivs`) — so no song, songbook, set list, event, proposal, vote,
  invite, role or look changes. It leaves the menus (the space switcher,
  `ListSpaces`) and the home page's and the bands page's cards
  (`ListBandsForPerson`); its members still reach it from **All my
  groups** (`ListArchivedBandsForPerson`), with an "Archived" badge, and
  each of its pages says it's archived (the space header). Holders of
  `rename_archive` unarchive it from its Settings; renaming stays
  possible while archived.

## Not yet

- An archived band's open invite links still let people join it (closing
  them is a manage_members action, off while archived); leaving the band
  (remove_members) is off too while archived. Personal things stay a
  member's own: their instruments, rating a recording.
- Invites (links, QR, roster) still give core or guest; a link for a
  band's own role would follow the community's (its links carry a role
  name).
