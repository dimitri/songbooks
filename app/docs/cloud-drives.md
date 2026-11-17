# More drives for a band's recordings

pCloud works today: the band pastes a public folder link, the server lists the
folder, the browser asks pCloud for each file's link and plays it; no audio
goes through us. This is the plan to do the same with the drives bands use.
The research behind it is in `research_notes/Cloud drives for recordings/`
(not in the repo); nothing below was tested live yet.

## What decides a drive

An `<audio>` element plays a file from another site and follows redirects
without CORS; what it can't do is send an `Authorization` header. So the
question for each drive is: can we get a URL that plays as is, with Range
requests (iOS Safari needs them to play and to seek)? And can the folder be
listed from a public link, or only with the owner's OAuth?

| Drive | List a public link | OAuth | A URL that plays | Verdict |
|---|---|---|---|---|
| **pCloud** | yes (showpublink) | exists, but its links are tied to the asker's IP | browser asks getpublinkdownload | done; keep public links |
| **Google Drive** | yes, with an API key (`'FOLDER' in parents`) | `drive.file` may not see a picked folder's files; `drive.readonly` is restricted (yearly CASA assessment, ~$500–4,500) | `files.get?alt=media&key=` for public files | **public links only** |
| **Nextcloud / ownCloud** | yes, WebDAV PROPFIND on `/public.php/dav/files/{token}` (Nextcloud 29+) | not needed | the public file URL | **easy, worth it** |
| **Dropbox** | yes, `list_folder` with `shared_link` and our own app token (one call per subfolder) | scoped app; 500 accounts before review | `get_temporary_link`, 4 h | **public links, then OAuth** |
| **OneDrive personal** | broke in 2025 (undocumented token only) | Graph `Files.Read` + `offline_access` | `@microsoft.graph.downloadUrl`, Range, minutes: made on play | **OAuth** |
| **OneDrive Business / SharePoint** | no, always authenticated | same Graph code; admin consent for site libraries | same | **OAuth, with personal** |
| **Box** | yes, our token with a `boxapi: shared_link=` header | possible | `/content` redirects to a ~15 min URL | if asked |
| **kDrive (Infomaniak)** | not documented | OAuth and a listing API | to test | if asked |
| **MEGA** | yes | — | files are encrypted: decrypt in a Service Worker | last |
| **Synology / QNAP** | not documented | WebDAV with a password: a proxy | — | no |
| **iCloud Drive, Proton Drive** | no public API | — | — | not possible |

## Order

1. **Google Drive public folder links**: an API key restricted to our site.
   The most used drive, and no OAuth review.
2. **Nextcloud / ownCloud public shares**: one WebDAV request, plain URLs:
   schools, associations and self-hosting musicians use them.
3. **Dropbox shared links** with our own app token; **Dropbox OAuth** after,
   for private folders, with its change cursor (cheap re-reads).
4. **Microsoft Graph OAuth**: personal and Business in one code path, its
   delta cursor, the File Picker to choose the folder.
5. Box and kDrive when a band asks; MEGA last. iCloud and Proton: say no.

## How it fits

A provider does four things, the rest of the code doesn't change (the
templates, the matching, the player):

```go
type Provider interface {
	ParseLink(link string) (Source, error)            // what the band pastes
	List(ctx, src, cursor) (tree, nextCursor, error)  // the folder, recursively
	Changed(ctx, src, cursor) (bool, error)           // cheap re-read check
	StreamLink(ctx, src, file) (Link, error)          // how the browser plays it
}

// Link.Kind: "direct" (a public URL), "browser" (the browser asks the
// drive, as pCloud), "minted" (the server makes a short-lived URL on play),
// "decrypt" (MEGA, a Service Worker); Link.URL, Link.Expires.
```

The player asks for a fresh link when one expires or the `<audio>` errors,
and seeks back to where it was. We keep the folder tree, the drive's change
cursor and, with OAuth, an encrypted refresh token, never the audio.
`band_recording_sources.provider` gains `gdrive`, `nextcloud`, `dropbox`,
`graph`.

## To check before building

- Each drive's download URL: Range support, content type (one report of
  Dropbox answering Range requests with `application/grpc`), and playback in
  the iPhone Home Screen app.
- Google: whether `drive.file` sees a folder picked with the Picker.
- Graph: how long `downloadUrl` lasts in practice.
