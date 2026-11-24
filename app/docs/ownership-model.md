# Songs, songbooks and who owns them — the model

**Why.** Musicians still share chord charts and scores by email, in
group chats or on paper. Every copy drifts, and nobody knows which one
is current. This sharing model brings the "Google Docs / Office 365"
way of working to that domain:

- **one shared, live, editable chart or score per song**, which everyone
  who plays it opens;
- **its history** kept, so any edit can be seen and reverted;
- **keys per place**: a band's, a student's or an event's key is kept
  where the song is played, not in another copy of the song.

Status: **agreed** (2026-11-22), ready to implement; §10 records the
last decisions. Replaces the
ad-hoc rules that grew around `variant_visibility` (global / user / band
/ community / setlist / jam) and the fixes of November 2026 (#141: band
copies made up front, another band's song refused).

"Public" in this document means **every signed-in person**. Showing
anything to logged-out visitors is a separate decision
([public-catalog.md](public-catalog.md)); band recordings shown to the
world (§8) would build on it.

## 1. The model in one paragraph

**Words** (2026-11-23). Three things used to be called "versions"; they
are kept apart now, in the app's words and in this document:

- **Versions** are a song's states over time: each edit, a fix of a
  chord or a typo, is a new version of the same song, kept in its
  **history** (revisions) and revertable. Nothing else is a version.
- **Recordings** (a catalogue song's **reference**) are different
  performances of the same work — the album take and a live one, the
  original and a cover (*Hurt* by Nine Inch Nails, *Hurt* by Johnny
  Cash). Each is its own catalogue song, followed by its chart, grouped
  under the work, and named by its title, artist and **reference label**.
- **Arrangements** (FR « arrangement ») are a band's, a person's or a
  community's own copy of a song, changed their way: "Make Mercy's
  arrangement", "Make my arrangement", "New arrangement from this…",
  its name the arrangement's name. The database keeps its column names
  (`forked_from_id`, `version_name`).

A song has **one owner** — a person, a band or a community — and is
either **private** (the owner's members) or **public** (everyone signed
in). A song is always created **in a songbook** of its owner. A songbook
holds its owner's own songs and **links** to songs its owner can see; a
link plays the linked song as it is — with the songbook's own **key** —
until someone makes **their own arrangement**, which is a new song.
Everyone belongs to one **global community**, whose songbook is
computed: every public song. The public songbook holds **well-known
recordings**: a song is identified by its title, its artist (the
performer of that recording) and its reference label, so "Stand By Me
— Ben E. King" and "Stand By Me — Tracy Chapman" are two songs of the
same work.

## 2. Who owns it, who sees it

| | Owner's members | Everyone signed in |
|---|---|---|
| **private** (default) | see, play; edit per role | — |
| **public** | see, play; edit per role | see, play, link, make their own arrangement |

- Owner: exactly one of person, band, community. The catalogue's songs,
  ownerless today, are owned by the global community (§6).
- Editing in place: the person; a band's current members (core and
  guest); a community's members with the "edit repertoire" right; for
  the global community's songs (the catalogue), **everyone signed in**,
  each edit weighed against the song's reference recording and applied
  at once or held for review, its staff (site admins) at once (§11). In
  a **coaching** community, the coach (its staff) and, for each of its
  songs, the students it's given to (their student songbook holds it):
  not another student's song, not the repertoire's others
  ([coaching.md](coaching.md), "The song the coach and the students
  share").
- **Making public** (a song or a whole songbook): a person for their
  own; a band's **core** members; a community's **staff**.
- **Everything goes public with the song**: the chart, the reference
  link, recordings, stems, the Guitar Pro file.
- A **closed** songbook (paid, e.g. Florent's methods, sold outside the
  app): its songs are private, can't be made public, and can't be
  linked, copied or made into an arrangement anywhere else — not even in a
  student's own songbook. Opening them takes the grant, as today.

## 3. Identity: title, artist and reference label; the work groups them

- A song is identified by **title + artist** (+ its **reference
  label**), the artist being the performer of that recording
  ("Wonderwall — Oasis", "Wonderwall — Ryan Adams"; "Hurt — Johnny
  Cash · American IV"). A catalogue song's public identity is **title –
  artist – reference label**: the label (the album, "Live at <place>,
  <date>", "Original", "Cover"…) is shown after the artist everywhere a
  catalogue song is listed (The Big Songbook, search, the Add dialog,
  the song's header). It is kept in `version_name`, which holds an
  arrangement's name on other songs. In the public songbook that
  identity is unique: a second "Wonderwall — Oasis" with the same label
  is a duplicate (§7).
- The **work** (`work_id`) groups every song of the same composition:
  the original and its well-known covers and live recordings, and every
  band's own arrangements. Search and the public songbook show a work
  once with its recordings and arrangements.
- A public song **follows its reference recording** (its YouTube link):
  its structure is the recording's. The app can check it with what it
  already has (the recording's analysis, the chart's sync to the
  recording, #117–#119): sections and bar counts that line up with the
  recording → "follows the recording"; a chart that doesn't → flagged.
  Two public songs with the same reference video are duplicates.

## 4. Songbooks: own songs and links

A songbook entry is either **an own song** (owned by the songbook's
owner) or **a link** to a song the owner can see — a public song for a
band or a community; for a person, also what they see through their
bands and communities, except closed songbooks' songs.

An entry carries what belongs to that songbook: its position and its
**key** (`semitones`). A key never makes an arrangement: a band's key, a
student's key (Chloé's) is the entry's.

A **set list** lists **songs of its owner's songbook** — a specific
arrangement when the band has several (§5). Set list items move out of the
text body into a table (`setlist_items`: set list, position, songbook
entry, note).

## 5. Arrangements

- **Changing a song one links makes an arrangement**: new chords, a
  different structure (skip the intro, a new solo, the chorus once
  more), new lyrics. An arrangement is a new song: owned by the
  songbook's owner, private, `forked_from_id` set, and it no longer
  follows the reference recording. (Editing one's own song in place
  makes a new *version* of it: its history.)
- **Except fixing the catalogue's song itself** (§11): a wrong chord, a
  typo, a missing section the recording plays is fixed in place by
  anyone signed in — the catalogue's song stays the one that follows
  its recording. Its **key** is never changed in place (it is the
  recording's): keys stay on the songbook entry, the student, the band.
- **Another recording** of a catalogue song (a live take, another
  performer) is another catalogue song: changing the reference of a
  catalogue song makes a new one for that recording (§11).
- **A band can have several arrangements of one song** — a punchy one
  and a psychedelic one — and its songbook shows **one row per
  arrangement** ("Bad Spell — psychedelic", the arrangement's name
  beside the title). A set list picks the one for that gig.
- On a linked song, **Edit** offers "Make Mercy's arrangement" (a band),
  "Make my arrangement" (a person), "Make the community's arrangement";
  on an arrangement already the band's, Edit edits it, and "New
  arrangement from this" makes another.
- **When the song an arrangement came from changes**: the arrangement's
  owner is told what changed and can take the change or keep theirs
  (the same idea as the Guitar Pro drift check).
- **Making a song private, or deleting it, while others link to it**:
  each songbook linking to it gets a **frozen copy** as it was, owned
  by that songbook's owner; set lists keep working; new people can't
  find it any more. (Not for closed songbooks: nothing links to them.)

## 6. The global community

- Everybody is a member, implicitly (no membership rows), whatever
  their role; its staff are the site admins.
- Its songbook is computed: every public song, by work.
- It owns today's catalogue songs.
- Name: **The Big Songbook** (FR « Le Grand Songbook »).

## 7. De-duplication

- Same **title + artist + reference label** (normalised: case, accents,
  "The", "&/and") among public songs → duplicates.
- Same **reference video** among public songs → duplicates.
- Duplicates are merged by the global community's staff: one stays,
  the others' links move to it, arrangements keep their `forked_from_id`
  to it. A reviewed command lists them first.

## 8. Band recordings, public

A band can make its recordings public (with a song, §2, or one by one):
in the public songbook a song shows "played by: Mercy (2 recordings)".
A band page anyone signed in can open, with its public songs and
recordings, is the next step; a page for the world is
[public-catalog.md](public-catalog.md)'s question.

## 9. Mapping today's data (staging, 2026-11-22)

| Today | Count | Becomes |
|---|---|---|
| `global` (catalogue) | 122 | owner: global community, public |
| `user` | 9 | owner: the person, private |
| `bandsongs` copies of Nov 2026, unchanged | 54 (Mercy 43, Couleur Acoustique 11) | removed; the band's entries become links again |
| Couleur Acoustique's songs differing from the catalogue | 15 | the band's arrangements, private |
| Other Lies | 30 | the band's originals, private |
| What's Up, Koalition (catalogue songs, never edited) | 9 | move to the catalogue (public, global community); the bands link to them. The Final Countdown: What's Up's chart (201) becomes the catalogue's; the catalogue's 52 is merged into it |
| Bands Camp's songbook | 66 links | stays a selection of catalogue songs (The Final Countdown: 201) |
| Florent Passamonti | 65 | the community's, private, its paid songbooks **closed** |
| Chloé Paul | 17 | catalogue material: covers close to the originals. Her charts replace the catalogue's (new revisions, the old charts in history); the community's copies go; students' entries link to the catalogue songs with their keys |
| `setlist`, `jam` | 0 | the values go |
| songbooks `shared` | 11 | replaced by public / closed |
| set list bodies (`variant:N`) | all | `setlist_items` rows pointing at songbook entries |

Done by a reviewed command with a dry run first (like `bandsongs`).

## 10. Decisions (2026-11-22)

1. The global community: **The Big Songbook** (« Le Grand Songbook »).
2. The Final Countdown: **What's Up's chart (201)** is the shared one.
3. Chloé's charts **replace** the catalogue's.
4. Florent's closed songbooks: a student may keep their own **key** on
   a song (an entry setting, nothing copied).
5. "Follows the recording" **flags only**: a badge or a warning; songs
   can be public either way; staff use it to tidy The Big Songbook.
   Since §11 it also weighs the edits of the catalogue's songs: one that
   makes the fit worse waits for review.
6. Keys on the entry; any change to a linked song an arrangement,
   several per band, one row each; the original's changes offered to
   its arrangements;
   frozen copies on unpublish/delete; everything public with a song;
   core members / staff publish.

## 11. The catalogue: open edits (2026-11-23)

The catalogue's songs (The Big Songbook's) are everyone's, so anyone
signed in fixes them — and nothing is lost.

- **Who**: everyone signed in edits a catalogue song's chart in place
  (Edit opens the editor; "Make my arrangement instead" is beside it). The
  recordings, the Guitar Pro file, erasing or deleting the song stay
  its staff's. Someone the staff keep from editing (the review queue's
  "Reject and keep … from editing") can't, until let again.
- **Nothing is lost**: every edit is a revision — who, when, and a
  one-line summary asked at save (optional) — listed in the song's
  History. One click reverts it: by its author while it is the latest,
  by a trusted editor or the staff any time. The arrangements made from
  the song are told what changed and take it or keep theirs (§5).
- **The reference recording is the anchor**: each catalogue song
  follows one recording, its `reference:`. The "follows the recording"
  check (§3, `songimport.RecordingFit`, cached in `song_recording_fit`)
  is read before and after the edit:
  - an edit that keeps or improves the fit is **applied at once** —
    chord fixes, lyrics typos, a missing section the recording plays;
  - an edit that makes it worse waits for **review**, and so does one
    that changes the lead-in (`lead:`), the Guitar Pro sync (`gp_start`,
    the sections' `gp[...]` lines) or the key, or removes the
    recording. It is saved as a pending edit:
    the song stays as it was for everyone; its author sees "waits for
    review" on the song and opens their text; it becomes current when
    approved (merged with what changed since, where it doesn't touch
    the same place);
  - a song whose recording has no analysis yet: a change of the
    sections or their bars waits too; other edits apply.
- **A fix, or another recording — asked at edit time**: an edit that
  changes the reference (in the Reference view or in Source), the title
  or the artist asks which it is:
  - **a fix** (the same recording: a dead link, a typo) edits the song
    in place, like any edit: not held for review on that account (the
    fit and the newcomer's rule still weigh it), a version in its
    history, revertable;
  - **another recording** (album and live, original and cover) makes a
    new catalogue song for it — same work, `forked_from_id` set,
    public, The Big Songbook's — with the edited chart and its
    **reference label** (prefilled with the video's title, #144); the
    original stays as it is. Its artist is that recording's performer:
    the album take and a couple of live ones, even by other artists,
    side by side. A new recording is **never held for review**, a
    newcomer's neither. When the reference didn't change (a new artist:
    a cover), its video is asked for.
- **The reference label** is edited in the Reference view by anyone
  who edits the catalogue, at once (a version, revertable). Title,
  artist and label identify the song (§3): the duplicates review (§7)
  takes the same three (or the same video) for duplicates, not two
  recordings labelled apart.
- **The key is the recording's**: a catalogue song isn't transposed in
  place (the editor's Transpose is off on it, and a chart transposed as
  a whole is refused); keys are set per songbook, student or band.
- **Light moderation**: a review queue (The Big Songbook → Review) for
  the staff and the **trusted editors** (10 accepted edits or more,
  computed): each pending edit with what it changes (`songdiff`), the
  fit before and after, Approve / Reject with a note for its author. A
  newcomer's first **3** edits wait whatever they change. A **Report**
  on a catalogue song (a reason) puts it in the queue until a reviewer
  marks it done. The author is told when their edit is approved or
  rejected; the reviewers are told when the queue has items — once
  while it does, not per edit.
- Migration 0125 (no column added since: the label is `version_name`):
  `revisions.summary`, `people.catalogue_banned_at`,
  `catalogue_edits` (every edit by someone not staff: applied, pending,
  approved, rejected, superseded; accepted ones count towards trust),
  `catalogue_reports`.
