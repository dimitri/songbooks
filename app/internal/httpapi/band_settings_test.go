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

// TestBandSettingsSections: which of a band's Settings one may change,
// by role — core all of them, a guest none (migration 0137), a role with
// only edit_look the Look, a site admin all and Delete; an archived
// band, only its name and Archive or delete.
func TestBandSettingsSections(t *testing.T) {
	privs := func(list ...string) store.BandPrivs {
		p := store.BandPrivs{}
		for _, k := range list {
			p[k] = true
		}
		return p
	}
	for _, c := range []struct {
		who      string
		privs    store.BandPrivs
		admin    bool
		sections string
		gear     bool
		delete   bool
		links    [2]bool // Members, Recordings
	}{
		{"core", privs(store.BandRoleDefaults["core"]...), false, "band look roles danger", true, false, [2]bool{true, true}},
		{"guest", privs(store.BandRoleDefaults["guest"]...), false, "", false, false, [2]bool{false, true}},
		{"a role with only edit_look", privs(store.BandPrivEditLook), false, "look", true, false, [2]bool{}},
		{"a role with only edit_band", privs(store.BandPrivEditBand), false, "band", true, false, [2]bool{}},
		{"a staff managing roles", privs(store.BandPrivManageRoles, store.BandPrivDecideProposals), false, "roles", true, false, [2]bool{}},
		{"a site admin", store.AllBandPrivs(false), true, "band look roles danger", true, true, [2]bool{true, true}},
		{"core, archived", privs(store.BandPrivRenameArchive), false, "band danger", true, false, [2]bool{}},
		{"a site admin, archived", store.AllBandPrivs(true), true, "band danger", true, true, [2]bool{}},
	} {
		a := bandSettingsFor(c.privs, c.admin)
		if got := strings.Join(a.Sections(), " "); got != c.sections {
			t.Errorf("%s: sections %q, want %q", c.who, got, c.sections)
		}
		if a.Any() != c.gear || a.Delete != c.delete || a.Members != c.links[0] || a.Recordings != c.links[1] {
			t.Errorf("%s: ⚙ %v delete %v links %v %v", c.who, a.Any(), a.Delete, a.Members, a.Recordings)
		}
	}
	// edit_band is The band's words: what it plays, its tagline and
	// website, its language; not its name.
	if a := bandSettingsFor(privs(store.BandPrivEditBand), false); a.Name || !a.Plays || !a.Words || a.Look {
		t.Errorf("edit_band's fields of The band: %+v", a)
	}
}

// TestBandSettingsSaves: each section saves on its own — The band's
// tagline and website alone (edit_band), the look kept; the Look's
// style (edit_look), the tagline and website kept; a website that isn't
// https refused; each route held to its privilege.
func TestBandSettingsSaves(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	lookerID, looker := e.person("Looker")
	bandID, err := e.st.CreateBand(ctx, "Saves "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	lookers, err := e.st.GroupRoles("band").Create(ctx, bandID, "Lookers", []string{store.BandPrivEditLook})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddBandMember(ctx, bandID, lookerID, "guest", coreID); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetBandMemberRole(ctx, bandID, lookerID, lookers, coreID); err != nil {
		t.Fatal(err)
	}
	b := fmt.Sprintf("/api/bands/%d", bandID)
	brandOf := func() map[string]string {
		t.Helper()
		var out map[string]string
		_ = json.Unmarshal(e.do("GET", b+"/brand", "", core).Body.Bytes(), &out)
		return out
	}

	if w := e.do("POST", b+"/brand", `{"preset":"stage","mark":"S"}`, core); w.Code != http.StatusOK {
		t.Fatalf("core saves a look: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", b+"/brand/words", `{"tagline":"Mine","website":""}`, looker); w.Code != http.StatusForbidden {
		t.Errorf("edit_look saves the words: %d", w.Code)
	}
	if w := e.do("POST", b+"/brand/words", `{"tagline":"Monday nights","website":"https://saves.example"}`, core); w.Code != http.StatusOK {
		t.Fatalf("core saves the words: %d %s", w.Code, w.Body)
	}
	if got := brandOf(); got["tagline"] != "Monday nights" || got["website"] != "https://saves.example" || got["preset"] != "stage" || got["mark"] != "S" {
		t.Errorf("after the words: %v", got)
	}
	if w := e.do("POST", b+"/brand/words", `{"tagline":"x","website":"saves.example"}`, core); w.Code != http.StatusBadRequest {
		t.Errorf("a website without https: %d", w.Code)
	}
	if w := e.do("POST", b+"/brand", `{"preset":"paper","tagline":"Not the Look's"}`, looker); w.Code != http.StatusOK {
		t.Fatalf("edit_look saves the Look: %d %s", w.Code, w.Body)
	}
	if got := brandOf(); got["preset"] != "paper" || got["mark"] != "" || got["tagline"] != "Monday nights" || got["website"] != "https://saves.example" {
		t.Errorf("the Look dropped The band's words, or kept its own fields: %v", got)
	}

	for _, c := range []struct{ method, path, body string }{
		{"POST", b + "/name", `{"name":"Not mine"}`},
		{"POST", b + "/music-type", `{"musicType":"covers"}`},
		{"PUT", b + "/locale", `{"locale":"fr"}`},
		{"POST", b + "/brand/words", `{"tagline":"x"}`},
	} {
		if w := e.do(c.method, c.path, c.body, looker); w.Code != http.StatusForbidden {
			t.Errorf("edit_look only: %s %s → %d", c.method, c.path, w.Code)
		}
	}
	if w := e.do("PUT", b+"/locale", `{"locale":"fr"}`, core); w.Code/100 != 2 {
		t.Errorf("core sets the language: %d", w.Code)
	}

	// Look-only: the page holds the Look and nothing else.
	page := e.do("GET", fmt.Sprintf("/bands/%d/settings", bandID), "", looker).Body.String()
	for _, want := range []string{`id="look-form"`, `id="look-save"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the Look-only page has no %s", want)
		}
	}
	for _, not := range []string{`id="band-form"`, `id="band-tagline"`, `id="band-roles"`, `id="archive-band-btn"`} {
		if strings.Contains(page, not) {
			t.Errorf("the Look-only page has %s", not)
		}
	}
}

// TestBandDeleteByName: a site admin deletes a band from a dialog that
// asks for its name (the button off until it's typed); core sees neither.
// Archiving, from the same section, still closes the band's links.
func TestBandDeleteByName(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Core")
	adminID, admin := e.person("Admin")
	if err := e.st.SetAdmin(ctx, adminID, true); err != nil {
		t.Fatal(err)
	}
	name := "Typed " + e.sfx
	bandID, err := e.st.CreateBand(ctx, name, coreID)
	if err != nil {
		t.Fatal(err)
	}
	page := e.do("GET", fmt.Sprintf("/bands/%d/settings", bandID), "", admin).Body.String()
	for _, want := range []string{`id="delete-dialog"`, fmt.Sprintf(`data-name="%s"`, name), `id="delete-dialog-go" disabled`, "Type " + name + " to confirm", `id="delete-archive-instead"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the admin's delete dialog has no %s", want)
		}
	}
	if strings.Contains(page, "confirm(") {
		t.Error("a browser confirm() for deleting")
	}
	if page := e.do("GET", fmt.Sprintf("/bands/%d/settings", bandID), "", core).Body.String(); strings.Contains(page, `id="delete-dialog"`) || !strings.Contains(page, `id="archive-dialog"`) {
		t.Error("core: a delete dialog, or no archive dialog")
	}

	_, guestRole := bandRoleIDs(t, e.st, bandID)
	if w := e.do("POST", fmt.Sprintf("/api/bands/%d/band-link", bandID), fmt.Sprintf(`{"roleId":%d}`, guestRole), core); w.Code != http.StatusOK {
		t.Fatalf("a band link: %d %s", w.Code, w.Body)
	}
	if page := e.do("GET", fmt.Sprintf("/bands/%d/settings", bandID), "", core).Body.String(); !strings.Contains(page, "1 open link stops working now.") {
		t.Error("the archive dialog doesn't count the open link")
	}
	if w := e.do("POST", fmt.Sprintf("/api/bands/%d/archive", bandID), `{"archived":true}`, core); w.Code != http.StatusOK {
		t.Fatalf("archive: %d", w.Code)
	}
	invites, err := e.st.ListGroupInvitesForTarget(ctx, "band", bandID)
	if err != nil {
		t.Fatal(err)
	}
	if len(invites) == 0 || len(bandLinks(invites)) != 0 {
		t.Errorf("archiving left %d of %d links open", len(bandLinks(invites)), len(invites))
	}
	if w := e.do("DELETE", fmt.Sprintf("/api/bands/%d", bandID), "", admin); w.Code != http.StatusOK {
		t.Errorf("the admin deletes: %d", w.Code)
	}
}

// TestMembersYourRole: Members says what one's own role allows, as a
// plain list; ?role= shows that role's members only.
func TestMembersYourRole(t *testing.T) {
	e := newInviteEnv(t)
	ctx := context.Background()
	coreID, core := e.person("Corey")
	guestID, guest := e.person("Gwen")
	bandID, err := e.st.CreateBand(ctx, "Roster "+e.sfx, coreID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddBandMember(ctx, bandID, guestID, "guest", coreID); err != nil {
		t.Fatal(err)
	}
	page := e.do("GET", fmt.Sprintf("/bands/%d/members", bandID), "", guest).Body.String()
	if !strings.Contains(page, `class="band-my-role"`) || !strings.Contains(page, "<strong>Guest</strong>") {
		t.Fatal("a guest's Members doesn't say their role")
	}
	if !strings.Contains(page, `<li class="is-yes"><span aria-hidden="true">✓</span> Propose songs`) || !strings.Contains(page, `<li class="is-no"><span aria-hidden="true">–</span> Say what it plays`) {
		t.Error("the guest's list of what their role allows")
	}
	if strings.Contains(page, "settings#roles") || strings.Contains(page, "in Settings") {
		t.Error("a guest is sent to Settings")
	}
	if !strings.Contains(e.do("GET", fmt.Sprintf("/bands/%d/members", bandID), "", core).Body.String(), "settings#roles") {
		t.Error("core isn't shown the roles side by side")
	}
	_, guestRole := bandRoleIDs(t, e.st, bandID)
	page = e.do("GET", fmt.Sprintf("/bands/%d/members?role=%d", bandID, guestRole), "", core).Body.String()
	if !strings.Contains(page, `data-name="Gwen"`) || strings.Contains(page, `data-name="Corey"`) || !strings.Contains(page, "Show everyone") {
		t.Error("the roster filtered by role")
	}
}
