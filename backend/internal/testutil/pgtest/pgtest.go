// Package pgtest provides throwaway PostgreSQL databases for integration tests.
//
// One container is started per test binary (call Main from TestMain). Each test
// gets its own database cloned from a migrated template, so tests are isolated,
// may commit freely and can run in parallel. With -short everything is skipped
// and Docker is not needed.
package pgtest

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tclog "github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/Baiel1922/zhim-back/backend/internal/repo"
)

const (
	image      = "postgres:16-alpine"
	templateDB = "zhim_template"
)

var (
	baseURL string // points at templateDB; empty until Main has started the container
	// CREATE DATABASE ... TEMPLATE fails if the template is in use, so clones are serialized.
	cloneMu sync.Mutex
	dbSeq   atomic.Int64
)

// Main starts PostgreSQL, applies migrations to the template database, runs the
// tests and removes the container. Use it as the body of TestMain.
func Main(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	if !testing.Verbose() {
		tclog.SetDefault(tclog.NewNoopLogger())
	}
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, image,
		postgres.WithDatabase(templateDB),
		postgres.WithUsername("zhim"),
		postgres.WithPassword("zhim"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pgtest: start postgres:", err)
		return 1
	}
	defer func() {
		if err := testcontainers.TerminateContainer(ctr); err != nil {
			fmt.Fprintln(os.Stderr, "pgtest: terminate postgres:", err)
		}
	}()

	connStr, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, "pgtest: connection string:", err)
		return 1
	}
	if err := migrate(ctx, connStr); err != nil {
		fmt.Fprintln(os.Stderr, "pgtest: migrate template:", err)
		return 1
	}
	baseURL = connStr
	return m.Run()
}

func migrate(ctx context.Context, connStr string) error {
	sqlDB, err := repo.OpenSQL(connStr)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	migrator, err := repo.NewMigrator(sqlDB)
	if err != nil {
		return err
	}
	_, err = migrator.Up(ctx)
	return err
}

// NewDatabase creates a fresh migrated database for t and returns its URL.
// It is dropped when the test ends. Skips the test under -short.
func NewDatabase(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test: needs Docker (run without -short)")
	}
	if baseURL == "" {
		t.Fatal("pgtest: call pgtest.Main from TestMain")
	}

	name := fmt.Sprintf("test_%d", dbSeq.Add(1))
	cloneMu.Lock()
	err := adminExec(t, "CREATE DATABASE "+name+" TEMPLATE "+templateDB)
	cloneMu.Unlock()
	if err != nil {
		t.Fatalf("pgtest: create database: %v", err)
	}
	t.Cleanup(func() {
		if err := adminExec(t, "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("pgtest: drop database: %v", err)
		}
	})
	return withDatabase(t, baseURL, name)
}

// NewPool is NewDatabase plus a connection pool that is closed when the test ends.
func NewPool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	dbURL := NewDatabase(t)
	pool, err := repo.NewPool(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("pgtest: connect: %v", err)
	}
	// Registered after the DROP cleanup, so it runs first.
	t.Cleanup(pool.Close)
	return pool
}

func adminExec(t testing.TB, sql string) error {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, withDatabase(t, baseURL, "postgres"))
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, sql)
	return err
}

func withDatabase(t testing.TB, connStr, name string) string {
	t.Helper()
	u, err := url.Parse(connStr)
	if err != nil {
		t.Fatalf("pgtest: parse url: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}
