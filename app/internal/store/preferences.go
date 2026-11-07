package store

import (
	"context"
	"encoding/json"
)

// ClickTrackPrefs is a person's silently-remembered display settings for
// the setlist click-track page — visual size, how many bars the beat
// dots group into, portrait/landscape layout, and every-beat vs
// beat-1-only clicking. Stored on the person (not per-browser
// localStorage alone) so it follows them to whatever device they next
// open the page on. Zero values are valid defaults, so an empty/missing
// row just means "nothing saved yet."
type ClickTrackPrefs struct {
	Size        string `json:"size,omitempty"`
	Group       string `json:"group,omitempty"`
	Orientation string `json:"orientation,omitempty"`
	ClickMode   string `json:"clickMode,omitempty"`
	Muted       bool   `json:"muted,omitempty"`
}

var (
	validClickTrackSize        = map[string]bool{"sm": true, "md": true, "lg": true}
	validClickTrackGroup       = map[string]bool{"1": true, "2": true, "4": true}
	validClickTrackOrientation = map[string]bool{"portrait": true, "landscape": true}
	validClickTrackClickMode   = map[string]bool{"every": true, "beat1": true}
)

// sanitize drops any field that isn't one of the recognized option
// values — a stale client sending an old/typo'd value shouldn't get
// permanently wedged into the stored prefs.
func (p ClickTrackPrefs) sanitize() ClickTrackPrefs {
	if !validClickTrackSize[p.Size] {
		p.Size = ""
	}
	if !validClickTrackGroup[p.Group] {
		p.Group = ""
	}
	if !validClickTrackOrientation[p.Orientation] {
		p.Orientation = ""
	}
	if !validClickTrackClickMode[p.ClickMode] {
		p.ClickMode = ""
	}
	return p
}

// GetClickTrackPrefs returns the person's saved click-track display
// preferences, zero-valued (all fields empty/false) if they've never set
// any.
func (s *Store) GetClickTrackPrefs(ctx context.Context, personID int64) (ClickTrackPrefs, error) {
	var raw []byte
	if err := s.db.QueryRow(ctx, `SELECT click_track_prefs FROM people WHERE id = $1`, personID).Scan(&raw); err != nil {
		return ClickTrackPrefs{}, err
	}
	var prefs ClickTrackPrefs
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &prefs) // malformed/legacy content just reads back as defaults
	}
	return prefs, nil
}

// SetClickTrackPrefs overwrites the person's saved click-track display
// preferences wholesale — the client always sends its full current
// settings, not a partial patch.
func (s *Store) SetClickTrackPrefs(ctx context.Context, personID int64, prefs ClickTrackPrefs) error {
	raw, err := json.Marshal(prefs.sanitize())
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `UPDATE people SET click_track_prefs = $2 WHERE id = $1`, personID, raw)
	return err
}
