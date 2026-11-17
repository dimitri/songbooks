package store

import (
	"fmt"
	"testing"
)

// The names the bands' folders have, read by the built-in templates.
func TestReadName(t *testing.T) {
	str := func(p *int) string {
		if p == nil {
			return "-"
		}
		return fmt.Sprint(*p)
	}
	for _, c := range []struct{ name, want string }{
		{"07 - Plan Chicago Blues - Take 2 - 2025-03-13", "set - n 7 take 2 title Plan Chicago Blues date 2025-03-13"},
		{"12 - Teens for Today - 2026-11-04", "set - n 12 take - title Teens for Today date 2026-11-04"},
		{"2 - 16 - Public - 2026-03-30", "set 2 n 16 take - title Public date 2026-03-30"},
		{"2 - 01 - Belle of the Live - 2026-03-15", "set 2 n 1 take - title Belle of the Live date 2026-03-15"},
		{"13 - Fin Kawazaki - Take 2 - 2024-02-26", "set - n 13 take 2 title Fin Kawazaki date 2024-02-26"},
		{"1-8 - Thierry - Impro - 2024-07-25", "set 1 n 8 take - title Thierry - Impro date 2024-07-25"},
		{"2-5 Teens4Today - 2024-03-15", "set 2 n 5 take - title Teens4Today date 2024-03-15"},
		{"2-8 Gone", "set 2 n 8 take - title Gone date "},
		{"1-0 Intro Take 1", "set 1 n 0 take - title Intro Take 1 date "},
		{"05-Teens4Today", "set - n 5 take - title Teens4Today date "},
		{"15 - v2-You know I am no good", "set - n 15 take - title You know I am no good date "},
		{"Concert - MJC Fresnes - 2025-03-22", "set - n - take - title Concert - MJC Fresnes date 2025-03-22"},
		{"Wonderwall Prise 3", "set - n - take 3 title Wonderwall date "},
		{"250313 Gone", "set - n - take - title Gone date 2025-03-13"},
	} {
		r, ok := readName(c.name, builtins)
		got := fmt.Sprintf("set %s n %s take %s title %s date %s", str(r.Set), str(r.Position), str(r.Take), r.Title, r.Date)
		if !ok || got != c.want {
			t.Errorf("%q (%s): got %q, want %q", c.name, r.Template, got, c.want)
		}
	}
	for _, raw := range []string{"TRACK01", "TRACK09_10", "MASTER", "PART 1 - MASTER", "ZOOM0004", "DR0000_0001", "STE-006-2025-03-22", "080101-009", "00000002", "IMG_2602", "VID_20240316_211523", "VIDEO-2024-03-17-12-14-00 (3)", "43d07893-f22f-4e6b-9837-967b52009d3f", "New Recording 12", "250505_220154", "TRACK09_10 2", "IMG_2602 (1)", "P1110501_1"} {
		if !rawName.MatchString(raw) {
			t.Errorf("%q isn't read as a recorder's own name", raw)
		}
	}
	if rawName.MatchString("Gone") || rawName.MatchString("07 - Gone - 2025-03-13") {
		t.Error("a song's name read as a recorder's")
	}
}

// A band's own template comes first; a bad one is refused.
func TestOwnTemplate(t *testing.T) {
	r, ok := readName("Other Lies Supersonic 2026-04-10 V08 Spatial V03 08 Dead End For Delia Binaural", templatesFor([]string{"{*} {date} {*} {n} {title} Binaural"}))
	if !ok || r.Title != "Dead End For Delia" || r.Position == nil || *r.Position != 8 || r.Date != "2026-04-10" {
		t.Errorf("got %+v", r)
	}
	for _, bad := range []string{"{date}", "{n} - {song}"} {
		if _, err := compileTemplate(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
