package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"app/internal/store"
)

// Band roles, round 2 (docs/band-roles.md, migration 0132), over HTTP.

// currentRole is personID's current role id in bandID, 0 when not a
// member.
func currentRole(t *testing.T, st *store.Store, bandID, personID int64) int64 {
	t.Helper()
	members, err := st.ListBandMembers(context.Background(), bandID, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range members {
		if m.PersonID == personID {
			return m.RoleID
		}
	}
	return 0
}

// TestInviteIntoCustomRoleHTTP: the members page offers the band's own
// roles; the open link, the one-off link, adding an account and a
// placeholder each take a roleId and give that role; an older client's
// core/guest still works; another band's role is refused.
func TestInviteIntoCustomRoleHTTP(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	bandID, err := e.st.CreateBand(ctx, "Invite Band "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	roadie, err := e.st.GroupRoles("band").Create(ctx, bandID, "Roadie", []string{store.BandPrivManageEvents})
	if err != nil {
		t.Fatal(err)
	}
	_, guestRole := bandRoleIDs(t, e.st, bandID)
	b := fmt.Sprintf("/api/bands/%d", bandID)

	page := e.do("GET", fmt.Sprintf("/bands/%d/members", bandID), "", core).Body.String()
	if !strings.Contains(page, fmt.Sprintf(`<option value="%d">Roadie</option>`, roadie)) || !strings.Contains(page, `id="band-link-role"`) {
		t.Error("the members page doesn't offer the band's own role")
	}

	// The open link: one per role, handed back when asked again.
	w := e.do("POST", b+"/band-link", fmt.Sprintf(`{"roleId":%d}`, roadie), core)
	var link struct {
		ID     int64  `json:"id"`
		URL    string `json:"url"`
		RoleID int64  `json:"roleId"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &link); err != nil || w.Code != http.StatusOK || link.RoleID != roadie {
		t.Fatalf("a Roadie link: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", b+"/band-link", fmt.Sprintf(`{"roleId":%d}`, roadie), core); !strings.Contains(w.Body.String(), fmt.Sprintf(`"id":%d`, link.ID)) {
		t.Errorf("asked again, another link: %s", w.Body)
	}
	if w := e.do("POST", b+"/band-link", `{"status":"guest"}`, core); w.Code != http.StatusOK || strings.Contains(w.Body.String(), fmt.Sprintf(`"id":%d`, link.ID)) {
		t.Errorf("an older client's guest link: %d %s", w.Code, w.Body)
	}
	if page := e.do("GET", fmt.Sprintf("/bands/%d/members", bandID), "", core).Body.String(); !strings.Contains(page, "“Roadie” link") || !strings.Contains(page, "Guest link") {
		t.Error("the members page doesn't name the links by their role")
	}
	joinerID, _ := e.person("Joiner")
	if _, err := e.st.ClaimGroupInviteUnlisted(ctx, strings.TrimPrefix(link.URL, "/join/"), joinerID, "Joiner"); err != nil {
		t.Fatal(err)
	}
	if got := currentRole(t, e.st, bandID, joinerID); got != roadie {
		t.Errorf("joined with the Roadie link: role %d", got)
	}

	// An account added by email.
	addedID, _ := e.person("Added")
	if w := e.do("POST", b+"/members", fmt.Sprintf(`{"email":"Added%s@example.test","roleId":%d}`, e.sfx, roadie), core); w.Code != http.StatusOK {
		t.Fatalf("added as a Roadie: %d %s", w.Code, w.Body)
	}
	if got := currentRole(t, e.st, bandID, addedID); got != roadie {
		t.Errorf("added as a Roadie: role %d", got)
	}
	// A one-off link, by form.
	if w := e.do("POST", b+"/invites", fmt.Sprintf("roleId=%d", roadie), core); w.Code != http.StatusOK {
		t.Fatalf("a Roadie one-off link: %d %s", w.Code, w.Body)
	}
	open, _ := e.st.ListOpenBandInvites(ctx, bandID)
	if len(open) != 1 || open[0].RoleID != roadie {
		t.Errorf("the one-off link's role: %+v", open)
	}
	// A placeholder.
	w = e.do("POST", b+"/personal-invites", fmt.Sprintf(`{"displayName":"Pending","roleId":%d}`, roadie), core)
	var pi struct {
		PersonID int64 `json:"personId"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pi); err != nil || w.Code != http.StatusOK {
		t.Fatalf("a Roadie placeholder: %d %s", w.Code, w.Body)
	}
	if got := currentRole(t, e.st, bandID, pi.PersonID); got != roadie {
		t.Errorf("a Roadie placeholder: role %d", got)
	}
	// An older client's placeholder: guest.
	w = e.do("POST", b+"/personal-invites", `{"displayName":"Old client"}`, core)
	if err := json.Unmarshal(w.Body.Bytes(), &pi); err != nil || currentRole(t, e.st, bandID, pi.PersonID) != guestRole {
		t.Errorf("an older client's placeholder: %s", w.Body)
	}

	// Another band's role: refused.
	otherID, err := e.st.CreateBand(ctx, "Elsewhere "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	otherCore, _ := bandRoleIDs(t, e.st, otherID)
	if w := e.do("POST", b+"/band-link", fmt.Sprintf(`{"roleId":%d}`, otherCore), core); w.Code != http.StatusBadRequest {
		t.Errorf("a link for another band's role: %d", w.Code)
	}
	if w := e.do("POST", b+"/members", fmt.Sprintf(`{"email":"Joiner%s@example.test","roleId":%d}`, e.sfx, otherCore), core); w.Code != http.StatusBadRequest {
		t.Errorf("added with another band's role: %d", w.Code)
	}
}

// TestBandSettingsTabByPrivilege: the band's ⚙ Settings shows to whoever
// holds one of the privileges it changes, not to a role with none of
// them; the page itself stays open to every member, ⚙ in its header.
func TestBandSettingsTabByPrivilege(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	fanID, fan := e.person("Fan")
	guestID, guest := e.person("Guest")
	bandID, err := e.st.CreateBand(ctx, "Nav Band "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	fans, err := e.st.GroupRoles("band").Create(ctx, bandID, "Fans", []string{store.BandPrivPropose})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{fanID, guestID} {
		if _, err := e.st.AddBandMember(ctx, bandID, id, "guest", coreID); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.st.SetBandMemberRole(ctx, bandID, fanID, fans, coreID); err != nil {
		t.Fatal(err)
	}
	gear := fmt.Sprintf(`href="/bands/%d/settings"`, bandID)
	overview := fmt.Sprintf("/bands/%d/members", bandID)
	for who, c := range map[string]struct {
		cookie *http.Cookie
		want   bool
	}{"core": {core, true}, "guest (edit_band)": {guest, true}, "a role with no setting": {fan, false}} {
		page := e.do("GET", overview, "", c.cookie).Body.String()
		if got := strings.Contains(page, `class="space-gear`) && strings.Contains(page, gear); got != c.want {
			t.Errorf("%s: Settings in the header %v, want %v", who, got, c.want)
		}
	}
	if w := e.do("GET", fmt.Sprintf("/bands/%d/settings", bandID), "", fan); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `class="space-gear active"`) {
		t.Errorf("the settings page, for a role with no setting: %d", w.Code)
	}
}

// TestGuestKeepsNotesAndProposes: a guest (no edit_songbook) doesn't edit
// the band's song but writes their own notes on it; proposes a song,
// fixing its draft in the check step; leaves the band themselves
// (remove_members is no longer theirs), not someone else.
func TestGuestKeepsNotesAndProposes(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	guestID, guest := e.person("Guest")
	otherID, _ := e.person("Other")
	bandID, err := e.st.CreateBand(ctx, "Notes Band "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{guestID, otherID} {
		if _, err := e.st.AddBandMember(ctx, bandID, id, "guest", coreID); err != nil {
			t.Fatal(err)
		}
	}
	song, err := e.st.CreateBandVariant(ctx, bandID, "Night Drive", "Us", coreID)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := e.st.SongRightsFor(ctx, guestID, song); err != nil || r.Edit || !r.See {
		t.Errorf("a guest's rights on the band's song: %+v %v, want see, not edit", r, err)
	}
	if w := e.do("POST", fmt.Sprintf("/api/variants/%d/sections/0/annotations", song), `{"body":"solo: sax","visibility":"private"}`, guest); w.Code/100 != 2 {
		t.Errorf("a guest's note: %d %s", w.Code, w.Body)
	}

	// Proposing: the draft, fixed, sent.
	w := e.do("POST", fmt.Sprintf("/api/bands/%d/proposal-drafts", bandID), `{"title":"Guest Pick","artist":"Them"}`, guest)
	var d struct {
		ID      int64  `json:"songId"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || w.Code != http.StatusOK || d.ID == 0 {
		t.Fatalf("a guest's draft: %d %s", w.Code, w.Body)
	}
	put := fmt.Sprintf(`{"version":%q,"title":"Guest Pick","artist":"Them","sections":[{"from":0,"label":"Verse","grid":"| C | G |"}]}`, d.Version)
	if w := e.do("PUT", fmt.Sprintf("/api/bands/%d/proposal-drafts/%d", bandID, d.ID), put, guest); w.Code != http.StatusOK {
		t.Errorf("a guest fixes their draft: %d %s", w.Code, w.Body)
	}
	if w := e.do("PUT", fmt.Sprintf("/api/bands/%d/proposal-drafts/%d", bandID, d.ID), put, e.cookieOf(otherID)); w.Code != http.StatusForbidden {
		t.Errorf("another guest fixes it: %d", w.Code)
	}
	if w := e.do("POST", fmt.Sprintf("/api/bands/%d/proposals", bandID), fmt.Sprintf(`{"kind":"new_song","targetVariantId":%d}`, d.ID), guest); w.Code/100 != 2 {
		t.Errorf("a guest proposes: %d %s", w.Code, w.Body)
	}

	// Leaving, not removing.
	if w := e.do("DELETE", fmt.Sprintf("/api/bands/%d/members/%d", bandID, otherID), "", guest); w.Code != http.StatusForbidden {
		t.Errorf("a guest removes someone: %d", w.Code)
	}
	if page := e.do("GET", fmt.Sprintf("/bands/%d/members", bandID), "", guest).Body.String(); !strings.Contains(page, "data-leave") {
		t.Error("no Leave the band for a guest")
	}
	if w := e.do("DELETE", fmt.Sprintf("/api/bands/%d/members/%d", bandID, guestID), "", guest); w.Code != http.StatusOK {
		t.Errorf("a guest leaves: %d %s", w.Code, w.Body)
	}
	// Not an archived band.
	if err := e.st.SetBandArchived(ctx, bandID, true); err != nil {
		t.Fatal(err)
	}
	otherCookie := e.cookieOf(otherID)
	if w := e.do("DELETE", fmt.Sprintf("/api/bands/%d/members/%d", bandID, otherID), "", otherCookie); w.Code != http.StatusForbidden {
		t.Errorf("leaving an archived band: %d", w.Code)
	}
	_ = core
}

// TestArchiveDialog: the Settings ask before archiving, in a dialog that
// says what archiving does, the invite links included; the archived band
// says its links stay closed.
func TestArchiveDialog(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	bandID, err := e.st.CreateBand(ctx, "Dialog Band "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	page := e.do("GET", fmt.Sprintf("/bands/%d/settings", bandID), "", core).Body.String()
	if !strings.Contains(page, `<dialog class="edit-offer-dialog" id="archive-dialog"`) || !strings.Contains(page, "Every invite link is turned off") {
		t.Error("no archive dialog saying what it does")
	}
	if err := e.st.SetBandArchived(ctx, bandID, true); err != nil {
		t.Fatal(err)
	}
	page = e.do("GET", fmt.Sprintf("/bands/%d/settings", bandID), "", core).Body.String()
	if strings.Contains(page, `id="archive-dialog"`) || !strings.Contains(page, "unarchiving doesn’t reopen them") {
		t.Error("the archived band's settings")
	}
}

// cookieOf is a fresh session for personID.
func (e *inviteEnv) cookieOf(personID int64) *http.Cookie {
	e.t.Helper()
	tok, err := e.st.CreateSession(context.Background(), personID)
	if err != nil {
		e.t.Fatal(err)
	}
	return &http.Cookie{Name: sessionCookie, Value: tok}
}
