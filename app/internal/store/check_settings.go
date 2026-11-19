package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// A coaching community's settings for checking singing (migration 0086,
// pitch-check.js): how strict each level is, and the words a student
// gets after a take. What the coach leaves alone is the app's.

// CheckLevel is one level: its name ("" for the app's), the margin to be
// in tune (cents) and on time (ms).
type CheckLevel struct {
	Key   string `json:"key"`
	Name  string `json:"name,omitempty"`
	Cents int    `json:"cents"`
	Ms    int    `json:"ms"`
}

// CheckLevelKeys are the three levels, easiest first.
var CheckLevelKeys = []string{"beginner", "intermediate", "advanced"}

// DefaultCheckLevels are the app's margins: research's "accurate" (±50
// cents), what anyone hears (±25), what a trained ear hears (±15, near
// the tracking's own error); entries measured to about ±60 ms.
var DefaultCheckLevels = map[string]CheckLevel{
	"beginner":     {Key: "beginner", Cents: 50, Ms: 250},
	"intermediate": {Key: "intermediate", Cents: 25, Ms: 160},
	"advanced":     {Key: "advanced", Cents: 15, Ms: 110},
}

// CheckSettings is a community's: its levels in order, its messages by
// situation (only those the coach wrote).
type CheckSettings struct {
	Levels   []CheckLevel      `json:"levels"`
	Messages map[string]string `json:"messages"`
	Custom   bool              `json:"custom"` // the coach changed something
}

// Level is one level by key, the app's when unknown.
func (c CheckSettings) Level(key string) CheckLevel {
	for _, l := range c.Levels {
		if l.Key == key {
			return l
		}
	}
	return DefaultCheckLevels["beginner"]
}

// GetCheckSettings is a community's settings, the app's where unset.
func (st *Store) GetCheckSettings(ctx context.Context, communityID int64) (CheckSettings, error) {
	var levelsRaw, messagesRaw []byte
	err := st.db.QueryRow(ctx, `SELECT levels, messages FROM coaching_check_settings WHERE community_id = $1`, communityID).Scan(&levelsRaw, &messagesRaw)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return CheckSettings{}, err
	}
	set := map[string]CheckLevel{}
	out := CheckSettings{Messages: map[string]string{}, Custom: err == nil}
	if len(levelsRaw) > 0 {
		json.Unmarshal(levelsRaw, &set)
	}
	if len(messagesRaw) > 0 {
		json.Unmarshal(messagesRaw, &out.Messages)
	}
	for _, k := range CheckLevelKeys {
		l := DefaultCheckLevels[k]
		if c, ok := set[k]; ok {
			if c.Cents >= 5 && c.Cents <= 200 {
				l.Cents = c.Cents
			}
			if c.Ms >= 30 && c.Ms <= 600 {
				l.Ms = c.Ms
			}
			l.Name = c.Name
		}
		out.Levels = append(out.Levels, l)
	}
	return out, nil
}

// SetCheckSettings saves a community's levels (margins kept in bounds:
// 5–200 cents, 30–600 ms) and messages (an empty one is the app's).
func (st *Store) SetCheckSettings(ctx context.Context, communityID int64, levels []CheckLevel, messages map[string]string) error {
	set := map[string]CheckLevel{}
	for _, l := range levels {
		if _, ok := DefaultCheckLevels[l.Key]; !ok {
			continue
		}
		l.Cents, l.Ms = max(5, min(200, l.Cents)), max(30, min(600, l.Ms))
		if len([]rune(l.Name)) > 30 {
			l.Name = string([]rune(l.Name)[:30])
		}
		set[l.Key] = l
	}
	keep := map[string]string{}
	for k, v := range messages {
		if v != "" && len(k) <= 40 {
			if len([]rune(v)) > 300 {
				v = string([]rune(v)[:300])
			}
			keep[k] = v
		}
	}
	lj, _ := json.Marshal(set)
	mj, _ := json.Marshal(keep)
	_, err := st.db.Exec(ctx, `
		INSERT INTO coaching_check_settings (community_id, levels, messages) VALUES ($1, $2, $3)
		ON CONFLICT (community_id) DO UPDATE SET levels = excluded.levels, messages = excluded.messages, updated_at = now()`,
		communityID, lj, mj)
	return err
}
