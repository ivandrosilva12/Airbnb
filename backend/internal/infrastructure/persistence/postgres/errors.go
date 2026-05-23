package postgres

import (
	"errors"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// mapError translates pgx/postgres errors into domain sentinel errors so the
// rest of the application stays decoupled from the driver.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return shared.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return shared.ErrConflict
		case "23503": // foreign_key_violation
			return shared.NewValidationError("referenced resource does not exist")
		case "23514": // check_violation
			return shared.NewValidationError("a value violates a database constraint")
		}
	}
	return err
}
