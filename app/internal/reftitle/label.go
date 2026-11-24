package reftitle

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// MaxLabel is the longest reference label kept, in characters (the
// Reference view's field).
const MaxLabel = 80

// Label is a catalogue song's reference label (the album, "Live at …",
// "Cover"…: version_name) made from its reference recording's title, the
// video's as YouTube gives it: what tells this recording apart, once
// what only repeats the song or the upload's wrapping is taken out. ""
// when nothing is left (then the label stays empty). The rules, in
// order:
//
//  1. The title is cut at its separators (" - ", " – ", " — ", " | ",
//     " // ", " · ", " • ") into parts, and each part's bracketed
//     groups ((…), […], {…}) are taken out of it as parts of their own.
//  2. A part of the upload's wrapping goes: every word of it one of
//     "official", "video", "audio", "lyrics", "lyric", "clip", "HD",
//     "HQ", "4K", "1080p", "remastered", "visualizer", "paroles",
//     "officiel", "vidéo"… (years allowed alongside: "Remastered 2011"),
//     and the same words trailing a part ("Hallelujah Official Video").
//     A part with any other word stays ("Live", "Acoustic", "Cover",
//     "Demo", an album, a place, a year alone).
//  3. The part that is the song's artist goes ("Oasis - …", "… by
//     Oasis", "Oasis ft. Someone"), and the part that is the song's
//     title ("… - Wonderwall"); compared without case, accents,
//     punctuation or a leading "The".
//  4. What is left, joined with ", ", quotes and spaces trimmed, at most
//     MaxLabel characters, is the label.
//
// For example, for "Someone Like You — Adele": "Adele - Someone Like You
// (Live at The Royal Albert Hall)" gives "Live at The Royal Albert
// Hall"; "Adele - Someone Like You (Official Video)" gives "" (nothing
// but the song and the wrapping).
func Label(videoTitle, songTitle, artist string) string {
	videoTitle = strings.Join(strings.Fields(videoTitle), " ")
	if videoTitle == "" {
		return ""
	}
	nt, na := fold(songTitle), fold(artist)
	ntBare := fold(bracketRE.ReplaceAllString(songTitle, " "))
	var kept []string
	for _, part := range parts(videoTitle) {
		part = trimPart(part)
		if part == "" || wrapping(part) {
			continue
		}
		part = trimPart(trailingWrapping(part))
		if part == "" {
			continue
		}
		if f := fold(part); f == "" || isArtist(f, na) || isTitle(f, nt, ntBare) ||
			(na != "" && (isTitle(strings.TrimPrefix(f, na+" "), nt, ntBare) && strings.HasPrefix(f, na+" "))) {
			continue
		}
		// "Wonderwall by Oasis": the title and the artist both.
		if na != "" {
			if i := strings.LastIndex(strings.ToLower(part), " by "); i > 0 && isArtist(fold(part[i+4:]), na) {
				head := trimPart(part[:i])
				if f := fold(head); f == "" || isTitle(f, nt, ntBare) {
					continue
				}
				part = head
			}
		}
		kept = append(kept, part)
	}
	label := strings.Join(kept, ", ")
	if utf8.RuneCountInString(label) > MaxLabel {
		label = strings.TrimSpace(string([]rune(label)[:MaxLabel]))
	}
	return label
}

// separators cut a video's title into its parts.
var separators = []string{" - ", " – ", " — ", " | ", " // ", " · ", " • ", " ~ "}

// bracketRE is a bracketed group: (…), […], {…}, 【…】.
var bracketRE = regexp.MustCompile(`\([^()]*\)|\[[^\[\]]*\]|\{[^{}]*\}|【[^【】]*】`)

// parts is the title cut at its separators, each part's bracketed
// groups made parts of their own, in order.
func parts(title string) []string {
	segs := []string{title}
	for _, sep := range separators {
		var next []string
		for _, s := range segs {
			next = append(next, strings.Split(s, sep)...)
		}
		segs = next
	}
	var out []string
	for _, s := range segs {
		groups := bracketRE.FindAllString(s, -1)
		rest := bracketRE.ReplaceAllString(s, " ")
		out = append(out, rest)
		for _, g := range groups {
			out = append(out, g[len(firstRune(g)):len(g)-len(lastRune(g))])
		}
	}
	return out
}

func firstRune(s string) string {
	_, n := utf8.DecodeRuneInString(s)
	return s[:n]
}

func lastRune(s string) string {
	_, n := utf8.DecodeLastRuneInString(s)
	return s[len(s)-n:]
}

// trimPart trims spaces, quotes and stray punctuation around a part.
func trimPart(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(`"'“”‘’«»-–—|:,;/~·•*`, r)
	})
}

// wrappingWords are the words of an upload's wrapping, folded.
var wrappingWords = map[string]bool{
	"official": true, "officiel": true, "officielle": true, "oficial": true,
	"music": true, "video": true, "videoclip": true, "clip": true, "audio": true,
	"lyric": true, "lyrics": true, "paroles": true, "letra": true, "testo": true,
	"with": true, "avec": true, "and": true, "et": true, "w": true, "+": true, "&": true,
	"hd": true, "hq": true, "4k": true, "1080p": true, "720p": true, "480p": true, "uhd": true, "high": true, "quality": true,
	"remaster": true, "remastered": true, "remasterise": true, "remasterisee": true,
	"visualizer": true, "visualiser": true, "mv": true, "m/v": true, "explicit": true, "clean": true,
	"full": true, "song": true, "the": true, "en": true, "francais": true,
	"sous": true, "titres": true, "subtitles": true, "sub": true, "subs": true, "vostfr": true,
}

// strongWrapping are the words that say a trailing run is the wrapping
// (not "Video" in "Video Killed the Radio Star").
var strongWrapping = map[string]bool{
	"official": true, "officiel": true, "officielle": true, "oficial": true,
	"lyrics": true, "lyric": true, "paroles": true, "hd": true, "hq": true, "4k": true,
	"remastered": true, "remaster": true, "visualizer": true, "visualiser": true, "clip": true,
}

var yearRE = regexp.MustCompile(`^(19|20)\d\d$`)

// wordsOf is a part's words, folded, punctuation dropped.
func wordsOf(s string) []string {
	return strings.Fields(fold(s))
}

// wrapping reports whether every word of part is the wrapping's (a year
// allowed beside them), with at least one that is.
func wrapping(part string) bool {
	ws := wordsOf(part)
	any := false
	for _, w := range ws {
		switch {
		case wrappingWords[w]:
			any = true
		case yearRE.MatchString(w):
		default:
			return false
		}
	}
	return any
}

// trailingWrapping drops a run of the wrapping's words ending part, when
// it holds one of strongWrapping ("Hallelujah Official Video").
func trailingWrapping(part string) string {
	fields := strings.Fields(part)
	cut, strong := len(fields), false
	for i := len(fields) - 1; i > 0; i-- {
		w := fold(fields[i])
		if !wrappingWords[w] && !yearRE.MatchString(w) {
			break
		}
		if strongWrapping[w] {
			strong = true
		}
		cut = i
	}
	if !strong {
		return part
	}
	return strings.Join(fields[:cut], " ")
}

// isTitle reports whether the folded part f is the song's folded title
// (nt, or ntBare: without its own bracketed groups).
func isTitle(f, nt, ntBare string) bool {
	return f != "" && (f == nt || f == ntBare)
}

// isArtist reports whether the folded part f is the folded artist na,
// alone or featuring someone ("oasis ft someone").
func isArtist(f, na string) bool {
	if na == "" {
		return false
	}
	if f == na {
		return true
	}
	for _, ft := range []string{" ft ", " feat ", " featuring ", " x ", " and "} {
		if strings.HasPrefix(f, na+ft) {
			return true
		}
	}
	return false
}

// fold is s compared loosely: lower case, without accents or
// punctuation, "&" as "and", a leading "the" dropped, spaces collapsed.
func fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r == '&':
			b.WriteString(" and ")
		case r == '/' || r == '+':
			b.WriteRune(r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '\'' || r == '’' || r == '.':
			// "Ben E. King", "Don't": joined, not split.
		default:
			b.WriteByte(' ')
		}
	}
	f := strings.Join(strings.Fields(b.String()), " ")
	return strings.TrimPrefix(f, "the ")
}
