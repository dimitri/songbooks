# Fixing a catalogue song: why Edit made a fork, and how to make "anyone fixes the shared song" the default

Status: **review and recommendation** (2026-11-26), nothing built yet.
Base: `review` (= `main` = production, migration 0139).
Related: [ownership-model.md](ownership-model.md) (§5 arrangements, §11
open edits), [coaching.md](coaching.md) ("A group works on one song").

## The report

> I tried to edit (fix) a global catalogue song entry and I had to fork
> it, I could not just edit.

The reporter is a site admin, a core member of a band (Mercy) and has a
My songbook. The goal is in ownership-model.md's **Why**: one shared,
live, editable chart per song, with its history and keys per place, the
Google Docs way of working. A fix should never need a copy.

## In short

- **The cause is a rule, not a slip of the finger.** When a band you
  belong to has the catalogue song in its songbook or a set list, Edit
  offers only "Make <band>'s arrangement", with no way to fix the song.
  This happens wherever you open the song from: the band's set list, the
  catalogue, a search, **even My songbook**. It applies to a site admin
  too, though the admin is the catalogue's staff. The code is
  `bandEditOffer` and the line after it in `handleSong`
  (`httpapi/handlers_pages.go`). The same happens in a non-coaching
  community's repertoire or course. In a coaching community, "Fix the
  catalogue's song" is there, but as the second choice.
- **The wording pushes people away from fixing**: "Changing it here
  would change it for everyone, not for the band." To a musician that
  sounds like a danger, when it is exactly the point of a fix.
- **A newcomer's fix is held for review**: the first 3 edits wait,
  whatever they change. Until a reviewer approves it, the author's own
  set list keeps the wrong chord. The editor says the opposite ("is
  applied at once").
- **Recommendation**: Edit always opens the editor on the shared song,
  from every place and for everyone signed in. A fix goes live at once
  and stays in History with one-click undo; reviewers check edits
  afterwards (Wikipedia's patrol) instead of holding them. "Make our
  arrangement" becomes a separate, explicit choice for playing the song
  differently, never for a key or a fix. Changes to the song's identity
  (title, artist, recording, key name, Guitar Pro sync) become inline
  **suggestions** (Google Docs' suggesting mode). An arrangement that
  only differs by a fix is offered a fold-back into the shared song.
- **Step 1 is a small fix** that can ship on its own: never let a band
  or community offer take away a catalogue editor's right to fix, and
  never offer an arrangement to someone who can't make one (a band
  guest).

## 1. The demo used

The demo dump was gone from the scratchpad, so the demo is a copy of a
full local demo cluster (anonymized production structure, migration 137,
migrated to 139 by the app) plus a song made for this review through the
app's own API (`POST /api/catalog/songs`, `PUT /api/songs/{id}/raw`,
`POST /api/songbooks/{id}/entries`, the band fork API). The song is a
chords-only chart, with no lyrics:

- **Harbour Lights Waltz — The Copper Kites** (song 335), The Big
  Songbook's, with a reference recording (a placeholder YouTube link, no
  analysis), key G, 3/4.
- It is linked from: Lucas's **My songbook**; **The Paper Lanterns'**
  songbook and its set list *Concert · La Péniche Rouge* (the band plays
  the "Mercy" part); **Nord Express**'s songbook; **Jam du Canal**'s
  repertoire (a community); **Studio Lumen** (coaching): its repertoire,
  Oliver's student songbook, and the *Mardi soir* group's songbook; the
  course *Méthode Blues vol. 1*, lesson 1 (Atelier Blues Rive Gauche).
- **Nord Express's arrangement** (song 336), made with "Make Nord
  Express's arrangement", then one chord changed (Verse 1, Am → Am7): an
  arrangement that is really only a fix.
- People (demo names):
  - **Lucas Mercier**: site admin; core member of The Paper Lanterns
    and Nord Express; staff of Jam du Canal; a student of Studio Lumen.
    This matches the reporter.
  - **Camille Prescott**: a newcomer (0 accepted edits) with the song in
    her My songbook.
  - **Hazel Prescott**: signed in, the song in none of her places.
  - **Oscar Winslow**: a trusted editor. His 10 accepted edits were
    seeded straight into `catalogue_edits` (approved).
  - **Margaux Chevalier**: a Paper Lanterns core member.
  - **Jade Kendrick**: a Paper Lanterns guest.
  - **Inès Marchal**: Studio Lumen's coach.
  - **Oliver Ashby**: a Studio Lumen student in *Mardi soir*.
  - **Grace Stanton**: Nord Express core member, Jam du Canal member.
  - **Romain Perrin**: Atelier Blues staff, who owns the course.

The screenshots are in the session scratchpad,
`cap/edit-review/<path>-<user>[-step]-{desktop,phone}.png` (1440 and 390,
light). They are not in this PR. Some dialogs are in French: the demo
served those places (Studio Lumen, the course) in French.

## 2. Every way into Edit, and what happens

What decides it, in `handleSong` (handlers_pages.go):

1. `store.SongRightsFor` works out the rights. On a catalogue song:
   `Edit` for the site's admins, and `Contribute` for anyone signed in
   who isn't banned. `CanEditChart = Edit || Contribute`.
2. `communityEditOffer`: the page was opened from a community's
   songbook (`?songbook=`, or a course found by `withCourseContext` when
   the address has no place), and the viewer may make that community's
   arrangement (`MayMakeVersion`). Without a songbook in the address,
   `coachingEditOffer` checks any coaching community that links the song.
3. Else `bandEditOffer`: one of the viewer's bands has the song in its
   songbook or a set list (`BandsHolding`). It uses the band in the
   address (`?band=`, `?setlist=`, a band's `?songbook=`). **With no
   band in the address, it takes the first such band for any public
   song**, so a catalogue song opened from the catalogue, a search, home
   or **My songbook** gets the band offer as well.
4. **If either offer exists: `canEdit, canEditChart = false, false`.**
   Edit opens `#edit-offer-dialog` instead of the editor. The only
   exception is a coaching community's link to a catalogue song
   (`CatalogueFix`), which adds "Fix the catalogue's song".

What each person saw on song 335, checked against the served page
(`data-edit-offer`, `data-can-edit`) for every place and person, and
captured:

**Lucas** (admin, band core):
- From the band's set list, My songbook, the catalogue, the band's
  songbook: **"Make The Paper Lanterns's arrangement" only**.
  Shots: `c-setlist-admin-lucas`, `b-mysongbook-admin-lucas`,
  `a-catalogue-admin-lucas`.
- From Jam du Canal's repertoire: **"Make the community's
  arrangement" only**. Shot: `d-community-admin-lucas`.
- From the course lesson: **the Paper Lanterns offer only** (in
  French). Shot: `f-lesson-admin-lucas`.
- From Studio Lumen (repertoire, student, group): the coaching dialog
  with Fix as the second choice.
- From Nord Express (`?band=16`), whose arrangement replaced the link:
  **the editor**, with "your edit changes it for everyone, at once".
  Shot: `c-band-with-arrangement-admin-lucas`.

**Margaux** (band core): **"Make The Paper Lanterns's arrangement"
only, from every place**. Shot: `c-setlist-bandcore-margaux`.

**Jade** (band guest): the same offer, from every place. Clicking it
fails: "Couldn't make the band's arrangement of this song. Try again."
**A dead end**, because a guest has no `edit_songbook` privilege. Shots:
`c-setlist-bandguest-jade`, `…-after-click`.

**Camille** (newcomer), from any place: the editor. Its note says an
edit "is applied at once" or waits. On save: **"Sent for review"**
("one of its author's first edits"). Shots:
`b-mysongbook-newcomer-camille-editor`, `…-saved`, `…-pending-banner`.

**Hazel** (signed in, a stranger), from any place: the editor. A new
reference asks "A fix, or another recording?". A transposed chart is
refused ("Not saved… transpose it in a songbook"). Shots:
`a-catalogue-stranger-hazel-editor`, `…-fix-or-recording`,
`…-transposed-refused`.

**Oscar** (trusted), from any place: the editor; a chord fix is
**applied at once** ("Saved."). Shots: `a-catalogue-trusted-oscar-editor`,
`…-saved`.

**Grace** (Nord Express core, Jam du Canal member, who may edit its
repertoire):
- From Jam du Canal's repertoire: "Make the community's arrangement"
  only: Jam du Canal's members edit its repertoire, so she gets the
  community offer and no fix. Shot: `d-community-member-grace`.
- From everywhere else: the editor.

**Inès** (coach), from every place, since the song is in Studio Lumen:
the dialog "Modifier ce morceau" (*Change this song*). The **primary**
button is "Make the community's arrangement"; "Fix the catalogue's
song" is second. Shot: `e-coaching-coach-ines`.

**Oliver** (student), from every place: the same dialog, the
arrangement first. Shot: `e-coaching-student-oliver`.

**Romain** (course staff), from every place, because of the course
context: **"Make the community's arrangement" only**. Shot:
`f-lesson-coursestaff-romain`.

So the paths where a plain fix is **impossible** are:

- **P1. Any band member, admin or not, on any catalogue song one of
  their bands links, from any place**: the catalogue, a search, home, My
  songbook, the band's songbook or set lists. The reporter, an admin and
  Mercy core member, is in this case for every song Mercy plays. The
  only way out was to open the song with `?band=` of a band that
  doesn't hold it, which nobody would do.
- **P2. Whoever edits a non-coaching community's repertoire or
  course** (Jam du Canal's staff, a course's staff), from that songbook,
  and from everywhere when the song is in a course lesson
  (`withCourseContext` makes the course the page's place).
- **P3. A band guest** is offered an arrangement they can't make: a
  dead end with a generic error.

And the paths where the fix is there but **steered away from**:

- **P4. Coaching** (coach and students, from everywhere once a coaching
  community links the song): the arrangement is the primary button, the
  fix the secondary one. The dialog adds a step before every edit.
- **P5. Everyone in the editor**: the note has a link "Make my
  arrangement instead" (for the admin too). For a newcomer, it promises
  "applied at once", which their first 3 edits never are.

Why it is built that way: #141 and §5 of the ownership model wanted
"never a silent copy" and "a band edits its own arrangement". When §11
(#165) opened the catalogue to everyone's edits, `CanEditChart` was
added for the plain path. But the band and community offers kept their
older meaning, "changing the shared song is wrong here, make yours".
Only #181 (coaching) added the fix beside it. The dialog's text still
says the old rule: "This is the catalogue's song, which Mercy links.
Changing it here would change it for everyone, not for the band."

## 3. What happens after a fix, today

- **Applied or held** (`catalogueSave`, catalogue_edits.go):
  - The site's admins: always applied.
  - Others: applied at once when the edit keeps or improves how the
    chart follows its recording (`songimport.RecordingFit`, only when
    the recording has an analysis).
  - Held for review when the edit:
    - makes that fit worse;
    - changes the sections or their bars when the recording has no
      analysis (most songs);
    - changes the lead-in, the Guitar Pro sync or the key name;
    - removes the recording;
    - is one of a **newcomer's first 3**, whatever it changes.
  - Refused: a chart transposed as a whole (422).
  - A new reference, title or artist asks "a fix, or another
    recording?" first.
- **Who reviews, how the author knows**:
  - The staff and the trusted editors (10 accepted edits) review, at
    The Big Songbook → Review (`after-review-queue-trusted-oscar`).
  - Reviewers get one notification while the queue has items.
  - The author sees a banner on the song, "waits for review, the song
    stays as it is for everyone" (`…-pending-banner`), and the edit's
    page (`after-pending-edit-page-newcomer-camille`). They are told
    when it's approved or rejected.
  - There is no time promise, and no reviewer is assigned.
  - **The author's own places show the old chord meanwhile.** Camille
    can't play her fix tonight.
- **Linked places see an applied fix live**: they link the song itself.
  Each keeps its key (the songbook entry's `semitones`, a student's or
  group's key). Nothing to do.
- **Arrangements are offered the change** (#150):
  - The arrangement's page shows "The original changed" with the bars,
    "Take this change" / "Keep ours", per section
    (`after-arrangement-original-changed-admin-lucas`).
  - On the demo, Nord Express's one-chord "arrangement" shows Verse 1
    "changed here too: ours stays", so the band's own fix and the
    shared one now differ in two places.
- **Home's "Changes in your songs"** (#180) lists it with "Take its
  changes" for the arrangement's editors
  (`after-home-changes-in-your-songs-admin-lucas`).
- **History and revert**: every save is a revision with its summary
  (`after-history-admin-lucas`). Its author reverts it while it's the
  latest; trusted editors and staff revert any time.
- **Two people at once**: a save carries the version it started from; a
  stale one gets 409 and "Copy your changes, reload the page, then save
  again". The whole chart is the unit; nothing is merged at save. A held
  edit, though, is merged at approval "where they don't touch the same
  place".

## 4. Recommendation: anyone fixes the shared song

### 4.1 The rule

**Edit on a catalogue song always edits the shared song.** This holds
from every place (catalogue, My songbook, a band's songbook or set list,
a repertoire, a coaching songbook, a lesson), for everyone signed in and
not banned, a band guest and a student included. Keys stay per place and
never need a copy. An arrangement is a separate, explicit act, for a
musical difference that one place wants and others don't.

This is §11's rule ("everyone signed in edits a catalogue song's chart
in place"). Only the band and community offers broke it.

### 4.2 Per path

All of these open the editor on the shared song:

- **(a) The catalogue, a search, home.**
- **(b) My songbook.** The band no longer captures it.
- **(c) A band's songbook or set list.** The editor's note names the
  band and its key. "Make <band>'s arrangement…" moves to the ⋯ menu
  and the note's link, and only for those who may make it.
- **(c′) A band that already has its own arrangement**, opened on the
  shared song: as today, the editor, with the notice "<band> has its own
  arrangement — open it" kept.
- **(d) A community repertoire.** As (c), with "Make the community's
  arrangement…".
- **(e) Coaching (coach, student, group).** No dialog first. "Make
  Studio Lumen's arrangement…" becomes secondary. A *teaching* version
  (a grid for singing, a shorter form) stays the coach's choice, worded
  as one.
- **(f) A lesson or course.** As (d). The course no longer turns every
  open into a "community" page.

What stays as it is:

- **Another owner's own song** (a band's or a person's song, not the
  catalogue's): Edit isn't theirs, so "Make our / my arrangement"
  stays, with the dialog that says why (`You can't change this song`).
- **Not signed in, or banned**: no Edit, as today.

### 4.3 Arrangements, worded as a choice for playing differently

- The arrangement moves to the ⋯ menu (and the phone's Song sheet) with
  its purpose in the label: *"Make The Paper Lanterns' arrangement… —
  play it your way: another form, your own intro, a groove the record
  doesn't have. The band's copy stops following this chart."*
- Confirming it asks once more when the person may only want a fix:
  "For a fix, edit the shared chart instead: everyone gets it, your band
  included, and your key stays." The two choices sit side by side, with
  no default.
- The band offer's current text goes ("Changing it here would change it
  for everyone, not for the band"). It describes a risk where the
  product wants the effect.
- An arrangement is never offered to someone who can't make it: check
  `MayMakeVersion` in `bandEditOffer`, as `communityEditOffer` already
  does (fixes P3).

### 4.4 What stays protected, and how

| What | Why it's special | How |
|---|---|---|
| Chords, lyrics, sections, notes | the chart itself | **Applied at once**, in History, one-click undo; reviewers check afterwards (patrol) |
| Transposing the whole chart | the recording's key; every place keeps its own | **Refused** as today, pointing to the place's key |
| Key name (`key:`), lead-in, Guitar Pro sync, `gp[...]` | every place's key name and the player's alignment depend on them | **Suggestion**: shown inline to its author, live when a reviewer accepts |
| Title, artist, reference label, reference link | the song's identity; duplicates are found by them | "A fix, or another recording?" stays. Another recording is a new song (never held); **a fix of the name is a suggestion** |
| Removing the recording | the song stops following one | Suggestion |
| Guitar Pro file, audio, sync points, erasing or deleting | heavy, hard to judge by a diff | Staff only (`CanEdit`), as today |
| Worse fit against an analysed recording | the chart stops matching what people hear | Applied, **flagged** for patrol (a reviewer's one-click revert), instead of held |

**Suggestions instead of held edits.** A suggestion is the same
`catalogue_edits` row as a pending edit today. What changes is how it
shows:

- Its author sees it in place, marked (a dashed underline, "suggested"),
  and so can play it.
- Reviewers see it inline on the song as well as in the queue.
- In a mixed save, the chord fixes go live and only the protected part
  waits ("Save — chords live, title suggested").
- Google Docs works the same way: identity changes are suggested, the
  rest edited.

### 4.5 Review and trust without blocking

- **Patrol instead of a gate** (Wikipedia):
  - Every applied edit by someone other than the staff or a trusted
    editor is *unpatrolled* until a reviewer marks it seen or reverts
    it. The review queue becomes "Recent edits to check", newest first,
    with the diff, the fit before and after, Revert, and Looks good.
  - Today's held edits become suggestions (4.4), so fewer things wait.
- **Newcomers**:
  - Today a newcomer's first 3 edits wait whatever they change. Their
    chord and lyric fixes should be applied and patrolled instead.
  - To protect against vandalism:
    - a rate limit (for example 10 applied catalogue edits a day until
      trusted);
    - one-click revert, which already exists;
    - the "keep … from editing" ban, which already exists;
    - an account's very first edit to a song that **none of its places
      holds** stays a suggestion (a passer-by's, not a player's).
  - A member of a band or community that plays the song is a player:
    applied at once.
- **Trust** stays computed (10 accepted edits). Accepted now means
  "applied and not reverted within 7 days", or "suggestion accepted".
  Trusted editors patrol and accept suggestions.
- **The author always knows**:
  - "Saved for everyone who plays it · Undo · History" after a save;
  - for a suggestion, "Suggested: you see it now; a reviewer accepts it
    (n reviewers)";
  - a reverted edit tells its author why (the note).

### 4.6 Two people editing

- At save, merge against the current song instead of refusing with 409.
  The code is already there: approving a held edit merges "where they
  don't touch the same place" (`songdiff`, store/catalogue_edits.go).
  Use the same three-way merge at save, with the version the editor
  opened as the base.
- Changes in different sections or bars merge silently ("Inès saved
  while you were editing: both kept"). The same bar asks once, "Hers:
  Am7 / Yours: A7".
- Later, optionally: presence ("Inès is editing this chart"), through
  the realtime channel the app already uses for jams, if it fits.
  Character-level live co-editing (Google Docs) isn't needed for charts.

### 4.7 Arrangements that were only fixes: fold them back

Some existing arrangements of catalogue songs are fixes in disguise:
the forced fork of P1/P2, or the silent copies before #141. On the
local demo, 5 of 8 personal copies of catalogue songs are identical to
their original today. Production should be counted the same way;
staging access was refused in this session.

- **Detect**:
  - for each arrangement of a catalogue song, its diff to the original
    at the time it was made (`base_body`) and to the original now;
  - **identical** means an accidental copy: offer to go back to the link
    (nothing to send);
  - **a few chords or lyric words, same structure** means probably a
    fix: offer to send it;
  - other structure changes or a name (`version_name`) mean a real
    arrangement: leave it alone.
- **Offer** (never automatic): on the arrangement's page and in
  "Changes in your songs":
  - "This arrangement differs from the shared chart by one chord: Verse
    1, bar 6 Am → Am7. Send the fix and use the shared chart / It's our
    arrangement — keep it."
  - Sending applies the diff to the shared song (as a normal edit, by
    that person, under 4.4 and 4.5). Every songbook and set list entry
    of the arrangement then points back to the shared song, its key
    kept. The arrangement is retired and kept in history, not deleted.
- **A tool**: `cmd/foldback` lists candidates (counts, ids and titles,
  no bodies), dry run by default, like `cmd/reflabels`.

### 4.8 What to borrow, what to avoid

- **Google Docs: edit vs suggest.**
  - Borrow: editing is the default; suggestions are inline, visible to
    their author, accepted in place; version history with restore.
  - Avoid: asking people to pick a mode. Here the field decides
    (identity is suggested, the chart is edited).
- **Wikipedia: edit, then patrol.**
  - Borrow: anyone edits live, recent changes are patrolled, revert is
    one click, a reason line, light protection only for contested or
    identity pages.
  - Avoid: the bureaucracy; long talk pages.
- **MusicBrainz: edits and votes.**
  - Borrow: identity data (artist, recording, label) is edited carefully,
    since it is what duplicates and links are matched on. That is the
    suggestion lane.
  - Avoid: voting periods of days for every edit. A band needs the
    right chord tonight.
- **Ultimate Guitar: one version per contributor.**
  - Avoid: this is the fork-per-person model that the forced
    arrangement recreates. It gives five half-right charts, a rating
    contest, and nobody knows which one is current. Its only good idea
    (several real arrangements side by side) is what arrangements are
    for.

### 4.9 Mockups

The mockups are rendered with Playwright Firefox from
`scratchpad/edit-review-mockup.html`:

- `cap/edit-review/mockup-desktop.png` (1440, all of them).
- `cap/edit-review/mockup-phone-1.png` … `mockup-phone-7.png` (390, one
  per screen); `mockup-phone.png` is the whole page.

The screens:

1. **Edit opens the shared chart**, from a band's set list: the note
   "One chart for everyone who plays this song" (where it shows, keys
   stay), "What did you fix?", the arrangement as a quiet link.
2. **Saved**: "Saved for everyone who plays it · Undo · History"; the
   band's set list showing the fix in its own key (A).
3. **The ⋯ menu**: "Edit the chart", "Key for The Paper Lanterns",
   then "Make The Paper Lanterns' arrangement…" with its purpose, and
   its confirmation pointing back to the fix.
4. **A title change as a suggestion**, with the fix-or-recording choice
   kept and "chords live, title suggested".
5. **Two people editing**: merged when apart, asked once when the same
   bar.
6. **Fold back**: an arrangement that differs by one chord.
7. **Desktop**: the editor note on one line and "who sees this chart"
   (places with their keys, arrangements offered the fix).

## 5. Step plan

1. **Stop forcing the fork** (small; the bug the reporter hit):
   - In `handleSong`, when `rights.Catalogue && rights.Contribute`, a
     band or community offer never clears `canEditChart` (nor `canEdit`
     for staff). Edit opens the editor.
   - The offer moves to the catalogue note's link ("Make <band>'s
     arrangement instead") and to the ⋯ menu.
   - `bandEditOffer` checks `MayMakeVersion`, as `communityEditOffer`
     does, so no offer is shown to a guest who can't make it.
   - `bandEditOffer` no longer falls back to the first band when the
     page has no band (an explicit menu item per band instead, when
     there are several).
   - The coaching dialog goes away for catalogue songs; its two
     sentences become the editor note.
   - Strings: FR catalog.
   - Tests: song page per place and role (admin and band core from a set
     list and My songbook get the editor; a guest gets no offer).
   - No migration.
2. **Wording and the newcomer promise**:
   - The editor note becomes "One chart for everyone who plays this
     song… keys stay each place's own; History keeps every edit".
   - The save toast gets Undo.
   - The note stops promising "at once" to someone whose edit will
     wait.
3. **Patrol instead of holding**: newcomers' chord and lyric edits are
   applied, with an *unpatrolled* flag on `catalogue_edits` (a
   migration), a rate limit, and the passer-by rule (4.5). The review
   page becomes "Recent edits to check". Trust is counted from edits not
   reverted.
4. **Suggestions**: held edits shown inline to their author and to
   reviewers. A mixed save splits into an applied part and a suggested
   part (4.4).
5. **Merge at save**: three-way merge with the opened version as the
   base; asked only for the same bar (4.6).
6. **Fold back**: detection (`cmd/foldback`, dry run), the offer on the
   arrangement and in "Changes in your songs", and "Send the fix and use
   the shared chart" (4.7).
7. Optional: "Who sees this chart" in the editor, and presence.

Steps 1–2 make the reported path work and can ship together. Steps 3–6
are independent of each other.

## 6. Open questions

1. **Newcomers**: apply and patrol (recommended), or keep holding the
   first edits but show them to their author (a suggestion)? And is the
   passer-by rule (a song none of your places holds) worth its
   complexity?
2. **A band's own way vs a fix**: a band that plays a section twice "on
   purpose" might "fix" the shared chart to its form. Should a
   structure change by a band member who has the song in a set list
   suggest "Make the band's arrangement" before saving (a hint, not a
   gate)?
3. **Key name (`key:`)**: is a key label fix really risky enough to
   hold, since no chord moves and every place keeps its semitones? It
   could be applied with patrol.
4. **Production numbers**: how many band, person and community
   arrangements of catalogue songs are identical, or one or two chords
   away, from their original? Staging is read-only, but this session
   was refused access.
5. **Coaching**: should a coach keep a one-tap "Make the teaching
   version" near Edit? Teaching versions are frequent and legitimate.
6. **Presence and live updates**: worth the realtime work for charts,
   or is merge-at-save enough?
