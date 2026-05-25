package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	storegen "deck-fleet/backend/internal/store/gen"
)

// WithTx runs fn in a write transaction; rolls back on error or panic.
// fn's *storegen.Queries is bound to the tx.
func (d *DB) WithTx(ctx context.Context, fn func(q *storegen.Queries) error) (err error) {
	return d.WithTxRaw(ctx, func(tx *sql.Tx, q *storegen.Queries) error {
		return fn(q)
	})
}

// WithTxRaw exposes *sql.Tx for hand-written queries sqlc cannot express
// (e.g. repeated bind variables). Prefer WithTx otherwise.
func (d *DB) WithTxRaw(ctx context.Context, fn func(tx *sql.Tx, q *storegen.Queries) error) (err error) {
	tx, err := d.Write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				err = errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
			}
			return
		}
		if cErr := tx.Commit(); cErr != nil {
			err = fmt.Errorf("commit: %w", cErr)
		}
	}()

	err = fn(tx, d.Queries.WithTx(tx))
	return err
}
