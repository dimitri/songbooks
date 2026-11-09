// installscores is a one-off admin script (same shape/convention as
// backfillaudio/bandscampredo — see deploy/Dockerfile's own comment on
// that pattern): installs real \new TabStaff riffs and \gtab chord
// diagrams from this repo's legacy LaTeX+LilyPond corpus (Mercy/,
// MySongs/, "Other Lies"/, Trio/) into this app's actual Postgres-backed
// song documents.
//
// Never writes to an existing published (visibility='global') variant —
// for each matched song it creates a NEW personal (visibility='user')
// variant, owned by the given --as username, carrying the addition, and
// prints its /song/{id} URL for review. The published variant is
// untouched; nothing is auto-merged.
//
// Usage (inside the deploy container, same network as the db service):
//
//	installscores --db "$DATABASE_URL" --as tapoueh --legacy-root /legacy [--apply]
//
// Without --apply it only prints what it WOULD do (dry run).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"app/internal/song"
	"app/internal/store"
	"app/internal/tab"

	"github.com/jackc/pgx/v5/pgxpool"
)

var titleRE = regexp.MustCompile(`\\beginsong\{([^}]*)\}(?:\s*\[\s*by\s*=\s*\{([^}]*)\}\s*\])?`)
var gtabRE = regexp.MustCompile(`\\gtab\{([^}]*)\}\{([^}]*)\}`)
var ampRE = regexp.MustCompile(`\\&`)

func unescapeLatex(s string) string { return ampRE.ReplaceAllString(s, "&") }

// normalizeChordName mirrors internal/latexsong's own normalizeChord:
// trims whitespace and maps the songs package's native "&" flat glyph to
// this app's own "b" spelling.
func normalizeChordName(raw string) string {
	c := strings.TrimSpace(raw)
	return strings.ReplaceAll(c, "&", "b")
}

func findTitle(dir string) (title, artist string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tex") {
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			if m := titleRE.FindSubmatch(b); m != nil {
				return unescapeLatex(string(m[1])), unescapeLatex(string(m[2]))
			}
		}
	}
	return "", ""
}

// sectionLabelFor guesses the target section label from a riff/score
// filename — "intro.ly" -> "Intro", "bass.ly"/"verse.ly" -> "Verse",
// "chorus.ly" -> "Chorus", "bridge.ly" -> "Bridge" — the same handful of
// names this corpus's own .ly files are consistently given.
func sectionLabelFor(filename string) string {
	stem := strings.ToLower(strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)))
	switch {
	case strings.Contains(stem, "intro"):
		return "Intro"
	case strings.Contains(stem, "chorus"):
		return "Chorus"
	case strings.Contains(stem, "bridge"):
		return "Bridge"
	case strings.Contains(stem, "outro"):
		return "Outro"
	case strings.Contains(stem, "bass"), strings.Contains(stem, "verse"):
		return "Verse"
	default:
		return "Intro"
	}
}

type riffMatch struct {
	relPath          string
	title, artist    string
	sectionLabelHint string
	lySource         string
}

type chordMatch struct {
	relPath       string
	title, artist string
	refs          []song.ChordRef
}

func surveyRiffs(root string) []riffMatch {
	var out []riffMatch
	for _, book := range []string{"Mercy", "MySongs", "Other Lies", "Trio"} {
		filepath.Walk(filepath.Join(root, book), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || filepath.Ext(path) != ".ly" || filepath.Base(path) == "header.ly" {
				return nil
			}
			if filepath.Base(filepath.Dir(path)) == "tmp-ly" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			riff, err := tab.ParseScore(string(data))
			if err != nil || riff == nil || len(riff.Bars) == 0 {
				return nil
			}
			title, artist := findTitle(filepath.Dir(path))
			if title == "" {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			out = append(out, riffMatch{relPath: rel, title: title, artist: artist, sectionLabelHint: sectionLabelFor(path), lySource: string(data)})
			return nil
		})
	}
	return out
}

func surveyChords(root string) []chordMatch {
	var out []chordMatch
	for _, book := range []string{"Mercy", "MySongs", "Other Lies", "Trio"} {
		filepath.Walk(filepath.Join(root, book), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || filepath.Ext(path) != ".tex" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			matches := gtabRE.FindAllSubmatch(data, -1)
			if len(matches) == 0 {
				return nil
			}
			title, artist := findTitle(filepath.Dir(path))
			if title == "" {
				return nil
			}
			var refs []song.ChordRef
			for _, m := range matches {
				refs = append(refs, song.ChordRef{Name: normalizeChordName(string(m[1])), Spec: strings.TrimSpace(string(m[2]))})
			}
			rel, _ := filepath.Rel(root, path)
			out = append(out, chordMatch{relPath: rel, title: title, artist: artist, refs: refs})
			return nil
		})
	}
	return out
}

func main() {
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "Postgres connection string")
	asUsername := flag.String("as", "", "username to own the new review variants")
	legacyRoot := flag.String("legacy-root", "/legacy", "path to the legacy corpus (Mercy/, MySongs/, Other Lies/, Trio/)")
	apply := flag.Bool("apply", false, "actually write; without this flag, only prints what would happen")
	publicBase := flag.String("public-base", "https://songbooks.tapoueh.org", "base URL to print song links against")
	flag.Parse()

	if *dbURL == "" || *asUsername == "" {
		log.Fatal("usage: installscores --db <url> --as <username> --legacy-root <path> [--apply]")
	}

	ctx := context.Background()
	st, err := store.New(ctx, *dbURL)
	if err != nil {
		log.Fatalf("store.New: %v", err)
	}
	pool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		log.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	var personID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM people WHERE username = $1`, *asUsername).Scan(&personID); err != nil {
		log.Fatalf("no such person %q: %v", *asUsername, err)
	}

	riffs := surveyRiffs(*legacyRoot)
	chordsByWork := map[string][]song.ChordRef{} // keyed by "norm_title|norm_artist"
	for _, cm := range surveyChords(*legacyRoot) {
		key := strings.ToLower(strings.TrimSpace(cm.title)) + "|" + strings.ToLower(strings.TrimSpace(cm.artist))
		chordsByWork[key] = append(chordsByWork[key], cm.refs...)
	}

	// Group riffs by target work, so one song with several riff files
	// (e.g. Kawasaki's bass.ly + chorus.ly) becomes ONE review variant,
	// not several competing ones.
	type pending struct {
		title, artist string
		riffs         []riffMatch
	}
	byWork := map[string]*pending{}
	var order []string
	for _, rm := range riffs {
		key := strings.ToLower(strings.TrimSpace(rm.title)) + "|" + strings.ToLower(strings.TrimSpace(rm.artist))
		if byWork[key] == nil {
			byWork[key] = &pending{title: rm.title, artist: rm.artist}
			order = append(order, key)
		}
		byWork[key].riffs = append(byWork[key].riffs, rm)
	}
	// Also include works that have ONLY chord diagrams (e.g. Gone, Vanity
	// Rewinds), not matched by any riff.
	for _, cm := range surveyChords(*legacyRoot) {
		key := strings.ToLower(strings.TrimSpace(cm.title)) + "|" + strings.ToLower(strings.TrimSpace(cm.artist))
		if byWork[key] == nil {
			byWork[key] = &pending{title: cm.title, artist: cm.artist}
			order = append(order, key)
		}
	}

	for _, key := range order {
		p := byWork[key]
		fmt.Printf("\n=== %s — %s ===\n", p.title, p.artist)

		var variantID int64
		var body string
		// Prefer the published catalog (global) variant, but a real song
		// can also live only as a band's own repertoire copy (visibility
		// 'band') — confirmed for this repo's own "Other Lies" originals,
		// owned by that band, never published to the global catalog.
		// Either way the new review draft below is always created as the
		// requesting person's own personal copy, never written back onto
		// whichever variant was matched here.
		err := pool.QueryRow(ctx, `
			SELECT s.id, s.body FROM songs s
			JOIN works w ON w.id = s.work_id
			WHERE w.norm_title = lower(trim($1)) AND w.norm_artist = lower(trim($2))
			ORDER BY (s.visibility = 'global') DESC, (s.visibility = 'band') DESC, s.id
			LIMIT 1`, p.title, p.artist).Scan(&variantID, &body)
		if err != nil {
			fmt.Printf("  NOT FOUND in catalog (title/artist mismatch?) — skipped\n")
			continue
		}
		fmt.Printf("  matched published variant id=%d\n", variantID)

		sg := song.Parse(body)

		for _, rm := range p.riffs {
			idx := -1
			for i, sec := range sg.Sections {
				if strings.EqualFold(sec.Label, rm.sectionLabelHint) {
					idx = i
					break
				}
			}
			if idx < 0 {
				sg.Sections = append(sg.Sections, song.Section{Label: rm.sectionLabelHint})
				idx = len(sg.Sections) - 1
				fmt.Printf("  + new section %q (none existed) for %s\n", rm.sectionLabelHint, rm.relPath)
			} else {
				fmt.Printf("  score -> section %q (#%d) from %s\n", sg.Sections[idx].Label, idx, rm.relPath)
			}
			// The riff's own real LilyPond source goes straight into the
			// body text now (song[label]: ..., inline — see
			// song.ScoreVoice's own doc comment), not a separate Postgres
			// row: no second save pass needed below any more. Label is the
			// source file's own base name, minus extension — descriptive,
			// and distinct across the (rare) case of two riffs landing on
			// the same section.
			label := strings.TrimSuffix(filepath.Base(rm.relPath), filepath.Ext(rm.relPath))
			sg.Sections[idx].Scores = append(sg.Sections[idx].Scores, song.ScoreVoice{Label: label, Body: rm.lySource})
		}

		if refs, ok := chordsByWork[key]; ok {
			sg.Chords = append(sg.Chords, refs...)
			fmt.Printf("  + %d chord diagram(s)\n", len(refs))
		}

		newText := song.Write(sg)

		if !*apply {
			fmt.Printf("  (dry run — would create a new personal variant for %s)\n", *asUsername)
			continue
		}

		newID, err := st.CreateVariant(ctx, store.OwnerPerson, personID, p.title, p.artist, personID)
		if err != nil {
			fmt.Printf("  ERROR creating variant: %v\n", err)
			continue
		}
		_, version, err := st.LoadVariantRaw(newID)
		if err != nil {
			fmt.Printf("  ERROR loading fresh variant: %v\n", err)
			continue
		}
		if _, err := st.SaveVariantRaw(newID, newText, version, personID); err != nil {
			fmt.Printf("  ERROR saving text: %v\n", err)
			continue
		}
		fmt.Printf("  -> %s/song/%d\n", *publicBase, newID)
	}
}
