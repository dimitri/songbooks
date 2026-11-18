package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPublicPages renders each public page in English and French: the
// whole page (a template error stops the output before </html>), and
// the French one in French.
func TestPublicPages(t *testing.T) {
	s := &Server{}
	pages := map[string]struct {
		h  http.HandlerFunc
		fr string
	}{
		"/":                  {s.handleMarketingHome, "prêt à jouer."},
		"/for/bands":         {s.publicPageHandler("public_bands"), "sur chaque téléphone."},
		"/for/teachers":      {s.publicPageHandler("public_teachers"), "leçon par leçon."},
		"/for/vocal-coaches": {s.publicPageHandler("public_vocal"), "dans la tonalité de chaque élève."},
		"/for/communities":   {s.publicPageHandler("public_communities"), "pour toute la jam."},
		"/tour":              {s.publicPageHandler("public_tour"), "de la grille à la scène."},
	}
	for path, p := range pages {
		for _, lang := range []string{"en", "fr"} {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", path+"?lang="+lang, nil)
			p.h(&langWriter{ResponseWriter: w, r: r}, r)
			body := w.Body.String()
			if w.Code != http.StatusOK || !strings.Contains(body, "</html>") {
				t.Errorf("%s (%s): %d, incomplete page", path, lang, w.Code)
				continue
			}
			if lang == "fr" && !strings.Contains(body, p.fr) {
				t.Errorf("%s (fr): missing %q", path, p.fr)
			}
			if !strings.Contains(body, `hreflang="fr"`) {
				t.Errorf("%s (%s): no alternates", path, lang)
			}
		}
	}

	w := httptest.NewRecorder()
	s.handleMarketingEducators(w, httptest.NewRequest("GET", "/for-educators?lang=fr", nil))
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/for/teachers?lang=fr" {
		t.Errorf("/for-educators: %d %q", w.Code, w.Header().Get("Location"))
	}
}
