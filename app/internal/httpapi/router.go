package httpapi

import (
	"net/http"

	"app/internal/lilypond"
	"app/internal/mailer"
	"app/internal/pdf"
	"app/internal/pdfnative"
	"app/internal/store"
	"app/internal/webassets"
)

type Server struct {
	store *store.Store
	// pdf defaults to pdfnative.Renderer — pure Go, in-process, no `typst`
	// binary or /v2 mount required. Falls back to the Typst-shelling
	// pdf.Bridge only when v2Root is explicitly given (kept as a rollback
	// path, not the default — see NewServer). Never nil either way, so the
	// handlers_pdf.go "s.pdf == nil" 503 guards are now dead in practice,
	// left in place rather than ripped out in this same change.
	pdf    pdf.Exporter
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
}

// NewServer wires the HTTP handler. PDF export always uses pdfnative's
// pure-Go in-process renderer; v2Root, now otherwise unused, keeps ONE
// legacy meaning — a non-empty value switches PDF export back to the
// Typst-shelling pdf.Bridge instead, a rollback path for a deployment
// that still has the `typst` binary and a /v2 checkout and wants it,
// rather than the normal default. m is the mailer magic-link login uses
// to "send" its emails — see internal/mailer and cmd/songbooks/main.go,
// which constructs a mailer.ConsoleMailer by default. devAutoLogin is nil
// in every normal (including production) deployment — see the
// Server.devAutoLogin doc comment above for what setting it does and why
// that must never happen in production. webauthnRPID is the configured
// passkey Relying Party ID (empty = derive per-request from Host, dev-only
// default — see webauthn.go's webauthnForRequest and main.go's
// --webauthn-rp-id flag).
func NewServer(st *store.Store, v2Root string, m mailer.Mailer, devAutoLogin *store.Person, webauthnRPID string) http.Handler {
	s := &Server{store: st, mailer: m, ly: lilypond.NewBridge(), devAutoLogin: devAutoLogin, webauthnRPID: webauthnRPID, webauthn: newWebAuthnState()}
	if v2Root != "" {
		s.pdf = pdf.New(v2Root)
	} else {
		s.pdf = pdfnative.New()
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

	// Group invites: a band/school-wide reusable roster link (see
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

	mux.HandleFunc("GET /", s.requireAuth(s.handleIndex))
	mux.HandleFunc("GET /b/{book}", s.requireRole("viewer", s.handleProject))
	mux.HandleFunc("GET /b/{book}/{stem}", s.requireRole("viewer", s.handleSong))
	mux.HandleFunc("GET /b/{book}/{stem}/play", s.requireRole("viewer", s.handleSongRedirect)) // old links -> the unified page
	mux.HandleFunc("GET /b/{book}/{stem}/prep", s.requireRole("viewer", s.handleSongRedirect))
	mux.HandleFunc("POST /api/preview", s.requireAuth(s.handlePreview))
	mux.HandleFunc("POST /api/import/lyrics", s.requireAuth(s.handleImportLyrics))
	mux.HandleFunc("GET /api/import/lyrics/find", s.requireAuth(s.handleImportLyricsFindFetch))
	mux.HandleFunc("POST /api/import/chords", s.requireAuth(s.handleImportChords))

	mux.HandleFunc("GET /api/songs/{book}/{stem}", s.requireRole("viewer", s.handleGetSong))
	mux.HandleFunc("PUT /api/songs/{book}/{stem}", s.requireRole("editor", s.handlePutSong))
	mux.HandleFunc("PUT /api/songs/{book}/{stem}/meta", s.requireRole("editor", s.handlePutMeta))
	mux.HandleFunc("PUT /api/variants/{id}/genre", s.requireAuth(s.handlePutVariantGenre))
	mux.HandleFunc("GET /api/songs/{book}/{stem}/raw", s.requireRole("viewer", s.handleGetRaw))
	mux.HandleFunc("PUT /api/songs/{book}/{stem}/raw", s.requireRole("editor", s.handlePutRaw))
	mux.HandleFunc("PUT /api/songs/{book}/{stem}/redo", s.requireRole("editor", s.handleRedoSong))

	mux.HandleFunc("PUT /api/books/{book}/order", s.requireRole("editor", s.handleSaveOrder))
	mux.HandleFunc("POST /api/books", s.requireAuth(s.handleCreateBook))
	mux.HandleFunc("DELETE /api/books/{book}", s.requireRole("owner", s.handleDeleteBook))
	mux.HandleFunc("POST /api/books/{book}/songs", s.requireRole("editor", s.handleCreateSong))
	mux.HandleFunc("DELETE /api/books/{book}/songs/{stem}", s.requireRole("editor", s.handleDeleteSong))
	mux.HandleFunc("GET /api/books/{book}/export", s.requireRole("viewer", s.handleExportBook))
	mux.HandleFunc("POST /api/books/import", s.requireAuth(s.handleImportBook))

	mux.HandleFunc("GET /b/{book}/members", s.requireRole("owner", s.handleMembersPage))
	mux.HandleFunc("POST /b/{book}/invites", s.requireRole("owner", s.handleCreateInvite))
	mux.HandleFunc("POST /b/{book}/members/{personID}/role", s.requireRole("owner", s.handleSetMemberRole))
	mux.HandleFunc("DELETE /b/{book}/members/{personID}", s.requireRole("owner", s.handleRemoveMember))

	mux.HandleFunc("GET /metronome", s.requireAuth(s.handleMetronomePage))

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
	mux.HandleFunc("GET /catalog", s.requireAuth(s.handleCatalogPage))
	mux.HandleFunc("GET /catalog/mine", s.requireAuth(s.handleMyCatalogPage))
	mux.HandleFunc("GET /works/{id}", s.requireAuth(s.handleWorkVersionsPage))
	mux.HandleFunc("GET /variants/{id}/history", s.requireAuth(s.handleVariantHistoryPage))

	mux.HandleFunc("POST /api/catalog", s.requireAuth(s.handleAddToCatalog))
	mux.HandleFunc("DELETE /api/catalog/{variantID}", s.requireAuth(s.handleRemoveFromCatalog))
	mux.HandleFunc("POST /api/variants/{id}/fork", s.requireAuth(s.handleForkVariant))
	mux.HandleFunc("POST /api/variants/{id}/annotations", s.requireAuth(s.handleCreateAnnotation))
	// PUT /api/variants/{id}: optionalPerson, not requireAuth — accepts
	// either a normal login OR a valid jam session (see
	// handleUpdateVariant's doc comment; phase 4 of the data-model
	// rewrite, jam-scoped variants).
	mux.HandleFunc("PUT /api/variants/{id}", s.optionalPerson(s.handleUpdateVariant))
	mux.HandleFunc("GET /api/revisions", s.requireAuth(s.handleListRevisions))
	mux.HandleFunc("POST /api/revisions/{id}/revert", s.requireAuth(s.handleRevertRevision))

	// Section annotations (phase 4 of the data-model rewrite): lightweight
	// per-section notes ("solo: sax"), usable both inside a jam and by a
	// logged-in person outside one. optionalPerson, not requireAuth — see
	// canAnnotateVariant's doc comment for who may call these and why
	// (any identified viewer, not gated by edit rights).
	mux.HandleFunc("POST /api/variants/{id}/sections/{index}/annotations", s.optionalPerson(s.handleCreateSectionAnnotation))
	mux.HandleFunc("GET /api/variants/{id}/sections/{index}/annotations", s.optionalPerson(s.handleListSectionAnnotations))
	mux.HandleFunc("GET /api/variants/{id}/annotations", s.optionalPerson(s.handleListAllSectionAnnotations))

	// Bands (phase 2 of the data-model rewrite): collaborative groups that
	// own shared, band-visibility catalog material. No band owner/admin
	// role exists — every band-scoped route below (other than listing
	// your own bands and creating a new one) is gated by
	// requireBandMember: any CURRENT member (core or guest alike) may
	// add/remove/promote/demote members, add/fork/remove repertoire
	// entries, or delete the whole band. See internal/httpapi/bands.go.
	mux.HandleFunc("GET /bands", s.requireAuth(s.handleBandsPage))
	mux.HandleFunc("POST /api/bands", s.requireAuth(s.handleCreateBand))
	mux.HandleFunc("GET /bands/{id}", s.requireBandMember(s.handleBandPage))
	mux.HandleFunc("DELETE /api/bands/{id}", s.requireBandMember(s.handleDeleteBand))
	mux.HandleFunc("POST /api/bands/{id}/music-type", s.requireBandMember(s.handleSetBandMusicType))
	mux.HandleFunc("POST /api/bands/{id}/members", s.requireBandMember(s.handleAddBandMember))
	mux.HandleFunc("POST /api/bands/{id}/members/{personID}/status", s.requireBandMember(s.handleSetBandMemberStatus))
	mux.HandleFunc("DELETE /api/bands/{id}/members/{personID}", s.requireBandMember(s.handleRemoveBandMember))
	mux.HandleFunc("POST /api/bands/{id}/invites", s.requireBandMember(s.handleCreateBandInvite))
	// Group invites (roster link, multi-use — see
	// internal/store/group_invites.go and join.go): any current band
	// member may create or close one, same "no hierarchy" rule as every
	// other band-scoped mutation above.
	mux.HandleFunc("POST /api/bands/{id}/group-invites", s.requireBandMember(s.handleCreateBandGroupInvite))
	mux.HandleFunc("POST /api/bands/{id}/group-invites/{groupInviteID}/close", s.requireBandMember(s.handleCloseBandGroupInvite))
	mux.HandleFunc("GET /bands/{id}/members", s.requireBandMember(s.handleBandMembersPage))
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

	// School mode: a close structural cousin of bands (same history-
	// preserving membership shape, same catalog pointer mechanism), but
	// with a REAL two-tier role (instructor/student) instead of bands'
	// deliberately flat membership. requireSchoolMember gates "may see
	// this school's material" (either role); requireSchoolInstructor
	// gates every instructor-only action (membership/catalog management,
	// rename, submission review). See internal/httpapi/schools.go.
	mux.HandleFunc("GET /schools", s.requireAuth(s.handleSchoolsPage))
	mux.HandleFunc("POST /api/schools", s.requireAuth(s.handleCreateSchool))
	mux.HandleFunc("GET /schools/{id}", s.requireSchoolMember(s.handleSchoolPage))
	mux.HandleFunc("GET /schools/{id}/admin", s.requireSchoolInstructor(s.handleSchoolAdminPage))
	mux.HandleFunc("POST /api/schools/{id}/rename", s.requireSchoolInstructor(s.handleRenameSchool))
	mux.HandleFunc("POST /api/schools/{id}/members", s.requireSchoolInstructor(s.handleAddSchoolMember))
	// Group invites (roster link, multi-use): instructor-only to
	// create/close, since schools (unlike bands) have a real hierarchy.
	mux.HandleFunc("POST /api/schools/{id}/group-invites", s.requireSchoolInstructor(s.handleCreateSchoolGroupInvite))
	mux.HandleFunc("POST /api/schools/{id}/group-invites/{groupInviteID}/close", s.requireSchoolInstructor(s.handleCloseSchoolGroupInvite))
	mux.HandleFunc("POST /api/schools/{id}/members/{personID}/role", s.requireSchoolInstructor(s.handleSetSchoolMemberRole))
	mux.HandleFunc("DELETE /api/schools/{id}/members/{personID}", s.requireSchoolInstructor(s.handleRemoveSchoolMember))
	mux.HandleFunc("POST /api/schools/{id}/catalog", s.requireSchoolInstructor(s.handleAddSchoolCatalogEntry))
	mux.HandleFunc("DELETE /api/schools/{id}/catalog/{variantID}", s.requireSchoolInstructor(s.handleRemoveSchoolCatalogEntry))
	mux.HandleFunc("POST /api/schools/{id}/catalog/fork/{variantID}", s.requireSchoolInstructor(s.handleForkVariantIntoSchool))
	mux.HandleFunc("POST /api/schools/{id}/variants", s.requireSchoolInstructor(s.handleCreateSchoolVariant))
	mux.HandleFunc("POST /api/schools/{id}/submissions", s.requireSchoolMember(s.handleCreateSchoolSubmission))
	mux.HandleFunc("POST /api/schools/{id}/submissions/{submissionID}/accept", s.requireSchoolInstructor(s.handleAcceptSchoolSubmission))
	mux.HandleFunc("POST /api/schools/{id}/submissions/{submissionID}/reject", s.requireSchoolInstructor(s.handleRejectSchoolSubmission))

	// Site-wide admin panel — gated by requireAdmin (is_admin on the
	// person), not requireRole (per-project role): see auth.go.
	mux.HandleFunc("GET /admin", s.requireAdmin(s.handleAdminPage))
	mux.HandleFunc("POST /admin/invites/plain", s.requireAdmin(s.handleCreatePlainInvite))
	mux.HandleFunc("POST /admin/people/{personID}/admin", s.requireAdmin(s.handleSetAdmin))
	mux.HandleFunc("POST /admin/people/{personID}/remove", s.requireAdmin(s.handleAdminRemovePerson))

	mux.HandleFunc("GET /setlists", s.requireAuth(s.handleSetlistsPage))
	mux.HandleFunc("GET /setlists/{book}/{stem}/gig", s.requireRole("viewer", s.handleSetlistGigPage))
	mux.HandleFunc("GET /setlists/{book}/{stem}", s.requireRole("viewer", s.handleSetlistEditPage))
	mux.HandleFunc("GET /api/setlists/{book}/{stem}", s.requireRole("viewer", s.handleGetSetlist))
	mux.HandleFunc("PUT /api/setlists/{book}/{stem}", s.requireRole("editor", s.handlePutSetlist))

	// Phase 3 of the data-model rewrite: person/band-scoped setlists,
	// addressed by their own id rather than by book/stem (see
	// internal/store/setlists.go's SetlistMeta/CreateSetlist/
	// CanEditSetlistByID), plus setlist_gigs and practice/listening
	// mode. All requireAuth only, not requireRole — a setlist's own
	// scope (project/person/band) governs who may view/edit it, checked
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
	mux.HandleFunc("GET /setlists/{id}/practice", s.requireAuth(s.handleSetlistPractice))
	mux.HandleFunc("GET /setlists/{id}/listening", s.requireAuth(s.handleSetlistListening))
	mux.HandleFunc("GET /setlists/{id}/clicktrack", s.requireAuth(s.handleSetlistClicktrack))

	mux.HandleFunc("GET /b/{book}/{stem}/export.pdf", s.requireRole("viewer", s.handleExportSongPDF))
	mux.HandleFunc("GET /setlists/{book}/{stem}/export.pdf", s.requireRole("viewer", s.handleExportSetlistPDF))
	mux.HandleFunc("GET /setlists/{book}/{stem}/export-setlist-page.pdf", s.requireRole("viewer", s.handleExportSetlistPageOnlyPDF))
	// Phase 3 (id-addressed person/band/project-scoped) setlists: same
	// pair of exports, gated by CanViewSetlistByID inside the handler
	// itself (requireAuth only here, same as every other /setlists/{id}/...
	// route) rather than a role/band middleware, since the right check
	// genuinely differs by scope.
	mux.HandleFunc("GET /setlists/{id}/export.pdf", s.requireAuth(s.handleExportSetlistByIDPDF))
	mux.HandleFunc("GET /setlists/{id}/export-setlist-page.pdf", s.requireAuth(s.handleExportSetlistPageOnlyByIDPDF))
	// Whole-songbook export: a project's full song list, or a band's full
	// repertoire — same Typst pipeline as a single song/setlist above, new
	// in this phase (previously PDF export only covered one song or a
	// curated setlist, never "every song this project/band has").
	mux.HandleFunc("GET /b/{book}/export-songbook.pdf", s.requireRole("viewer", s.handleExportProjectSongbookPDF))
	mux.HandleFunc("GET /bands/{id}/export-songbook.pdf", s.requireBandMember(s.handleExportBandSongbookPDF))

	// LilyPond export: a second, independent PDF pipeline (real engraved
	// chords-over-a-staff notation + lyrics, see internal/lilypond) next
	// to the Typst "chart" export above — same route scoping/role checks,
	// distinct paths so both stay independently reachable. Band
	// repertoire export is new in this pipeline (the Typst bridge never
	// had one); requireBandMember matches every other band-scoped route.
	mux.HandleFunc("GET /b/{book}/{stem}/export-ly.pdf", s.requireRole("viewer", s.handleExportSongLilyPDF))
	mux.HandleFunc("GET /setlists/{book}/{stem}/export-ly.pdf", s.requireRole("viewer", s.handleExportSetlistLilyPDF))
	mux.HandleFunc("GET /bands/{id}/export-ly.pdf", s.requireBandMember(s.handleExportBandRepertoireLilyPDF))

	// Jam mode: organiser routes are normal {book}-scoped, role-gated
	// routes. /jam/{code}/* is reachable WITHOUT a login — a nickname-only
	// jam_profiles identity, set via its own cookie in jam.go, not
	// requireAuth/requireRole at all.
	mux.HandleFunc("POST /b/{book}/jam-events", s.requireRole("editor", s.handleCreateJamEvent))
	// requireAuth only (not requireRole): a designated jam organizer with
	// no project-wide role at all must still be able to see this page —
	// handleJamRosterPage itself checks "viewer-or-above OR organizer".
	mux.HandleFunc("GET /b/{book}/jam/{code}/roster", s.requireAuth(s.handleJamRosterPage))
	mux.HandleFunc("GET /b/{book}/jam/{code}/add", s.requireRole("editor", s.handleJamAddSongsPage))
	mux.HandleFunc("POST /b/{book}/jam/{code}/songs", s.requireRole("editor", s.handleAddJamSong))
	mux.HandleFunc("POST /b/{book}/jam/{code}/songs/{songID}/remove", s.requireRole("editor", s.handleRemoveJamSong))
	// Jam organizer (Phase 5): song-order and "now calling" control, gated
	// on organizer-for-this-event OR project editor/owner, not plain
	// editor like the song-list management routes above.
	mux.HandleFunc("POST /b/{book}/jam/{code}/reorder", s.requireJamOrganizer(s.handleReorderJamSongs))
	mux.HandleFunc("POST /b/{book}/jam/{code}/call", s.requireJamOrganizer(s.handleCallJamParticipant))
	// requireAuth only, same "viewer-or-organizer" reasoning as the roster
	// route above — handleJamCallStatus checks it itself.
	mux.HandleFunc("GET /b/{book}/jam/{code}/call-status", s.requireAuth(s.handleJamCallStatus))
	mux.HandleFunc("POST /b/{book}/jam/{code}/organizers", s.requireRole("editor", s.handleAddJamOrganizer))
	mux.HandleFunc("GET /jam/{code}", s.optionalPerson(s.handleJamLandingPage))
	mux.HandleFunc("POST /jam/{code}/join", s.handleJamJoin)
	mux.HandleFunc("GET /jam/{code}/songs", s.handleJamSongsPage)
	mux.HandleFunc("POST /jam/{code}/picks", s.handleJamPick)
	mux.HandleFunc("DELETE /jam/{code}/picks", s.handleJamPick)
	// Final phase of the data-model rewrite: a jam PARTICIPANT (any valid
	// jam session for this event, via requireJamProfile — not an
	// organizer/editor) can add any global-catalog song to the jam's own
	// pickable list, create a brand-new jam-scoped variant from scratch,
	// or fork an existing variant into jam scope. See handleJamCatalogPage
	// et al. in jam.go.
	mux.HandleFunc("GET /jam/{code}/catalog", s.requireJamProfile(s.handleJamCatalogPage))
	mux.HandleFunc("POST /jam/{code}/catalog", s.requireJamProfile(s.handleJamAddCatalogSong))
	mux.HandleFunc("GET /jam/{code}/new", s.requireJamProfile(s.handleJamNewVariantPage))
	mux.HandleFunc("POST /jam/{code}/new", s.requireJamProfile(s.handleJamCreateVariant))
	mux.HandleFunc("POST /jam/{code}/fork", s.requireJamProfile(s.handleJamForkVariant))

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

	return mux
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
