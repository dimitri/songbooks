package store

import (
	"context"
	"encoding/json"
	"fmt"

	"app/internal/brand"
)

// brandTable is where each kind of owner keeps its brand.
var brandTable = map[string]string{"community": "communities", "band": "bands"}

// GetBrand loads a community's or band's brand and name.
func (st *Store) GetBrand(ctx context.Context, kind string, id int64) (brand.Brand, string, error) {
	table, ok := brandTable[kind]
	if !ok {
		return brand.Brand{}, "", fmt.Errorf("no brand for %q", kind)
	}
	var raw []byte
	var name string
	if err := st.db.QueryRow(ctx, `SELECT brand, name FROM `+table+` WHERE id = $1`, id).Scan(&raw, &name); err != nil {
		return brand.Brand{}, "", err
	}
	return brand.Parse(raw), name, nil
}

// SetBrand saves a community's or band's brand, cleaned (brand.Clean).
func (st *Store) SetBrand(ctx context.Context, kind string, id int64, b brand.Brand) error {
	table, ok := brandTable[kind]
	if !ok {
		return fmt.Errorf("no brand for %q", kind)
	}
	raw, err := json.Marshal(brand.Clean(b))
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
