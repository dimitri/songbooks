package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"regexp"
	"strings"
)

// A Guitar Pro 7/8 file (.gp) is a zip whose Content/score.gpif holds
// the score as XML: its <Score> header names the song, its artist, album,
// author, transcriber and copyright, and page headers and footers repeat
// them; its tracks may carry lyrics. DemoGP rewrites those and nothing
// else: the notes, the tracks, the embedded recording stay.

const gpifPath = "Content/score.gpif"

var (
	gpifField = func(tag string) *regexp.Regexp {
		return regexp.MustCompile(`(?s)<` + tag + `>(<!\[CDATA\[.*?\]\]>|[^<]*)</` + tag + `>`)
	}
	gpTitle  = gpifField("Title")
	gpArtist = gpifField("Artist")
	// Cleared: who wrote, transcribed and owns it, and what pages print.
	gpCleared = []*regexp.Regexp{
		gpifField("SubTitle"), gpifField("Album"), gpifField("Words"), gpifField("Music"),
		gpifField("WordsAndMusic"), gpifField("Copyright"), gpifField("Tabber"),
		gpifField("Instructions"), gpifField("Notices"),
		gpifField("FirstPageHeader"), gpifField("FirstPageFooter"),
		gpifField("PageHeader"), gpifField("PageFooter"),
		gpifField("OriginalFilePath"),
		// A beat's free text: anything the transcriber wrote on it.
		gpifField("FreeText"),
	}
	gpLyrics = regexp.MustCompile(`(?s)<Lyrics(\s[^>]*)?>.*?</Lyrics>`)
	gpCDATA  = regexp.MustCompile(`(?s)<!\[CDATA\[.*?\]\]>`)
	gpText   = regexp.MustCompile(`(?s)>[^<]+<`)
)

// errNotGP is a file that isn't a .gp zip (an older .gp5, say).
var errNotGP = errors.New("not a Guitar Pro 7 file")

func cdata(s string) string {
	return "<![CDATA[" + strings.ReplaceAll(s, "]]>", "") + "]]>"
}

// DemoGPIF rewrites a score.gpif: the score's title and artist set, its
// other credits and page texts emptied, every lyric emptied.
func DemoGPIF(x []byte, title, artist string) []byte {
	score := func(re *regexp.Regexp, tag, val string) {
		// Only the first one: the <Score> header comes before any track.
		loc := re.FindIndex(x)
		if loc == nil {
			return
		}
		rep := []byte("<" + tag + ">" + cdata(val) + "</" + tag + ">")
		x = append(x[:loc[0]:loc[0]], append(rep, x[loc[1]:]...)...)
	}
	score(gpTitle, "Title", title)
	score(gpArtist, "Artist", artist)
	for _, re := range gpCleared {
		x = re.ReplaceAllFunc(x, func(m []byte) []byte {
			tag := string(m[1:bytes.IndexByte(m, '>')])
			return []byte("<" + tag + ">" + cdata("") + "</" + tag + ">")
		})
	}
	x = gpLyrics.ReplaceAllFunc(x, func(m []byte) []byte {
		m = gpCDATA.ReplaceAll(m, []byte(cdata("")))
		return gpText.ReplaceAllFunc(m, func(t []byte) []byte {
			if len(bytes.TrimSpace(t[1:len(t)-1])) == 0 {
				return t
			}
			// A plain-text lyric (no CDATA): emptied, but a number
			// (an offset) stays.
			if isNumber(string(bytes.TrimSpace(t[1 : len(t)-1]))) {
				return t
			}
			return []byte("><")
		})
	})
	return x
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

// DemoGP rewrites a .gp file's score (DemoGPIF), every other entry
// copied as it was.
func DemoGP(data []byte, title, artist string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errNotGP
	}
	found := false
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		if f.Name != gpifPath {
			if err := zw.Copy(f); err != nil {
				return nil, err
			}
			continue
		}
		found = true
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		x, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		hdr := f.FileHeader
		w, err := zw.CreateHeader(&zip.FileHeader{Name: hdr.Name, Method: hdr.Method, Modified: hdr.Modified})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(DemoGPIF(x, title, artist)); err != nil {
			return nil, err
		}
	}
	if !found {
		return nil, errNotGP
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
