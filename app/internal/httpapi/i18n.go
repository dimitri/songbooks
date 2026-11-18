package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"

	"app/internal/i18n"
	"app/internal/store"
)

// The language a page is in (docs/i18n.md): the member's own choice; else
// the language of the community or band the page belongs to; else the
// browser's; else English. Every response goes through a langWriter that
// knows its request (and, once logged in, its person: requireAuth and
// optionalPerson tell it), so render can pick the pages in that language
// without each handler passing it along.

type langWriter struct {
	http.ResponseWriter
	r      *http.Request
	person *store.Person
	lang   string // resolved, "" until asked
}

func (w *langWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *langWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *langWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("hijack not supported")
}

// withLang wraps every request's writer in a langWriter.
func withLang(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(&langWriter{ResponseWriter: w, r: r}, r)
	})
}

// setLangPerson tells the writer who's asking, once known.
func setLangPerson(w http.ResponseWriter, p *store.Person) {
	if lw, ok := w.(*langWriter); ok {
		lw.person = p
	}
}

// langOf is the language of the response w writes.
func (s *Server) langOf(w http.ResponseWriter) string {
	lw, ok := w.(*langWriter)
	if !ok {
		return "en"
	}
	if lw.lang == "" {
		lw.lang = s.resolveLang(lw.r.Context(), lw.r, lw.person)
	}
	return lw.lang
}

// langOfRequest is the language for a request whose writer isn't at hand
// (an email, a JSON message): the same choice.
func (s *Server) langOfRequest(r *http.Request) string {
	return s.resolveLang(r.Context(), r, personFromContext(r))
}

func (s *Server) resolveLang(ctx context.Context, r *http.Request, p *store.Person) string {
	if p != nil {
		if l, _ := s.store.PersonLocale(ctx, p.ID); l != "" {
			return l
		}
	}
	if l := s.contextLang(ctx, r); l != "" {
		return l
	}
	if l := i18n.Accept(r.Header.Get("Accept-Language")); l != "" {
		return l
	}
	return "en"
}

// contextLang is the language of the community or band the page is in:
// /communities/{id}…, /bands/{id}…, /songbooks/{id}…, a song opened from
// a songbook (?songbook=…).
func (s *Server) contextLang(ctx context.Context, r *http.Request) string {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	id := func(i int) int64 {
		if len(parts) <= i {
			return 0
		}
		n, _ := strconv.ParseInt(parts[i], 10, 64)
		return n
	}
	var l string
	switch {
	case len(parts) > 1 && parts[0] == "communities":
		l, _ = s.store.CommunityLocale(ctx, id(1))
	case len(parts) > 1 && parts[0] == "bands":
		l, _ = s.store.BandLocale(ctx, id(1))
	case len(parts) > 1 && parts[0] == "songbooks":
		l, _ = s.store.SongbookLocale(ctx, id(1))
	}
	if l == "" {
		if sb, err := strconv.ParseInt(r.URL.Query().Get("songbook"), 10, 64); err == nil && sb > 0 {
			l, _ = s.store.SongbookLocale(ctx, sb)
		}
	}
	return l
}

// handleI18nJS is GET /i18n/{lang}.js: the scripts' catalog and their t().
func (s *Server) handleI18nJS(w http.ResponseWriter, r *http.Request) {
	lang := strings.TrimSuffix(r.PathValue("file"), ".js")
	if !i18n.Valid(lang) {
		http.NotFound(w, r)
		return
	}
	langJSON, _ := json.Marshal(lang)
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Write([]byte("window.SB_LANG=" + string(langJSON) + ";window.SB_I18N=" + string(i18n.JS(lang)) + ";" + jsT))
}

// jsT is the scripts' t(): t('Saved'), t('%d songs', n) (fmt's %d and %s,
// in order), t('notation|Staff') (a context before the bar); tn(n, '%d
// song', '%d songs').
const jsT = `window.t=function(m){var s=(window.SB_I18N||{})[m],b=m.indexOf('|');if(s===undefined)s=(b>0&&m.slice(0,b).indexOf(' ')<0)?m.slice(b+1):m;var a=Array.prototype.slice.call(arguments,1),i=0;return s.replace(/%[ds]/g,function(){return String(a[i++])})};` +
	`window.tn=function(n,one,other){var single=window.SB_LANG==='fr'?(n===0||n===1):n===1;return window.t(single?one:other,n)};`

// handlePutMyLocale is PUT /api/me/locale: the person's language ("" for
// automatic).
func (s *Server) handlePutMyLocale(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Locale string `json:"locale"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetPersonLocale(r.Context(), personFromContext(r).ID, req.Locale); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePutCommunityLocale is PUT /api/communities/{id}/locale.
func (s *Server) handlePutCommunityLocale(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Locale string `json:"locale"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetCommunityLocale(r.Context(), id, req.Locale); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePutBandLocale is PUT /api/bands/{id}/locale.
func (s *Server) handlePutBandLocale(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Locale string `json:"locale"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetBandLocale(r.Context(), id, req.Locale); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
