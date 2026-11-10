package httpapi

import (
	"net/http"

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
	mux.HandleFunc("GET /docs/song-format/example-tab", s.handleDocsSongFormatExampleTab)
	mux.HandleFunc("GET /for-educators", s.optionalPerson(s.handleMarketingEducators))
	mux.HandleFunc("GET /home", s.requireAuth(s.handleIndex))

	// Songbooks: a named, ordered collection of variant pointers owned by
	// a person or a band (see internal/store/songbooks.go) — the "project"
	// container's replacement. /songbooks/mine lists the logged-in
	// person's own; /songbooks/{id} is any one songbook's page.
	mux.HandleFunc("GET /songbooks/mine", s.requireAuth(s.handleMySongbooksPage))
	mux.HandleFunc("POST /api/songbooks", s.requireAuth(s.handleCreateSongbook))
	mux.HandleFunc("GET /songbooks/{id}", s.requireAuth(s.handleSongbookPage))
	mux.HandleFunc("POST /api/songbooks/{id}/rename", s.requireAuth(s.handleRenameSongbook))
	mux.HandleFunc("DELETE /api/songbooks/{id}", s.requireAuth(s.handleDeleteSongbook))
	mux.HandleFunc("PUT /api/songbooks/{id}/order", s.requireAuth(s.handleSaveSongbookOrder))
	mux.HandleFunc("POST /api/songbooks/{id}/songs", s.requireAuth(s.handleCreateSongInSongbook))
	mux.HandleFunc("POST /api/catalog/songs", s.requireAuth(s.handleCreateCatalogSong))
	mux.HandleFunc("POST /api/songbooks/{id}/entries", s.requireAuth(s.handleAddSongbookEntry))
	mux.HandleFunc("DELETE /api/songbooks/{id}/entries/{variantID}", s.requireAuth(s.handleRemoveSongbookEntry))
	mux.HandleFunc("GET /songbooks/{id}/playlist/metronome", s.requireAuth(s.handleMetronomePlaylist))
	mux.HandleFunc("GET /songbooks/{id}/playlist/mp3", s.requireAuth(s.handleMP3Playlist))

	// Songs: addressed by their own variant id from here on — see
	// migration 0041's "remove the project notion" doc comment.
	mux.HandleFunc("GET /song/{id}", s.requireVariantRole("viewer", s.handleSong))
	mux.HandleFunc("GET /api/qr", s.requireAuth(s.handleQRCode))
	mux.HandleFunc("POST /api/preview", s.requireAuth(s.handlePreview))
	mux.HandleFunc("POST /api/import/lyrics", s.requireAuth(s.handleImportLyrics))
	mux.HandleFunc("GET /api/import/lyrics/find", s.requireAuth(s.handleImportLyricsFindFetch))
	mux.HandleFunc("POST /api/import/chords", s.requireAuth(s.handleImportChords))
	mux.HandleFunc("GET /api/import/ug/find", s.requireAuth(s.handleImportUGFind))
	mux.HandleFunc("GET /api/import/ireal/find", s.requireAuth(s.handleImportIRealFind))
	mux.HandleFunc("POST /api/import/ireal/merge", s.requireAuth(s.handleImportIRealMerge))
	mux.HandleFunc("POST /api/import/ultimate-guitar/parse", s.requireAuth(s.handleImportUGParse))
	mux.HandleFunc("POST /api/import/ultimate-guitar/convert", s.requireAuth(s.handleImportUGConvert))

	mux.HandleFunc("GET /api/songs/{id}", s.requireVariantRole("viewer", s.handleGetSong))
	mux.HandleFunc("PUT /api/songs/{id}", s.requireVariantRole("editor", s.handlePutSong))
	mux.HandleFunc("PUT /api/songs/{id}/meta", s.requireVariantRole("editor", s.handlePutMeta))
	mux.HandleFunc("PUT /api/variants/{id}/genre", s.requireAuth(s.handlePutVariantGenre))
	mux.HandleFunc("GET /api/songs/{id}/raw", s.requireVariantRole("viewer", s.handleGetRaw))
	mux.HandleFunc("GET /api/songs/{id}/structure", s.requireVariantRole("viewer", s.handleGetSongStructure))
	mux.HandleFunc("GET /api/songs/{id}/drummer-rows", s.requireVariantRole("viewer", s.handleGetDrummerRows))
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
	mux.HandleFunc("PUT /api/songs/{id}/audio/{category}/markers/{sectionIndex}", s.requireVariantRole("editor", s.handlePutAudioMarker))
	mux.HandleFunc("DELETE /api/songs/{id}/audio/{category}/markers/{sectionIndex}", s.requireVariantRole("editor", s.handleDeleteAudioMarker))
	mux.HandleFunc("PUT /api/songs/{id}/audio/gp/sync-points", s.requireVariantRole("editor", s.handlePutGPBarSyncPoints))
	mux.HandleFunc("PUT /api/songs/{id}/gp-file", s.requireVariantRole("editor", s.handlePutSongGPFile))
	mux.HandleFunc("GET /song/{id}/gp-file", s.requireVariantRole("viewer", s.handleGetSongGPFile))

	// A section's "score[label]:" voices (internal/tab's LilyPond-
	// compatible subset, or a GP-imported riff's own JSON encoding — see
	// tab.DecodeScoreSource) are plain text in the body itself now (see
	// song.ScoreVoice's own doc comment) — no separate route to read a
	// voice, or for a human to write or preview one: Source's existing
	// raw-text save/preview already covers that (/api/songs/{id}/raw,
	// /api/preview), the same way any other "grid:"/"note:" content is
	// edited. The GP import wizard (gp-import.js) is the one exception:
	// it writes several sections' worth of imported riffs in a tight
	// machine-driven sequence, where round-tripping the WHOLE raw text
	// through the browser for every single section would mean threading
	// a save/reload/re-slice cycle through its own import loop for no
	// real benefit — handlePutSectionScore does that same splice
	// server-side instead, where it already has song.Parse/song.Write.
	mux.HandleFunc("PUT /api/songs/{id}/sections/{index}/score/{label}", s.requireVariantRole("editor", s.handlePutSectionScore))
	mux.HandleFunc("POST /api/chord-diagrams/preview", s.requireAuth(s.handleChordDiagramPreview))

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
	mux.HandleFunc("GET /bands", s.requireAuth(s.handleBandsPage))
	mux.HandleFunc("POST /api/bands", s.requireAuth(s.handleCreateBand))
	mux.HandleFunc("GET /bands/{id}", s.requireBandMember(s.handleBandPage))
	mux.HandleFunc("DELETE /api/bands/{id}", s.requireBandMember(s.handleDeleteBand))
	mux.HandleFunc("POST /api/bands/{id}/name", s.requireBandMember(s.handleSetBandName))
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
	mux.HandleFunc("GET /communities/{id}/repertoire", s.requireCommunityMember(s.handleCommunityRepertoirePage))
	mux.HandleFunc("GET /communities/{id}/songbooks", s.requireCommunityMember(s.handleCommunitySongbooksPage))
	mux.HandleFunc("GET /communities/{id}/admin", s.requireCommunityAnyManagePrivilege(s.handleCommunityAdminPage))
	mux.HandleFunc("POST /api/communities/{id}/rename", s.requireCommunityPrivilege(store.PrivManageSettings, s.handleRenameCommunity))
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
	mux.HandleFunc("POST /api/bands/{id}/events", s.requireBandMember(s.handleCreateBandEvent))
	mux.HandleFunc("GET /events/{id}", s.optionalPerson(s.handleEventPage))
	mux.HandleFunc("GET /events/{id}/ics", s.optionalPerson(s.handleEventICS))
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
	// CanEditSetlistByID), plus setlist_gigs and practice/listening
	// mode. All requireAuth only, not a role middleware — a setlist's
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
	mux.HandleFunc("GET /setlists/{id}/practice", s.requireAuth(s.handleSetlistPractice))
	mux.HandleFunc("GET /setlists/{id}/listening", s.requireAuth(s.handleSetlistListening))
	mux.HandleFunc("GET /setlists/{id}/clicktrack", s.requireAuth(s.handleSetlistClicktrack))
	mux.HandleFunc("GET /api/me/click-track-prefs", s.requireAuth(s.handleGetClickTrackPrefs))
	mux.HandleFunc("PUT /api/me/click-track-prefs", s.requireAuth(s.handleSetClickTrackPrefs))

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
