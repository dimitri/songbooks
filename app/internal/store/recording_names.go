package store

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// How a recording's file name is read: name templates, after Audacity's
// "Export Multiple" (a file per label, "01-Label", numbered before the
// label or after a prefix), written with fields between braces:
//
//	{n}      the song's place in the session: 07
//	{set}    the set, before the place: 2 - 16 - Public
//	{title}  the song
//	{take}   a take's number, after "Take" (or "Prise")
//	{date}   2025-03-13, 20250313, 250313 or 13-03-2025
//	{*}      anything, not read: the band's name, a venue
//
// Spaces and dashes around a field are loose: "{n} - {title}" reads
// "07 - Gone", "07- Gone" and "07 -Gone" (and "{set} - {n}" reads
// "2-5" too). A band's own templates are tried first, then the built-in
// ones, most specific first: the first that reads the whole name wins.
var builtinTemplates = []string{
	"{set} - {n} - {title} - Take {take} - {date}",
	"{set} - {n} - {title} - {date}",
	"{set}-{n} {title} - {date}",
	"{set}-{n} - {title}",
	"{set}-{n} {title}",
	"{n} - {title} - Take {take} - {date}",
	"{n} - Take {take} - {title} - {date}",
	"{n} - {title} - {date}",
	"{n} - {title} - Take {take}",
	"{n} - {title}",
	"{n}-{title}",
	"{n} {title}",
	"{title} - Take {take} - {date}",
	"{title} - {date}",
	"{date} - {title}",
	"{date} {title}",
	"{title} - Take {take}",
	"{title} Take {take}",
}

// nameTemplate is a template made ready to read names.
type nameTemplate struct {
	Text   string
	re     *regexp.Regexp
	fields []string // the capturing groups' fields, in order
}

var (
	templateField = regexp.MustCompile(`\{(n|set|title|take|date|\*)\}`)
	literalPart   = regexp.MustCompile(`\s*[-_]\s*|\s+|[^-_\s]+`)
	fieldPattern  = map[string]string{
		"n": `(\d{1,3})`, "set": `(\d{1,2})`, "take": `(\d{1,2})`, "title": `(.+?)`, "*": `(.+?)`,
		"date": `(\d{4}-\d{2}-\d{2}|\d{8}|\d{6}|\d{2}-\d{2}-\d{4}|\d{4}\.\d{2}\.\d{2})`,
	}
)

// compileTemplate makes a template ready; it needs a {title} or a {n}.
func compileTemplate(text string) (*nameTemplate, error) {
	text = strings.TrimSpace(text)
	locs := templateField.FindAllStringSubmatchIndex(text, -1)
	if len(locs) == 0 || !strings.Contains(text, "{title}") && !strings.Contains(text, "{n}") {
		return nil, fmt.Errorf("a name template has a {title} or a {n}: %q", text)
	}
	if bad := regexp.MustCompile(`\{[^}]*\}`).FindAllString(templateField.ReplaceAllString(text, ""), 1); len(bad) > 0 {
		return nil, fmt.Errorf("unknown field %s: use {n}, {set}, {title}, {take}, {date} or {*}", bad[0])
	}
	var b strings.Builder
	b.WriteString(`(?i)^\s*`)
	t := &nameTemplate{Text: text}
	at := 0
	// Literal text: a dash or an underscore with any spaces around reads
	// any of them; spaces read one or more ("Take" then none or more);
	// "Take" reads "Prise" too.
	literal := func(s string) {
		parts := literalPart.FindAllString(s, -1)
		for i, part := range parts {
			p := strings.TrimSpace(part)
			switch {
			case p == "" && i > 0 && strings.EqualFold(strings.TrimSpace(parts[i-1]), "take"):
				b.WriteString(`\s*`)
			case p == "":
				b.WriteString(`\s+`)
			case p == "-" || p == "_":
				b.WriteString(`\s*[-_]\s*`)
			case strings.EqualFold(p, "take"):
				b.WriteString(`(?:take|prise)`)
			default:
				b.WriteString(regexp.QuoteMeta(p))
			}
		}
	}
	for _, l := range locs {
		literal(text[at:l[0]])
		f := text[l[2]:l[3]]
		b.WriteString(fieldPattern[f])
		t.fields = append(t.fields, f)
		at = l[1]
	}
	literal(text[at:])
	b.WriteString(`\s*$`)
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, err
	}
	t.re = re
	return t, nil
}

// builtins is the built-in templates, compiled once.
var builtins = func() []*nameTemplate {
	var out []*nameTemplate
	for _, s := range builtinTemplates {
		t, err := compileTemplate(s)
		if err != nil {
			panic(err)
		}
		out = append(out, t)
	}
	return out
}()

// templatesFor is a band's own templates (valid ones) before the built-in.
func templatesFor(own []string) []*nameTemplate {
	var out []*nameTemplate
	for _, s := range own {
		if t, err := compileTemplate(s); err == nil {
			out = append(out, t)
		}
	}
	return append(out, builtins...)
}

// nameRead is what a template reads in a name.
type nameRead struct {
	Template string
	Set      *int
	Position *int
	Take     *int
	Title    string
	Date     string // 2025-03-13
}

// readName reads a file's name (without its extension) with the first
// template that reads all of it; ok is false when none does.
func readName(stem string, tmpls []*nameTemplate) (r nameRead, ok bool) {
	for _, t := range tmpls {
		m := t.re.FindStringSubmatch(stem)
		if m == nil {
			continue
		}
		r = nameRead{Template: t.Text}
		for i, f := range t.fields {
			v := strings.TrimSpace(m[i+1])
			switch f {
			case "n", "set", "take":
				n, _ := strconv.Atoi(v)
				switch f {
				case "n":
					r.Position = &n
				case "set":
					r.Set = &n
				default:
					r.Take = &n
				}
			case "title":
				r.Title = cleanTitle(v)
			case "date":
				r.Date = normalDate(v)
			}
		}
		if strings.Contains(t.Text, "{title}") && r.Title == "" {
			continue // "07 - v2": no title left
		}
		return r, true
	}
	return r, false
}

// cleanTitle drops what a title often carries that isn't the song: a
// version before it ("v2-You know I'm no good"), quotes around it.
var versionPrefix = regexp.MustCompile(`(?i)^v\d+\s*[-_ ]\s*`)

func cleanTitle(s string) string {
	s = versionPrefix.ReplaceAllString(strings.TrimSpace(s), "")
	return strings.Trim(s, `"'“”‘’ -_`)
}

// normalDate is a date as 2025-03-13, "" when it isn't one.
func normalDate(s string) string {
	digits := strings.NewReplacer("-", "", ".", "").Replace(s)
	switch {
	case isoDate.MatchString(s):
		return s
	case len(digits) == 8 && strings.Count(s, "-") == 2 && len(s) == 10 && s[2] == '-': // 13-03-2025
		return digits[4:] + "-" + digits[2:4] + "-" + digits[:2]
	case len(digits) == 8: // 20250313, 2025.03.13
		return digits[:4] + "-" + digits[4:6] + "-" + digits[6:]
	case len(digits) == 6: // 250313
		return "20" + digits[:2] + "-" + digits[2:4] + "-" + digits[4:]
	}
	return ""
}

// rawName is a file a recorder, a camera or a phone names by itself: a
// mixer's tracks (TRACK01, MASTER), a Zoom's or a Tascam's takes, a
// photo app's video. A recording all the same, but no song's: it's
// listed, never asked about.
var rawName = regexp.MustCompile(`(?i)^(` + strings.Join([]string{
	`track\s*\d+(_\d+)*`, `(part\s*\d+\s*-\s*)?master`, `zoom\d+(_\w+)?`, `dr\d+_\d+`, `ste-\d+.*`,
	`\d{6}_\d{4,6}(_\w+)?`, `\d{6}-\d{3}`, `\d{8}`, `(img|vid|mvi|dsc|pxl|mov)_?\d+.*`, `video-\d{4}-\d{2}-\d{2}.*`,
	`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`, `new recording( \d+)?`, `(voice|audio|recording)[ _-]?\d+`,
	`p\d{7}(_\d+)?`,
}, "|") + `)( \d+| \(\d+\))?$`) // a copy: "TRACK09_10 2", "IMG_2602 (1)"

// NameTemplates is bandID's own name templates.
func (st *Store) NameTemplates(ctx context.Context, bandID int64) ([]string, error) {
	var own []string
	err := st.db.QueryRow(ctx, `SELECT name_templates FROM band_recording_sources WHERE band_id = $1`, bandID).Scan(&own)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return own, err
}

// SetNameTemplates saves bandID's own name templates (each must compile)
// and reads its recordings' names again with them.
func (st *Store) SetNameTemplates(ctx context.Context, bandID int64, templates []string) error {
	var clean []string
	for _, t := range templates {
		if t = strings.TrimSpace(t); t == "" {
			continue
		}
		if _, err := compileTemplate(t); err != nil {
			return err
		}
		clean = append(clean, t)
	}
	if clean == nil {
		clean = []string{}
	}
	tag, err := st.db.Exec(ctx, `UPDATE band_recording_sources SET name_templates = $2 WHERE band_id = $1`, bandID, clean)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return st.ReparseBandRecordings(ctx, bandID)
}

// ReparseBandRecordings reads bandID's recordings' folders and names again
// (after its templates changed), then matches them again.
func (st *Store) ReparseBandRecordings(ctx context.Context, bandID int64) error {
	var band string
	var own []string
	if err := st.db.QueryRow(ctx, `SELECT b.name, coalesce(s.name_templates, '{}') FROM bands b LEFT JOIN band_recording_sources s ON s.band_id = b.id WHERE b.id = $1`, bandID).Scan(&band, &own); err != nil {
		return err
	}
	tmpls := templatesFor(own)
	rows, err := st.db.Query(ctx, `SELECT id, path, name FROM band_recordings WHERE band_id = $1`, bandID)
	if err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for rows.Next() {
		var id int64
		var dir, name string
		if err := rows.Scan(&id, &dir, &name); err != nil {
			rows.Close()
			return err
		}
		var folders []string
		if dir != "" {
			folders = strings.Split(dir, "/")
		}
		p, ok := parseRecording(folders, name, band, tmpls)
		if !ok {
			continue
		}
		batch.Queue(`UPDATE band_recordings SET kind = $2, folder_date = $3, file_date = $4, position = $5, title = $6, take = $7, raw = $8 WHERE id = $1`,
			id, p.Kind, p.FolderDate, p.FileDate, p.Position, p.Title, p.Take, p.Raw)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if err := st.db.SendBatch(ctx, batch).Close(); err != nil {
		return err
	}
	return st.ResolveBandRecordings(ctx, bandID)
}

// NameUse is how many of a band's file names a template reads, with a few
// of them ("" Template: those none reads; Raw: a recorder's own names).
type NameUse struct {
	Template string
	Own      bool
	Raw      bool
	Count    int
	Examples []string
}

// NameReport is how bandID's file names are read: each template that
// reads some, the most first, then the recorder's own names, then those
// none reads.
func (st *Store) NameReport(ctx context.Context, bandID int64) ([]NameUse, error) {
	var band string
	var own []string
	if err := st.db.QueryRow(ctx, `SELECT b.name, coalesce(s.name_templates, '{}') FROM bands b LEFT JOIN band_recording_sources s ON s.band_id = b.id WHERE b.id = $1`, bandID).Scan(&band, &own); err != nil {
		return nil, err
	}
	tmpls := templatesFor(own)
	ownSet := map[string]bool{}
	for _, t := range own {
		ownSet[strings.TrimSpace(t)] = true
	}
	rows, err := st.db.Query(ctx, `SELECT name FROM band_recordings WHERE band_id = $1 AND NOT gone ORDER BY path, name`, bandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	uses := map[string]*NameUse{}
	var order []string
	add := func(key string, u NameUse, example string) {
		if uses[key] == nil {
			uses[key] = &u
			order = append(order, key)
		}
		uses[key].Count++
		if len(uses[key].Examples) < 3 {
			uses[key].Examples = append(uses[key].Examples, example)
		}
	}
	bandKey := matchKey(band)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		stem := strings.TrimSpace(strings.TrimSuffix(name, path.Ext(name)))
		if rawName.MatchString(stem) {
			add("raw", NameUse{Raw: true}, name)
			continue
		}
		var kept []string
		for _, tok := range strings.Split(stem, " - ") {
			if bandKey == "" || matchKey(tok) != bandKey {
				kept = append(kept, tok)
			}
		}
		if r, ok := readName(strings.Join(kept, " - "), tmpls); ok {
			add("t:"+r.Template, NameUse{Template: r.Template, Own: ownSet[r.Template]}, name)
		} else {
			add("none", NameUse{}, name)
		}
	}
	out := make([]NameUse, 0, len(order))
	for _, k := range order {
		out = append(out, *uses[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		rank := func(u NameUse) int {
			switch {
			case u.Template != "":
				return 0
			case u.Raw:
				return 1
			}
			return 2
		}
		if rank(out[i]) != rank(out[j]) {
			return rank(out[i]) < rank(out[j])
		}
		return out[i].Count > out[j].Count
	})
	return out, rows.Err()
}

// UnmatchedFile is a recording of a band's that names none of its songs
// and that no correction speaks for: one to say which song it is.
type UnmatchedFile struct {
	FileID int64
	Kind   string
	Path   string
	Name   string
	Title  string
	Date   *time.Time
}

// UnmatchedFiles is bandID's recordings of rehearsals, concerts and
// auditions with no song, the latest sessions first; a recorder's own
// names are left out unless raw.
func (st *Store) UnmatchedFiles(ctx context.Context, bandID int64, raw bool) ([]UnmatchedFile, error) {
	rows, err := st.db.Query(ctx, `
		SELECT file_id, kind, path, name, title, session_date FROM band_recordings
		WHERE band_id = $1 AND NOT gone AND variant_id IS NULL AND raw = $2 AND kind IN ('rehearsal', 'concert', 'audition')
		  AND file_id::text NOT IN (SELECT from_value FROM band_recording_fixes WHERE band_id = $1 AND kind = 'file')
		ORDER BY session_date DESC NULLS LAST, path, name`, bandID, raw)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UnmatchedFile
	for rows.Next() {
		var f UnmatchedFile
		if err := rows.Scan(&f.FileID, &f.Kind, &f.Path, &f.Name, &f.Title, &f.Date); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// TemplateTry is what a template would read in a band's file names: how
// many it reads, and the first few with what it read.
type TemplateTry struct {
	Count    int
	Total    int
	Examples []TemplateTryRow
}

// TemplateTryRow is a file name and what a template reads in it.
type TemplateTryRow struct {
	Name     string
	Set      *int
	Position *int
	Take     *int
	Title    string
	Date     string
}

// TryTemplate reads bandID's file names with one template alone.
func (st *Store) TryTemplate(ctx context.Context, bandID int64, text string) (*TemplateTry, error) {
	t, err := compileTemplate(text)
	if err != nil {
		return nil, err
	}
	rows, err := st.db.Query(ctx, `SELECT name FROM band_recordings WHERE band_id = $1 AND NOT gone AND NOT raw ORDER BY session_date DESC NULLS LAST, path, name`, bandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &TemplateTry{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out.Total++
		r, ok := readName(strings.TrimSpace(strings.TrimSuffix(name, path.Ext(name))), []*nameTemplate{t})
		if !ok {
			continue
		}
		out.Count++
		if len(out.Examples) < 8 {
			out.Examples = append(out.Examples, TemplateTryRow{Name: name, Set: r.Set, Position: r.Position, Take: r.Take, Title: r.Title, Date: r.Date})
		}
	}
	return out, rows.Err()
}
