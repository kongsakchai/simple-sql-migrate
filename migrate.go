package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Sentinel versions for Options.Version.
const (
	// VersionUp applies all pending migrations.
	VersionUp = "VERSION_UP"
	// VersionDown rolls back all applied migrations.
	VersionDown = "VERSION_DOWN"

	defaultTimeout   = 10 * time.Minute
	defaultTableName = "schema_migrations"
	suffixUp         = "up.sql"
	suffixDown       = "down.sql"

	versionSeparator = "_"
)

// ErrNoSource is returned when Options.Source is empty.
var ErrNoSource = errors.New("simple migration: no migration source")

// Options controls a migration run.
type Options struct {
	TableName string        // version tracking table (default schema_migrations)
	Source    string        // directory containing migration files (required)
	Version   string        // target version, or the VersionUp / VersionDown sentinel
	Timeout   time.Duration // overall migration timeout (default 10 minutes)
}

// Migrate moves the database to opt.Version: up when the target is ahead of
// (or equal to) the current version, down when it is behind. Each migration
// file runs in its own transaction together with its version bookkeeping.
func Migrate(db *sql.DB, opts ...Options) error {
	opt, err := prepareOptions(opts...)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), opt.Timeout)
	defer cancel()

	if _, err := os.Stat(opt.Source); err != nil {
		return fmt.Errorf("simple migration: %w", err)
	}
	if err := prepareTable(ctx, db, opt.TableName); err != nil {
		return fmt.Errorf("simple migration: %w", err)
	}

	cur, err := currentVersion(ctx, db, opt.TableName)
	if err != nil {
		return fmt.Errorf("simple migration: %w", err)
	}

	if opt.Version == VersionDown || cur > opt.Version {
		return migrateDown(ctx, db, cur, opt)
	}

	return migrateUp(ctx, db, cur, opt)
}

func prepareOptions(opts ...Options) (Options, error) {
	var opt Options
	if len(opts) > 0 {
		opt = opts[0]
	}
	if opt.TableName == "" {
		opt.TableName = defaultTableName
	}
	if opt.Version == "" {
		opt.Version = VersionUp
	}
	if opt.Timeout == 0 {
		opt.Timeout = defaultTimeout
	}
	if opt.Source == "" {
		return Options{}, ErrNoSource
	}
	return opt, nil
}

// execute

func migrateUp(ctx context.Context, db *sql.DB, cur string, opt Options) error {
	files, err := migrateUpFiles(cur, opt)
	if err != nil {
		return err
	}

	for _, m := range files {
		filename := filepath.Join(opt.Source, m.Filename)
		b, err := os.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("simple migration: %w", err)
		}

		statements := []string{string(b), addVersion(m.Version, opt)}
		if err := execScripts(ctx, db, statements); err != nil {
			return fmt.Errorf("simple migration: apply %s: %w", m.Filename, err)
		}
	}

	return nil
}

func migrateDown(ctx context.Context, db *sql.DB, cur string, opt Options) error {
	files, err := migrateDownFiles(cur, opt)
	if err != nil {
		return err
	}

	for _, m := range files {
		filename := filepath.Join(opt.Source, m.Filename)
		b, err := os.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("simple migration: %w", err)
		}

		statements := []string{string(b), removeVersion(m.Version, opt)}
		if err := execScripts(ctx, db, statements); err != nil {
			return fmt.Errorf("simple migration: apply %s: %w", m.Filename, err)
		}
	}
	return nil
}

func addVersion(version string, opt Options) string {
	return fmt.Sprintf("INSERT INTO %s (version) VALUES ('%s');", opt.TableName, escapeSQLString(version))
}

func removeVersion(version string, opt Options) string {
	return fmt.Sprintf("DELETE FROM %s WHERE version = '%s';", opt.TableName, escapeSQLString(version))
}

func escapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func execScripts(ctx context.Context, db *sql.DB, statements []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// Rollback is a no-op once the transaction is committed.
	defer func() { _ = tx.Rollback() }()

	for _, stmt := range statements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func prepareTable(ctx context.Context, db *sql.DB, tableName string) error {
	query := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %s (
		version VARCHAR(100) PRIMARY KEY,
		timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`, tableName)
	_, err := db.ExecContext(ctx, query)
	return err
}

// currentVersion returns the highest applied version. Versions are compared
// lexicographically, the same rule used everywhere else in this package.
func currentVersion(ctx context.Context, db *sql.DB, tableName string) (string, error) {
	query := fmt.Sprintf("SELECT version FROM %s ORDER BY version DESC LIMIT 1", tableName)
	var version string
	err := db.QueryRowContext(ctx, query).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	return version, nil
}

// migration file

type migrationFile struct {
	Version  string
	Filename string
}

func migrationFiles(source string, suffix string) ([]migrationFile, error) {
	files, err := os.ReadDir(source)
	if err != nil {
		return nil, err
	}

	var migrationFiles []migrationFile
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		n := file.Name()
		if !strings.HasSuffix(n, suffix) {
			continue
		}
		version, _, found := strings.Cut(n, versionSeparator)
		if !found {
			continue
		}
		migrationFiles = append(migrationFiles, migrationFile{Version: version, Filename: n})
	}

	return migrationFiles, nil
}

// migrateUpFiles returns the up migrations to apply, i.e. every file with
// cur < version <= opt.Version, sorted ascending.
func migrateUpFiles(cur string, opt Options) ([]migrationFile, error) {
	files, err := migrationFiles(opt.Source, suffixUp)
	if err != nil {
		return nil, err
	}

	pending := make([]migrationFile, 0, len(files))
	for _, file := range files {
		// already applied
		if file.Version <= cur {
			continue
		}
		// beyond the target version
		if opt.Version != VersionUp && file.Version > opt.Version {
			continue
		}
		pending = append(pending, file)
	}

	slices.SortFunc(pending, func(a, b migrationFile) int {
		return strings.Compare(a.Version, b.Version)
	})

	return pending, nil
}

// migrateDownFiles returns the down migrations to apply, i.e. every file with
// opt.Version < version <= cur, sorted descending.
func migrateDownFiles(cur string, opt Options) ([]migrationFile, error) {
	files, err := migrationFiles(opt.Source, suffixDown)
	if err != nil {
		return nil, err
	}

	pending := make([]migrationFile, 0, len(files))
	for _, file := range files {
		// not applied yet
		if file.Version > cur {
			continue
		}
		// at or below the target version: stays applied
		if opt.Version != VersionDown && file.Version <= opt.Version {
			continue
		}
		pending = append(pending, file)
	}

	slices.SortFunc(pending, func(a, b migrationFile) int {
		return strings.Compare(b.Version, a.Version)
	})

	return pending, nil
}
