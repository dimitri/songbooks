package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ClaimPersonalInviteAs is a personal invite (a placeholder added by
// name, CreateShellPerson) taken by someone who already has an account:
// personID, signed in. The placeholder is merged into that account —
// everything that names the placeholder (its band and community
// memberships and roles, its instruments, a coach's notes and
// assignments, …) names the account instead — and the placeholder row
// goes; the link stays, used (markInviteUsedTx). Where the account already has the same thing
// (a current membership in the same band, the same instrument), the
// account's own stays and the placeholder's is dropped.
//
// ErrNotFound when the link is unknown, expired, or its placeholder has
// an account since; ErrConflict when personID is itself a placeholder.
func (st *Store) ClaimPersonalInviteAs(ctx context.Context, token string, personID int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var shellID int64
	err = tx.QueryRow(ctx, `
		SELECT i.person_id FROM invites i JOIN people p ON p.id = i.person_id
		WHERE i.token = $1 AND i.expires_at > now() AND i.used_at IS NULL AND p.is_shell
		FOR UPDATE OF i, p`, token).Scan(&shellID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("no such invite: %w", ErrNotFound)
	}
	if err != nil {
		return err
	}
	var targetShell bool
	err = tx.QueryRow(ctx, `SELECT is_shell FROM people WHERE id = $1 FOR UPDATE`, personID).Scan(&targetShell)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("no such person id %d: %w", personID, ErrNotFound)
	}
	if err != nil {
		return err
	}
	if targetShell {
		return fmt.Errorf("person %d has no account to merge into: %w", personID, ErrConflict)
	}
	// This link is kept, used (markInviteUsedTx: before the merge, which
	// moves the placeholder's memberships it names the first of); the
	// placeholder's other links go: moved to the account, one would show
	// its memberships to whoever has it.
	if err := markInviteUsedTx(ctx, tx, token, personID, shellID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM invites WHERE person_id = $1 AND token <> $2`, shellID, token); err != nil {
		return err
	}
	if shellID != personID {
		if err := mergePersonTx(ctx, tx, shellID, personID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// mergePersonTx points every foreign key to people that names from at
// into instead — every one the database has, so a table added later is
// covered without touching this — then deletes from. A row that can't
// move because into already has its equivalent (a unique or exclusion
// constraint: the same band, the same instrument) stays with from and
// goes with it (ON DELETE CASCADE), or keeps from's id to be nulled.
func mergePersonTx(ctx context.Context, tx pgx.Tx, from, into int64) error {
	rows, err := tx.Query(ctx, `
		SELECT c.conrelid::regclass::text, a.attname
		FROM pg_constraint c
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
		WHERE c.contype = 'f' AND c.confrelid = 'people'::regclass AND cardinality(c.conkey) = 1
		ORDER BY 1, 2`)
	if err != nil {
		return err
	}
	type fk struct{ table, column string }
	var fks []fk
	for rows.Next() {
		var f fk
		if err := rows.Scan(&f.table, &f.column); err != nil {
			return err
		}
		fks = append(fks, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, f := range fks {
		// regclass::text is already quoted where it needs to be.
		col := pgx.Identifier{f.column}.Sanitize()
		update := fmt.Sprintf(`UPDATE %s SET %s = $1 WHERE %s = $2`, f.table, col, col)
		err := savepoint(ctx, tx, func() error { _, err := tx.Exec(ctx, update, into, from); return err })
		if err == nil {
			continue
		}
		if !isDuplicate(err) {
			return fmt.Errorf("merging person %d into %d (%s.%s): %w", from, into, f.table, f.column, err)
		}
		// Some rows clash: one at a time, leaving the ones that do.
		ctids, err := collectStrings(ctx, tx, fmt.Sprintf(`SELECT ctid::text FROM %s WHERE %s = $1`, f.table, col), from)
		if err != nil {
			return err
		}
		one := fmt.Sprintf(`UPDATE %s SET %s = $1 WHERE ctid = $2::tid`, f.table, col)
		for _, ctid := range ctids {
			if err := savepoint(ctx, tx, func() error { _, err := tx.Exec(ctx, one, into, ctid); return err }); err != nil && !isDuplicate(err) {
				return fmt.Errorf("merging person %d into %d (%s.%s): %w", from, into, f.table, f.column, err)
			}
		}
	}
	_, err = tx.Exec(ctx, `DELETE FROM people WHERE id = $1`, from)
	return err
}

// savepoint runs f in a savepoint, rolled back to if f fails, so the
// transaction goes on.
func savepoint(ctx context.Context, tx pgx.Tx, f func() error) error {
	if _, err := tx.Exec(ctx, `SAVEPOINT merge_person`); err != nil {
		return err
	}
	if err := f(); err != nil {
		if _, rerr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT merge_person`); rerr != nil {
			return rerr
		}
		return err
	}
	_, err := tx.Exec(ctx, `RELEASE SAVEPOINT merge_person`)
	return err
}

// isDuplicate: a unique (23505) or exclusion (23P01) constraint refused.
func isDuplicate(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "23505" || pgErr.Code == "23P01")
}

func collectStrings(ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]string, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
