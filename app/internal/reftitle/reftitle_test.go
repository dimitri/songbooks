package reftitle

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A fake YouTube oEmbed endpoint and a few pages: the title comes from
// oEmbed for a known host, from og:title or <title> for any other.
func TestTitle(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.String())
		switch r.URL.Path {
		case "/oembed":
			if !strings.Contains(r.URL.Query().Get("url"), "jNQXAC9IVRw") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"title":"Me at the zoo","author_name":"jawed","type":"video"}`))
		case "/og":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(`<html><head><title>Ignored</title><meta content="Les Feuilles &amp; mortes" property="og:title"></head></html>`))
		case "/title":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html><head><title>\n  A   live\tversion - YouTube </title></head></html>"))
		case "/none":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><body>nothing</body></html>`))
		case "/pdf":
			w.Header().Set("Content-Type", "application/pdf")
			w.Write([]byte("%PDF"))
		case "/slow":
			time.Sleep(300 * time.Millisecond)
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<title>Too late</title>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := &Fetcher{Client: srv.Client(), Providers: map[string]string{"youtube.com": srv.URL + "/oembed?format=json", "youtu.be": srv.URL + "/oembed?format=json"}}
	ctx := context.Background()

	for ref, want := range map[string]string{
		"https://www.youtube.com/watch?v=jNQXAC9IVRw": "Me at the zoo",
		"https://youtu.be/jNQXAC9IVRw":                "Me at the zoo",
		srv.URL + "/og":                               "Les Feuilles & mortes",
		srv.URL + "/title":                            "A live version",
	} {
		got, err := f.Title(ctx, ref)
		if err != nil || got != want {
			t.Errorf("Title(%q) = %q, %v; want %q", ref, got, err, want)
		}
	}
	if !strings.Contains(asked[0], "format=json") || !strings.Contains(asked[0], "url=https%3A%2F%2Fwww.youtube.com%2Fwatch%3Fv%3DjNQXAC9IVRw") {
		t.Errorf("oEmbed asked as %q", asked[0])
	}
	for _, ref := range []string{
		"https://www.youtube.com/watch?v=unknown1234", // the endpoint says 404
		srv.URL + "/none", srv.URL + "/pdf", srv.URL + "/missing",
		"ftp://example.com/x", "not a link", "",
	} {
		if got, err := f.Title(ctx, ref); err == nil {
			t.Errorf("Title(%q) = %q, want an error", ref, got)
		}
	}
	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := f.Title(short, srv.URL+"/slow"); err == nil {
		t.Error("a slow page: no timeout")
	}
}

// The real client never reaches a local or private address, not even
// one the test server is on; nor through a redirect.
func TestClientRefusesPrivateAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<title>secret</title>`))
	}))
	defer srv.Close()
	f := New()
	if got, err := f.Title(context.Background(), srv.URL+"/"); err == nil || !strings.Contains(err.Error(), "public") {
		t.Errorf("a loopback page: %q, %v", got, err)
	}
	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "::1", "fd00::1", "fe80::1", "0.0.0.0"} {
		if Public(net.ParseIP(ip)) {
			t.Errorf("%s is public", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "142.250.74.78", "2a00:1450:4007::200e"} {
		if !Public(net.ParseIP(ip)) {
			t.Errorf("%s isn't public", ip)
		}
	}
	// A redirect beyond three, or to another scheme, is refused.
	c := Client()
	req, _ := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	if err := c.CheckRedirect(req, make([]*http.Request, 3)); err == nil {
		t.Error("a fourth redirect")
	}
	req2, _ := http.NewRequest(http.MethodGet, "file:///etc/passwd", nil)
	if err := c.CheckRedirect(req2, nil); err == nil {
		t.Error("a redirect to file:")
	}
}

func TestClean(t *testing.T) {
	long := strings.Repeat("é", 300)
	if got := Clean(long); len([]rune(got)) != maxTitle || !strings.HasSuffix(got, "…") {
		t.Errorf("long: %d runes", len([]rune(got)))
	}
	if got := Clean("  Rock &#39;n&#39; roll \n"); got != "Rock 'n' roll" {
		t.Errorf("entities: %q", got)
	}
}
