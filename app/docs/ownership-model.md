# Songs, songbooks and who owns them — the model

Status: **proposed**, for review before implementation. Replaces the
ad-hoc rules that grew around `variant_visibility` (global / user / band
/ community / setlist / jam) and the fixes of November 2026 (#141: band
copies made up front, another band's song refused). Open questions are
in §9; nothing here is built until they're answered.

"Public" in this document means **every signed-in person**. Showing
anything to logged-out visitors is a separate, later decision
([public-catalog.md](public-catalog.md)).

## 1. The model in one paragraph

A song has **one owner** — a person, a band or a community — and is
either **private** (the owner's members) or **public** (everyone signed
in). A song is always created **in a songbook** of its owner. A
songbook holds its owner's own songs and **links** to songs its owner
can see; a link plays the linked song as it is (as at a jam) until
someone chooses to make **their own version** of it, which replaces the
link. Everyone belongs to one **global community**, whose songbook is
not stored: it is the union of every public song.

## 2. Two separate questions: who owns it, who sees it

| | Owner's members | Everyone signed in |
|---|---|---|
| **private** (default) | see, play, (edit: per role) | — |
| **public** | see, play, edit | see, play, link, make their own version |

- Owner: exactly one of `owner_person_id`, `owner_band_id`,
  `owner_community_id`. The catalogue's songs, which have no owner
  today, are owned by the global community (§6).
- Editing a song in place is for its owner only: the person; a band's
  current members (core and guest); a community's members with the
  "edit repertoire" right; the global community's staff (site admins).
- A **songbook** can be made public as a whole: all its own songs are
  public while they're in it. A paid songbook (`requires_grant`) can't
  be public, and neither can a song only reachable through one.

## 3. Songbooks hold own songs and links

A songbook entry is either:

- **an own song** — owned by the songbook's owner; or
- **a link** — to any song the songbook's owner can see: a public song
  for a band or a community; for a person's songbook, also the songs
  they see through their bands and communities.

An entry carries what belongs to that songbook and not to the song: its
position, its **key** (`semitones`, §5), and the **arrangement** (§5).

A **set list** lists entries of its owner's songbooks (not songs
directly): a band's set list plays the band's entries, with their keys
and arrangements. Set list items move out of the text body into a
table (`setlist_items`: set list, position, songbook entry, note).

## 4. Making your own version

On a linked song, **Edit** offers "Edit Mercy's version" (a band),
"Edit my version" (a person), "Edit the community's version": it copies
the song into the songbook's owner (`forked_from_id` set), and the copy
replaces the link in that songbook — same position, key, arrangement,
set list items. The copy is private; its owner can make it public.

Until someone edits, everybody plays the shared version. Nothing is
copied up front (§7 undoes the up-front copies of November 2026).

What happens to an own version when the song it came from changes is
open (§9, question 3).

## 5. What changes a song, what doesn't

Not every difference needs a version (open, §9 questions 1–2):

| Change | Where it lives |
|---|---|
| Key (a band's key, a student's key) | the songbook entry (`semitones`) — no version |
| Arrangement: skip the intro, a solo added in an existing section's place, the chorus once more | open: the entry, or a version (§9 q2) |
| New chords, new sections, new lyrics, a different form | an own version |
| Notes (personal, band, coach) | annotations, as today — never a version |

## 6. The global community

- Everybody is a member, implicitly (no membership rows): admins,
  staff, students, any role. Its staff are the site admins.
- Its songbook is computed: every public song, grouped by work
  (`work_id`) with "n versions", the most used first.
- It owns today's catalogue songs (122 on staging).
- Name: open (§9).

## 7. Mapping today's data (staging, 2026-11-22)

| Today | Count | Becomes |
|---|---|---|
| `global` (catalogue) | 122 | owner: global community, public |
| `user` | 9 | owner: the person, private |
| band copies made by `bandsongs` (unchanged) | 54 (Mercy 43, Couleur Acoustique 11) | removed; the band's entries become links to the public songs again |
| band songs differing from the catalogue | 15 (Couleur Acoustique) | the band's own versions, private |
| band originals | 39 (Other Lies 30, What's Up 8, Koalition 1) | the band's, private |
| community songs | 82 (Florent 65, Chloé 17) | the community's, private |
| `setlist`, `jam` | 0 | the values go |
| songbooks `shared` (copies allowed) | 11 | not "public": becomes "others may make their own version" |
| set list bodies (`variant:N`) | all | `setlist_items` rows pointing at songbook entries |

Done by a reviewed command with a dry run first (like `bandsongs`).

## 8. What the screens need

1. **Catalogue / search**: everything the person can see, each with its
   owner and visibility ("Mercy · private", "Other Lies · original ·
   public", "Yours · private", "<global> · public"); filters All /
   Mine / My bands / My communities / Public; versions grouped.
2. **Add / Create** in every songbook: one search over everything
   visible; Add makes a link; Create has "Goes into" (defaults to the
   songbook it was opened from, any songbook the person can add to) and
   "Who sees it" (private / public).
3. **Song page**: owner and visibility under the title; "Edit <owner>'s
   version" on a link; for the owner, the visibility toggle and what
   goes public with the song (§9).
4. **Songbook settings**: public as a whole; "others may make their own
   version".
5. **The global community page**: its computed songbook, by work.
6. **Making private / deleting a song others use**: a confirmation that
   says who uses it and what happens to them (§9).

## 9. Open questions

1. **Keys.** A key on the entry for bands and coaching, never a
   version?
2. **Arrangements** (skip the intro, a new solo, the chorus once more):
   an arrangement on the entry over the shared chart, or always an own
   version?
3. **The original changes after a version was made**: notify the
   version's owner with what changed and let them take it, or nothing?
4. **Making private / deleting a song others link to**: their songbooks
   keep a frozen copy, or the link goes?
5. **What goes public with a song**: chart and reference link only,
   recordings and the Guitar Pro file opt-in one by one?
6. **Who can make a band's / community's song public**: core members,
   any member, staff only?
7. **The global community's name.**
8. **Undo the 54 up-front copies** of November 2026?
