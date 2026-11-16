# Courses and lessons

A **course** is a songbook whose songs are grouped in **lessons**. It is not
a new kind of product. The songbook is still what is sold or open, shared or
private, and what holds the files (see [community-downloads.md](community-downloads.md)).
A course only adds order and structure on top.

```
songbook (kind = 'course')
  ├── lessons: number, title, text
  │     ├── songs (songbook_entries.lesson_id, in lesson_position order)
  │     └── files (a file's own lesson_id, or else its song's lesson)
  └── songs in no lesson
```

## The model (migration 0073)

- `songbooks.kind` is `songbook` or `course`. Changing it keeps the lessons,
  so a course can go back to being a plain songbook and back again.
- A lesson (`lessons`) has the author's `number` (optional), a `title`, and a
  `body`: plain text, where a blank line starts a new paragraph. Lessons are
  in number order; unnumbered ones come last.
- A song is in at most one lesson of a songbook: `songbook_entries.lesson_id`.
  The same song can still be in other songbooks.
- A file goes with its song's lesson. A file with no song, such as lesson
  20's PDF and backing track, has its own `songbook_files.lesson_id`.
- **Progress** belongs to each person:
  - *started* means they opened one of the lesson's songs (`song_visits`)
    or files (`songbook_downloads`);
  - *done* means they said so (`lesson_progress`).

What follows from that:

- A course's song order is its lessons' order (`ListSongsOrdered`). The
  song page's previous/next, the songbook PDF and the playlists all follow
  it, with the songs in no lesson last.
- Who can open a lesson, its songs and its files is exactly who can open the
  songbook (`CanAccessSongbook`). The rules for downloading and copying are
  unchanged.

## Pages

- **/songbooks/{id}**, for a course: the lessons in order, with each
  person's progress, and a *Start*, *Continue* or *Next* button. The
  community's default songbook page sends a course here.
  `?view=songs` shows the plain songbook page.
- **/songbooks/{id}/lessons/{lessonID}**: *Mark as done*, the lesson's text,
  its songs (opened in the course, so *back* returns to the lesson), its
  files (download, play, open), and the previous and next lessons.
- **The songbook's *Sharing and access* page** lets staff choose Songbook or
  Course.
- **Downloads** lists a course's files by lesson, each linked to the lesson.
- **Stats**:
  - a *Lessons* table per course: songs, views, viewers, people done, file
    downloads;
  - *Lessons done* for each member, listed on the member's own page.

  What used to be called "lessons" in the stats, the songs, is now
  called songs.

## Organizing a course

People who can change the songbook (`CanEditSongbook`: `edit_repertoire`
plus access to the songbook) can:

- **Make lessons from the files.** There's a button on the course page, and
  `sbfiles course --songbook N` does the same. A lesson is made for each
  number that a top folder (`07_Plan Chicago Blues/`) or a file name
  (`07_Plan Chicago Blues PB.mp3`) starts with.
  - The lesson's title comes from the folder, or else from the Guitar Pro
    files' common name ("Plan psychédélique 1" and "… 2" give
    "Plan psychédélique"). The song's own title is used when it is the
    same words with accents ("Hommage a SRV" becomes "Hommage à SRV").
  - Songs and files already in a lesson stay where they are, so running it
    again only adds what is new.
  - Uploading a zip to a course does this by itself.
- Add a lesson, then edit its number, title and text, or delete it. Deleting
  a lesson leaves its songs and files in the course, in no lesson.
- Put a song in a lesson or take it out, and move it up or down within it.

The HTTP API is in `internal/httpapi/courses.go`:
- `POST /api/songbooks/{id}/kind`;
- `POST`, `PUT` and `DELETE /api/songbooks/{id}/lessons[/{lessonID}]`;
- `POST /api/songbooks/{id}/lessons/from-files`;
- `PUT /api/songbooks/{id}/entries/{variantID}/lesson`;
- `POST /api/songbooks/{id}/lessons/{lessonID}/songs/{variantID}/move`;
- `PUT /api/songbooks/{id}/files/{fileID}/lesson`;
- `PUT /api/songbooks/{id}/lessons/{lessonID}/done`.

Moving a file to a different lesson has an API but no button yet.

## Florent's two courses

Built from the files:

- *La Nouvelle Méthode Blues Rock* has 34 lessons, numbered 1 to 35 with no
  lesson 31. Lesson 20, "Same old Blues", has no song: just its PDF and its
  backing track.
- *Jouez le Blues* has 30 lessons. Lesson 1 has both "Mon premier Blues"
  songs.

Most titles come from Florent's file names, which have no accents
("La note quil vous manque"). He can fix them on each lesson's page and add
the lesson texts there too.
