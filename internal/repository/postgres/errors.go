package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// PostgreSQL error codes
const (
	// Class 23 - Integrity Constraint Violation
	errCodeUniqueViolation     = "23505"
	errCodeForeignKeyViolation = "23503"
	errCodeNotNullViolation    = "23502"
	errCodeCheckViolation      = "23514"
)

// isUniqueViolation checks if the error is a PostgreSQL unique constraint violation.
func isUniqueViolation(err error) bool {
	return isPgError(err, errCodeUniqueViolation)
}

// isForeignKeyViolation checks if the error is a PostgreSQL foreign key violation.
func isForeignKeyViolation(err error) bool { //nolint:unused
	return isPgError(err, errCodeForeignKeyViolation)
}

// isNotNullViolation checks if the error is a PostgreSQL NOT NULL constraint violation.
func isNotNullViolation(err error) bool { //nolint:unused
	return isPgError(err, errCodeNotNullViolation)
}

// isCheckViolation checks if the error is a PostgreSQL CHECK constraint violation.
func isCheckViolation(err error) bool { //nolint:unused
	return isPgError(err, errCodeCheckViolation)
}

// isPgError checks if the error is a PostgreSQL error with the given code.
func isPgError(err error, code string) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == code
	}
	return false
}

// getPgErrorConstraint returns the constraint name from a PostgreSQL error.
func getPgErrorConstraint(err error) string { //nolint:unused
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.ConstraintName
	}
	return ""
}

// getPgErrorDetail returns the detail message from a PostgreSQL error.
func getPgErrorDetail(err error) string { //nolint:unused
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Detail
	}
	return ""
}
