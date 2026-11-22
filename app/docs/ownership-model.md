# Songs, songbooks and who owns them — the model

Status: **proposed**, for review before implementation; decisions of
2026-11-22 folded in, the remaining questions in §10. Replaces the
ad-hoc rules that grew around `variant_visibility` (global / user / band
/ community / setlist / jam) and the fixes of November 2026 (#141: band
copies made up front, another band's song refused).

"Public" in this document means **every signed-in person**. Showing
anything to logged-out visitors is a separate decision
([public-catalog.md](public-catalog.md)); band recordings shown to the
world (§8) would build on it.

## 1. The model in one paragraph

A song has **one owner** — a person, a band or a community — and is
either **private** (the owner's members) or **public** (everyone signed
in). A song is always created **in a songbook** of its owner. A songbook
holds its owner's own songs and **links** to songs its owner can see; a
link plays the linked song as it is — with the songbook's own **key** —
until someone makes **their own version**, which is a new song. Everyone
belongs to one **global community**, whose songbook is computed: every
public song. The public songbook holds **well-known versions**: a song
is identified by its title and its artist (the performer of that
version), so "Stand By Me — Ben E. King" and "Stand By Me — Tracy
Chapman" are two songs of the same work.

## 2. Who owns it, who sees it

| | Owner's members | Everyone signed in |
|---|---|---|
| **private** (default) | see, play; edit per role | — |
| **public** | see, play; edit per role | see, play, link, make their own version |

- Owner: exactly one of person, band, community. The catalogue's songs,
  ownerless today, are owned by the global community (§6).
- Editing in place: the person; a band's current members (core and
  guest); a community's members with the "edit repertoire" right; the
  global community's staff (site admins).
- **Making public** (a song or a whole songbook): a person for their
  own; a band's **core** members; a community's **staff**.
- **Everything goes public with the song**: the chart, the reference
  link, recordings, stems, the Guitar Pro file.
- A **closed** songbook (paid, e.g. Florent's methods, sold outside the
  app): its songs are private, can't be made public, and can't be
  linked, copied or made into a version anywhere else — not even in a
  student's own songbook. Opening them takes the grant, as today.

## 3. Identity: title and artist; the work groups versions

- A song is identified by **title + artist**, the artist being the
  performer of that version ("Wonderwall — Oasis", "Wonderwall — Ryan
  Adams"). In the public songbook that pair is unique: a second
  "Wonderwall — Oasis" is a duplicate (§7).
- The **work** (`work_id`) groups every song of the same composition:
  the original and its well-known covers, and every band's own
  versions. Search and the public songbook show a work once with its
  versions.
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
**key** (`semitones`). A key never makes a version: a band's key, a
student's key (Chloé's) is the entry's.

A **set list** lists **songs of its owner's songbook** — a specific
version when the band has several (§5). Set list items move out of the
text body into a table (`setlist_items`: set list, position, songbook
entry, note).

## 5. Versions

- **Any change to the song itself makes a version**: new chords, a
  different structure (skip the intro, a new solo, the chorus once
  more), new lyrics. A version is a new song: owned by the songbook's
  owner, private, `forked_from_id` set, and it no longer follows the
  reference recording.
- **A band can have several versions of one song** — a punchy one and a
  psychedelic one — and its songbook shows **one row per version**
  ("Bad Spell — psychedelic", a version name beside the title). A set
  list picks the one for that gig.
- On a linked song, **Edit** offers "Make Mercy's version" (a band),
  "Make my version" (a person), "Make the community's version"; on a
  version already the band's, Edit edits it, and "New version from
  this" makes another.
- **When the song a version came from changes**: the version's owner is
  told what changed and can take the change or keep theirs (the same
  idea as the Guitar Pro drift check).
- **Making a song private, or deleting it, while others link to it**:
  each songbook linking to it gets a **frozen copy** as it was, owned
  by that songbook's owner; set lists keep working; new people can't
  find it any more. (Not for closed songbooks: nothing links to them.)

## 6. The global community

- Everybody is a member, implicitly (no membership rows), whatever
  their role; its staff are the site admins.
- Its songbook is computed: every public song, by work.
- It owns today's catalogue songs.
- Name: open (§10).

## 7. De-duplication

- Same **title + artist** (normalised: case, accents, "The", "&/and")
  among public songs → duplicates.
- Same **reference video** among public songs → duplicates.
- Duplicates are merged by the global community's staff: one stays,
  the others' links move to it, versions keep their `forked_from_id`
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
| Couleur Acoustique's songs differing from the catalogue | 15 | the band's versions, private |
| Other Lies | 30 | the band's originals, private |
| What's Up, Koalition (catalogue songs, never edited) | 9 | move to the catalogue (public, global community); the bands link to them. The Final Countdown: What's Up's (201) and the catalogue's (52) — see §10 |
| Bands Camp's songbook | 66 links | stays a selection of catalogue songs |
| Florent Passamonti | 65 | the community's, private, its paid songbooks **closed** |
| Chloé Paul | 17 | catalogue material: covers close to the originals. Her charts differ from the catalogue's — see §10; students' entries link to the catalogue songs with their keys |
| `setlist`, `jam` | 0 | the values go |
| songbooks `shared` | 11 | replaced by public / closed |
| set list bodies (`variant:N`) | all | `setlist_items` rows pointing at songbook entries |

Done by a reviewed command with a dry run first (like `bandsongs`).

## 10. Open questions

1. **The global community's name** — candidates around "songbook" (see
   the PR thread).
2. **The Final Countdown**: What's Up's chart (201) becomes the
   catalogue's (replacing 52), and Bands Camp lists it?
3. **Chloé's charts**: they differ from the catalogue's versions of the
   same songs (none is an unchanged copy). Do hers replace the
   catalogue's (as new revisions), or the catalogue's stay and hers go?
4. **Florent's closed songbooks**: may a student still keep their own
   key on a song (an entry setting, nothing copied)?
5. **"Follows the recording"**: a check that flags only, or does a
   public song have to pass it to be published?
