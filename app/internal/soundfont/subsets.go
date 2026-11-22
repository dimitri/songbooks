package soundfont

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The presets a subset may name: at most MaxPresets of them, melodic banks
// up to 2047 (alphaTab keeps a channel's bank to 11 bits), programs 0–127.
const (
	MaxPresets = 64
	MaxBank    = 2047
)

// Metronome is the preset alphaTab's metronome and count-in click play
// (the percussion bank's kit 0, MetronomeKey), and the kit it falls back
// to for a kit the bank doesn't have. A subset that doesn't ask for it
// has its click alone (the whole kit is most of a small subset's size).
var Metronome = PresetID{DrumBank, 0}

// MetronomeKey is the key alphaTab's metronome plays (SynthConstants).
const MetronomeKey = 33

// Key is the canonical name of a set of presets, "0-24_0-33_128-0": each
// one's bank-program, sorted, no duplicates. Files playing the same
// instruments share one subset.
func Key(presets []PresetID) string {
	set := map[PresetID]bool{}
	for _, p := range presets {
		set[p] = true
	}
	list := make([]PresetID, 0, len(set))
	for p := range set {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Bank < list[j].Bank || (list[i].Bank == list[j].Bank && list[i].Program < list[j].Program)
	})
	parts := make([]string, len(list))
	for i, p := range list {
		parts[i] = p.String()
	}
	return strings.Join(parts, "_")
}

// ParseKey reads a key (canonical or not: Key(presets) is the canonical
// one), checking its size and ranges.
func ParseKey(key string) ([]PresetID, error) {
	if key == "" || len(key) > MaxPresets*9 {
		return nil, errors.New("soundfont: bad preset list")
	}
	parts := strings.Split(key, "_")
	if len(parts) > MaxPresets {
		return nil, fmt.Errorf("soundfont: more than %d presets", MaxPresets)
	}
	out := make([]PresetID, 0, len(parts))
	for _, s := range parts {
		b, p, ok := strings.Cut(s, "-")
		bank, err1 := strconv.Atoi(b)
		prog, err2 := strconv.Atoi(p)
		if !ok || err1 != nil || err2 != nil || b != strconv.Itoa(bank) || p != strconv.Itoa(prog) {
			return nil, fmt.Errorf("soundfont: bad preset %q", s)
		}
		if bank < 0 || bank > MaxBank || prog < 0 || prog > 127 {
			return nil, fmt.Errorf("soundfont: preset %q out of range", s)
		}
		out = append(out, PresetID{bank, prog})
	}
	return out, nil
}

// Subsets makes, caches and finds the subsets of one SoundFont file.
type Subsets struct {
	path     string
	dir      string // the cache: <dir>/<ID>/<hash of key>.sf2
	maxBytes int64

	once sync.Once
	file *os.File
	bank *Bank
	id   string
	err  error

	mu       sync.Mutex
	building map[string]*build
}

type build struct {
	done chan struct{}
	path string
	err  error
}

// NewSubsets serves subsets of the SoundFont at path, cached under dir up
// to maxBytes. The file is read on first use.
func NewSubsets(path, dir string, maxBytes int64) *Subsets {
	return &Subsets{path: path, dir: dir, maxBytes: maxBytes, building: map[string]*build{}}
}

func (s *Subsets) open() {
	s.once.Do(func() {
		f, err := os.Open(s.path)
		if err != nil {
			s.err = err
			return
		}
		h := sha256.New()
		size, err := io.Copy(h, f)
		if err != nil {
			f.Close()
			s.err = err
			return
		}
		b, err := Parse(f, size)
		if err != nil {
			f.Close()
			s.err = fmt.Errorf("%s: %w", s.path, err)
			return
		}
		s.file, s.bank = f, b
		s.id = hex.EncodeToString(h.Sum(nil))[:12]
	})
}

// ID names the SoundFont's content (the start of its SHA-256), "" when it
// can't be read: subset URLs carry it, so a new file gets new URLs.
func (s *Subsets) ID() string {
	if s == nil {
		return ""
	}
	s.open()
	return s.id
}

// Err is why the SoundFont can't be read, nil if it can.
func (s *Subsets) Err() error {
	s.open()
	return s.err
}

// Name is the SoundFont's own name (its INFO INAM), "" if unreadable.
func (s *Subsets) Name() string {
	if s.ID() == "" {
		return ""
	}
	return s.bank.InfoField("INAM")
}

// Get is the cached file of the subset with presets (made now if it
// isn't cached yet), and its ETag.
func (s *Subsets) Get(ctx context.Context, presets []PresetID) (path, etag string, err error) {
	if s.ID() == "" {
		return "", "", s.err
	}
	key := Key(presets)
	sum := sha256.Sum256([]byte(key))
	name := hex.EncodeToString(sum[:])[:32]
	etag = `"` + s.id + "-" + name[:16] + `"`
	path = filepath.Join(s.dir, s.id, name+".sf2")
	if st, err := os.Stat(path); err == nil {
		// Recently used, for the trimming (at most once an hour).
		if time.Since(st.ModTime()) > time.Hour {
			now := time.Now()
			_ = os.Chtimes(path, now, now)
		}
		return path, etag, nil
	}

	// One build per key at a time; the others wait for it.
	s.mu.Lock()
	if b, ok := s.building[key]; ok {
		s.mu.Unlock()
		select {
		case <-b.done:
			return b.path, etag, b.err
		case <-ctx.Done():
			return "", "", ctx.Err()
		}
	}
	b := &build{done: make(chan struct{})}
	s.building[key] = b
	s.mu.Unlock()

	b.path, b.err = s.make(presets, path)
	close(b.done)
	s.mu.Lock()
	delete(s.building, key)
	s.mu.Unlock()
	if b.err == nil {
		s.trim()
	}
	return b.path, etag, b.err
}

func (s *Subsets) make(presets []PresetID, path string) (string, error) {
	var keys map[PresetID][]int
	if !slices.Contains(presets, Metronome) {
		presets = append(slices.Clone(presets), Metronome)
		keys = map[PresetID][]int{Metronome: {MetronomeKey}}
	}
	sub, err := s.bank.Subset(presets, keys)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if _, err := sub.WriteTo(&buf); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".subset-*")
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return path, nil
}

// trim keeps the cache under its size: past it, the least recently used
// subsets go until it's down to three quarters of it. Subsets of another
// SoundFont (an older one) go first.
func (s *Subsets) trim() {
	if s.maxBytes <= 0 {
		return
	}
	type entry struct {
		path  string
		size  int64
		mtime time.Time
		other bool
	}
	var all []entry
	var total int64
	_ = filepath.WalkDir(s.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".sf2") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		other := filepath.Base(filepath.Dir(p)) != s.id
		all = append(all, entry{p, info.Size(), info.ModTime(), other})
		total += info.Size()
		return nil
	})
	if total <= s.maxBytes {
		return
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].other != all[j].other {
			return all[i].other
		}
		return all[i].mtime.Before(all[j].mtime)
	})
	for _, e := range all {
		if total <= s.maxBytes*3/4 {
			break
		}
		if os.Remove(e.path) == nil {
			total -= e.size
		}
	}
}
