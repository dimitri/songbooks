package i18n

import (
	"testing"
	"time"
)

func TestT(t *testing.T) {
	if got := T("fr", "Language"); got != "Langue" {
		t.Errorf("T: %q", got)
	}
	if got := T("fr", "Not in the catalog"); got != "Not in the catalog" {
		t.Errorf("an untranslated string: %q", got)
	}
	if got := T("en", "Language"); got != "Language" {
		t.Errorf("English: %q", got)
	}
}

func TestN(t *testing.T) {
	for _, c := range []struct {
		lang string
		n    int
		want string
	}{
		{"en", 0, "0 widgets"}, {"en", 1, "1 widget"}, {"en", 2, "2 widgets"},
		{"fr", 0, "0 widget"}, {"fr", 1, "1 widget"}, {"fr", 2, "2 widgets"},
	} {
		// Untranslated keys: only the plural rule is under test.
		if got := N(c.lang, c.n, "%d widget", "%d widgets"); got != c.want {
			t.Errorf("N(%s, %d) = %q", c.lang, c.n, got)
		}
	}
}

func TestAccept(t *testing.T) {
	for h, want := range map[string]string{
		"fr-FR,fr;q=0.9,en;q=0.8": "fr",
		"en-US,en;q=0.9,fr;q=0.8": "en",
		"de-DE,fr;q=0.5":          "fr",
		"de-DE":                   "",
		"":                        "",
	} {
		if got := Accept(h); got != want {
			t.Errorf("Accept(%q) = %q, want %q", h, got, want)
		}
	}
}

func TestDate(t *testing.T) {
	d := time.Date(2026, 11, 20, 18, 5, 0, 0, time.UTC)
	for layout, want := range map[string]string{
		"Monday 2 January":        "vendredi 20 novembre",
		"Mon 2 Jan, 15:04":        "ven. 20 nov., 18:05",
		"2 January 2006":          "20 novembre 2026",
		"Monday 2 January, 15:04": "vendredi 20 novembre, 18:05",
	} {
		if got := Date("fr", d, layout); got != want {
			t.Errorf("Date(%q) = %q, want %q", layout, got, want)
		}
	}
	if got := Date("en", d, "Mon 2 Jan"); got != "Fri 20 Nov" {
		t.Errorf("English date: %q", got)
	}
}

func TestNoteName(t *testing.T) {
	if got := NoteName("en", 60); got != "C4" {
		t.Errorf("en: %q", got)
	}
	if got := NoteName("fr", 60); got != "do3" {
		t.Errorf("fr: %q", got)
	}
	if got := NoteName("fr", 69); got != "la3" {
		t.Errorf("fr A4: %q", got)
	}
}

func TestContext(t *testing.T) {
	catalogs["fr"]["notation|Staff"] = "Portée"
	if got := T("fr", "notation|Staff"); got != "Portée" {
		t.Errorf("fr: %q", got)
	}
	if got := T("en", "notation|Staff"); got != "Staff" {
		t.Errorf("en: %q", got)
	}
	if got := T("fr", "nowhere|Staff"); got != "Staff" {
		t.Errorf("untranslated: %q", got)
	}
	if got := T("fr", "Bass | treble"); got != "Bass | treble" {
		t.Errorf("a bar in a sentence: %q", got)
	}
}
