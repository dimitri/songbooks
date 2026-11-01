package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ImportTree does a one-time load of every book under root (any directory
// with a songs/ subdirectory, same rule the old filesystem store used) into
// the database: a projects row per book (skipped if the slug already
// exists), a songs row per *.song file, a setlists row per
// <book>/setlists/*.setlist file. Idempotent — re-running adds nothing
// that's already there.
//
// A single people row (username "tapoueh", display name "Dimitri") is
// created if absent and made owner of every imported project; this is a
// one-off bootstrap for the person running the import, not a general
// user-provisioning path — Phase 2's auth work replaces/extends it.
func ImportTree(ctx context.Context, st *Store, root string) error {
	var personID int64
	err := st.db.QueryRow(ctx, `
		INSERT INTO people (username, display_name) VALUES ('tapoueh', 'Dimitri')
		ON CONFLICT (username) DO UPDATE SET username = EXCLUDED.username
		RETURNING id`).Scan(&personID)
	if err != nil {
		return fmt.Errorf("upserting owner person: %w", err)
	}
	// The import tool's one bootstrap person is also the only reasonable
	// candidate for the instance's first admin — there's otherwise no
	// self-serve way to get an initial admin account at all (see
	// --grant-admin in cmd/songbooks for every subsequent one).
	if _, err := st.db.Exec(ctx, `UPDATE people SET is_admin = true WHERE id = $1`, personID); err != nil {
		return fmt.Errorf("granting the import owner admin: %w", err)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	var books []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if info, err := os.Stat(filepath.Join(root, e.Name(), "songs")); err == nil && info.IsDir() {
			books = append(books, e.Name())
		}
	}
	sort.Strings(books)

	var projectCount, songCount, setlistCount int
	for _, book := range books {
		playlist := ""
		if data, err := os.ReadFile(filepath.Join(root, book, "book.json")); err == nil {
			var meta BookMeta
			if json.Unmarshal(data, &meta) == nil {
				playlist = meta.Playlist
			}
		}
		orderJSON := []byte("[]")
		if data, err := os.ReadFile(filepath.Join(root, book, "order.txt")); err == nil {
			groups := parseOrderText(string(data))
			if raw, err := json.Marshal(groups); err == nil {
				orderJSON = raw
			}
		}

		var projectID int64
		var inserted bool
		err := st.db.QueryRow(ctx, `
			INSERT INTO projects (slug, name, playlist_url, song_order) VALUES ($1, $1, $2, $3)
			ON CONFLICT (slug) DO UPDATE SET slug = EXCLUDED.slug
			RETURNING id, (xmax = 0)`, book, playlist, orderJSON).Scan(&projectID, &inserted)
		if err != nil {
			return fmt.Errorf("upserting project %q: %w", book, err)
		}
		if inserted {
			projectCount++
		}

		if _, err := st.db.Exec(ctx, `
			INSERT INTO project_members (project_id, person_id, role) VALUES ($1, $2, 'owner')
			ON CONFLICT (project_id, person_id) DO NOTHING`, projectID, personID); err != nil {
			return fmt.Errorf("granting ownership on %q: %w", book, err)
		}

		songsDir := filepath.Join(root, book, "songs")
		songEntries, err := os.ReadDir(songsDir)
		if err != nil {
			return err
		}
		for _, se := range songEntries {
			if se.IsDir() || !strings.HasSuffix(se.Name(), ".song") {
				continue
			}
			stem := strings.TrimSuffix(se.Name(), ".song")
			var already bool
			if err := st.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM songs WHERE project_id = $1 AND stem = $2)`, projectID, stem).Scan(&already); err != nil {
				return err
			}
			if already {
				continue
			}
			data, err := os.ReadFile(filepath.Join(songsDir, se.Name()))
			if err != nil {
				return err
			}
			if err := st.ImportSong(book, stem, string(data)); err != nil {
				return fmt.Errorf("importing song %s/%s: %w", book, stem, err)
			}
			songCount++
		}

		setlistsDir := filepath.Join(root, book, "setlists")
		slEntries, err := os.ReadDir(setlistsDir)
		if err == nil {
			for _, le := range slEntries {
				if le.IsDir() || !strings.HasSuffix(le.Name(), ".setlist") {
					continue
				}
				stem := strings.TrimSuffix(le.Name(), ".setlist")
				var already bool
				if err := st.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM setlists WHERE project_id = $1 AND stem = $2)`, projectID, stem).Scan(&already); err != nil {
					return err
				}
				if already {
					continue
				}
				data, err := os.ReadFile(filepath.Join(setlistsDir, le.Name()))
				if err != nil {
					return err
				}
				if err := st.ImportSetlist(book, stem, string(data)); err != nil {
					return fmt.Errorf("importing setlist %s/%s: %w", book, stem, err)
				}
				setlistCount++
			}
		}
	}

	fmt.Printf("imported %d projects, %d songs, %d setlists\n", projectCount, songCount, setlistCount)
	return nil
}
