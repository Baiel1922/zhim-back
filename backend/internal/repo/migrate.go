package repo

import (
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"

	"github.com/Baiel1922/zhim-back/backend/migrations"
)

// OpenSQL opens a database/sql handle (goose works on database/sql, the app on pgxpool).
func OpenSQL(databaseURL string) (*sql.DB, error) {
	sqlDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return sqlDB, nil
}

// NewMigrator returns a goose provider over the embedded migrations.
func NewMigrator(sqlDB *sql.DB, opts ...goose.ProviderOption) (*goose.Provider, error) {
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS, opts...)
	if err != nil {
		return nil, fmt.Errorf("create migrator: %w", err)
	}
	return p, nil
}
