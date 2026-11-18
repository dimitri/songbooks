package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Locales (migration 0081, docs/i18n.md): a person's, a community's and a
// band's language, "" when they leave it to the next (the person to the
// page's community or band, that to the browser).

func (st *Store) locale(ctx context.Context, table string, id int64) (string, error) {
	var l string
	err := st.db.QueryRow(ctx, `SELECT locale FROM `+table+` WHERE id = $1`, id).Scan(&l)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return l, err
}

func (st *Store) setLocale(ctx context.Context, table string, id int64, locale string) error {
	if locale != "" && locale != "en" && locale != "fr" {
		return fmt.Errorf("invalid language %q", locale)
	}
	_, err := st.db.Exec(ctx, `UPDATE `+table+` SET locale = $2 WHERE id = $1`, id, locale)
	return err
}

// PersonLocale is a person's own choice of language.
func (st *Store) PersonLocale(ctx context.Context, personID int64) (string, error) {
	return st.locale(ctx, "people", personID)
}

// SetPersonLocale sets a person's language ("" for automatic).
func (st *Store) SetPersonLocale(ctx context.Context, personID int64, locale string) error {
	return st.setLocale(ctx, "people", personID, locale)
}

// CommunityLocale is a community's language, for its pages.
func (st *Store) CommunityLocale(ctx context.Context, communityID int64) (string, error) {
	return st.locale(ctx, "communities", communityID)
}

// SetCommunityLocale sets a community's language.
func (st *Store) SetCommunityLocale(ctx context.Context, communityID int64, locale string) error {
	return st.setLocale(ctx, "communities", communityID, locale)
}

// BandLocale is a band's language, for its pages.
func (st *Store) BandLocale(ctx context.Context, bandID int64) (string, error) {
	return st.locale(ctx, "bands", bandID)
}

// SetBandLocale sets a band's language.
func (st *Store) SetBandLocale(ctx context.Context, bandID int64, locale string) error {
	return st.setLocale(ctx, "bands", bandID, locale)
}

// SongbookLocale is the language of the community or band owning a
// songbook, "" for a person's.
func (st *Store) SongbookLocale(ctx context.Context, songbookID int64) (string, error) {
	var l string
	err := st.db.QueryRow(ctx, `
		SELECT coalesce(c.locale, b.locale, '') FROM songbooks sb
		LEFT JOIN communities c ON c.id = sb.owner_community_id
		LEFT JOIN bands b ON b.id = sb.owner_band_id
		WHERE sb.id = $1`, songbookID).Scan(&l)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return l, err
}
