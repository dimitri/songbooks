package store

import "context"

// DemoStatus is what a demo copy's admin page says (docs/demo-data.md):
// how many rows the fixtures made, and whether the copy is anonymized
// (its coaching community renamed Studio Lumen).
func (st *Store) DemoStatus(ctx context.Context) (rows int64, anonymized bool, err error) {
	err = st.db.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM demo_fixture_rows),
		       EXISTS (SELECT 1 FROM communities WHERE name = 'Studio Lumen')`).Scan(&rows, &anonymized)
	return rows, anonymized, err
}
