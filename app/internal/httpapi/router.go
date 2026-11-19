package httpapi

import (
	"context"
	"net/http"

	"app/internal/brand"
	"app/internal/lilypond"
	"app/internal/mailer"
	"app/internal/pdfnative"
	"app/internal/store"
	"app/internal/webassets"
)

type Server struct {
	store *store.Store
	// pdf is pdfnative's pure-Go, in-process renderer — no `typst`/
	// `lilypond` binary, no /v2 mount, never nil.
	pdf    *pdfnative.Renderer
	ly     *lilypond.Bridge // the LilyPond export pipeline — needs no external template tree, so always non-nil; a missing `lilypond` binary only surfaces as a request-time 500
	mailer mailer.Mailer

	// devAutoLogin, when non-nil, makes requireAuth (and everything built
	// on it: requireRole/requireAdmin/requireBandMember/.../optionalPerson)
	// treat EVERY request as already authenticated as this person, with no
	// session cookie needed at all. Wired from --dev-auto-login /
	// SONGBOOKS_DEV_AUTO_LOGIN (see cmd/songbooks/main.go), which defaults
	// to unset/off and must be deliberately set — this field is nil, and
	// the normal cookie-checking path in requireAuth runs completely
	// unchanged, unless that flag was explicitly given. NEVER set on the
	// production deployment: it disables authentication for anyone who can
	// reach the server at all. See main.go's loud startup banner.
	devAutoLogin *store.Person

	// webauthnRPID is the configured WebAuthn Relying Party ID
	// (--webauthn-rp-id / SONGBOOKS_WEBAUTHN_RP_ID — see main.go). Empty
	// means "derive it from each request's own Host header instead" — see
	// webauthn.go's webauthnForRequest for what that means and why
	// production must set this explicitly rather than rely on the default.
	webauthnRPID string
	// webauthn holds every in-flight (begun, not yet finished) passkey
	// registration/login ceremony's challenge state — see webauthn.go's
	// webauthnState doc comment.
	webauthn *webauthnState

	// adminFileDir, when non-empty, serves files from this directory at
	// GET /admin/files/{name} (requireAdmin) — a small, deliberately
	// narrow staging area for moving a large local file (a bulk import
	// source, say) onto the SAME origin a browser session is already
	// authenticated against, without routing it through a third-party
	// host or a cross-origin fetch (blocked either by mixed content, if
	// the source is plain HTTP, or by CORS, since this app's own API
	// isn't configured for cross-origin browser access). Empty (the
	// default) disables the route entirely — see --admin-file-dir in
	// cmd/songbooks/main.go.
	adminFileDir string
}

// NewServer wires the HTTP handler. m is the mailer magic-link login uses
// to "send" its emails — see internal/mailer and cmd/songbooks/main.go,
// which constructs a mailer.ConsoleMailer by default. devAutoLogin is nil
// in every normal (including production) deployment — see the
// Server.devAutoLogin doc comment above for what setting it does and why
// that must never happen in production. webauthnRPID is the configured
// passkey Relying Party ID (empty = derive per-request from Host, dev-only
// default — see webauthn.go's webauthnForRequest and main.go's
// --webauthn-rp-id flag).
func NewServer(st *store.Store, m mailer.Mailer, devAutoLogin *store.Person, webauthnRPID, adminFileDir string) http.Handler {
	s := &Server{store: st, mailer: m, pdf: pdfnative.New(), ly: lilypond.NewBridge(), devAutoLogin: devAutoLogin, webauthnRPID: webauthnRPID, webauthn: newWebAuthnState(), adminFileDir: adminFileDir}
	webassets.ShelfLookup = func(kind string, id int64) (int, int) {
		b, c, err := st.ShelfCounts(context.Background(), kind, id)
		if err != nil {
			return 0, 0
		}
		return b, c
	}
	webassets.BrandLookup = func(kind string, id int64) *brand.Theme {
		b, name, err := st.GetBrand(context.Background(), kind, id)
		if err != nil {
			return nil
		}
		return brand.Resolve(b, name)
	}
	mux := http.NewServeMux()

	// Auth itself: reachable without a session (that's the point).
	// Magic-link login (no password): GET /login asks for an email,
	// POST /login "sends" the link, GET /login/confirm validates it and
	// starts the session. See internal/httpapi/auth.go.
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("GET /login/confirm", s.handleLoginConfirm)
	mux.HandleFunc("POST /logout", s.handleLogout)
	// Passkey (WebAuthn) login, the third first-class /login option (see
	// webauthn.go) — unauthenticated, same as the routes above: this is
	// the "log in" side, not the "add a passkey to my account" side
	// (that's requireAuth-gated, below with the rest of /profile).
	mux.HandleFunc("POST /api/webauthn/login/begin", s.handleWebAuthnLoginBegin)
	mux.HandleFunc("POST /api/webauthn/login/finish", s.handleWebAuthnLoginFinish)
	mux.HandleFunc("GET /invite/{token}", s.handleInvitePage)
	mux.HandleFunc("POST /invite/{token}", s.handlePostInvite)

	// Group invites: a band/community-wide reusable roster link (see
	// internal/store/group_invites.go) — distinct from the single-use
	// /invite/{token} above. Public, no auth required to view (same
	// "must work from a bare WhatsApp link" requirement); see join.go.
	mux.HandleFunc("GET /join/{token}", s.handleJoinPage)
	mux.HandleFunc("POST /join/{token}", s.handlePostJoin)
	mux.HandleFunc("GET /join/{token}/login", s.handleJoinLoginRedirect)

	// Dev-only: a local stand-in for an inbox, listing every email the
	// console mailer has "sent" (see internal/mailer.ConsoleMailer) with
	// clickable links. Admin-gated, not publicly reachable — it contains
	// live login links.
	mux.HandleFunc("GET /dev/outbox", s.requireAdmin(s.handleDevOutbox))

	// GET / is always the public marketing homepage, signed in or not —
	// optionalPerson only so its nav can show "My Songbooks" instead of
	// "Sign in" for a visitor who's already authenticated. The "my
	// songbooks" dashboard (the old handleIndex, previously mounted at
	// / itself) lives at /home instead, so root is never contested
	// between "the pitch" and "your stuff." The rest of the marketing
	// site is public the same way, for the same reason.
	mux.HandleFunc("GET /", s.optionalPerson(s.handleMarketingHome))
	mux.HandleFunc("GET /editor", s.optionalPerson(s.handleMarketingEditor))
	mux.HandleFunc("GET /guitar-pro", s.optionalPerson(s.handleMarketingGuitarPro))
	mux.HandleFunc("GET /docs/song-format", s.optionalPerson(s.handleDocsSongFormat))
	mux.HandleFunc("GET /docs/song-format/example", s.handleDocsSongFormatExample)
	mux.HandleFunc("GET /docs/song-format/example/{key}", s.handleDocsScoreExample)
	mux.HandleFunc("GET /for-educators", s.optionalPerson(s.handleMarketingEducators))
	mux.HandleFunc("GET /for/bands", s.optionalPerson(s.publicPageHandler("public_bands")))
	mux.HandleFunc("GET /for/teachers", s.optionalPerson(s.publicPageHandler("public_teachers")))
	mux.HandleFunc("GET /for/vocal-coaches", s.optionalPerson(s.publicPageHandler("public_vocal")))
	mux.HandleFunc("GET /for/communities", s.optionalPerson(s.publicPageHandler("public_communities")))
	mux.HandleFunc("GET /tour", s.optionalPerson(s.publicPageHandler("public_tour")))
	mux.HandleFunc("GET /home", s.requireAuth(s.handleIndex))

	// Songbooks: a named, ordered collection of variant pointers owned by
	// a person or a band (see internal/store/songbooks.go) — the "project"
	// container's replacement. /songbooks/mine lists the logged-in
	// person's own; /songbooks/{id} is any one songbook's page.
	mux.HandleFunc("GET /songbooks/mine", s.requireAuth(s.handleMySongbooksPage))
	mux.HandleFunc("POST /api/songbooks", s.requireAuth(s.handleCreateSongbook))
	mux.HandleFunc("GET /songbooks/{id}", s.requireAuth(s.handleSongbookPage))
	// Courses: a songbook whose songs are in lessons (courses.go).
	mux.HandleFunc("GET /songbooks/{id}/lessons/{lessonID}", s.requireAuth(s.handleLessonPage))
	mux.HandleFunc("POST /api/songbooks/{id}/kind", s.requireAuth(s.handleSetSongbookKind))
	mux.HandleFunc("POST /api/songbooks/{id}/lessons", s.requireAuth(s.handleCreateLesson))
	mux.HandleFunc("POST /api/songbooks/{id}/lessons/from-files", s.requireAuth(s.handleLessonsFromFiles))
	mux.HandleFunc("PUT /api/songbooks/{id}/lessons/{lessonID}", s.requireAuth(s.handleUpdateLesson))
	mux.HandleFunc("DELETE /api/songbooks/{id}/lessons/{lessonID}", s.requireAuth(s.handleDeleteLesson))
	mux.HandleFunc("PUT /api/songbooks/{id}/lessons/{lessonID}/done", s.requireAuth(s.handleSetLessonDone))
	mux.HandleFunc("POST /api/songbooks/{id}/lessons/{lessonID}/songs/{variantID}/move", s.requireAuth(s.handleMoveLessonSong))
	mux.HandleFunc("PUT /api/songbooks/{id}/entries/{variantID}/lesson", s.requireAuth(s.handleSetSongLesson))
	mux.HandleFunc("PUT /api/songbooks/{id}/files/{fileID}/lesson", s.requireAuth(s.handleSetFileLesson))
	mux.HandleFunc("POST /api/songbooks/{id}/rename", s.requireAuth(s.handleRenameSongbook))
	mux.HandleFunc("DELETE /api/songbooks/{id}", s.requireAuth(s.handleDeleteSongbook))
	mux.HandleFunc("PUT /api/songbooks/{id}/order", s.requireAuth(s.handleSaveSongbookOrder))
	mux.HandleFunc("POST /api/songbooks/{id}/songs", s.requireAuth(s.handleCreateSongInSongbook))
	mux.HandleFunc("POST /api/catalog/songs", s.requireAuth(s.handleCreateCatalogSong))
	mux.HandleFunc("POST /api/songbooks/{id}/entries", s.requireAuth(s.handleAddSongbookEntry))
	mux.HandleFunc("DELETE /api/songbooks/{id}/entries/{variantID}", s.requireAuth(s.handleRemoveSongbookEntry))
	mux.HandleFunc("GET /songbooks/{id}/playlist/metronome", s.requireAuth(s.handleMetronomePlaylist))
	mux.HandleFunc("GET /songbooks/{id}/playlist", s.requireAuth(s.handlePlaylistPage))
	mux.HandleFunc("GET /songbooks/{id}/playlist/mp3", s.requireAuth(s.handleMP3Playlist))

	// Songs: addressed by their own variant id from here on — see
	// migration 0041's "remove the project notion" doc comment.
	mux.HandleFunc("GET /song/{id}", s.requireVariantRole("viewer", s.handleSong))
	mux.HandleFunc("GET /api/qr", s.requireAuth(s.handleQRCode))
	mux.HandleFunc("POST /api/preview", s.requireAuth(s.handlePreview))
	mux.HandleFunc("POST /api/transpose", s.requireAuth(s.handleTranspose))
	mux.HandleFunc("POST /api/import/lyrics", s.requireAuth(s.handleImportLyrics))
	mux.HandleFunc("GET /api/import/lyrics/find", s.requireAuth(s.handleImportLyricsFindFetch))
	mux.HandleFunc("POST /api/import/chords", s.requireAuth(s.handleImportChords))
	mux.HandleFunc("GET /api/import/ug/find", s.requireAuth(s.handleImportUGFind))
	mux.HandleFunc("GET /api/import/gp/find", s.requireAuth(s.handleImportGPFind))
	mux.HandleFunc("GET /api/import/gp/fetch", s.requireAuth(s.handleImportGPFetch))
	mux.HandleFunc("GET /api/import/ireal/find", s.requireAuth(s.handleImportIRealFind))
	mux.HandleFunc("POST /api/import/ireal/merge", s.requireAuth(s.handleImportIRealMerge))
	mux.HandleFunc("POST /api/import/ultimate-guitar/parse", s.requireAuth(s.handleImportUGParse))
	mux.HandleFunc("POST /api/import/ultimate-guitar/convert", s.requireAuth(s.handleImportUGConvert))
	// The import wizard's reference recording, cleanup, lyrics, singer's
	// draft, key check and draft merge (handlers_wizard.go,
	// internal/songimport, internal/importer).
	mux.HandleFunc("GET /api/import/youtube/find", s.requireAuth(s.handleImportYouTubeFind))
	mux.HandleFunc("POST /api/import/cleanup", s.requireAuth(s.handleImportCleanup))
	mux.HandleFunc("POST /api/import/lyrics/place", s.requireAuth(s.handleImportLyricsPlace))
	mux.HandleFunc("GET /api/import/vocal", s.requireAuth(s.handleImportVocal))
	mux.HandleFunc("POST /api/import/keycheck", s.requireAuth(s.handlePostKeyCheck))
	mux.HandleFunc("POST /api/import/merge", s.requireAuth(s.handleImportMerge))

	mux.HandleFunc("GET /api/songs/{id}", s.requireVariantRole("viewer", s.handleGetSong))
	mux.HandleFunc("PUT /api/songs/{id}", s.requireVariantRole("editor", s.handlePutSong))
	mux.HandleFunc("PUT /api/songs/{id}/meta", s.requireVariantRole("editor", s.handlePutMeta))
	mux.HandleFunc("PUT /api/variants/{id}/genre", s.requireAuth(s.handlePutVariantGenre))
	mux.HandleFunc("GET /api/songs/{id}/raw", s.requireVariantRole("viewer", s.handleGetRaw))
	mux.HandleFunc("GET /api/songs/{id}/structure", s.requireVariantRole("viewer", s.handleGetSongStructure))
	mux.HandleFunc("GET /api/songs/{id}/drummer-rows", s.requireVariantRole("viewer", s.handleGetDrummerRows))
	mux.HandleFunc("GET /api/songs/{id}/mixer", s.requireVariantRole("viewer", s.handleGetSongMixer))
	mux.HandleFunc("GET /api/songs/{id}/mixer/prefs", s.requireVariantRole("viewer", s.handleGetMixerPrefs))
	mux.HandleFunc("PUT /api/songs/{id}/mixer/prefs", s.requireVariantRole("viewer", s.handlePutMixerPrefs))
	mux.HandleFunc("PUT /api/songs/{id}/raw", s.requireVariantRole("editor", s.handlePutRaw))
	mux.HandleFunc("DELETE /api/songs/{id}", s.requireVariantRole("editor", s.handleDeleteSong))
	mux.HandleFunc("PUT /api/songs/{id}/redo", s.requireVariantRole("editor", s.handleRedoSong))

	// Reference audio (app/docs/mp3-reference-audio.md): metadata/serving
	// are viewer-level (same as reading the chart itself), upload/delete/
	// markers are editor-level (same as editing the chart itself).
	mux.HandleFunc("GET /api/songs/{id}/audio", s.requireVariantRole("viewer", s.handleGetSongAudioMeta))
	// category is a query param (?category=youtube|band), not a path
	// segment — a {category} segment with a literal ".mp3" suffix
	// attached is rejected outright by Go 1.22+ ServeMux ("bad wildcard
	// segment," must be the whole path segment).
	mux.HandleFunc("GET /song/{id}/audio.mp3", s.requireVariantRole("viewer", s.handleGetSongAudioData))
	mux.HandleFunc("PUT /api/songs/{id}/audio/{category}", s.requireVariantRole("editor", s.handlePutSongAudio))
	mux.HandleFunc("POST /api/songs/{id}/audio/youtube", s.requireVariantRole("editor", s.handlePostSongAudioYouTube))
	mux.HandleFunc("DELETE /api/songs/{id}/audio/{category}", s.requireVariantRole("editor", s.handleDeleteSongAudio))
	mux.HandleFunc("PUT /api/songs/{id}/audio/{category}/kind", s.requireVariantRole("editor", s.handlePutSongAudioKind))
	mux.HandleFunc("POST /api/songs/{id}/audio/{category}/stems", s.requireVariantRole("editor", s.handlePostSongAudioStems))
	mux.HandleFunc("GET /api/songs/{id}/import/assess", s.requireVariantRole("editor", s.handleGetImportAssess))
	mux.HandleFunc("POST /api/songs/{id}/audio/{category}/analyze", s.requireVariantRole("editor", s.handlePostSongAudioAnalyze))
	mux.HandleFunc("POST /api/songs/{id}/audio/{category}/sync", s.requireVariantRole("editor", s.handlePostSongAudioSync))
	mux.HandleFunc("PUT /api/songs/{id}/audio/{category}/markers/{sectionIndex}", s.requireVariantRole("editor", s.handlePutAudioMarker))
	mux.HandleFunc("DELETE /api/songs/{id}/audio/{category}/markers/{sectionIndex}", s.requireVariantRole("editor", s.handleDeleteAudioMarker))
	mux.HandleFunc("PUT /api/songs/{id}/audio/gp/sync-points", s.requireVariantRole("editor", s.handlePutGPBarSyncPoints))
	mux.HandleFunc("PUT /api/songs/{id}/gp-file", s.requireVariantRole("editor", s.handlePutSongGPFile))
	mux.HandleFunc("PUT /api/songs/{id}/gp-file/facts", s.requireVariantRole("viewer", s.handlePutSongGPFileFacts))
	mux.HandleFunc("PUT /api/songs/{id}/sections/{index}/gp", s.requireVariantRole("editor", s.handlePutSectionGP))
	mux.HandleFunc("DELETE /api/songs/{id}/sections/{index}/gp", s.requireVariantRole("editor", s.handleDeleteSectionGP))
	mux.HandleFunc("GET /api/songs/{id}/gp-drift", s.requireVariantRole("editor", s.handleGetGPDrift))
	mux.HandleFunc("POST /api/songs/{id}/gp-drift/keep", s.requireVariantRole("editor", s.handlePostGPDriftKeep))
	mux.HandleFunc("POST /api/songs/{id}/gp-drift/take", s.requireVariantRole("editor", s.handlePostGPDriftTake))
	mux.HandleFunc("GET /song/{id}/gp-file", s.requireVariantRole("viewer", s.handleGetSongGPFile))
	mux.HandleFunc("GET /song/{id}/gp-edit", s.requireVariantRole("editor", s.handleGPEditPage))
	mux.HandleFunc("GET /song/{id}/gp-file/versions/{vid}", s.requireVariantRole("viewer", s.handleGetSongGPFileVersion))

	// A section's "score[label]:" voices (internal/tab's LilyPond-
	// compatible subset) are plain text in the body itself, edited in
	// Source like any "grid:" or "note:" line (/api/songs/{id}/raw,
	// /api/preview). The GP import wizard writes a section's "gp[Track]:"
	// line server-side, where song.Parse/song.Write already are.
	mux.HandleFunc("POST /api/chord-diagrams/preview", s.requireAuth(s.handleChordDiagramPreview))

	mux.HandleFunc("GET /metronome", s.requireAuth(s.handleMetronomePage))
	mux.HandleFunc("GET /tuner", s.requireAuth(s.handleTunerPage))

	// In-app notifications: a lightweight inbox, not email/push — see
	// internal/store/notifications.go. requireAuth only; every store call
	// scopes by the logged-in person's own id.
	mux.HandleFunc("GET /notifications", s.requireAuth(s.handleNotificationsPage))
	mux.HandleFunc("GET /api/notifications/unread-count", s.requireAuth(s.handleUnreadNotificationCount))
	mux.HandleFunc("POST /notifications/{id}/read", s.requireAuth(s.handleMarkNotificationRead))
	mux.HandleFunc("POST /notifications/read-all", s.requireAuth(s.handleMarkAllNotificationsRead))

	// Profile (phase 6: instrument choice): a logged-in person's own
	// account settings — currently just "which instruments do you play",
	// used to pre-fill jam signup. See internal/httpapi/profile.go.
	mux.HandleFunc("GET /profile", s.requireAuth(s.handleProfilePage))
	mux.HandleFunc("POST /profile/instruments", s.requireAuth(s.handleUpdateInstruments))

	// Getting-started wizard (phase 36): re-enterable at any time from
	// /profile's "Setup" link, auto-offered once right after account
	// creation (see invite.go/join.go). See wizard.go.
	mux.HandleFunc("GET /welcome", s.requireAuth(s.handleWelcomePage))
	mux.HandleFunc("POST /welcome/finish", s.requireAuth(s.handlePostWelcomeFinish))
	// Set/change your own password — the "opt into password login later"
	// path for a magic-link-only account. See profile.go.
	mux.HandleFunc("POST /profile/password", s.requireAuth(s.handleSetPasswordProfile))
	mux.HandleFunc("POST /profile/email", s.requireAuth(s.handleSetEmailProfile))
	mux.HandleFunc("POST /profile/public", s.requireAuth(s.handleSetPublicProfile))
	// Passkeys: add one to an already-logged-in account (requireAuth —
	// account creation itself stays invite-only, see webauthn.go's package
	// doc comment), rename, or remove.
	mux.HandleFunc("POST /api/webauthn/register/begin", s.requireAuth(s.handleWebAuthnRegisterBegin))
	mux.HandleFunc("POST /api/webauthn/register/finish", s.requireAuth(s.handleWebAuthnRegisterFinish))
	mux.HandleFunc("POST /profile/passkeys/{id}/nickname", s.requireAuth(s.handleSetWebAuthnNickname))
	mux.HandleFunc("POST /profile/passkeys/{id}/remove", s.requireAuth(s.handleRemoveWebAuthnCredential))

	// Catalog (phase 1 of the data-model rewrite): works/variants browsing,
	// personal catalog, forking, annotations, and the generic
	// revision/undo mechanism. All auth-gated only (requireAuth), not
	// requireRole — a variant's visibility ('global' vs 'user') governs
	// what's visible/editable here, not project membership; see
	// internal/store/catalog.go and internal/store/revisions.go.
	mux.HandleFunc("GET /songbooks", s.requireAuth(s.handleSongbooksIndexPage))
	mux.HandleFunc("GET /catalog", s.requireAuth(s.handleCatalogPage))
	mux.HandleFunc("GET /api/catalog/search", s.requireAuth(s.handleCatalogSearch))
	mux.HandleFunc("GET /api/catalog/match", s.requireAuth(s.handleCatalogMatch))
	mux.HandleFunc("GET /works/{id}", s.requireAuth(s.handleWorkVersionsPage))
	mux.HandleFunc("GET /variants/{id}/history", s.requireAuth(s.handleVariantHistoryPage))

	mux.HandleFunc("POST /api/variants/{id}/fork", s.requireAuth(s.handleForkVariant))
	mux.HandleFunc("POST /api/variants/{id}/annotations", s.requireAuth(s.handleCreateAnnotation))
	mux.HandleFunc("PUT /api/variants/{id}", s.requireAuth(s.handleUpdateVariant))
	mux.HandleFunc("GET /api/revisions", s.requireAuth(s.handleListRevisions))
	mux.HandleFunc("POST /api/revisions/{id}/revert", s.requireAuth(s.handleRevertRevision))

	// Section annotations: lightweight per-section notes ("solo: sax").
	// requireAuth — jam-session (no-login) annotating was retired along
	// with the rest of the old jam system (migration 0051).
	// (any identified viewer, not gated by edit rights).
	mux.HandleFunc("POST /api/variants/{id}/sections/{index}/annotations", s.requireAuth(s.handleCreateSectionAnnotation))
	mux.HandleFunc("GET /api/variants/{id}/sections/{index}/annotations", s.requireAuth(s.handleListSectionAnnotations))
	mux.HandleFunc("GET /api/variants/{id}/annotations", s.requireAuth(s.handleListAllSectionAnnotations))

	// Bands (phase 2 of the data-model rewrite): collaborative groups that
	// own shared, band-visibility catalog material. No band owner/admin
	// role exists — every band-scoped route below (other than listing
	// your own bands and creating a new one) is gated by
	// requireBandMember: any CURRENT member (core or guest alike) may
	// add/remove/promote/demote members, add/fork/remove repertoire
	// entries, or delete the whole band. See internal/httpapi/bands.go.
	mux.HandleFunc("GET /groups", s.requireAuth(s.handleGroupsPage))
	mux.HandleFunc("GET /bands", s.requireAuth(s.handleBandsPage))
	mux.HandleFunc("POST /api/bands", s.requireAuth(s.handleCreateBand))
	mux.HandleFunc("GET /bands/{id}", s.requireBandMember(s.handleBandPage))
	mux.HandleFunc("DELETE /api/bands/{id}", s.requireBandMember(s.handleDeleteBand))
	mux.HandleFunc("POST /api/bands/{id}/name", s.requireBandMember(s.handleSetBandName))
	mux.HandleFunc("GET /api/bands/{id}/brand", s.requireBandMember(s.handleGetBrand("band")))
	mux.HandleFunc("POST /api/bands/{id}/brand", s.requireBandCore(s.handleSetBrand("band")))
	mux.HandleFunc("PUT /api/bands/{id}/locale", s.requireBandCore(s.handlePutBandLocale))
	mux.HandleFunc("GET /api/bands/{id}/brand/from-songs", s.requireBandMember(s.handleBrandFromSongs("band")))
	mux.HandleFunc("POST /api/bands/{id}/music-type", s.requireBandMember(s.handleSetBandMusicType))
	mux.HandleFunc("POST /api/bands/{id}/members", s.requireBandMember(s.handleAddBandMember))
	mux.HandleFunc("POST /api/bands/{id}/members/{personID}/status", s.requireBandCore(s.handleSetBandMemberStatus))
	mux.HandleFunc("DELETE /api/bands/{id}/members/{personID}", s.requireBandMember(s.handleRemoveBandMember))
	mux.HandleFunc("POST /api/bands/{id}/invites", s.requireBandMember(s.handleCreateBandInvite))
	// Group invites (roster link, multi-use — see
	// internal/store/group_invites.go and join.go): any current band
	// member may create or close one, same "no hierarchy" rule as every
	// other band-scoped mutation above.
	mux.HandleFunc("POST /api/bands/{id}/personal-invites", s.requireBandMember(s.handleCreateBandPersonalInvite))
	mux.HandleFunc("POST /api/people/{personID}/personal-invites", s.requireAuth(s.handleCreatePersonalInviteForPerson))
	mux.HandleFunc("POST /api/bands/{id}/group-invites", s.requireBandMember(s.handleCreateBandGroupInvite))
	mux.HandleFunc("POST /api/bands/{id}/group-invites/{groupInviteID}/close", s.requireBandMember(s.handleCloseBandGroupInvite))
	mux.HandleFunc("POST /api/bands/{id}/group-invites/{groupInviteID}/slots", s.requireBandMember(s.handleAddGroupInviteSlot))
	mux.HandleFunc("POST /api/bands/{id}/group-invites/{groupInviteID}/slots/{slotID}", s.requireBandMember(s.handleUpdateGroupInviteSlot))
	mux.HandleFunc("DELETE /api/bands/{id}/group-invites/{groupInviteID}/slots/{slotID}", s.requireBandMember(s.handleRemoveGroupInviteSlot))
	mux.HandleFunc("GET /bands/{id}/members", s.requireBandMember(s.handleBandMembersPage))
	mux.HandleFunc("GET /bands/{id}/settings", s.requireBandMember(s.handleBandSettingsPage))
	mux.HandleFunc("GET /bands/{id}/setlists", s.requireBandMember(s.handleBandSetlistsPage))
	mux.HandleFunc("GET /bands/{id}/catalog", s.requireBandMember(s.handleBandCatalogPage))
	mux.HandleFunc("POST /api/bands/{id}/catalog", s.requireBandMember(s.handleAddBandCatalogEntry))
	mux.HandleFunc("DELETE /api/bands/{id}/catalog/{variantID}", s.requireBandMember(s.handleRemoveBandCatalogEntry))
	mux.HandleFunc("POST /api/bands/{id}/catalog/fork/{variantID}", s.requireBandMember(s.handleForkVariantIntoBand))
	mux.HandleFunc("POST /api/bands/{id}/variants", s.requireBandMember(s.handleCreateBandVariant))

	// Band proposals + voting (phase 7): propose a new song (an existing
	// catalog variant, for the band's repertoire) or a new reference URL
	// for an existing band variant; any current member may vote
	// yes/veto or manually accept/reject. See
	// internal/store/band_proposals.go for the veto/threshold/
	// notification judgment calls. The listening-party page is a focused
	// UI over this same data (open 'new_song' proposals), not a separate
	// system.
	mux.HandleFunc("GET /bands/{id}/proposals", s.requireBandMember(s.handleBandProposalsPage))
	mux.HandleFunc("POST /api/bands/{id}/proposals", s.requireBandMember(s.handleCreateProposal))
	mux.HandleFunc("GET /bands/{id}/proposals/{proposalID}", s.requireBandMember(s.handleBandProposalDetailPage))
	mux.HandleFunc("POST /api/bands/{id}/proposals/{proposalID}/accept", s.requireBandMember(s.handleAcceptProposal))
	mux.HandleFunc("POST /api/bands/{id}/proposals/{proposalID}/reject", s.requireBandMember(s.handleRejectProposal))
	mux.HandleFunc("POST /api/bands/{id}/proposals/{proposalID}/votes", s.requireBandMember(s.handleCastVote))
	mux.HandleFunc("GET /bands/{id}/listening-party", s.requireBandMember(s.handleListeningPartyPage))

	// Community mode: a close structural cousin of bands (same history-
	// preserving membership shape, same catalog pointer mechanism), but
	// with real configurable roles (see internal/store/roles.go) instead
	// of bands' deliberately flat membership. requireCommunityMember
	// gates "may see this community's material" (any role, including
	// guest); requireCommunityPrivilege(priv) gates a specific
	// fine-grained action on the viewer's current role actually granting
	// that privilege. See internal/httpapi/communities.go.
	mux.HandleFunc("GET /communities", s.requireAuth(s.handleCommunitiesPage))
	mux.HandleFunc("POST /api/communities", s.requireAuth(s.handleCreateCommunity))
	mux.HandleFunc("GET /communities/{id}", s.requireCommunityMember(s.handleCommunityPage))
	mux.HandleFunc("GET /communities/{id}/songbooks/default", s.requireCommunityMember(s.handleCommunityDefaultSongbookPage))
	// Old "Repertoire" tab URL — kept as a redirect for existing
	// bookmarks/links now that it's reached only via the Songbooks list.
	mux.HandleFunc("GET /communities/{id}/repertoire", s.requireCommunityMember(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/communities/"+r.PathValue("id")+"/songbooks/default", http.StatusMovedPermanently)
	}))
	mux.HandleFunc("GET /communities/{id}/songbooks", s.requireCommunityMember(s.handleCommunitySongbooksPage))
	// Per-songbook access grants (migration 0061, the paywall primitive) —
	// staff-only, same gate as the admin page.
	mux.HandleFunc("GET /communities/{id}/songbooks/{sbID}/access", s.requireCommunityAnyManagePrivilege(s.handleSongbookAccessPage))
	mux.HandleFunc("POST /api/communities/{id}/songbooks/{sbID}/requires-grant", s.requireCommunityAnyManagePrivilege(s.handleSetSongbookRequiresGrant))
	mux.HandleFunc("POST /api/communities/{id}/songbooks/{sbID}/shared", s.requireCommunityAnyManagePrivilege(s.handleSetSongbookShared))
	mux.HandleFunc("POST /api/communities/{id}/songbooks/{sbID}/grants", s.requireCommunityAnyManagePrivilege(s.handleGrantSongbookAccess))
	mux.HandleFunc("DELETE /api/communities/{id}/songbooks/{sbID}/grants/{personID}", s.requireCommunityAnyManagePrivilege(s.handleRevokeSongbookAccess))
	// The download area (downloads.go): one listing of the files of every
	// songbook the viewer has access to; staff add and remove files.
	mux.HandleFunc("GET /communities/{id}/downloads", s.requireCommunityMember(s.handleCommunityDownloadsPage))
	mux.HandleFunc("GET /communities/{id}/downloads/{fileID}/{name}", s.requireCommunityMember(s.handleDownloadSongbookFile))
	mux.HandleFunc("GET /communities/{id}/downloads/songbooks/{sbID}", s.requireCommunityMember(s.handleCommunityDownloadsPage))
	mux.HandleFunc("GET /communities/{id}/downloads/songbooks/{sbID}/zip/{name}", s.requireCommunityMember(s.handleDownloadSongbookZip))
	mux.HandleFunc("GET /communities/{id}/downloads/songbooks/{sbID}/browse/{path...}", s.requireCommunityMember(s.handleBrowseSongbookFiles))
	mux.HandleFunc("POST /api/communities/{id}/songbooks/{sbID}/files", s.requireCommunityPrivilege(store.PrivManageFiles, s.handleUploadSongbookFiles))
	mux.HandleFunc("DELETE /api/communities/{id}/songbooks/{sbID}/files", s.requireCommunityPrivilege(store.PrivManageFiles, s.handleDeleteSongbookFolder))
	mux.HandleFunc("PATCH /api/communities/{id}/downloads/{fileID}", s.requireCommunityPrivilege(store.PrivManageFiles, s.handleSetSongbookFileSong))
	mux.HandleFunc("DELETE /api/communities/{id}/downloads/{fileID}", s.requireCommunityPrivilege(store.PrivManageFiles, s.handleDeleteSongbookFile))
	mux.HandleFunc("GET /communities/{id}/admin/stats", s.requireCommunityAnyManagePrivilege(s.handleCommunityStatsPage))
	mux.HandleFunc("GET /communities/{id}/admin/stats/members/{personID}", s.requireCommunityAnyManagePrivilege(s.handleCommunityMemberStatsPage))
	mux.HandleFunc("GET /communities/{id}/admin", s.requireCommunityAnyManagePrivilege(s.handleCommunityAdminPage))
	// A coaching community (coaching.go, docs/coaching.md).
	mux.HandleFunc("POST /api/communities/{id}/kind", s.requireCommunityPrivilege(store.PrivManageSettings, s.handleSetCommunityKind))
	mux.HandleFunc("GET /communities/{id}/students", s.requireCommunityMember(s.handleCoachingStudentsPage))
	mux.HandleFunc("GET /communities/{id}/students/{personID}", s.requireCommunityMember(s.handleCoachingStudentPage))
	mux.HandleFunc("PUT /api/communities/{id}/students/{personID}/ranges/{register}", s.requireCommunityMember(s.handlePutStudentRange))
	mux.HandleFunc("PUT /api/communities/{id}/students/{personID}/notes", s.requireCommunityMember(s.handlePutStudentNotes))
	mux.HandleFunc("POST /api/communities/{id}/songbooks/{sbID}/songs", s.requireCommunityMember(s.handlePostCoachingSong))
	mux.HandleFunc("PUT /api/communities/{id}/songbooks/{sbID}/songs/{variantID}/key", s.requireCommunityMember(s.handlePutCoachingSongKey))
	mux.HandleFunc("GET /api/songs/{id}/coach-keys", s.requireAuth(s.handleGetCoachKeys))
	mux.HandleFunc("POST /api/songs/{id}/coach-keys", s.requireAuth(s.handlePostCoachKey))
	mux.HandleFunc("DELETE /api/communities/{id}/songbooks/{sbID}/songs/{variantID}", s.requireCommunityMember(s.handleDeleteCoachingSong))
	mux.HandleFunc("POST /api/communities/{id}/students/{personID}/teaching-notes", s.requireCommunityMember(s.handlePostTeachingNote))
	mux.HandleFunc("PUT /api/communities/{id}/students/{personID}/teaching-notes/{noteID}", s.requireCommunityMember(s.handlePutTeachingNote))
	mux.HandleFunc("GET /communities/{id}/groups", s.requireCommunityMember(s.handleCoachingGroupsPage))
	mux.HandleFunc("GET /communities/{id}/groups/{groupID}", s.requireCommunityMember(s.handleCoachingGroupPage))
	mux.HandleFunc("POST /api/communities/{id}/groups", s.requireCommunityMember(s.handlePostCoachingGroup))
	mux.HandleFunc("PUT /api/communities/{id}/groups/{groupID}", s.requireCommunityMember(s.handlePutCoachingGroup))
	mux.HandleFunc("PUT /api/communities/{id}/groups/{groupID}/members", s.requireCommunityMember(s.handlePutCoachingGroupMembers))
	mux.HandleFunc("DELETE /api/communities/{id}/groups/{groupID}", s.requireCommunityMember(s.handleDeleteCoachingGroup))
	mux.HandleFunc("GET /communities/{id}/lessons", s.requireCommunityMember(s.handleCoachingLessonsPage))
	// Practice (practice.go): the coach's assignments, the student's log,
	// and the song page's count of the time a song played.
	mux.HandleFunc("POST /api/communities/{id}/assignments", s.requireCommunityMember(s.handlePostAssignment))
	mux.HandleFunc("PUT /api/communities/{id}/assignments/{assignmentID}", s.requireCommunityMember(s.handlePutAssignment))
	mux.HandleFunc("DELETE /api/communities/{id}/assignments/{assignmentID}", s.requireCommunityMember(s.handleDeleteAssignment))
	mux.HandleFunc("POST /api/communities/{id}/practice", s.requireCommunityMember(s.handlePostPractice))
	mux.HandleFunc("DELETE /api/communities/{id}/practice/{logID}", s.requireCommunityMember(s.handleDeletePractice))
	mux.HandleFunc("POST /api/practice/played", s.requireAuth(s.handlePostPlayed))
	mux.HandleFunc("POST /api/practice/pitch", s.requireAuth(s.handlePostPitchTake))
	mux.HandleFunc("GET /api/practice/pitch", s.requireAuth(s.handleGetPitchProgress))
	// An online lesson's join link (live.go): the logged-in coach or
	// student, or one known by their calendar feed's address (?f=).
	mux.HandleFunc("GET /communities/{id}/lessons/{eventID}/join", s.optionalPerson(s.handleLessonJoin))
	mux.HandleFunc("PUT /api/me/live-headphones", s.requireAuth(s.handlePutLiveHeadphones))
	// The repertoire: every song the students and groups have, in their
	// keys; an event's program, planned from it (coach_program.go).
	mux.HandleFunc("GET /communities/{id}/student-songs", s.requireCommunityMember(s.handleCoachRepertoirePage))
	mux.HandleFunc("POST /api/communities/{id}/program-plan", s.requireCommunityMember(s.handlePostProgramPlan))
	mux.HandleFunc("GET /communities/{id}/events/{eventID}/program", s.requireCommunityMember(s.handleEventProgramPage))
	mux.HandleFunc("POST /api/communities/{id}/events/{eventID}/program", s.requireCommunityMember(s.handlePostProgramSongs))
	mux.HandleFunc("PUT /api/communities/{id}/events/{eventID}/program/{programID}/key", s.requireCommunityMember(s.handlePutProgramKey))
	mux.HandleFunc("PUT /api/communities/{id}/events/{eventID}/program/{programID}/lead", s.requireCommunityMember(s.handlePutProgramLead))
	mux.HandleFunc("PUT /api/communities/{id}/events/{eventID}/program/{programID}/backing", s.requireCommunityMember(s.handlePutProgramBacking))
	mux.HandleFunc("POST /api/communities/{id}/events/{eventID}/program/{programID}/move", s.requireCommunityMember(s.handlePostProgramMove))
	mux.HandleFunc("DELETE /api/communities/{id}/events/{eventID}/program/{programID}", s.requireCommunityMember(s.handleDeleteProgramSong))
	mux.HandleFunc("POST /api/communities/{id}/lessons", s.requireCommunityMember(s.handlePostCoachingLesson))
	mux.HandleFunc("PUT /api/communities/{id}/lessons/{eventID}/students", s.requireCommunityMember(s.handlePutCoachingLessonStudents))
	mux.HandleFunc("PUT /api/communities/{id}/lessons/{eventID}/mode", s.requireCommunityMember(s.handlePutCoachingLessonMode))
	mux.HandleFunc("DELETE /api/communities/{id}/lessons/{eventID}", s.requireCommunityMember(s.handleDeleteCoachingLesson))
	mux.HandleFunc("POST /api/communities/{id}/rename", s.requireCommunityPrivilege(store.PrivManageSettings, s.handleRenameCommunity))
	mux.HandleFunc("GET /api/communities/{id}/brand", s.requireCommunityPrivilege(store.PrivManageSettings, s.handleGetBrand("community")))
	mux.HandleFunc("POST /api/communities/{id}/brand", s.requireCommunityPrivilege(store.PrivManageSettings, s.handleSetBrand("community")))
	mux.HandleFunc("GET /api/communities/{id}/brand/from-songs", s.requireCommunityPrivilege(store.PrivManageSettings, s.handleBrandFromSongs("community")))
	mux.HandleFunc("POST /api/communities/{id}/default-join-role", s.requireCommunityPrivilege(store.PrivManageSettings, s.handleSetCommunityDefaultJoinRole))
	mux.HandleFunc("POST /api/communities/{id}/members", s.requireCommunityPrivilege(store.PrivManageMembers, s.handleAddCommunityMember))
	mux.HandleFunc("POST /api/communities/{id}/personal-invites", s.requireCommunityPrivilege(store.PrivManageMembers, s.handleCreateCommunityPersonalInvite))
	// Group invites (roster link, multi-use): manage_members to create/close.
	mux.HandleFunc("POST /api/communities/{id}/group-invites", s.requireCommunityPrivilege(store.PrivManageMembers, s.handleCreateCommunityGroupInvite))
	mux.HandleFunc("POST /api/communities/{id}/community-link", s.requireCommunityPrivilege(store.PrivManageMembers, s.handleCreateCommunityLink))
	mux.HandleFunc("POST /api/communities/{id}/group-invites/{groupInviteID}/close", s.requireCommunityPrivilege(store.PrivManageMembers, s.handleCloseCommunityGroupInvite))
	mux.HandleFunc("POST /api/communities/{id}/members/{personID}/role", s.requireCommunityPrivilege(store.PrivManageMembers, s.handleSetCommunityMemberRole))
	mux.HandleFunc("DELETE /api/communities/{id}/members/{personID}", s.requireCommunityPrivilege(store.PrivManageMembers, s.handleRemoveCommunityMember))
	// Role management itself (create/edit/delete a role): also manage_members.
	mux.HandleFunc("GET /api/communities/{id}/roles", s.requireCommunityMember(s.handleListCommunityRoles))
	mux.HandleFunc("POST /api/communities/{id}/roles", s.requireCommunityPrivilege(store.PrivManageMembers, s.handleCreateCommunityRole))
	mux.HandleFunc("POST /api/communities/{id}/roles/{roleID}/privileges", s.requireCommunityPrivilege(store.PrivManageMembers, s.handleSetCommunityRolePrivileges))
	mux.HandleFunc("POST /api/communities/{id}/roles/{roleID}/rename", s.requireCommunityPrivilege(store.PrivManageMembers, s.handleRenameCommunityRole))
	mux.HandleFunc("DELETE /api/communities/{id}/roles/{roleID}", s.requireCommunityPrivilege(store.PrivManageMembers, s.handleDeleteCommunityRole))
	// edit_repertoire, not just member-level — a guest (read-only, the
	// role a jam-event invite/QR code targets) can see the repertoire
	// (requireCommunityMember on the page itself) but not change it.
	mux.HandleFunc("POST /api/communities/{id}/catalog", s.requireCommunityPrivilege(store.PrivEditRepertoire, s.handleAddCommunityCatalogEntry))
	mux.HandleFunc("DELETE /api/communities/{id}/catalog/{variantID}", s.requireCommunityPrivilege(store.PrivEditRepertoire, s.handleRemoveCommunityCatalogEntry))
	mux.HandleFunc("POST /api/communities/{id}/catalog/fork/{variantID}", s.requireCommunityPrivilege(store.PrivEditRepertoire, s.handleForkVariantIntoCommunity))
	mux.HandleFunc("POST /api/communities/{id}/variants", s.requireCommunityPrivilege(store.PrivEditRepertoire, s.handleCreateCommunityVariant))
	mux.HandleFunc("POST /api/communities/{id}/submissions", s.requireCommunityMember(s.handleCreateCommunitySubmission))
	mux.HandleFunc("POST /api/communities/{id}/submissions/{submissionID}/accept", s.requireCommunityPrivilege(store.PrivManageSubmissions, s.handleAcceptCommunitySubmission))
	mux.HandleFunc("POST /api/communities/{id}/submissions/{submissionID}/reject", s.requireCommunityPrivilege(store.PrivManageSubmissions, s.handleRejectCommunitySubmission))

	// Events: see internal/httpapi/events.go's own doc comments.
	// Creation stays at the owner-staff level (requireCommunityStaff /
	// requireBandMember, both admin-bypassed); the public event/
	// community-events pages and registration are unauthenticated
	// routes that do their own per-event visibility check inline (a
	// band-owned event is never public). Manager ASSIGNMENT is a
	// community-only concept — a band is flat (no staff hierarchy), so
	// any current band member already manages any of that band's
	// events via CanManageEvent's own fallback; there's nothing for a
	// band-scoped manager-assignment route to add.
	mux.HandleFunc("GET /communities/{id}/events", s.optionalPerson(s.handleCommunityEventsPage))
	mux.HandleFunc("POST /api/communities/{id}/events", s.requireCommunityPrivilege(store.PrivManageEvents, s.handleCreateCommunityEvent))
	mux.HandleFunc("POST /api/communities/{id}/events/{eventID}/managers", s.requireCommunityPrivilege(store.PrivManageEvents, s.handleAssignEventManager))
	mux.HandleFunc("DELETE /api/communities/{id}/events/{eventID}/managers/{personID}", s.requireCommunityPrivilege(store.PrivManageEvents, s.handleRemoveEventManager))
	mux.HandleFunc("GET /bands/{id}/events", s.requireBandMember(s.handleBandEventsPage))
	// A band's recordings, from the folder it shares (band_recordings.go).
	mux.HandleFunc("GET /bands/{id}/rehearsals", s.requireBandMember(s.handleBandRehearsalsPage))
	mux.HandleFunc("GET /bands/{id}/concerts", s.requireBandMember(s.handleBandConcertsPage))
	mux.HandleFunc("GET /bands/{id}/recordings/songs/{variantID}", s.requireBandMember(s.handleBandSongRecordingsPage))
	mux.HandleFunc("GET /bands/{id}/recordings/fixes", s.requireBandMember(s.handleBandRecordingFixesPage))
	mux.HandleFunc("POST /api/bands/{id}/recordings/source", s.requireBandMember(s.handleSetBandRecordingSource))
	mux.HandleFunc("POST /api/bands/{id}/recordings/sync", s.requireBandMember(s.handleSyncBandRecordings))
	mux.HandleFunc("PUT /api/bands/{id}/recordings/{recID}/stars", s.requireBandMember(s.handleRateBandRecording))
	mux.HandleFunc("PUT /api/bands/{id}/songs/{variantID}/recording", s.requireBandMember(s.handleSetBandSongRecording))
	mux.HandleFunc("POST /api/bands/{id}/recordings/fixes", s.requireBandMember(s.handleSetBandRecordingFix))
	mux.HandleFunc("PUT /api/bands/{id}/recordings/templates", s.requireBandMember(s.handleSetNameTemplates))
	mux.HandleFunc("POST /api/bands/{id}/recordings/templates/try", s.requireBandMember(s.handleTryNameTemplate))
	mux.HandleFunc("DELETE /api/bands/{id}/recordings/fixes/{fixID}", s.requireBandMember(s.handleDeleteBandRecordingFix))
	mux.HandleFunc("POST /api/bands/{id}/events", s.requireBandMember(s.handleCreateBandEvent))
	mux.HandleFunc("GET /events/{id}", s.optionalPerson(s.handleEventPage))
	mux.HandleFunc("GET /events/{id}/ics", s.optionalPerson(s.handleEventICS))
	// A person's calendar as an iCal feed, at a secret address (calendar_feed.go).
	mux.HandleFunc("GET /calendar/feed/{token}", s.handleCalendarFeed)
	mux.HandleFunc("GET /calendar", s.requireAuth(s.handleCalendarPage))
	mux.HandleFunc("GET /calendar/subscribe", s.requireAuth(s.handleCalendarSubscribePage))
	mux.HandleFunc("POST /api/calendar/feed/reset", s.requireAuth(s.handleResetCalendarFeed))
	mux.HandleFunc("POST /events/{id}/register", s.optionalPerson(s.handleRegisterForEvent))
	mux.HandleFunc("POST /events/{id}/songs", s.requireAuth(s.handleRegisterSongForEvent))
	mux.HandleFunc("DELETE /events/{id}/songs/{regID}", s.requireAuth(s.handleRemoveSongRegistration))
	mux.HandleFunc("GET /events/{id}/manage", s.requireEventManager(s.handleEventManagePage))
	mux.HandleFunc("POST /events/{id}/setlist", s.requireEventManager(s.handleCreateEventSetlist))
	mux.HandleFunc("POST /events/{id}/songbook", s.requireEventManager(s.handleCreateEventSongbook))
	mux.HandleFunc("POST /events/{id}/songbook/attach", s.requireEventManager(s.handleAttachEventSongbook))

	// Site-wide admin panel — gated by requireAdmin (is_admin on the
	// person), not requireRole (per-project role): see auth.go.
	mux.HandleFunc("GET /admin", s.requireAdmin(s.handleAdminPage))
	mux.HandleFunc("POST /admin/invites/plain", s.requireAdmin(s.handleCreatePlainInvite))
	mux.HandleFunc("POST /admin/group-invites/{id}/close", s.requireAdmin(s.handleAdminCloseGroupInvite))
	mux.HandleFunc("POST /admin/people/{personID}/admin", s.requireAdmin(s.handleSetAdmin))
	mux.HandleFunc("GET /admin/files/{name}", s.requireAdmin(s.handleAdminFile))
	mux.HandleFunc("POST /admin/people/{personID}/remove", s.requireAdmin(s.handleAdminRemovePerson))
	// Per-person profile review: memberships across every band/community,
	// with role adjustment, all from one page — see handlers_admin.go's
	// handleAdminPersonPage doc comment.
	mux.HandleFunc("GET /admin/people/{personID}", s.requireAdmin(s.handleAdminPersonPage))
	mux.HandleFunc("POST /admin/people/{personID}/memberships", s.requireAdmin(s.handleAdminAddMembership))
	mux.HandleFunc("POST /admin/people/{personID}/memberships/{kind}/{targetID}/role", s.requireAdmin(s.handleAdminSetMembershipRole))
	mux.HandleFunc("POST /admin/people/{personID}/memberships/{kind}/{targetID}/remove", s.requireAdmin(s.handleAdminRemoveMembership))

	mux.HandleFunc("GET /setlists", s.requireAuth(s.handleSetlistsPage))

	// Person/band-scoped setlists, addressed by their own id (see
	// internal/store/setlists.go's SetlistMeta/CreateSetlist/
	// CanEditSetlistByID), plus setlist_gigs; /practice and /listening
	// only redirect to the running order now. All requireAuth only, not a role middleware — a setlist's
	// own scope (person/band) governs who may view/edit it, checked
	// inside each handler via CanViewSetlistByID/CanEditSetlistByID,
	// same division of responsibility CanEditVariant/requireBandMember
	// already use elsewhere.
	mux.HandleFunc("POST /api/setlists", s.requireAuth(s.handleCreateSetlist))
	mux.HandleFunc("GET /api/setlists/{id}", s.requireAuth(s.handleGetSetlistByID))
	mux.HandleFunc("PUT /api/setlists/{id}", s.requireAuth(s.handlePutSetlistByID))
	mux.HandleFunc("DELETE /api/setlists/{id}", s.requireAuth(s.handleDeleteSetlistByID))
	mux.HandleFunc("GET /api/setlists/{id}/gigs", s.requireAuth(s.handleListSetlistGigs))
	mux.HandleFunc("POST /api/setlists/{id}/gigs", s.requireAuth(s.handleAddSetlistGig))
	mux.HandleFunc("DELETE /api/setlists/{id}/gigs/{gigID}", s.requireAuth(s.handleRemoveSetlistGig))
	mux.HandleFunc("GET /setlists/{id}", s.requireAuth(s.handleSetlistOverview))
	mux.HandleFunc("GET /setlists/{id}/practice", s.handleSetlistOldStep)
	mux.HandleFunc("GET /setlists/{id}/listening", s.handleSetlistOldStep)
	mux.HandleFunc("GET /setlists/{id}/clicktrack", s.requireAuth(s.handleSetlistClicktrack))
	// A band's set lists live inside the band; /setlists/{id} redirects
	// there (atSetlistPath). {id} is the set list here, as above; who may
	// see it is the set list's own check, same as at /setlists/{id}.
	mux.HandleFunc("GET /bands/{bandID}/setlists/{id}", s.requireAuth(s.handleSetlistOverview))
	mux.HandleFunc("GET /bands/{bandID}/setlists/{id}/clicktrack", s.requireAuth(s.handleSetlistClicktrack))
	mux.HandleFunc("GET /api/me/click-track-prefs", s.requireAuth(s.handleGetClickTrackPrefs))
	mux.HandleFunc("PUT /api/me/click-track-prefs", s.requireAuth(s.handleSetClickTrackPrefs))
	mux.HandleFunc("GET /api/me/notation-prefs", s.requireAuth(s.handleGetNotationPrefs))
	mux.HandleFunc("PUT /api/me/notation-prefs", s.requireAuth(s.handleSetNotationPrefs))

	mux.HandleFunc("GET /song/{id}/export.pdf", s.requireVariantRole("viewer", s.handleExportSongPDF))
	mux.HandleFunc("GET /setlists/{id}/export.pdf", s.requireAuth(s.handleExportSetlistByIDPDF))
	mux.HandleFunc("GET /setlists/{id}/export-setlist-page.pdf", s.requireAuth(s.handleExportSetlistPageOnlyByIDPDF))
	// Whole-songbook export: same Typst pipeline as a single song/setlist
	// above, covering every entry a songbook has.
	mux.HandleFunc("GET /songbooks/{id}/export-songbook.pdf", s.requireAuth(s.handleExportSongbookPDF))
	mux.HandleFunc("GET /bands/{id}/export-songbook.pdf", s.requireBandMember(s.handleExportBandSongbookPDF))
	mux.HandleFunc("GET /communities/{id}/export-songbook.pdf", s.requireCommunityMember(s.handleExportCommunitySongbookPDF))

	// LilyPond export: a second, independent PDF pipeline (real engraved
	// chords-over-a-staff notation + lyrics, see internal/lilypond) next
	// to the Typst "chart" export above — same route scoping/role checks,
	// distinct paths so both stay independently reachable.
	mux.HandleFunc("GET /song/{id}/export-ly.pdf", s.requireVariantRole("viewer", s.handleExportSongLilyPDF))
	mux.HandleFunc("GET /bands/{id}/export-ly.pdf", s.requireBandMember(s.handleExportBandRepertoireLilyPDF))

	// Jam mode retired (migration 0051): the old nickname-only,
	// no-login jam_events/jam_profiles system predates the community
	// model and never overlapped with it. Jams now only exist as a
	// community event (event_type='jam', see migration 0049 and
	// handleCreateCommunityEvent) — real account registration, no
	// separate jam-specific routes needed here at all.

	// Every /static/ URL served by a template carries a content-hash
	// "?v=" query param (webassets.staticVersion), so the URL itself
	// changes whenever an asset's bytes do — safe to cache aggressively
	// and immutably keyed on that URL, rather than relying on the
	// browser's own (and previously entirely absent) revalidation.
	mux.Handle("GET /static/", cacheForever(http.StripPrefix("/static/", http.FileServerFS(webassets.StaticFS()))))

	// PWA: served from the root, not /static/, so the service worker's
	// default scope covers the whole app (see handlers_pwa.go). Left
	// unauthenticated — needed for the login page itself to install/render.
	mux.HandleFunc("GET /manifest.json", s.handleManifest)
	mux.HandleFunc("GET /sw.js", s.handleServiceWorker)
	mux.HandleFunc("GET /apple-touch-icon.png", s.handleAppIcon)

	// Languages (i18n.go, docs/i18n.md).
	mux.HandleFunc("GET /i18n/{file}", s.handleI18nJS)
	mux.HandleFunc("PUT /api/me/locale", s.requireAuth(s.handlePutMyLocale))
	mux.HandleFunc("PUT /api/communities/{id}/locale", s.requireCommunityPrivilege(store.PrivManageSettings, s.handlePutCommunityLocale))

	return withLang(mux)
}

// cacheForever sets a one-year, immutable Cache-Control on every response —
// safe only because every caller URL is busted with a content-hash query
// param (see webassets.staticVersion), so a stale cache can never serve
// content that doesn't match its own URL.
func cacheForever(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		h.ServeHTTP(w, r)
	})
}
