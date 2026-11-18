// Package i18n translates the app's English into its other languages
// (French so far): docs/i18n.md.
//
// The catalogs are keyed by the English text itself, as it is in the
// templates and scripts: a template says {{t "Add a song"}}, the French
// catalog maps "Add a song" to "Ajouter un morceau", and a string with no
// translation yet shows in English. The catalogs are JSON files, one per
// part of the app (fr/*.json), merged at start; the files named js-*.json
// are the scripts' strings, served to the browser (/i18n/fr.js).
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strings"
	"time"
)

//go:embed fr/*.json
var catalogFS embed.FS

// Langs are the languages the app speaks, English first (the source).
var Langs = []string{"en", "fr"}

// Names are each language's name, in itself.
var Names = map[string]string{"en": "English", "fr": "Français"}

var (
	catalogs   = map[string]map[string]string{} // lang -> English -> translation
	jsCatalogs = map[string]map[string]string{} // the scripts' part
)

func init() {
	for _, lang := range Langs[1:] {
		catalogs[lang] = map[string]string{}
		jsCatalogs[lang] = map[string]string{}
		files, err := fs.Glob(catalogFS, lang+"/*.json")
		if err != nil {
			log.Fatal(err)
		}
		sort.Strings(files)
		for _, f := range files {
			raw, err := catalogFS.ReadFile(f)
			if err != nil {
				log.Fatal(err)
			}
			var m map[string]string
			if err := json.Unmarshal(raw, &m); err != nil {
				log.Fatalf("i18n: %s: %v", f, err)
			}
			js := strings.HasPrefix(f[len(lang)+1:], "js-")
			for k, v := range m {
				if v == "" {
					continue
				}
				catalogs[lang][k] = v
				if js {
					jsCatalogs[lang][k] = v
				}
			}
		}
	}
}

// Valid reports whether lang is one the app speaks.
func Valid(lang string) bool {
	for _, l := range Langs {
		if l == lang {
			return true
		}
	}
	return false
}

// T is msg in lang (msg itself when untranslated); with args, msg is a
// fmt format, translated first.
func T(lang, msg string, args ...any) string {
	if lang != "en" {
		if v, ok := catalogs[lang][msg]; ok {
			msg = v
		}
	}
	if len(args) > 0 {
		return fmt.Sprintf(msg, args...)
	}
	return msg
}

// N is one or other (each a fmt format with the count, "%d song" and
// "%d songs") for n, in lang: French says 0 and 1 in the singular.
func N(lang string, n int, one, other string) string {
	single := n == 1
	if lang == "fr" {
		single = n == 0 || n == 1
	}
	if single {
		return T(lang, one, n)
	}
	return T(lang, other, n)
}

// JS is the scripts' catalog for lang, as JSON.
func JS(lang string) []byte {
	b, _ := json.Marshal(jsCatalogs[lang])
	return b
}

// Accept is the language an Accept-Language header asks for first, among
// the app's, "" when none.
func Accept(header string) string {
	best, bestQ := "", -1.0
	for _, part := range strings.Split(header, ",") {
		tag, q := strings.TrimSpace(part), 1.0
		if i := strings.Index(tag, ";"); i >= 0 {
			fmt.Sscanf(strings.TrimSpace(tag[i+1:]), "q=%g", &q)
			tag = tag[:i]
		}
		base := strings.ToLower(strings.SplitN(tag, "-", 2)[0])
		if Valid(base) && q > bestQ {
			best, bestQ = base, q
		}
	}
	return best
}

var (
	frDays      = []string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"}
	frDaysShort = []string{"dim.", "lun.", "mar.", "mer.", "jeu.", "ven.", "sam."}
	frMonths    = []string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}
	frMonthsAbb = []string{"janv.", "févr.", "mars", "avr.", "mai", "juin", "juil.", "août", "sept.", "oct.", "nov.", "déc."}
)

// Date formats t with a Go layout, its day and month names in lang:
// "Monday 2 January" is "lundi 2 janvier" in French. Layouts with a
// month before its day ("Jan 2") read best rewritten day first.
func Date(lang string, t time.Time, layout string) string {
	if lang != "fr" {
		return t.Format(layout)
	}
	// Each name in the layout becomes a placeholder, formatted, then
	// replaced by its French: Go only knows English names.
	repl := []struct{ in, mark, out string }{
		{"Monday", "\x01", frDays[t.Weekday()]},
		{"Mon", "\x02", frDaysShort[t.Weekday()]},
		{"January", "\x03", frMonths[t.Month()-1]},
		{"Jan", "\x04", frMonthsAbb[t.Month()-1]},
	}
	l := layout
	for _, r := range repl {
		l = strings.ReplaceAll(l, r.in, r.mark)
	}
	out := t.Format(l)
	for _, r := range repl {
		out = strings.ReplaceAll(out, r.mark, r.out)
	}
	// French times: 15:04 stays; "3:04 PM" isn't French.
	out = strings.ReplaceAll(out, " AM", "")
	out = strings.ReplaceAll(out, " PM", "")
	return out
}

var (
	enNotes = []string{"C", "C♯", "D", "E♭", "E", "F", "F♯", "G", "A♭", "A", "B♭", "B"}
	frNotes = []string{"do", "do♯", "ré", "mi♭", "mi", "fa", "fa♯", "sol", "la♭", "la", "si♭", "si"}
)

// NoteName is a MIDI note's name, a singer's way: C4 (60) in English,
// do3 in French (middle C is do3 there).
func NoteName(lang string, midi int) string {
	if lang == "fr" {
		return fmt.Sprintf("%s%d", frNotes[midi%12], midi/12-2)
	}
	return fmt.Sprintf("%s%d", enNotes[midi%12], midi/12-1)
}
