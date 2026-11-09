package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"app/internal/tab"
)

var titleRE = regexp.MustCompile(`\\beginsong\{([^}]*)\}(?:\s*\[\s*by\s*=\s*\{([^}]*)\}\s*\])?`)
var gtabRE = regexp.MustCompile(`\\gtab\{([^}]*)\}\{([^}]*)\}`)

func findTitle(dir string) (title, artist string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tex") {
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			if m := titleRE.FindSubmatch(b); m != nil {
				return string(m[1]), string(m[2])
			}
		}
	}
	return "", ""
}

func main() {
	root := "/Users/dim/dev/songbooks"
	var lyFiles []string
	for _, book := range []string{"Mercy", "MySongs", "Other Lies", "Trio"} {
		filepath.Walk(filepath.Join(root, book), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if filepath.Base(filepath.Dir(path)) == "tmp-ly" {
				return nil
			}
			if filepath.Ext(path) == ".ly" && filepath.Base(path) != "header.ly" {
				lyFiles = append(lyFiles, path)
			}
			return nil
		})
	}

	fmt.Println("=== Real TabStaff riffs (non-empty) ===")
	for _, f := range lyFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		riff, err := tab.ParseScore(string(data))
		if err != nil || riff == nil || len(riff.Bars) == 0 {
			continue
		}
		dir := filepath.Dir(f)
		title, artist := findTitle(dir)
		rel, _ := filepath.Rel(root, f)
		nbeats := 0
		for _, b := range riff.Bars {
			nbeats += len(b.Beats)
		}
		fmt.Printf("%s | title=%q artist=%q | bars=%d beats=%d strings=%d\n", rel, title, artist, len(riff.Bars), nbeats, len(riff.Tuning))
	}

	fmt.Println()
	fmt.Println("=== \\gtab chord diagrams ===")
	var texFiles []string
	for _, book := range []string{"Mercy", "MySongs", "Other Lies", "Trio"} {
		filepath.Walk(filepath.Join(root, book), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if filepath.Ext(path) == ".tex" {
				texFiles = append(texFiles, path)
			}
			return nil
		})
	}
	for _, f := range texFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		matches := gtabRE.FindAllSubmatch(data, -1)
		if len(matches) == 0 {
			continue
		}
		dir := filepath.Dir(f)
		title, artist := findTitle(dir)
		rel, _ := filepath.Rel(root, f)
		fmt.Printf("%s | title=%q artist=%q\n", rel, title, artist)
		for _, m := range matches {
			fmt.Printf("    gtab %s {%s}\n", string(m[1]), string(m[2]))
		}
	}
}
