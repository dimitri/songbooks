package store

import (
	"context"
	"testing"
)

// CountForTest is sql's one integer on st's database, for this
// directory's external tests that check nothing was made (no copy of a
// song on assign or on edit).
func CountForTest(t *testing.T, st *Store, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := st.db.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
