// Package reftitle finds the name of a song's reference: the title of the
// video or page its link points to, for the song page's header and its
// Reference view. YouTube's and a few other hosts' oEmbed endpoints (no
// key needed), else the page's og:title or <title>.
//
// Only ever server side, in the background or right after the link is
// saved, never while a page renders. The links are the users' own, so the
// client refuses any address that isn't public (loopback, private,
// link-local…), redirects included, keeps to http and https, and reads
// little: a short timeout and a size limit.
package reftitle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// Timeout is the most a title is waited for, redirects included.
const Timeout = 5 * time.Second

// maxBody is the most bytes read of an oEmbed answer or a page: a page's
// <title> and og:title are in its <head>.
const maxBody = 512 << 10

// maxTitle is the longest title kept, in characters.
const maxTitle = 200

const userAgent = "songbooks (https://songbooks.tapoueh.org)"

// ErrNoTitle is a page or an oEmbed answer without a title.
var ErrNoTitle = errors.New("reftitle: no title")

// Fetcher finds titles. Its zero value uses Client() and the public
// endpoints; tests point the endpoints and the client at an httptest
// server.
type Fetcher struct {
	Client *http.Client
	// Providers are the oEmbed endpoints by host (without "www." or
	// "m."), each taking the link as its url parameter; nil is
	// DefaultProviders.
	Providers map[string]string
}

// DefaultProviders are the hosts whose oEmbed endpoint is known; any
// other host's page is read for its og:title or <title>.
var DefaultProviders = map[string]string{
	"youtube.com":       "https://www.youtube.com/oembed?format=json",
	"music.youtube.com": "https://www.youtube.com/oembed?format=json",
	"youtu.be":          "https://www.youtube.com/oembed?format=json",
	"vimeo.com":         "https://vimeo.com/api/oembed.json",
	"soundcloud.com":    "https://soundcloud.com/oembed?format=json",
	"open.spotify.com":  "https://open.spotify.com/oembed",
	"dailymotion.com":   "https://www.dailymotion.com/services/oembed?format=json",
}

// New is a Fetcher on the public endpoints with the guarded client.
func New() *Fetcher { return &Fetcher{Client: Client()} }

// Client is an http.Client for the users' links: it connects only to
// public addresses (checked on the address actually dialled, so a name
// that resolves to 127.0.0.1 or a redirect to 10.0.0.1 fails too),
// follows at most three redirects, to http or https only, and gives up
// after Timeout.
func Client() *http.Client {
	dialer := &net.Dialer{Timeout: Timeout, Control: publicOnly}
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   Timeout,
		ResponseHeaderTimeout: Timeout,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("reftitle: too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("reftitle: redirect to %s", req.URL.Scheme)
			}
			return nil
		},
	}
}

// publicOnly refuses to connect to an address that isn't public.
func publicOnly(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !Public(ip) {
		return fmt.Errorf("reftitle: %s isn't a public address", host)
	}
	return nil
}

// Public reports whether ip is a public unicast address: not loopback,
// private (10/8, 172.16/12, 192.168/16, fc00::/7), link-local,
// unspecified, multicast, or the carrier-grade NAT range.
func Public(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 64 { // 100.64.0.0/10
		return false
	}
	return true
}

func (f *Fetcher) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return Client()
}

// Host is a link's host for the providers: lower case, without "www."
// or "m.".
func Host(u *url.URL) string {
	h := strings.ToLower(u.Hostname())
	h = strings.TrimPrefix(h, "www.")
	return strings.TrimPrefix(h, "m.")
}

// Title is the title of the video or page ref points to.
func (f *Fetcher) Title(ctx context.Context, ref string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(ref))
	if err != nil {
		return "", err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", fmt.Errorf("reftitle: not a web link: %q", ref)
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	providers := f.Providers
	if providers == nil {
		providers = DefaultProviders
	}
	if endpoint, ok := providers[Host(u)]; ok {
		return f.oembed(ctx, endpoint, u.String())
	}
	return f.page(ctx, u.String())
}

func (f *Fetcher) get(ctx context.Context, u, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", accept)
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("reftitle: %s: %s", u, resp.Status)
	}
	return resp, nil
}

func (f *Fetcher) oembed(ctx context.Context, endpoint, link string) (string, error) {
	e, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	q := e.Query()
	q.Set("url", link)
	e.RawQuery = q.Encode()
	resp, err := f.get(ctx, e.String(), "application/json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&out); err != nil {
		return "", err
	}
	if t := Clean(out.Title); t != "" {
		return t, nil
	}
	return "", ErrNoTitle
}

var (
	ogTitleRe = regexp.MustCompile(`(?is)<meta\s[^>]*?property\s*=\s*["']og:title["'][^>]*>`)
	contentRe = regexp.MustCompile(`(?is)\bcontent\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	titleRe   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

func (f *Fetcher) page(ctx context.Context, link string) (string, error) {
	resp, err := f.get(ctx, link, "text/html,application/xhtml+xml")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt != "text/html" && mt != "application/xhtml+xml" {
		return "", fmt.Errorf("reftitle: %s is %s, not a page", link, mt)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return "", err
	}
	return PageTitle(string(b))
}

// PageTitle is a page's og:title, else its <title>.
func PageTitle(page string) (string, error) {
	if tag := ogTitleRe.FindString(page); tag != "" {
		if m := contentRe.FindStringSubmatch(tag); m != nil {
			if t := Clean(m[1] + m[2]); t != "" {
				return t, nil
			}
		}
	}
	if m := titleRe.FindStringSubmatch(page); m != nil {
		if t := Clean(m[1]); t != "" {
			return t, nil
		}
	}
	return "", ErrNoTitle
}

// Clean is a title as shown: entities decoded, spaces collapsed, at most
// maxTitle characters, a trailing " - YouTube" dropped.
func Clean(s string) string {
	s = strings.Join(strings.Fields(html.UnescapeString(s)), " ")
	s = strings.TrimSuffix(s, " - YouTube")
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	if r := []rune(s); len(r) > maxTitle {
		s = strings.TrimSpace(string(r[:maxTitle-1])) + "…"
	}
	return s
}
