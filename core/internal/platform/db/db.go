// Package db is Lucero's only SQLite write path: PRAGMA layering, a serialized
// writer, transactional aggregate+event touches and mandatory redaction.
// It carries no business semantics (ADR-004).
package db

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

// synchronous is per-connection in SQLite and does not persist in the file, so
// it is applied on every pooled connection rather than once at init.
const connPragmas = "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=synchronous(FULL)"

// DB holds two handles over one file: a read pool and a single-connection
// writer. MaxOpenConns(1) on the writer *is* the serialized write channel
// (domain model "Go DB 并发建议"); no extra mutex is needed.
type DB struct {
	Read  *sql.DB
	write *sql.DB
	path  string

	mu   sync.Mutex
	cols map[string]map[string]bool
}

// Open dials path and applies the database-level PRAGMA exactly once, outside
// any transaction: it cannot be applied from inside one (ADR-007).
func Open(path string) (*DB, error) {
	dsn := filepath.ToSlash(path) + connPragmas
	read, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open read pool: %w", err)
	}
	read.SetMaxOpenConns(4)
	read.SetMaxIdleConns(4)

	write, err := sql.Open("sqlite", dsn)
	if err != nil {
		read.Close()
		return nil, fmt.Errorf("open write channel: %w", err)
	}
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)

	d := &DB{Read: read, write: write, path: path, cols: map[string]map[string]bool{}}
	if err := d.initStorage(); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) initStorage() error {
	ctx := context.Background()
	c, err := d.write.Conn(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	var mode string
	if err := c.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
		return fmt.Errorf("set journal_mode: %w", err)
	}
	if mode != "wal" {
		return fmt.Errorf("journal_mode=WAL requested but got %q", mode)
	}
	var sync int
	if err := c.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&sync); err != nil {
		return fmt.Errorf("read synchronous: %w", err)
	}
	if sync != 2 {
		return fmt.Errorf("synchronous must be FULL(2), got %d", sync)
	}
	return nil
}

// JournalMode reports the persistent journal mode of the database file.
func (d *DB) JournalMode() (string, error) {
	var mode string
	err := d.Read.QueryRow("PRAGMA journal_mode").Scan(&mode)
	return mode, err
}

func (d *DB) Close() error {
	err := d.Read.Close()
	if e := d.write.Close(); err == nil {
		err = e
	}
	return err
}
