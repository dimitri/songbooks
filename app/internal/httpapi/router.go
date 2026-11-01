package httpapi

import (
	"net/http"

	"app/internal/lilypond"
	"app/internal/mailer"
	"app/internal/pdf"
	"app/internal/store"
	"app/internal/webassets"
)

type Server struct {
	store  *store.Store
	pdf    *pdf.Bridge      // nil when v2Root is unset — PDF export routes 503 in that case
	ly     *lilypond.Bridge // the LilyPond export pipeline — needs no external template tree, so always non-nil; a missing `lilypond` binary only surfaces as a request-time 500
	mailer mailer.Mailer
}

// NewServer wires the HTTP handler. v2Root is the path to the v2/ Typst
// pipeline (lib/, render.typ, render-book.typ) that PDF export shells out
// to; pass "" to disable PDF export (e.g. a deployment with no v2/ checkout
// available). m is the mailer magic-link login uses to "send" its emails —
// see internal/mailer and cmd/songbooks/main.go, which constructs a
// mailer.ConsoleMailer by default.
func NewServer(st *store.Store, v2Root string, m mailer.Mailer) http.Handler {
	s := &Server{store: st, mailer: m, ly: lilypond.NewBridge()}
	if v2Root != "" {
		s.pdf = pdf.New(v2Root)
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
	mux.HandleFunc("GET /invite/{token}", s.handleInvitePage)
	mux.HandleFunc("POST /invite/{token}", s.handlePostInvite)

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
	mux.HandleFunc("POST /api/import/chords", s.requireAuth(s.handleImportChords))

	mux.HandleFunc("GET /api/songs/{book}/{stem}", s.requireRole("viewer", s.handleGetSong))
	mux.HandleFunc("PUT /api/songs/{book}/{stem}", s.requireRole("editor", s.handlePutSong))
	mux.HandleFunc("PUT /api/songs/{book}/{stem}/meta", s.requireRole("editor", s.handlePutMeta))
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
	mux.HandleFunc("POST /api/bands/{id}/members", s.requireBandMember(s.handleAddBandMember))
	mux.HandleFunc("POST /api/bands/{id}/members/{personID}/status", s.requireBandMember(s.handleSetBandMemberStatus))
	mux.HandleFunc("DELETE /api/bands/{id}/members/{personID}", s.requireBandMember(s.handleRemoveBandMember))
	mux.HandleFunc("POST /api/bands/{id}/invites", s.requireBandMember(s.handleCreateBandInvite))
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

	mux.HandleFunc("GET /b/{book}/{stem}/export.pdf", s.requireRole("viewer", s.handleExportSongPDF))
	mux.HandleFunc("GET /setlists/{book}/{stem}/export.pdf", s.requireRole("viewer", s.handleExportSetlistPDF))

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

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(webassets.StaticFS())))

	// PWA: served from the root, not /static/, so the service worker's
	// default scope covers the whole app (see handlers_pwa.go). Left
	// unauthenticated — needed for the login page itself to install/render.
	mux.HandleFunc("GET /manifest.json", s.handleManifest)
	mux.HandleFunc("GET /sw.js", s.handleServiceWorker)
	mux.HandleFunc("GET /apple-touch-icon.png", s.handleAppIcon)

	return mux
}
