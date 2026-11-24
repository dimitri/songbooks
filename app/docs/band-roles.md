# A band's roles

Some bands have a staff that makes the decisions. A band's members each
hold a **role**: a name and a set of **privileges** from a fixed catalog,
the model a community's roles already use (`internal/store/roles.go`,
migration 0052), for bands. Code: `internal/store/band_roles.go` (the
catalog, the defaults, `HasBandPrivilege`), `internal/store/roles.go`
(the role operations, written once for both: `roleScope`, `GroupRoles`),
`internal/httpapi/roles.go` (the roles API, both kinds),
`internal/httpapi/band_roles.go` + `templates/band_roles.html` +
`static/js/band-roles.js` (the editor), migrations 0130, 0131 and 0132.

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
defaults were what core and guest members could do before roles (the
inventory below), then the user's decisions changed a few (migrations
0131, 0132, 0137: below). `store.BandRoleDefaults` and the latest
`seed_band_roles` say the same (a test checks it).

`band_members.role_id` is what a member may do. `band_members.status`
stays, kept in step by a trigger: **guest** for the guest role, **core**
for the core role and for any role of the band's own. Code that sets a
status (an invite's core/guest, the old core/guest switch, a revert)
points the row at that built-in role. Every membership row, past ones
included, got the built-in role of its status: no data lost.

A band's own role counts as **core** for what still reads the status:
display only (below, "The status that remains"). Every right asks for a
privilege.

## The privileges

| Privilege | What it allows | core | guest |
|---|---|---|---|
| `edit_songbook` | Add, remove, arrange songs in the band's songbooks; make and edit the band's songs, its arrangements ("Make the band's version"), its songbooks; undo a songbook change | ✓ | not since 0132 |
| `publish` | Make the band's songs, songbooks and recordings public | ✓ | |
| `manage_setlists` | Create, edit, delete set lists, their gigs, a line's version, the click track notes | ✓ | ✓ |
| `manage_events` | Create and change the band's events (rehearsals, gigs…); sign the band up for a community's event | ✓ | ✓ |
| `manage_recordings` | The recordings' folder and sync, name templates, fixes, a song's recording | ✓ | ✓ |
| `propose` | Propose songs; add to Discover and Influences | ✓ | ✓ |
| `vote` | One's votes count: in a proposal's average, for/against, "n of m voted", who hasn't voted, the list's order. Without it a member still votes, listed apart ("Guests"), counted in none of it | ✓ | |
| `decide_proposals` | Add a proposed song, or turn it down | ✓ | ✓ |
| `curate_listen` | Remove anyone's songs from Discover and Influences (whoever added one always removes it) | ✓ | |
| `manage_members` | Invite people (every way: someone they play with, picked by name, personal links, the roster link and its names, the band's open link and QR code); change others' instruments | ✓ | |
| `remove_members` | Remove a member (leaving the band oneself is every member's) | ✓ | not since 0132 |
| `manage_roles` | Give members a role; create, rename, delete roles, change what they allow | ✓ | |
| `rename_archive` | Rename the band; archive and unarchive it (the one privilege an archived band still grants) | ✓ | |
| `edit_band` | Say what the band plays (covers or original music); its tagline and website; the language of its pages (The band section of its Settings) | ✓ | not since 0137 |
| `edit_look` | The band's look: its logo, card picture, style, colours and fonts (the Look section of its Settings) | ✓ | |

Proposals (docs/band-votes.md): `store.MayPropose`, `MayVote`,
`MayDecideProposal` and `MayShareListen` ask `HasBandPrivilege` (propose,
vote, decide_proposals, propose); the accept and turn-down routes are
gated by `decide_proposals`, proposing and its drafts by `propose` (a
proposer without `edit_songbook` fixes the song made for their proposal
in the check step until it's sent: `MayFixProposedSong`); the tallies
count the members whose role grants `vote` (`BandMember.Votes`,
`has_band_privilege(…, 'vote')`): "n of m voted" is of them. Any other
current member votes too (`MayCastVote`: not in an archived band), their
votes listed apart (`ProposalTally.Guests`), in no count, average nor
order. The user's rule is "the tool helps, the band decides; any
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
| Language | `PUT …/locale` | core | `edit_look`; `edit_band` since 0137 |
| Delete the band | `DELETE /api/bands/{id}` | any member | site admins only (the user's decision; no privilege) |
| Add someone they play with, picked by name | `POST …/members` (`personId`; `roleId`, or `status`) | core | `manage_members`; any of the band's roles (0132) |
| Personal invite, new link for a placeholder | `POST …/personal-invites`, `…/members/{p}/invite-link`, `/api/people/{p}/personal-invites` (`CanInvitePerson`) | core | `manage_members` |
| Roster link and its names, close it | `POST …/group-invites…` | core | `manage_members` |
| The band's open link / QR code (#129) | `POST …/band-link`, close | core | `manage_members` |
| Someone else's instruments (a joined member) | `PUT …/members/{p}/instruments` (`MayEditMemberInstruments`) | core (anyone for a placeholder, oneself) | `manage_members` (same exceptions) |
| Core/guest switch | `POST …/members/{p}/status` | core | `manage_roles` |
| A member's role, the roles | `POST …/members/{p}/role`, `…/roles…` | — (new) | `manage_roles` |
| Remove a member | `DELETE …/members/{p}` | any member | `remove_members` (core by default since 0132); leaving the band oneself: any member, not while it's archived |
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
| Remove from Discover / Influences | `DELETE …/listens/{id}` (`DeleteBandListen`) | who added it | who added it, or `curate_listen` (anyone's); no one while archived |
| Vote | `POST …/proposals/{id}/votes` (`MayCastVote`) | core | any member (0132) |
| Be counted in a tally | `MayVote`, `TallyOf`, `proposalFrom` | core | `vote`; the others' votes listed apart |
| Accept / turn down a proposal | `…/accept`, `…/reject` (`MayDecideProposal`) | any member | `decide_proposals` |

## The roles editor

The Roles section of the band's **Settings** (docs/band-settings-proposal.md),
for whoever may manage roles only. On a wide screen the band's roles as
columns (core, guest, then its own), what they may do as rows in four
groups (Songs, Proposals, Members, The band), a tick per cell that saves
at once. Each column head says how many members hold the role, a link to
Members showing only them (`/bands/{id}/members?role={roleID}`); a
band's own role has ⋯ on its head (Rename, in a dialog; Delete — not one
a current member holds); the last column is "+ New role" (a dialog: a
name, starting empty or with another role's ticks). On a phone, one
role at a time: a chip per role (with its count) and "+ New", the chosen
role's privileges as switches, Rename and Delete under a band's own
role's chips.

**Giving someone a role** is done on Members, on their row (a select,
saving as it changes, for `manage_roles`); Settings no longer has a
"Who has which role" list. A member who can't manage roles doesn't see
the matrix: Members says **"Your role: … · What it allows"**, a plain
list (✓ / –) of what their own role grants (`roleAllows`).

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
  Settings only for them (Archive or delete: a dialog asking for the
  band's name, typed, since the Settings in sections). `delete_band` was
  dropped from every role.
- **Renaming and archiving** is `rename_archive`, the core role's by
  default (migration 0131 also gave it to every band's own role holding
  `manage_roles`, its staff). `edit_band` keeps what the band plays.
- **Archiving** (`bands.archived_at`; the Settings' "Archive or delete",
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
- **Archiving asks first** (Settings: a dialog saying what it does) and
  **closes every invite into the band still open** — its open links and
  QR codes, its roster links (`SetBandArchived`).
  Unarchiving reopens none of them; new ones can be made. A
  placeholder's personal link stays (it lets no one new in). No one
  leaves an archived band; personal things stay a member's own: their
  instruments, rating a recording, their notes.

## Round 2 (migration 0132)

The user's decisions (2026-11-24):

- **Invites into any role.** Every way of inviting — an account by
  email or by name, a one-off link (removed since, see accounts.md), a
  placeholder's personal link, a roster
  link's names, the open link and its QR code — takes one of the band's
  roles (`roleId`; `store.ResolveBandInviteRole`), a band's own
  included, and the claim gives it. An invite keeps its role
  (`band_role_id` on `invites`, `group_invites`, `group_invite_slots`)
  beside its status: an invite made before (no role) gives its status's
  built-in role, as it did; a deleted role's invite gives its status,
  "guest" for a band's own role. An older client's `status` ("core",
  "guest") still works. The members page offers the roles in its
  selects, and one open link per role, named after it.
- **Guests don't edit the songs nor remove people**: `edit_songbook`
  and `remove_members` left the guest role of every band (nothing else
  of a band's choices changed). Their **notes** are their own, as
  before (section annotations: anyone who sees the song, not gated by
  `edit_songbook`). They **propose** (`propose`) and add to Discover and
  Influences, the Add buttons shown only to holders. Leaving the band
  oneself is every member's (the roster's "Leave the band").
- **Guests' votes are listed apart**: a guest — any member whose role
  doesn't grant `vote` — rates a proposal or says no thanks; it shows
  under "Guests", never counted (average, for/against, no thanks, "n of
  m voted", who hasn't voted, the order). The guest role doesn't get
  `vote` (that's what counts); a band's own role counts with the band
  if it holds `vote`, else it's listed apart with the guests. The
  blind rule is the same for everyone: one sees the others' votes once
  one has voted.
- **`curate_listen`**: removing anyone's Discover and Influences entries,
  core's by default (and every band's own role managing roles, as 0131
  did for `rename_archive`); the author always removes their own.
- **The band's ⚙ Settings** shows to whoever holds one of the
  privileges it changes (`rename_archive`, `edit_band`, `edit_look`,
  `manage_roles`), not by status (`IsCurrentBandCore` is gone). Since
  the Settings in sections (below), the page itself is theirs only.

## The Settings in sections (migration 0137)

The user's decisions (2026-11-24, docs/band-settings-proposal.md, its
open questions answered by default):

- **The band's Settings** share the community's Admin shell
  (`settings_shell.html`, `settings-shell.js`): The band (name:
  `rename_archive`; what it plays, tagline, website, language:
  `edit_band`), Look (`edit_look`), Roles (`manage_roles`), links to
  Members (`manage_members`) and the recordings' source
  (`manage_recordings`), and last Archive or delete (`rename_archive`;
  Delete: site admins). Each person sees the sections they may change
  (`bandSettingsFor`); a field of a section they open but may not change
  reads as text; no "your role doesn't let you" lines. The ⚙ shows when
  one section is theirs; `/bands/{id}/settings` sends anyone else to
  Members. An archived band's Settings open on Archive or delete.
- **Guests lose `edit_band` by default** (migration 0137): the guest
  role of every band that still granted exactly what 0132 left it loses
  it; a band that changed its guest role keeps its choice. A new band's
  guest starts without it. So a guest has no ⚙ by default.
- **The language** of the band's pages, its **tagline and website** go
  with what it plays: `edit_band` (`PUT …/locale`, `POST …/brand/words`).
  The Look (`POST …/brand`, `edit_look`) keeps the stored tagline and
  website of a band whatever it's sent.

## The status that remains

`band_members.status` (core/guest, kept in step with the role; a band's
own role is "core") is read only for display now:

- The home page's and the bands page's member lists (`list_cards.go`):
  core first, guests marked "guest". A band's own role sorts and shows
  with core, unmarked.
- A placeholder's invite page and the admin panel's memberships
  (`GetPersonMemberships`): the status word; the admin's core/guest
  select (`SetBandMemberStatus`) sets a built-in role — a site admin's
  fix-up, kept.
- The band's "what's new" digest (`band_digest.go`): who joined as core
  or guest.
- Invites keep a status beside their role (the fallback above).

## Not yet

- `decide_proposals` stays a guest's by default (the user's rule "any
  member adds or turns down a proposal"); a band keeps it to a staff by
  taking it from guest.
- The admin panel's membership select offers core and guest only.
