package store

import (
	"context"
	"io"
	"log"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/ledongthuc/pdf"
)

// An author's book (a course's PDF) links each lesson to its video: the
// lesson's title, clicked, opens it on YouTube (Florent's books also print
// it as a QR code, the same link). LessonVideosFromBooks reads those links
// and the words printed around each one, and gives each lesson without a
// video of its own the one whose words name it best.

// YouTubeID is a YouTube link's video id: watch?v=, youtu.be/, shorts/,
// embed/ or live/; "" for anything else.
func YouTubeID(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(strings.TrimPrefix(u.Hostname(), "www."), "m.")
	switch host {
	case "youtu.be":
		return strings.Split(strings.TrimPrefix(u.Path, "/"), "/")[0]
	case "youtube.com", "youtube-nocookie.com":
		if u.Path == "/watch" {
			return u.Query().Get("v")
		}
		parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		if len(parts) >= 2 && (parts[0] == "shorts" || parts[0] == "embed" || parts[0] == "live") {
			return parts[1]
		}
	}
	return ""
}

// bookVideo is a video a book links to, with the words printed around
// each of its links.
type bookVideo struct {
	ID     string
	Pages  []int
	Page   int        // the first page linking it among few links (a lesson's own page); 0: none
	Around []linkText // the text at each of its links
}

// pdfLink is one link of a PDF page: where it is (PDF units, from the
// bottom left) and where it goes.
type pdfLink struct {
	Page           int
	URI            string
	X1, Y1, X2, Y2 float64
}

// pdfLinks reads every link annotation of the PDF at path.
func pdfLinks(path string) ([]pdfLink, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []pdfLink
	for i := 1; i <= r.NumPage(); i++ {
		annots := r.Page(i).V.Key("Annots")
		for j := 0; j < annots.Len(); j++ {
			a := annots.Index(j)
			if a.Key("Subtype").Name() != "Link" {
				continue
			}
			uri := a.Key("A").Key("URI").RawString()
			if uri == "" {
				continue
			}
			rect := a.Key("Rect")
			out = append(out, pdfLink{Page: i, URI: uri,
				X1: rect.Index(0).Float64(), Y1: rect.Index(1).Float64(), X2: rect.Index(2).Float64(), Y2: rect.Index(3).Float64()})
		}
	}
	return out, nil
}

// textRun is a piece of text a page shows, at the height of its
// baseline (PDF units, from the bottom).
type textRun struct {
	Y    float64
	Text string
}

// affine is a PDF transformation matrix [a b c d e f].
type affine [6]float64

var identity = affine{1, 0, 0, 1, 0, 0}

// times is m then n: a point goes through m first.
func (m affine) times(n affine) affine {
	return affine{
		m[0]*n[0] + m[1]*n[2], m[0]*n[1] + m[1]*n[3],
		m[2]*n[0] + m[3]*n[2], m[2]*n[1] + m[3]*n[3],
		m[4]*n[0] + m[5]*n[2] + n[4], m[4]*n[1] + m[5]*n[3] + n[5],
	}
}

func affineOf(v pdf.Value) affine {
	if v.Len() != 6 {
		return identity
	}
	var m affine
	for i := range m {
		m[i] = v.Index(i).Float64()
	}
	return m
}

// pageText is the text runs of a page, in the order it shows them: its
// content, and the forms (XObjects) it draws, where tools like Canva put
// all of a page's text — which pdf's own text extraction doesn't open.
func pageText(p pdf.Page) []textRun {
	var out []textRun
	var walk func(content pdf.Value, res pdf.Value, ctm affine, depth int)
	walk = func(content pdf.Value, res pdf.Value, ctm affine, depth int) {
		if depth > 8 {
			return
		}
		type state struct{ ctm affine }
		var stack []state
		var tm, tlm affine
		var leading float64
		var font pdf.Font
		show := func(raw string) {
			if raw == "" || font.V.IsNull() {
				return
			}
			m := tm.times(ctm)
			out = append(out, textRun{Y: m[5], Text: font.Encoder().Decode(raw)})
		}
		moveLine := func(tx, ty float64) {
			tlm = affine{1, 0, 0, 1, tx, ty}.times(tlm)
			tm = tlm
		}
		pdf.Interpret(content, func(stk *pdf.Stack, op string) {
			args := make([]pdf.Value, stk.Len())
			for i := len(args) - 1; i >= 0; i-- {
				args[i] = stk.Pop()
			}
			switch op {
			case "q":
				stack = append(stack, state{ctm})
			case "Q":
				if n := len(stack); n > 0 {
					ctm, stack = stack[n-1].ctm, stack[:n-1]
				}
			case "cm":
				if len(args) == 6 {
					var m affine
					for i := range m {
						m[i] = args[i].Float64()
					}
					ctm = m.times(ctm)
				}
			case "BT":
				tm, tlm = identity, identity
			case "Tf":
				if len(args) == 2 {
					font = pdf.Font{V: res.Key("Font").Key(args[0].Name())}
				}
			case "TL":
				if len(args) == 1 {
					leading = args[0].Float64()
				}
			case "Td", "TD":
				if len(args) == 2 {
					if op == "TD" {
						leading = -args[1].Float64()
					}
					moveLine(args[0].Float64(), args[1].Float64())
				}
			case "Tm":
				if len(args) == 6 {
					var m affine
					for i := range m {
						m[i] = args[i].Float64()
					}
					tm, tlm = m, m
				}
			case "T*":
				moveLine(0, -leading)
			case "Tj":
				if len(args) == 1 {
					show(args[0].RawString())
				}
			case "'", "\"":
				moveLine(0, -leading)
				if len(args) > 0 {
					show(args[len(args)-1].RawString())
				}
			case "TJ":
				if len(args) == 1 {
					var b strings.Builder
					for i := 0; i < args[0].Len(); i++ {
						if e := args[0].Index(i); e.Kind() == pdf.String {
							b.WriteString(e.RawString())
						}
					}
					show(b.String())
				}
			case "Do":
				if len(args) == 1 {
					x := res.Key("XObject").Key(args[0].Name())
					if x.Key("Subtype").Name() == "Form" {
						xres := x.Key("Resources")
						if xres.IsNull() {
							xres = res
						}
						walk(x, xres, affineOf(x.Key("Matrix")).times(ctm), depth+1)
					}
				}
			}
		})
	}
	walk(p.V.Key("Contents"), p.Resources(), identity, 0)
	return out
}

// bookVideos is the YouTube videos the PDF at path links to, each with
// the words printed on the lines around its links: a lesson's title, its
// subtitle, an artist's name.
func bookVideos(path string) ([]bookVideo, error) {
	links, err := pdfLinks(path)
	if err != nil {
		return nil, err
	}
	f, r, err := pdf.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	pages := map[int][]textRun{}
	// A page of many links (a table of contents) has them a line apart:
	// only each link's own line names it there.
	perPage := map[int]int{}
	for _, l := range links {
		if YouTubeID(l.URI) != "" {
			perPage[l.Page]++
		}
	}
	byID := map[string]*bookVideo{}
	var order []string
	for _, l := range links {
		id := YouTubeID(l.URI)
		if id == "" {
			continue
		}
		if _, ok := pages[l.Page]; !ok {
			pages[l.Page] = pageText(r.Page(l.Page))
		}
		v := byID[id]
		if v == nil {
			v = &bookVideo{ID: id}
			byID[id] = v
			order = append(order, id)
		}
		v.Pages = append(v.Pages, l.Page)
		if perPage[l.Page] <= 3 && (v.Page == 0 || l.Page < v.Page) {
			v.Page = l.Page
		}
		// The link's own lines, and one either side: a title over two
		// lines, a subtitle under it.
		// The link's own text (a lesson's title), and the lines near it
		// (its subtitle, an artist) unless the page is a list of links.
		near := 30.0
		if perPage[l.Page] > 3 {
			near = 2
		}
		v.Around = append(v.Around, linkText{
			Own:  textBetween(pages[l.Page], l.Y1-2, l.Y2+2),
			Near: textBetween(pages[l.Page], l.Y1-near, l.Y2+near),
		})
	}
	out := make([]bookVideo, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

// linkText is the text at a link: its own lines, and those near it.
type linkText struct{ Own, Near string }

// textBetween is the text of runs whose baseline is between y1 and y2.
func textBetween(runs []textRun, y1, y2 float64) string {
	var b strings.Builder
	for _, t := range runs {
		if t.Y >= y1 && t.Y <= y2 {
			b.WriteString(t.Text)
			b.WriteByte(' ')
		}
	}
	return matchKey(b.String())
}

// noise is what names share without saying which lesson it is.
var noise = map[string]bool{
	"a": true, "de": true, "la": true, "le": true, "les": true, "du": true, "des": true, "et": true, "en": true,
	"un": true, "une": true, "vos": true, "the": true, "of": true, "in": true, "manière": true, "maniere": true,
	"blues": true, "page": true, "tonalite": true, "niveau": true, "www": true, "fr": true, "pb": true,
}

func nameWords(s string) []string {
	var out []string
	for _, w := range strings.Fields(matchKey(s)) {
		if !noise[w] {
			out = append(out, w)
		}
	}
	return out
}

// nameScore is how well the text at a video's links names a lesson: the
// share of one of its names' words found at one link, the best — found in
// the link's own text (its title) counting more than nearby.
func nameScore(names []string, around []linkText) float64 {
	best := 0.0
	for _, t := range around {
		s := nameScoreAt(names, t.Own)
		if n := 0.8 * nameScoreAt(names, t.Near); n > s {
			s = n
		}
		if s > best {
			best = s
		}
	}
	return best
}

// numberWords are the numbers an author spells or writes ("Trois riffs",
// "3 riffs"): either one finds the other.
var numberWords = map[string]string{
	"un": "1", "une": "1", "deux": "2", "trois": "3", "quatre": "4", "cinq": "5", "six": "6", "sept": "7",
	"huit": "8", "neuf": "9", "dix": "10", "one": "1", "two": "2", "three": "3", "four": "4", "five": "5",
}

// nameScoreAt is the share of a name's words the text (matchKey'd) has,
// the best name's. A PDF's text shows in pieces, often a letter at a
// time, its spaces not where the words are: words are looked for with
// spaces aside, a short one counting half, and a name needs one of its
// long words found to score at all.
func nameScoreAt(names []string, text string) float64 {
	joined := strings.ReplaceAll(text, " ", "")
	found := func(w string) bool {
		if strings.Contains(joined, w) {
			return true
		}
		if d, ok := numberWords[w]; ok && strings.Contains(joined, d) {
			return true
		}
		for word, d := range numberWords {
			if d == w && strings.Contains(joined, word) {
				return true
			}
		}
		return false
	}
	best := 0.0
	for _, n := range names {
		ws := nameWords(n)
		var total, hit float64
		long := false
		for _, w := range ws {
			weight := 1.0
			if len(w) < 3 {
				weight = 0.5
			}
			total += weight
			if found(w) {
				hit += weight
				long = long || len(w) >= 3
			}
		}
		if total == 0 || !long {
			continue
		}
		if s := hit / total; s > best {
			best = s
		}
	}
	return best
}

// LessonVideosFromBooks gives the lessons of course songbookID that have
// no video of their own the one their book's PDF links to them, matching
// the lesson's title, its songs' and its files' names to the words printed
// around each link. A video goes to one lesson. It returns how many
// lessons it gave one.
func (st *Store) LessonVideosFromBooks(ctx context.Context, songbookID int64) (int, error) {
	files, err := st.ListSongbookFiles(ctx, []int64{songbookID})
	if err != nil {
		return 0, err
	}
	var videos []bookVideo
	for _, f := range files {
		if f.Kind != "book" {
			continue
		}
		vs, err := st.songbookFileVideos(ctx, f)
		if err != nil {
			log.Printf("songbook %d: %s: %v", songbookID, f.Filename, err)
			continue
		}
		videos = append(videos, vs...)
	}
	if len(videos) == 0 {
		return 0, nil
	}
	lessons, _, err := st.ListLessons(ctx, songbookID)
	if err != nil {
		return 0, err
	}
	taken := map[string]bool{}
	for _, l := range lessons {
		if id := YouTubeID(l.VideoURL); id != "" {
			taken[id] = true
		}
	}
	// Each lesson's names: its title, its songs', its files'.
	names := map[int64][]string{}
	for _, l := range lessons {
		names[l.ID] = append(names[l.ID], l.Title)
		for _, s := range l.Songs {
			names[l.ID] = append(names[l.ID], s.Title)
		}
	}
	for _, f := range files {
		if f.LessonID != nil && f.Kind != "book" {
			names[*f.LessonID] = append(names[*f.LessonID], lessonTitle(f.Filename))
		}
	}
	type pair struct {
		lesson Lesson
		video  string
		score  float64
	}
	var pairs []pair
	for _, l := range lessons {
		if l.VideoURL != "" {
			continue
		}
		for _, v := range videos {
			if s := nameScore(names[l.ID], v.Around); s >= 0.6 {
				pairs = append(pairs, pair{l, v.ID, s})
			}
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].score > pairs[j].score })
	chosen := map[int64]string{} // lesson: video
	for _, p := range pairs {
		if chosen[p.lesson.ID] != "" || taken[p.video] {
			continue
		}
		chosen[p.lesson.ID], taken[p.video] = p.video, true
	}

	// The book's order fills what names missed ("Cinq plans" for "5
	// plans"): between two lessons placed in the book, as many lessons
	// left as videos left on the pages between theirs go in order.
	pageOf := map[string]int{}
	for _, v := range videos {
		if v.Page > 0 && (pageOf[v.ID] == 0 || v.Page < pageOf[v.ID]) {
			pageOf[v.ID] = v.Page
		}
	}
	lessonPage := func(l Lesson) int {
		if id := YouTubeID(l.VideoURL); id != "" {
			return pageOf[id]
		}
		return pageOf[chosen[l.ID]]
	}
	for i := 0; i < len(lessons); {
		if lessonPage(lessons[i]) > 0 || lessons[i].VideoURL != "" {
			i++
			continue
		}
		j := i
		for j < len(lessons) && lessonPage(lessons[j]) == 0 && lessons[j].VideoURL == "" {
			j++
		}
		from, to := 0, 1<<30
		if i > 0 {
			from = lessonPage(lessons[i-1])
		}
		if j < len(lessons) {
			to = lessonPage(lessons[j])
		}
		var free []bookVideo
		for _, v := range videos {
			if !taken[v.ID] && v.Page >= from && v.Page <= to && from > 0 && to < 1<<30 {
				free = append(free, v)
			}
		}
		sort.SliceStable(free, func(a, b int) bool { return free[a].Page < free[b].Page })
		if len(free) == j-i {
			for k := range free {
				chosen[lessons[i+k].ID], taken[free[k].ID] = free[k].ID, true
			}
		}
		i = j
	}

	filled := 0
	for _, l := range lessons {
		if chosen[l.ID] == "" {
			continue
		}
		if _, err := st.db.Exec(ctx, `UPDATE lessons SET video_url = $2 WHERE id = $1 AND video_url = ''`,
			l.ID, "https://youtu.be/"+chosen[l.ID]); err != nil {
			return filled, err
		}
		filled++
	}
	return filled, nil
}

// songbookFileVideos is bookVideos for a stored file: written to a
// temporary file first, the tools reading it from disk.
func (st *Store) songbookFileVideos(ctx context.Context, f SongbookFile) ([]bookVideo, error) {
	tmp, err := os.CreateTemp("", "book-*.pdf")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	_, err = io.Copy(tmp, st.OpenSongbookFile(ctx, f))
	tmp.Close()
	if err != nil {
		return nil, err
	}
	return bookVideos(tmp.Name())
}
