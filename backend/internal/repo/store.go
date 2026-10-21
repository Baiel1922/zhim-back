// Package repo is the persistence layer: sqlc-generated queries (package db),
// connection pooling, transactions and migrations.
package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
)

// Store bundles the generated queries with the pool they run on.
type Store struct {
	*db.Queries
	pool *pgxpool.Pool
}

// NewStore wraps a pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{Queries: db.New(pool), pool: pool}
}

// InTx runs fn inside a transaction. It commits when fn returns nil and rolls back otherwise.
func (s *Store) InTx(ctx context.Context, fn func(q *db.Queries) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(s.WithTx(tx))
	})
}

// NewPool connects to PostgreSQL and verifies the connection.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// PostgreSQL error codes used by the service layer.
const (
	codeUniqueViolation     = "23505"
	codeForeignKeyViolation = "23503"
	codeCheckViolation      = "23514"
)

// IsNotFound reports whether err means "no rows".
func IsNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// IsUniqueViolation reports whether err violates the named unique constraint
// (any unique constraint when constraint is empty).
func IsUniqueViolation(err error, constraint string) bool {
	return isConstraintError(err, codeUniqueViolation, constraint)
}

// IsCheckViolation reports whether err violates the named CHECK constraint
// (any CHECK constraint when constraint is empty).
func IsCheckViolation(err error, constraint string) bool {
	return isConstraintError(err, codeCheckViolation, constraint)
}

// IsForeignKeyViolation reports whether err violates the named foreign key
// (any foreign key when constraint is empty).
func IsForeignKeyViolation(err error, constraint string) bool {
	return isConstraintError(err, codeForeignKeyViolation, constraint)
}

func isConstraintError(err error, code, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		return false
	}
	return constraint == "" || pgErr.ConstraintName == constraint
}
