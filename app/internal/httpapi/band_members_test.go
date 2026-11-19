package httpapi

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"app/internal/store"
	"app/internal/webassets"
)

// TestBandMembersRosterRows renders the Members page: one row per
// person, a placeholder's invite actions on that same row, and Edit only
// where the viewer may change the instruments.
func TestBandMembersRosterRows(t *testing.T) {
	until := time.Date(2026, 12, 19, 12, 0, 0, 0, time.UTC)
	me := &store.Person{ID: 1, Username: "guest", DisplayName: "Guest"}
	rows := []bandRosterRow{
		{BandMember: store.BandMember{PersonID: 1, Username: "guest", DisplayName: "Guest", Status: "guest", Instruments: []string{"bass"}},
			CanEditInstruments: store.MayEditMemberInstruments(false, true, false)},
		{BandMember: store.BandMember{PersonID: 2, Username: "corey", DisplayName: "Corey", Status: "core"},
			CanEditInstruments: store.MayEditMemberInstruments(false, false, false)},
		{BandMember: store.BandMember{PersonID: 3, Username: "pending-abc", DisplayName: "Marc", Status: "core", IsShell: true, Instruments: []string{"drums", "vocals"}},
			InviteToken: "tok123", InviteUntil: until, CanEditInstruments: store.MayEditMemberInstruments(false, false, true)},
		{BandMember: store.BandMember{PersonID: 4, Username: "pending-def", DisplayName: "Lea", Status: "guest", IsShell: true},
			CanEditInstruments: true},
	}
	data := map[string]any{"Me": me, "Band": &store.Band{ID: 7, Name: "The Band"}, "Members": rows, "IsCore": false}

	for _, lang := range []string{"en", "fr"} {
		var buf bytes.Buffer
		if err := webassets.TemplatesFor(lang).ExecuteTemplate(&buf, "band_members", data); err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		page := buf.String()
		if !strings.Contains(page, "</html>") {
			t.Fatalf("%s: incomplete page", lang)
		}
		if n := strings.Count(page, `class="roster-row"`) + strings.Count(page, `class="roster-row `); n != 4+1 { // + the quick-add form
			t.Errorf("%s: %d roster rows, want one per person plus quick-add", lang, n)
		}
		if strings.Count(page, `class="roster-invite"`) != 2 || strings.Count(page, `class="new-link-btn"`) != 2 {
			t.Errorf("%s: want invite actions on both placeholders' rows only", lang)
		}
		if !strings.Contains(page, `data-url="/invite/tok123"`) {
			t.Errorf("%s: Marc's link missing", lang)
		}
		if strings.Contains(page, "pending-abc") {
			t.Errorf("%s: a placeholder's internal username shows", lang)
		}
		if n := strings.Count(page, `class="roster-edit-btn"`); n != 3 {
			t.Errorf("%s: %d Edit buttons, want 3 (self and the two placeholders)", lang, n)
		}
		if strings.Contains(page, "roster-pending-header") {
			t.Errorf("%s: the second table is back", lang)
		}
	}
	var buf bytes.Buffer
	_ = webassets.TemplatesFor("fr").ExecuteTemplate(&buf, "band_members", data)
	for _, want := range []string{"Nouveau lien", "Copier le lien", "expire le 19 décembre 2026", "pas encore de lien", "Modifier les instruments de Marc"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("fr: missing %q", want)
		}
	}
}
