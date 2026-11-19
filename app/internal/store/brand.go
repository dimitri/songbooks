package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"app/internal/brand"
)

// brandTable is where each kind of owner keeps its brand.
var brandTable = map[string]string{"community": "communities", "band": "bands"}

// logoOwner is each kind of owner's column in brand_logos.
var logoOwner = map[string]string{"community": "community_id", "band": "band_id"}

// GetBrand loads a community's or band's brand and name, with its
// uploaded logo's address (brand.LogoImage) when it has one.
func (st *Store) GetBrand(ctx context.Context, kind string, id int64) (brand.Brand, string, error) {
	table, ok := brandTable[kind]
	if !ok {
		return brand.Brand{}, "", fmt.Errorf("no brand for %q", kind)
	}
	var raw []byte
	var name string
	var sha, contentType *string
	if err := st.db.QueryRow(ctx, `
		SELECT t.brand, t.name, l.sha256, l.content_type
		  FROM `+table+` t LEFT JOIN brand_logos l ON l.`+logoOwner[kind]+` = t.id
		 WHERE t.id = $1`, id).Scan(&raw, &name, &sha, &contentType); err != nil {
		return brand.Brand{}, "", err
	}
	b := brand.Parse(raw)
	b.LogoImage = ""
	if sha != nil && contentType != nil {
		b.LogoImage = brand.LogoPath(kind, id, *sha, *contentType)
	}
	return b, name, nil
}

// SetBrand saves a community's or band's brand, cleaned (brand.Clean).
// Its uploaded logo isn't part of it (SetBrandLogo).
func (st *Store) SetBrand(ctx context.Context, kind string, id int64, b brand.Brand) error {
	table, ok := brandTable[kind]
	if !ok {
		return fmt.Errorf("no brand for %q", kind)
	}
	b = brand.Clean(b)
	b.LogoImage = ""
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	tag, err := st.db.Exec(ctx, `UPDATE `+table+` SET brand = $2 WHERE id = $1`, id, raw)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetBrandLogo keeps a community's or band's uploaded logo (made by
// brand.PrepareLogo), in place of the one it had; ErrNotFound when there's
// no such community or band.
func (st *Store) SetBrandLogo(ctx context.Context, kind string, id int64, logo brand.LogoImage, byPersonID int64) error {
	table, ok := brandTable[kind]
	if !ok {
		return fmt.Errorf("no brand for %q", kind)
	}
	col := logoOwner[kind]
	var by *int64
	if byPersonID != 0 {
		by = &byPersonID
	}
	tag, err := st.db.Exec(ctx, `
		INSERT INTO brand_logos (`+col+`, content_type, width, height, byte_size, sha256, data, created_by_person_id)
		SELECT id, $2, $3, $4, $5, $6, $7, $8 FROM `+table+` WHERE id = $1
		ON CONFLICT (`+col+`) DO UPDATE SET
		  content_type = EXCLUDED.content_type, width = EXCLUDED.width, height = EXCLUDED.height,
		  byte_size = EXCLUDED.byte_size, sha256 = EXCLUDED.sha256, data = EXCLUDED.data,
		  created_at = now(), created_by_person_id = EXCLUDED.created_by_person_id`,
		id, logo.ContentType, logo.Width, logo.Height, len(logo.Data), logo.SHA256, logo.Data, by)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteBrandLogo removes a community's or band's uploaded logo, if any.
func (st *Store) DeleteBrandLogo(ctx context.Context, kind string, id int64) error {
	col, ok := logoOwner[kind]
	if !ok {
		return fmt.Errorf("no brand for %q", kind)
	}
	_, err := st.db.Exec(ctx, `DELETE FROM brand_logos WHERE `+col+` = $1`, id)
	return err
}

// BrandLogo is an uploaded logo, to serve.
type BrandLogo struct {
	Data        []byte
	ContentType string
	SHA256      string
	CreatedAt   time.Time
}

// GetBrandLogo loads a community's or band's uploaded logo; ErrNotFound
// when it has none.
func (st *Store) GetBrandLogo(ctx context.Context, kind string, id int64) (BrandLogo, error) {
	col, ok := logoOwner[kind]
	if !ok {
		return BrandLogo{}, ErrNotFound
	}
	var l BrandLogo
	err := st.db.QueryRow(ctx, `SELECT data, content_type, sha256, created_at FROM brand_logos WHERE `+col+` = $1`, id).
		Scan(&l.Data, &l.ContentType, &l.SHA256, &l.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return BrandLogo{}, ErrNotFound
	}
	return l, err
}
