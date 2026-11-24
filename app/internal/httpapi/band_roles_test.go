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

// bandRoleIDs is a band's built-in roles' ids.
func bandRoleIDs(t *testing.T, st *store.Store, bandID int64) (core, guest int64) {
	t.Helper()
	roles, err := st.ListBandRoles(context.Background(), bandID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range roles {
		switch r.BuiltinKey {
		case "core":
			core = r.ID
		case "guest":
			guest = r.ID
		}
	}
	return core, guest
}

// TestBandRolesEditorAccess: anyone in the band sees the roles; only a
// role with manage_roles (core, by default) changes them or gives them;
// a band's roles API never reaches another band's role.
func TestBandRolesEditorAccess(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	guestID, guest := e.person("Guest")
	otherID, other := e.person("Other")
	bandID, err := e.st.CreateBand(ctx, "Roles Band "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddBandMember(ctx, bandID, guestID, "guest", coreID); err != nil {
		t.Fatal(err)
	}
	otherBand, err := e.st.CreateBand(ctx, "Other Band "+e.sfx, otherID)
	if err != nil {
		t.Fatal(err)
	}
	b := fmt.Sprintf("/api/bands/%d", bandID)
	coreRole, guestRole := bandRoleIDs(t, e.st, bandID)
	otherCore, _ := bandRoleIDs(t, e.st, otherBand)

	if w := e.do("GET", b+"/roles", "", guest); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"BuiltinKey":"core"`) {
		t.Errorf("a guest lists the roles: %d %s", w.Code, w.Body)
	}
	if w := e.do("GET", b+"/roles", "", other); w.Code != http.StatusForbidden {
		t.Errorf("someone outside lists the roles: %d", w.Code)
	}
	for _, c := range []struct{ name, method, path, body string }{
		{"create a role", "POST", b + "/roles", `{"name":"Staff","privileges":["decide_proposals"]}`},
		{"change a role", "POST", fmt.Sprintf("%s/roles/%d/privileges", b, guestRole), `{"privileges":[]}`},
		{"give a role", "POST", fmt.Sprintf("%s/members/%d/role", b, guestID), fmt.Sprintf(`{"roleId":%d}`, coreRole)},
		{"core/guest switch", "POST", fmt.Sprintf("%s/members/%d/status", b, guestID), `{"status":"core"}`},
	} {
		if w := e.do(c.method, c.path, c.body, guest); w.Code != http.StatusForbidden {
			t.Errorf("%s as a guest: %d %s", c.name, w.Code, w.Body)
		}
	}

	w := e.do("POST", b+"/roles", `{"name":"Staff","privileges":["decide_proposals","vote"]}`, core)
	if w.Code != http.StatusOK {
		t.Fatalf("core creates Staff: %d %s", w.Code, w.Body)
	}
	var created struct{ RoleID int64 }
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if w := e.do("POST", fmt.Sprintf("%s/members/%d/role", b, guestID), fmt.Sprintf(`{"roleId":%d}`, created.RoleID), core); w.Code != http.StatusOK {
		t.Fatalf("core gives Staff: %d %s", w.Code, w.Body)
	}
	if ok, _ := e.st.HasBandPrivilege(ctx, bandID, guestID, store.BandPrivDecideProposals); !ok {
		t.Error("Staff doesn't decide")
	}
	if ok, _ := e.st.HasBandPrivilege(ctx, bandID, guestID, store.BandPrivEditSongbook); ok {
		t.Error("Staff edits the songbook, never given")
	}

	// Held to the band.
	if w := e.do("POST", fmt.Sprintf("%s/roles/%d/privileges", b, otherCore), `{"privileges":[]}`, core); w.Code != http.StatusNotFound {
		t.Errorf("another band's role through this band: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", fmt.Sprintf("%s/members/%d/role", b, guestID), fmt.Sprintf(`{"roleId":%d}`, otherCore), core); w.Code != http.StatusNotFound {
		t.Errorf("gave another band's role: %d %s", w.Code, w.Body)
	}
	// Someone keeps the roles.
	if w := e.do("POST", fmt.Sprintf("%s/members/%d/role", b, coreID), fmt.Sprintf(`{"roleId":%d}`, guestRole), core); w.Code != http.StatusConflict {
		t.Errorf("the last core member made a guest: %d %s", w.Code, w.Body)
	}

	// The settings page: the editor for core, read only for the others.
	page := e.do("GET", fmt.Sprintf("/bands/%d/settings", bandID), "", core).Body.String()
	for _, want := range []string{`id="roles"`, `id="band-role-new-form"`, `class="band-member-role"`, "Staff"} {
		if !strings.Contains(page, want) {
			t.Errorf("core's settings page has no %s", want)
		}
	}
	if n := disabledTicks(page); n != 0 {
		t.Errorf("%d of core's ticks are disabled", n)
	}
	page = e.do("GET", fmt.Sprintf("/bands/%d/settings", bandID), "", guest).Body.String()
	if !strings.Contains(page, `id="roles"`) || strings.Contains(page, `id="band-role-new-form"`) || strings.Contains(page, `class="band-member-role"`) {
		t.Error("Staff (no manage_roles) sees the editor's controls")
	}
	if strings.Contains(page, `class="band-role-priv"`) || !strings.Contains(page, `class="band-role-yes"`) {
		t.Error("Staff (no manage_roles) gets ticks, not marks")
	}
	if !strings.Contains(page, "Only members whose role lets them manage roles can change these.") {
		t.Error("the read-only editor doesn't say why")
	}
}

// TestBandPrivilegesGateRoutes: a guest says what the band plays and
// makes set lists, doesn't rename it, set its look nor (since migration
// 0132) add songs; once the band gives guests the songbook, the route
// lets them.
func TestBandPrivilegesGateRoutes(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	guestID, guest := e.person("Guest")
	bandID, err := e.st.CreateBand(ctx, "Gate Band "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddBandMember(ctx, bandID, guestID, "guest", coreID); err != nil {
		t.Fatal(err)
	}
	b := fmt.Sprintf("/api/bands/%d", bandID)
	// Renaming is rename_archive, core's; what it plays stays every member's.
	if w := e.do("POST", b+"/name", `{"name":"Renamed `+e.sfx+`"}`, guest); w.Code != http.StatusForbidden {
		t.Errorf("a guest renames the band: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", b+"/name", `{"name":"Renamed `+e.sfx+`"}`, core); w.Code != http.StatusOK {
		t.Errorf("core renames the band: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", b+"/music-type", `{"musicType":"covers"}`, guest); w.Code != http.StatusOK {
		t.Errorf("a guest says what the band plays: %d %s", w.Code, w.Body)
	}
	if w := e.do("PUT", b+"/locale", `{"locale":"fr"}`, guest); w.Code != http.StatusForbidden {
		t.Errorf("a guest sets the language: %d", w.Code)
	}
	if w := e.do("PUT", b+"/locale", `{"locale":"fr"}`, core); w.Code/100 != 2 {
		t.Errorf("core sets the language: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", b+"/variants", `{"title":"Guest Song","artist":"X"}`, guest); w.Code != http.StatusForbidden {
		t.Errorf("a guest adds a song: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", "/api/setlists", fmt.Sprintf(`{"scope":"band","band_id":%d,"title":"Set"}`, bandID), guest); w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Errorf("a guest makes a set list: %d %s", w.Code, w.Body)
	}

	_, guestRole := bandRoleIDs(t, e.st, bandID)
	keep := []string{`"` + store.BandPrivEditSongbook + `"`}
	for _, p := range store.BandRoleDefaults["guest"] {
		keep = append(keep, `"`+p+`"`)
	}
	if w := e.do("POST", fmt.Sprintf("%s/roles/%d/privileges", b, guestRole), `{"privileges":[`+strings.Join(keep, ",")+`]}`, core); w.Code != http.StatusOK {
		t.Fatalf("core gives guests the songbook: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", b+"/variants", `{"title":"Another","artist":"X"}`, guest); w.Code != http.StatusOK {
		t.Errorf("a guest given the songbook adds a song: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", b+"/variants", `{"title":"Core's","artist":"X"}`, core); w.Code != http.StatusOK {
		t.Errorf("core adds a song: %d %s", w.Code, w.Body)
	}
}

// TestCommunityRolesHeldToTheCommunity: the shared roles API holds a
// community's staff to their own community's roles (it took any role
// id before).
func TestCommunityRolesHeldToTheCommunity(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	aID, a := e.person("StaffA")
	bID, _ := e.person("StaffB")
	ca, err := e.st.CreateCommunity(ctx, "A "+e.sfx, aID)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := e.st.CreateCommunity(ctx, "B "+e.sfx, bID)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := e.st.ListCommunityRoles(ctx, cb)
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/communities/%d/roles/%d/privileges", ca, theirs[0].ID)
	if w := e.do("POST", path, `{"privileges":[]}`, a); w.Code != http.StatusNotFound {
		t.Errorf("A's staff changes B's role: %d %s", w.Code, w.Body)
	}
	if r, _ := e.st.GetCommunityRole(ctx, theirs[0].ID); len(r.Privileges) == 0 {
		t.Error("B's staff role lost its privileges")
	}
	mine, _ := e.st.ListCommunityRoles(ctx, ca)
	if w := e.do("POST", fmt.Sprintf("/api/communities/%d/roles/%d/privileges", ca, mine[2].ID), `{"privileges":["download_files"]}`, a); w.Code != http.StatusOK {
		t.Errorf("A's staff changes A's guest role: %d %s", w.Code, w.Body)
	}
}

// disabledTicks counts the roles editor's disabled checkboxes in page.
func disabledTicks(page string) int {
	n := 0
	for _, part := range strings.Split(page, `class="band-role-priv"`)[1:] {
		if tag := part[:strings.Index(part, ">")]; strings.Contains(tag, " disabled") {
			n++
		}
	}
	return n
}

// TestBandDeleteAdminOnly: no member deletes a band, core included; a
// site admin does, and only they see the button.
func TestBandDeleteAdminOnly(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	adminID, admin := e.person("Admin")
	if err := e.st.SetAdmin(ctx, adminID, true); err != nil {
		t.Fatal(err)
	}
	bandID, err := e.st.CreateBand(ctx, "Doomed "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range e.mustRoles(t, bandID) {
		if r.HasPrivilege("delete_band") {
			t.Errorf("%s grants delete_band", r.Name)
		}
	}
	if page := e.do("GET", fmt.Sprintf("/bands/%d/settings", bandID), "", core).Body.String(); strings.Contains(page, `id="delete-band-btn"`) {
		t.Error("core sees the delete button")
	}
	if page := e.do("GET", fmt.Sprintf("/bands/%d/settings", bandID), "", admin).Body.String(); !strings.Contains(page, `id="delete-band-btn"`) {
		t.Error("a site admin doesn't see the delete button")
	}
	if w := e.do("DELETE", fmt.Sprintf("/api/bands/%d", bandID), "", core); w.Code != http.StatusForbidden {
		t.Errorf("core deletes the band: %d", w.Code)
	}
	if w := e.do("DELETE", fmt.Sprintf("/api/bands/%d", bandID), "", admin); w.Code != http.StatusOK {
		t.Errorf("an admin deletes the band: %d %s", w.Code, w.Body)
	}
}

// TestBandArchive: core archives (a guest can't); archived, the band is
// read only for everyone — admins too — but unarchiving, gone from the
// switcher and the home page's cards, listed with a badge in All my
// groups, its pages say so; unarchived, all is back.
func TestBandArchive(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	guestID, guest := e.person("Guest")
	adminID, admin := e.person("Admin")
	if err := e.st.SetAdmin(ctx, adminID, true); err != nil {
		t.Fatal(err)
	}
	bandID, err := e.st.CreateBand(ctx, "Shelved "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddBandMember(ctx, bandID, guestID, "guest", coreID); err != nil {
		t.Fatal(err)
	}
	b := fmt.Sprintf("/api/bands/%d", bandID)
	if w := e.do("POST", b+"/archive", `{"archived":true}`, guest); w.Code != http.StatusForbidden {
		t.Errorf("a guest archives: %d", w.Code)
	}
	if w := e.do("POST", b+"/archive", `{"archived":true}`, core); w.Code != http.StatusOK {
		t.Fatalf("core archives: %d %s", w.Code, w.Body)
	}

	for who, c := range map[string]*http.Cookie{"core": core, "guest": guest, "admin": admin} {
		if w := e.do("POST", b+"/variants", `{"title":"New","artist":"X"}`, c); w.Code != http.StatusForbidden {
			t.Errorf("%s adds a song to an archived band: %d", who, w.Code)
		}
		if w := e.do("POST", "/api/setlists", fmt.Sprintf(`{"scope":"band","band_id":%d,"title":"Set"}`, bandID), c); w.Code != http.StatusForbidden {
			t.Errorf("%s makes a set list: %d", who, w.Code)
		}
		if w := e.do("POST", b+"/events", `{"name":"Rehearsal","eventType":"rehearsal","startsAt":"2030-01-01T20:00:00Z"}`, c); w.Code != http.StatusForbidden {
			t.Errorf("%s makes an event: %d", who, w.Code)
		}
		if w := e.do("POST", b+"/name", `{"name":"Again"}`, c); who != "guest" && w.Code != http.StatusOK {
			t.Errorf("%s renames the archived band: %d", who, w.Code)
		}
	}
	if w := e.do("POST", b+"/music-type", `{"musicType":"covers"}`, guest); w.Code != http.StatusForbidden {
		t.Errorf("a guest edits an archived band: %d", w.Code)
	}
	if ok, _ := e.st.HasBandPrivilege(ctx, bandID, guestID, store.BandPrivPropose); ok {
		t.Error("proposing to an archived band")
	}
	if page := e.do("GET", fmt.Sprintf("/bands/%d", bandID), "", guest).Body.String(); !strings.Contains(page, `class="space-archived"`) {
		t.Error("the archived band's page doesn't say so")
	}
	spaces, _ := e.st.ListSpaces(ctx, guestID)
	for _, sp := range spaces {
		if sp.Kind == "band" && sp.ID == bandID {
			t.Error("the archived band is in the switcher")
		}
	}
	if bands, _ := e.st.ListBandsForPerson(ctx, guestID); len(bands) != 0 {
		t.Errorf("the archived band is in the cards: %v", bands)
	}
	groups := e.do("GET", "/groups", "", guest).Body.String()
	if !strings.Contains(groups, fmt.Sprintf(`href="/bands/%d"`, bandID)) || !strings.Contains(groups, `class="badge-archived"`) {
		t.Error("All my groups doesn't list the archived band with its badge")
	}

	if w := e.do("POST", b+"/archive", `{"archived":false}`, core); w.Code != http.StatusOK {
		t.Fatalf("core unarchives: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", "/api/setlists", fmt.Sprintf(`{"scope":"band","band_id":%d,"title":"Back"}`, bandID), guest); w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Errorf("a guest makes a set list once unarchived: %d %s", w.Code, w.Body)
	}
	if bands, _ := e.st.ListBandsForPerson(ctx, guestID); len(bands) != 1 {
		t.Error("the unarchived band isn't back in the cards")
	}
}

func (e *inviteEnv) mustRoles(t *testing.T, bandID int64) []store.BandRole {
	t.Helper()
	roles, err := e.st.ListBandRoles(context.Background(), bandID)
	if err != nil {
		t.Fatal(err)
	}
	return roles
}
