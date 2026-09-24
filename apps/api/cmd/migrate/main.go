// Command migrate runs SQL migrations from /migrations (bound into the container
// image or pointed to via MIGRATIONS_DIR).  It uses a simple "schema_migrations"
// table so it's compatible with plain PostgreSQL without external tooling.
package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "github.com/lib/pq"

	"github.com/visionforge/visionforge/packages/config"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: migrate <up|down|version> [N]")
		os.Exit(1)
	}
	cmd := os.Args[1]
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	db, err := sql.Open("postgres", cfg.DB.DSN())
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	dir := os.Getenv("MIGRATIONS_DIR")
	if dir == "" {
		// default when running from repository root via docker compose
		dir = "/migrations"
		if _, err := os.Stat(dir); err != nil {
			// fallback relative path for host runs
			dir = "../../../migrations"
		}
	}

	switch cmd {
	case "up":
		n := 0
		if len(os.Args) >= 3 {
			n, _ = strconv.Atoi(os.Args[2])
		}
		if err := Up(db, dir, n); err != nil {
			log.Fatalf("migrate up: %v", err)
		}
	case "down":
		n := 1
		if len(os.Args) >= 3 {
			n, _ = strconv.Atoi(os.Args[2])
		}
		if err := Down(db, dir, n); err != nil {
			log.Fatalf("migrate down: %v", err)
		}
	case "version":
		v, err := Version(db)
		if err != nil {
			log.Fatalf("version: %v", err)
		}
		fmt.Printf("current version: %d\n", v)
	default:
		log.Fatalf("unknown command: %s", cmd)
	}
}

// Up applies pending migrations up to N (or all if N <= 0).
func Up(db *sql.DB, dir string, n int) error {
	files, err := listMigrations(dir, ".up.sql")
	if err != nil {
		return err
	}
	if err := ensureMigrationTable(db); err != nil {
		return err
	}
	current, err := Version(db)
	if err != nil {
		return err
	}
	applied := 0
	for _, f := range files {
		if f.version <= current {
			continue
		}
		content, err := os.ReadFile(f.path)
		if err != nil {
			return err
		}
		log.Printf("applying migration %d: %s", f.version, f.name)
		if err := runInTx(db, func(tx *sql.Tx) error {
			if _, err := tx.Exec(string(content)); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, f.version, f.name); err != nil {
				return err
			}
			return nil
		}); err != nil {
			return fmt.Errorf("migration %d failed: %w", f.version, err)
		}
		applied++
		if n > 0 && applied >= n {
			break
		}
	}
	if applied == 0 {
		log.Println("no pending migrations")
	}
	return nil
}

// Down rolls back up to N migrations using matching .down.sql files.
func Down(db *sql.DB, dir string, n int) error {
	ups, err := listMigrations(dir, ".up.sql")
	if err != nil {
		return err
	}
	downs, err := listMigrations(dir, ".down.sql")
	if err != nil {
		return err
	}
	downMap := map[int]migrationFile{}
	for _, d := range downs {
		downMap[d.version] = d
	}
	current, err := Version(db)
	if err != nil {
		return err
	}
	// walk up files in reverse order
	for i := len(ups) - 1; i >= 0 && n > 0; i-- {
		f := ups[i]
		if f.version > current {
			continue
		}
		d, ok := downMap[f.version]
		if !ok {
			return fmt.Errorf("no down migration for version %d", f.version)
		}
		content, err := os.ReadFile(d.path)
		if err != nil {
			return err
		}
		log.Printf("reverting migration %d: %s", f.version, f.name)
		if err := runInTx(db, func(tx *sql.Tx) error {
			if _, err := tx.Exec(string(content)); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM schema_migrations WHERE version = $1`, f.version); err != nil {
				return err
			}
			return nil
		}); err != nil {
			return fmt.Errorf("down migration %d failed: %w", f.version, err)
		}
		n--
	}
	return nil
}

// Version returns the currently applied schema version (0 if none).
func Version(db *sql.DB) (int, error) {
	if err := ensureMigrationTable(db); err != nil {
		return 0, err
	}
	var v sql.NullInt64
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		return 0, err
	}
	if !v.Valid {
		return 0, nil
	}
	return int(v.Int64), nil
}

func ensureMigrationTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version BIGINT PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`)
	return err
}

type migrationFile struct {
	version int
	name    string
	path    string
}

func listMigrations(dir, suffix string) ([]migrationFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("migrations directory does not exist: %s", dir)
		}
		return nil, err
	}
	var out []migrationFile
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, suffix) {
			continue
		}
		base := strings.TrimSuffix(name, suffix)
		// names look like 000001_init_schema
		parts := strings.SplitN(base, "_", 2)
		if len(parts) == 0 {
			continue
		}
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		out = append(out, migrationFile{version: v, name: name, path: filepath.Join(dir, name)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func runInTx(db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
