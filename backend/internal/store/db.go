package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB wraps sql.DB with the driver name so Migrate can select the right dialect.
type DB struct {
	*sql.DB
	driver string
}

// Open opens a database connection and verifies it is reachable.
// driver is "sqlite" or "pgx"; dsn is the data source name.
func Open(driver, dsn string) (*DB, error) {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	if driver == "sqlite" {
		// Foreign key enforcement is off by default in SQLite.
		// WAL mode improves concurrent read performance.
		if _, err := db.Exec("PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL;"); err != nil {
			return nil, fmt.Errorf("sqlite pragmas: %w", err)
		}
	}
	return &DB{DB: db, driver: driver}, nil
}

// Migrate runs all pending goose migrations embedded in the binary.
func (d *DB) Migrate() error {
	fsys, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migrations fs: %w", err)
	}

	dialect := goose.DialectSQLite3
	if d.driver == "pgx" {
		dialect = goose.DialectPostgres
	}

	provider, err := goose.NewProvider(dialect, d.DB, fsys)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}

	if _, err := provider.Up(context.Background()); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}
