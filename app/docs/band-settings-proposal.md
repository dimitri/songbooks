# A band's Settings: review and proposal

Status: proposal (2026-11-24), nothing built. Scope: the band's ⚙
Settings page (`/bands/{id}/settings`) as it is on `review` after #179
(band-roles) and #184 (band-roles-2), the band's Members page around it,
and how comparable products organise the same thing.

**The recommendation, in one paragraph.** Give the band's Settings the
shell the community's Admin already has (`community_admin.html`,
`.admin-layout`): a sidebar of sections on a wide screen, a list to tap
into on a phone, one section at a time. Four sections, in this order:
**The band** (name, what it plays, tagline, website, language), **Look**
(one logo, the card picture, the style), **Roles** (the matrix, the
band's own roles managed from their column), and, last and apart,
**Archive or delete**. The roster, invites and who-has-which-role stay
on **Members**, linked from Settings. Each person sees the sections
they can change and nothing else: no ⚙ for a member who can change
nothing there, no greyed fields saying "your role doesn't let you".

Mockup: `band-settings-mockup.html` (desktop and phone frames, light,
demo data), with its screenshots — see "Files" at the end.

## 1. What's there today

The page is one column of seven cards, in a fixed order, the same for
everyone who opens it. The Paper Lanterns (demo data, 6 members, 3
roles, a custom look, two open links), seen by a core member:

| # | Block | What it does | Who may change it (privilege → default roles) | How often | Height 1440 / 390 | How it saves |
|---|---|---|---|---|---|---|
| 1 | **The band** | Band name; what it plays (covers, original music, not set) | name: `rename_archive` → core; what it plays: `edit_band` → core **and guest** | setup once; a rename is rare | 409 / 409 px | "Save changes", page reloads |
| 2 | **Logo / image** | What the band's card shows on the home page: automatic, the logo (with its own upload), a covers mosaic, covers picked; live card preview | `edit_look` → core | setup once | 408 / 698 | "Save the picture" |
| 3 | **Language** | Language of the band's pages for members who haven't chosen | `edit_look` → core | setup once | 186 / 186 | saves on change, page reloads |
| 4 | **Look & feel** | Preset, "From our songs", 7 colours, title and text fonts, buttons, mark, logo (upload or URL), tagline, website | `edit_look` → core (others get a lone line "Your role doesn't let you change the band's look") | setup once, a tweak now and then | **1,367 / 1,920** | "Save look"; "Reset to the app's look" |
| 5 | **Roles** | Matrix: 15 privileges in 4 groups × the band's roles (a tick saves at once); the band's own roles (rename, delete); "New role" form; "Who has which role" (one select per member, saves at once) | `manage_roles` → core; every member sees the matrix read only | setup once; then monthly at most | **2,159 / 1,823** | per tick / per select |
| 6 | **Archive the band** | Archive (dialog: read only, out of menus, every invite closed) / unarchive | `rename_archive` → core | once in a band's life | 219 / 219 | dialog |
| 7 | **Delete the band** | Delete for everyone | site admins only (no privilege) | almost never | 159 / 179 | browser `confirm()` |

The whole page: **5,203 px at 1440, 5,629 px at 390** — 6 to 7 phone
screens of scrolling, for things nearly all set once.

**Who sees the ⚙.** Whoever holds one of `rename_archive`, `edit_band`,
`edit_look`, `manage_roles` (`bandSettingsPrivileges`, space_nav.go). The
page itself is open to every member (`requireBandMember`). Because the
guest role holds `edit_band` by default, **every guest gets the ⚙** — to
change one radio — and lands on a page where the name is a read-only
field with "Your role doesn't let you rename the band", the look is one
line saying they can't change it, and the 16-row matrix is read only
(2,410 px at 1440).

**The band's other admin-ish places** (not on Settings, and mostly
rightly so):

| Where | What | Who | How often |
|---|---|---|---|
| Members (tab) | Roster: instruments, a role select per row (`manage_roles`), a placeholder's personal link (copy, QR, new link), Remove / Leave the band; quick add by name | everyone sees it; `manage_members`, `manage_roles`, `remove_members` change it | weekly |
| Members | "Add people to this band": someone you play with or a one-off link (into any role); the band's open link and QR code, one per role, with who joined by it | `manage_members` → core | per rehearsal or gig |
| Members | Past members (toggle) | everyone | rarely |
| Recordings | The pCloud folder ("Use this folder", "Read it again"); Corrections: file name templates, dates and titles to fix | `manage_recordings` → core and guest | setup once, then after each rehearsal |
| Songbook | PDF of the songbook; playlist | everyone | weekly |
| Discover / Influences | Removing anyone's entry (`curate_listen`) — in place, on the list | core | in passing |
| Calendar subscription | Personal (`/calendar`, the member's own feed): not a band setting | each member | once |
| Vote settings | None any more: the star threshold went in #184; whose vote counts is the `vote` privilege, in Roles | — | — |

Screenshots of today's pages (demo data, light): see "Files".

### What goes wrong

1. **One long page for set-once things.** Seven cards, three different
   widths (`card--md`, the full-width look editor, `card--narrow`), the
   two biggest (Look & feel, Roles) are 70 % of the height. Nothing
   tells you what's set without scrolling through every form.
2. **Six save behaviours on one page**: Save + reload; a second Save
   for the picture; Language saves itself and reloads; "Save look";
   ticks and role selects save at once; Archive asks in a dialog, Delete
   in `confirm()`. A user changing the tagline and the language does two
   different things and loses the first if they don't save before the
   reload.
3. **The same thing in two places.**
   - The logo is uploaded from "Logo / image" *and* from "Look & feel"
     (the same `brand/logo`, card-image.js says so).
   - "Who has which role" repeats the Members roster's role select.
   - Tagline and website (words) sit inside "Look & feel" (looks), far
     from the name.
4. **Telling people what they can't do.** "Your role doesn't let you
   rename the band", "…change the band's look", "Only members whose role
   lets them manage roles can change these": three refusals for a
   guest who opened ⚙ to change one radio.
5. **Members and Settings point at each other**: Members says "the
   band's roles are in its settings" and "name, look and deletion are in
   Settings" (deletion is for site admins only); Settings says "make new
   ones from Members". Each one is fine; together they say the split
   isn't obvious.
6. **Two patterns for one job.** A community's ⚙ ("Admin") already has
   sections with a sidebar and a phone list (`.admin-layout`, its
   `#hash` script); a band's ⚙ ("Settings") is a long page. Same app,
   same ⚙, two shapes.
7. **Phone.** The hub row says "Settings — name, look" whatever is set;
   the Look editor is 1,920 px of fields; the roles matrix fits three
   roles at 390 px and scrolls sideways beyond.
8. **Names in code**: `IsCore` on the page means "`edit_look` or site
   admin" — not the core role. Harmless, confusing.

What works and should stay: the archive dialog (what it does, invites
closed, how to undo); the roles matrix on a wide screen (comparing roles
at a glance is the point); the card picture's live preview; ticks that
save at once; the rule "someone must keep `manage_roles`".

## 2. What others do

Sources are official help pages unless marked *[memory]* (not verified
now) or *[snippet]* (the official page blocked fetching; its search
snippet was used).

- **Discord, server settings**: full-screen **sidebar of sections**
  (Overview, Roles, Members, Invites, …); each section shows only to
  someone with its permission; **Delete Server** at the very bottom,
  owner only, **type the server name**. On a phone: list → section.
  <https://support.discord.com/hc/en-us/articles/206029707>,
  <https://support.discord.com/hc/en-us/articles/213595197> *[snippet]*
- **GitHub, organisation**: **People** is a top tab, *outside* Settings
  (members, invitations, a role per row); **Settings** is a grouped
  sidebar where roles are *defined*; a red **Danger zone** at the bottom
  of General, delete by **typing the name**.
  <https://docs.github.com/en/organizations/managing-membership-in-your-organization/inviting-users-to-join-your-organization>,
  <https://docs.github.com/en/organizations/managing-organization-settings/deleting-an-organization-account>
- **Linear**: Settings is a sidebar; everyone sees their own and team
  settings, only admins an **Administration** group (Members, Security…).
  Deleting: an emailed code, then 48 hours to undo.
  <https://linear.app/docs/workspaces>, <https://linear.app/docs/members-roles>
- **Notion**: Settings is a grouped modal (Account / Workspace); People
  with Members / Guests / Groups sub-tabs; Danger zone at the bottom of
  General, type the workspace name; workspace admin settings **don't
  exist on mobile**. <https://www.notion.com/help/workspace-settings>
- **Slack**: Workspace settings is a page with tabs (Settings,
  Permissions); Delete Workspace at its bottom, owner only, checkbox +
  password. <https://slack.com/help/articles/115004868646>,
  <https://slack.com/help/articles/204067366-Delete-a-workspace> *[snippet]*
- **Google Groups**: one settings page of sections (General, Member
  privacy, Posting policies, Email options, Member moderation); members
  on their own page. <https://support.google.com/groups/answer/2464926>
- **WhatsApp / Telegram group info** (phone): one screen — header, a few
  settings, the member list, then red **Exit group** / **Delete group**
  at the bottom, each with its own confirmation sheet; Telegram grants
  admin rights per person, never more than one holds.
  <https://faq.whatsapp.com/498814665492149> *[snippet]*,
  <https://telegram.org/faq> *[snippet]*; layout *[memory]*
- **Band tools.** **BandHelper**: an Admin menu (Users, Billing…),
  permissions set per user (editing None / Added data / All, viewing
  limits), no role matrix; users deactivated, not deleted
  (<https://www.bandhelper.com/tutorials/managing_users.html>).
  **BandLab Bands**: members from the band's ⋯ → Manage Members (Make
  admin, Transfer ownership, Remove); a leaving owner transfers or
  deletes (<https://help.bandlab.com/hc/en-us/articles/48011428986009>
  *[snippet]*). **Planning Center Services**: ranked permission levels
  per person (Viewer < Scheduler < Editor < Administrator), overridable
  per folder; account settings for org admins only
  (<https://help.planningcenter.com/en/136863-permissions-in-services.html>).
  **Back On Stage**: Company settings with Roles and Members as sibling
  sections. "Bandmate" as a product couldn't be identified; Gigwell's
  docs are too thin to say.

**The patterns:**

1. **Desktop: a sidebar of sections**, never one long page of forms.
   **Phone: list → section** (Discord, Telegram, WhatsApp) — or nothing
   (Notion).
2. **Members and invites are their own place**, often outside Settings
   (GitHub People, BandLab Manage Members, Google Groups). Where they're
   inside, they're their own section.
3. **Roles are defined in one place, given in another**: the role's
   definition in Settings, the person's role on the member row.
4. **What you can't change isn't shown** (Discord per permission, Linear
   "Administration", Planning Center's account settings).
5. **The danger zone is last, red, apart**, and the confirmation grows
   with the damage: a dialog for something undoable, **typing the name**
   for a delete.
6. **Archive / deactivate before delete** where history matters
   (BandHelper, GitHub).
7. **Band tools stay simpler than workspaces**: a few roles, or
   per-person permissions; nobody shows a band a big matrix on a phone.

## 3. Proposal

### 3.1 The shape: the community's Admin shell, shared

One settings shell for every kind of space: the `.admin-layout` that
`community_admin.html` already uses (sidebar ≥ 900 px, a list below; one
section at a time by `#hash`, every section shown without scripts; "‹
All sections" on a phone). Move its markup and its 30-line script into a
shared template (`settings-shell`), use it for both. A section is
addressable (`/bands/14/settings#roles`), so Members can link "What each
role allows" straight to it.

Why this and not tabs or real sub-pages: it already exists, is already
translated and tested on phones, and makes the band and the community
look like one app. Sub-pages (`/settings/look`) can come later if a
section grows a second level; nothing in the proposal needs them.

### 3.2 The sections

Order: most-used first, irreversible last.

| Section (EN / FR) | What's in it | Shown to (privilege) | State on its row |
|---|---|---|---|
| **The band** / **Le groupe** | Band name · what it plays (a segmented control) · tagline · website · language of the band's pages. One Save, enabled once something changed, no reload | any of `rename_archive` (name), `edit_band` (what it plays), `edit_look` (tagline, website, language); fields one can't change show as plain text | "Covers · language automatic" |
| **Look** / **Apparence** | One logo (upload, or an https address) · what the card shows (automatic, logo, covers mosaic, covers picked) · style: presets as swatches, "From our songs" · **Fine-tune** (folded): 7 colours, 2 fonts, buttons, mark · live preview (the home card and the band's header) · one Save · "Back to the app's look" | `edit_look` | "Paper · logo" / "The app's look · no logo yet" |
| **Roles** / **Rôles** | The matrix (desktop) or one role at a time with switches (phone) · "+ New role" as the last column head · a band's own role: ⋯ Rename / Delete on its head · each role's member count links to Members filtered by role | `manage_roles` | "Core, Guest, Staff" |
| *On other pages* / *Sur d'autres pages* | **Members and invites ›** / **Membres et invitations ›** (to Members) · **Where the recordings come from ›** / **D'où viennent les enregistrements ›** (to Recordings) | `manage_members` / `manage_recordings` | "6 members · 2 open links" / "pCloud, read 22 Nov" or "Not set yet" |
| **Archive or delete** / **Archiver ou supprimer** — last, apart, red | Archive (its dialog, as now) / Unarchive; Delete (site admins) with the band's name typed to confirm | `rename_archive`; Delete: site admins | "The band is active" / "Archived on 3 Dec" |

Moved or dropped:

- **"Who has which role"** leaves Settings: the Members roster already
  has the same select on every row. Roles keeps a count per role, linking
  to Members (`?role=`).
- **"The band's own roles"** list and the long **"New role"** form fold
  into the matrix's column heads (⋯ and "+ New role" popover: name,
  start from Core / Guest / nothing).
- **Tagline and website** move from Look to The band (stored where they
  are, in the brand; only the form moves).
- **The second logo upload** goes: Look has one logo, used by the card
  and the header.
- **Language** stops being its own card and stops reloading on change.
- **"Your role doesn't let you…" lines** go: a section someone can't
  change isn't listed; a field they can't change reads as text.

### 3.3 Who sees what

The rule: **⚙ is for changing things; reading is done where the thing
is used.**

- The ⚙ shows when at least one section is the viewer's, as today
  (`bandSettingsPrivileges`). `manage_members` and `manage_recordings`
  don't add it: their "On other pages" rows are only links, shown to
  someone who already has the ⚙.
- **Recommended (needs the user's yes):** take `edit_band` from the
  guest role by default. It's a set-once choice, and it is today the only
  reason every guest sees ⚙. A band that wants guests to set it ticks it
  back. Alternative if guests must keep it: keep it, and accept the ⚙
  for them with one section (The band, with only "What it plays"
  editable).
- Members who can't manage roles read **what their own role allows** on
  Members: "Your role: Guest · What it allows ›" opens a short plain list
  (✓ / –), not the 16-row matrix. The read-only matrix stays reachable
  at `settings#roles` for the curious, linked from there.
- An **archived** band: Settings shows only Archive or delete (with
  Unarchive first) and The band's name — the only things
  `rename_archive` still allows.

### 3.4 Desktop and phone navigation

- **Desktop (≥ 900 px):** the band's header and tabs as on every band
  page; under them, the sidebar (250 px) and the open section. Settings
  opens on The band. The ⚙ in the header is "current".
- **Phone:** the band's hub row "Settings" gets a real state (the same
  as the sections': "Covers · Paper look · 3 roles") instead of the fixed
  "name, look". Settings is the list; a section opens full screen with
  "‹ Settings"; long forms (The band, Look) keep their Save in a bar at
  the bottom of the screen; Archive's dialog is a bottom sheet with
  44 px buttons.
- **Roles on a phone:** chips for the roles (with their counts) and "+
  New"; the chosen role's privileges as switches, in the four groups.
  No sideways scrolling whatever the number of roles.

### 3.5 The danger zone

- Last in the list, after a divider, its icon and name in the danger
  colour; never on the way to anything else.
- **Archive**: the current dialog (already good), with real numbers
  ("its 2 open links and QR codes stop working").
- **Delete** (site admins): a dialog, not `confirm()`; the button stays
  off until the band's name is typed; the dialog offers "archive
  instead".
- Not proposed now: ownership transfer (bands have no owner, roles
  cover it), a grace period for delete (site admins only, restorable
  from backups).

### 3.6 Empty and first-run states

Settings has no empty state of its own: every section has a sensible
default (covers/originals "Not set", the app's look, Core and Guest).
First run belongs on the band's **Overview**, for whoever created the
band: a "Get the band ready" checklist — name ✓, songs in the songbook,
what it plays (→ The band), a logo or a look (→ Look), invite the band
(→ Members) — with "Roles: Core and Guest are ready; add one only if a
staff decides". Gone when done, or hidden by hand.

### 3.7 Names (EN / FR)

| EN | FR | Note |
|---|---|---|
| Settings | Réglages | the ⚙, unchanged |
| The band | Le groupe | existing string; not "Profile", which is the person's own page |
| Look | Apparence | the section; "Look & feel" already says "Apparence" |
| Fine-tune: colours, fonts, buttons, mark | Affiner : couleurs, polices, boutons, symbole | |
| Roles | Rôles | |
| On other pages | Sur d'autres pages | |
| Members and invites | Membres et invitations | |
| Where the recordings come from | D'où viennent les enregistrements | |
| Archive or delete | Archiver ou supprimer | |
| Type %s to confirm | Tapez %s pour confirmer | |
| Archive instead | Archiver plutôt | |
| Your role: %s | Votre rôle : %s | |
| What it allows | Ce qu'il permet | |
| Get the band ready | Préparer le groupe | |

The community keeps "Admin" for now (open question below).

## 4. Implementation plan

Each step is its own PR, shippable alone.

1. **Shared shell.** Move `.admin-layout`'s markup (nav, sections,
   "All sections" link) and its script from `community_admin.html` into
   `settings-shell` (templates) + `static/js/settings-shell.js`; the
   community's Admin uses it unchanged (screenshots before/after).
   jstest for the `#hash` switching.
2. **Band Settings in sections.** `band_settings.html` on the shell:
   The band, Look, Roles, Archive or delete; one nav row per section the
   viewer may change (a `bandSettingsSections(privs, isAdmin)` helper,
   unit-tested per role: core, guest, a staff role, site admin,
   archived); the "On other pages" rows; row states. Rename `IsCore` →
   `CanEditLook`.
3. **The band section.** Name, what it plays, tagline, website,
   language in one form, one Save (no reload: update the header in
   place); tagline and website leave the brand form (same API).
4. **Look.** One logo (drop the card editor's upload, keep the brand's);
   the card picture's choices beside it; presets as swatches;
   "Fine-tune" in a `<details>`; one Save for the brand and the card
   picture (two requests, one button).
5. **Roles.** Drop "Who has which role" (Members has it) and the "own
   roles" list; ⋯ and "+ New role" in the column heads; counts link to
   `members?role=`; the phone's one-role-at-a-time view. Members gets
   "Your role · What it allows". Members' filter by role.
6. **Archive or delete.** Section last; Delete's typed-name dialog;
   archived bands open on it.
7. **Gating.** If the user agrees: migration taking `edit_band` from the
   guest role's defaults (bands that changed it keep their choice? — see
   open questions), docs/band-roles.md updated.
8. **Phone hub state and the first-run checklist** on the Overview.
9. Strings in `fr/*.json`; docs (`band-roles.md`'s "The roles editor",
   this file → "Done").

Out of scope here, worth its own pass: the Members page itself (a
placeholder's row carries six controls; "Add people" is two cards with
long hints).

## 5. Open questions

1. **Guests and ⚙**: take `edit_band` from the guest role by default
   (recommended), for every band or only bands that never changed the
   guest role?
2. **Language**: stays with `edit_look`, or goes with `edit_band` (it's
   identity more than looks)?
3. **"Admin" vs "Settings"**: rename the community's ⚙ to "Settings"
   too, now that both share a shell?
4. **First-run checklist**: on the Overview (proposed) or nowhere?
5. **Members' "What it allows"**: plain list (proposed) or the read-only
   matrix?

## Files

Not in the repository (session scratchpad):

- Mockup: `band-settings-mockup.html` (self-contained, Google Fonts only).
- Mockup screenshots: `cap/band-settings/mockup-desktop.png` (1440),
  `cap/band-settings/mockup-phone.png` (390).
- Today's pages: `cap/band-settings/current-settings-{desktop,phone}.png`
  (core member), `current-settings-guest-{desktop,phone}.png`,
  `current-members-{desktop,phone}.png`, `current-home-{desktop,phone}.png`;
  the community's Admin for reference:
  `reference-community-admin-{desktop,phone}.png`.
