# A community's download area, and songbooks as products

A community that teaches (Florent Passamonti's, first) hands out files with
each of its courses: the book as a PDF, the original Guitar Pro files, the
backing tracks. This adds one download page per community listing the files
of every songbook the reader has — lesson by lesson, as one zip, or folder by
folder — a zip upload for the people who manage them, download privileges
on the community's roles, stats of who viewed and downloaded what, and
reviews the access model so that each songbook can be sold on its own, or
in a bundle, once payments exist.

## 1. The model

```
community ── songbooks ── songs
                 │
                 └── files (songbook_files: book PDF, .gp, .mp3, …, in
                            folders; a file may go with one song of the
                            songbook)
                        └── blob (file_blobs: the bytes, once per content)
```

- **A songbook is the product.** A community's songbook is either open to
  every member, or *sold separately* (`requires_grant`): then a member needs
  a grant on it.
- **A file belongs to a songbook**, so the songbook's access is the file's
  access: whose files they are.
- **Downloading is a privilege of a role** (`download_files`), and so is
  managing the files (`manage_files`: adding, moving to another lesson,
  removing). A role without `download_files` sees no files, even of a
  songbook it has. Migration 0071 gave `download_files` to every role but
  guest, `manage_files` to staff; a new community's member role has
  `edit_repertoire` and `download_files`.
- **A grant** (`songbook_access_grants`) is someone's right to one songbook.
  It says where it came from (`source`: `manual` from the access page,
  `purchase`, `bundle`), the order it came with (`reference`), and may end
  (`expires_at`). An expired grant is kept, for history, and opens nothing.
- **A bundle is sold as several songbooks**: buying it writes one grant per
  songbook, with the same reference and `source = 'bundle'`. No bundle table
  until there is something to sell; the songbook stays the unit everything
  checks.

Who can open a community songbook (`store.CanAccessSongbook`):

| | open songbook | sold separately |
|---|---|---|
| not a member | no | no |
| member | yes | with a live grant |
| staff (any manage privilege) | yes | yes |
| site admin | yes | yes |

What the songbook gives: its page, its songs (`CanViewVariant`: a song in
only sold songbooks needs a grant on one of them), its PDF, its playlists,
its downloads.

Changing a songbook or its songs takes the `edit_repertoire` privilege *and*
access to the songbook.

## 2. The download page

`/communities/{id}/downloads`, a tab of every community page.

- One list, songbook by songbook, of the songbooks the reader has; a
  songbook they don't have isn't shown, nor are its files.
- Per songbook: *Everything · zip*, *Browse the folders*, the book (PDFs
  that go with no song and aren't numbered as a lesson) with *Open* and
  *Download*, then one row per lesson — the song's files (Guitar Pro first,
  then audio, a play button for audio), or loose files grouped by the
  author's lesson number (`20_Same old Blues.pdf`, `20_Same old Blues PB.mp3`).
- **Browsing** (`…/downloads/songbooks/{sbID}/browse/{folder…}`): the
  songbook's files as in its zip, folder by folder, each folder also as a
  zip of its own.
- **The zip** (`…/downloads/songbooks/{sbID}/zip/{name}?path=folder`) is
  made as it's sent: files stored as they are (PDFs and MP3s don't
  compress), so its size and every byte's place are known up front — the
  browser shows progress, and a broken download resumes (range requests).
- A file is served by `GET /communities/{id}/downloads/{fileID}/{name}`:
  a 404 for anyone without the songbook or the privilege, range requests
  answered, `?view=1` to open in the browser.
- Who manages the files also sees every songbook, whether it's open or
  sold (and to how many people), *Add files or a zip* (in the folder
  shown), which lesson each file goes with (a menu per file in the
  folders), remove a file or a folder.
- Files are stored in Postgres once per content (`file_blobs`, by hash) in
  1 MiB chunks, streamed, never read whole. The same content in two places
  (two songbooks, or an author's two copies) is stored once.

### Adding a zip

An upload of a `.zip` (`POST /api/communities/{id}/songbooks/{sbID}/files`)
is unpacked on the server (`store.ImportSongbookZip`), into the folder it
was added from:

- its folders are kept, less a top folder named as the zip itself;
  `__MACOSX`, `.DS_Store` and the like are left out; a zip inside is
  refused (add it on its own); names are read as UTF-8, or the old DOS
  code page, and stored composed (NFC);
- a file of the same name in the same folder is replaced; the same content
  again changes nothing;
- each file goes with the song of the songbook it's for: a Guitar Pro file
  the song was made from (same bytes, now or before) or named as the song;
  then any file named as a song (less a backing track's "PB"); then the
  files of a folder holding one song's Guitar Pro files; then the files
  with a matched lesson number. The page lists what went with no lesson,
  to set by hand.

`sbfiles import --songbook ID --name X.zip < X.zip` does the same from the
command line; `sbfiles add` adds one file.

Florent's two zips, unpacked this way, give the same file → lesson mapping
as the one made by hand from the pCloud folders, and the songbook's zip
gives back his files byte for byte.

## 3. What changed in the access checks

Found while reviewing, fixed here:

1. The default songbook's page and its PDF export (`/communities/{id}/songbooks/default`,
   `/communities/{id}/export-songbook.pdf`) only checked membership: a
   sold default songbook was open to every member there. They now check the
   songbook's own access.
2. `CanEditSongbook` gave `edit_repertoire` holders a sold songbook they
   hadn't bought. It now needs the songbook too.
3. `CanEditVariant` let anyone who could see a community song edit it,
   whatever their role. It now needs `edit_repertoire` — a guest, or a
   buyer joined as a guest, reads but doesn't change the lessons.

Members of a community without `edit_repertoire` can't change its songs
(above); they can still propose one (submissions) and copy one into their
own songbook (fork).

Still open:

- A song added to someone's own songbook or a set list while they had
  access: its page closes when the grant ends (`CanViewVariant`), but a set
  list's PDF export and a personal songbook's playlist still include it.
- Files are served as they are: no per-buyer watermark on the PDFs.
- A member can fork a lesson into their own songbook, which keeps it (and
  its Guitar Pro file) after their access ends.

## 4. Payments, later

Nothing here takes payment. When it comes:

- a product per songbook (and bundles: a name, a price, the songbooks);
- checkout (Stripe Checkout, say) for a product, by a logged-in person;
- the payment provider's webhook: join the community as a guest if not a
  member, then `GrantSongbookAccessFrom(songbook, person, 0, "purchase" or
  "bundle", <checkout id>, <end or nil>)` for each songbook;
- a refund revokes the grants with that reference; a subscription renews
  `expires_at`.

## 5. Stats

`/communities/{id}/admin/stats`, for the community's staff (linked from
its admin page):

- totals: lesson views, files downloaded, zips, backing tracks played,
  files opened, people downloading;
- per member: lessons viewed, views, files, downloads, zips, last active —
  and per member, every lesson viewed (how often, first and last) and every
  download;
- per songbook: its zips, its lessons (views, viewers, downloads of their
  files, last viewed), its files (downloads, plays, opens, people, last).

A download is a request for the whole file or its start (`songbook_downloads`,
one row each; the same again within 30 s is the same). A view is an opening
of the song's page (`song_visits.views`, counted from migration 0071 on).

## 6. Florent Passamonti's community, today

- Two songbooks: *La Nouvelle Méthode Blues Rock* (34 lessons, the default)
  and *Jouez le Blues — La méthode ultime* (31). **Both are open to every
  member.** To sell them separately, tick *Only members given access* on
  each songbook's access page, and grant the people who bought each.
- Members join with the `member` role, which there is read-only:
  `download_files` only (no `edit_repertoire`). Staff has every privilege.
- Florent himself is still an invited account (`is_shell`): he has to
  claim it from his invite to sign in and manage the files.
- The lesson songs are community songs (not global), so no other route
  shows them.
