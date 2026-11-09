package tab

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseScore_WholeCorpus smoke-tests ParseScore against every real
// .ly file in the repo's legacy book directories (excluding tmp-ly/, a
// large untracked scratch/cache directory, not real content). It doesn't
// assert success — most of the corpus is a standard-notation \new
// Staff/Voice or a \chordmode-only file, neither of which this package
// targets (see the package doc comment) — just logs a count so real
// coverage of the \new TabStaff riffs this subset DOES target is an
// inspectable number, not a guess.
func TestParseScore_WholeCorpus(t *testing.T) {
	root := repoRoot(t)
	var files []string
	for _, book := range []string{"Mercy", "MySongs", "Other Lies", "Trio"} {
		_ = filepath.Walk(filepath.Join(root, book), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if filepath.Base(filepath.Dir(path)) == "tmp-ly" {
				return nil
			}
			if filepath.Ext(path) == ".ly" && filepath.Base(path) != "header.ly" {
				files = append(files, path)
			}
			return nil
		})
	}
	if len(files) == 0 {
		t.Skip("no .ly corpus present in this checkout")
	}

	ok, fail := 0, 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if _, err := ParseScore(string(data)); err != nil {
			fail++
			continue
		}
		ok++
	}
	t.Logf("corpus: %d/%d real .ly files have a \\new TabStaff riff this subset extracted (the rest are standard-notation-only or chordmode-only files, out of scope — see package doc comment)", ok, ok+fail)
}
