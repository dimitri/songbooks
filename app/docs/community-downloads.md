# A community's download area, and songbooks as products

A community that teaches (Florent Passamonti's, first) hands out files with
each of its courses: the book as a PDF, the original Guitar Pro files, the
backing tracks. This adds one download page per community listing the files
of every songbook the reader has, and reviews the access model so that each
songbook can be sold on its own, or in a bundle, once payments exist.

## 1. The model

```
community ── songbooks ── songs
                 │
                 └── files (songbook_files: book PDF, .gp, .mp3, …;
                            a file may go with one song of the songbook)
```

- **A songbook is the product.** A community's songbook is either open to
  every member, or *sold separately* (`requires_grant`): then a member needs
  a grant on it.
- **A file belongs to a songbook**, so the songbook's access is the file's
  access. There is no separate download permission.
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
- Per songbook: the book (PDFs that go with no song) with *Open* and
  *Download*, then one row per lesson — the song's files (Guitar Pro first,
  then audio, a play button for audio), or loose files grouped by the
  author's lesson number (`20_Same old Blues.pdf`, `20_Same old Blues PB.mp3`).
- Staff also see every songbook, whether it's open or sold (and to how many
  people), a link to its access page, *Add files* (with the song they go
  with) and a remove button per file.
- A file is served by `GET /communities/{id}/downloads/{fileID}/{name}`:
  a 404 for anyone without the songbook, range requests answered (a 80 MB
  PDF resumes), `?view=1` to open in the browser.
- Files are stored in Postgres in 1 MiB chunks (`songbook_file_chunks`),
  streamed, never read whole. The same file twice in a songbook is kept
  once.
- `sbfiles add` (cmd/sbfiles) adds a file from the command line, for a
  bulk load.

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

Still open:

- A song added to someone's own songbook or a set list while they had
  access: its page closes when the grant ends (`CanViewVariant`), but a set
  list's PDF export and a personal songbook's playlist still include it.
- Files are served as they are: no per-buyer watermark on the PDFs.

## 4. Payments, later

Nothing here takes payment. When it comes:

- a product per songbook (and bundles: a name, a price, the songbooks);
- checkout (Stripe Checkout, say) for a product, by a logged-in person;
- the payment provider's webhook: join the community as a guest if not a
  member, then `GrantSongbookAccessFrom(songbook, person, 0, "purchase" or
  "bundle", <checkout id>, <end or nil>)` for each songbook;
- a refund revokes the grants with that reference; a subscription renews
  `expires_at`.

## 5. Florent Passamonti's community, today

- Two songbooks: *La Nouvelle Méthode Blues Rock* (34 lessons, the default)
  and *Jouez le Blues — La méthode ultime* (31). **Both are open to every
  member.** To sell them separately, tick *Only members given access* on
  each songbook's access page, and grant the people who bought each.
- New members join with the `member` role, which has `edit_repertoire`:
  set the community's default join role to `guest` so buyers can't change
  the lessons.
- The lesson songs are community songs (not global), so no other route
  shows them.
