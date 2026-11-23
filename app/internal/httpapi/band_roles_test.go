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

// TestBandPrivilegesGateRoutes: a guest renames the band, adds songs,
// makes set lists (as before roles), not the look; once the band takes
// adding songs from guests, the route refuses them.
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
	if w := e.do("POST", b+"/name", `{"name":"Renamed `+e.sfx+`"}`, guest); w.Code != http.StatusOK {
		t.Errorf("a guest renames the band: %d %s", w.Code, w.Body)
	}
	if w := e.do("PUT", b+"/locale", `{"locale":"fr"}`, guest); w.Code != http.StatusForbidden {
		t.Errorf("a guest sets the language: %d", w.Code)
	}
	if w := e.do("PUT", b+"/locale", `{"locale":"fr"}`, core); w.Code/100 != 2 {
		t.Errorf("core sets the language: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", b+"/variants", `{"title":"Guest Song","artist":"X"}`, guest); w.Code != http.StatusOK {
		t.Errorf("a guest adds a song: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", "/api/setlists", fmt.Sprintf(`{"scope":"band","band_id":%d,"title":"Set"}`, bandID), guest); w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Errorf("a guest makes a set list: %d %s", w.Code, w.Body)
	}

	_, guestRole := bandRoleIDs(t, e.st, bandID)
	var keep []string
	for _, p := range store.BandRoleDefaults["guest"] {
		if p != store.BandPrivEditSongbook {
			keep = append(keep, `"`+p+`"`)
		}
	}
	if w := e.do("POST", fmt.Sprintf("%s/roles/%d/privileges", b, guestRole), `{"privileges":[`+strings.Join(keep, ",")+`]}`, core); w.Code != http.StatusOK {
		t.Fatalf("core takes the songbook from guests: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", b+"/variants", `{"title":"Another","artist":"X"}`, guest); w.Code != http.StatusForbidden {
		t.Errorf("a guest still adds a song: %d %s", w.Code, w.Body)
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
