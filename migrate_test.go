package migrate

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	_ "modernc.org/sqlite"
)

func openSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	assert.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func writeMigrationFile(t *testing.T, dir, name, content string) {
	t.Helper()
	assert.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

func versionsInTable(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query("SELECT version FROM " + table + " ORDER BY version")
	assert.NoError(t, err)
	defer rows.Close()

	var versions []string
	for rows.Next() {
		var v string
		assert.NoError(t, rows.Scan(&v))
		versions = append(versions, v)
	}
	return versions
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	assert.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name,
	).Scan(&n))
	return n > 0
}

func TestPrepareTable(t *testing.T) {
	t.Run("should create the migration table", func(t *testing.T) {
		db := openSQLite(t)

		assert.NoError(t, prepareTable(t.Context(), db, "test_migrations"))
		assert.True(t, tableExists(t, db, "test_migrations"))
	})

	t.Run("should be idempotent", func(t *testing.T) {
		db := openSQLite(t)

		assert.NoError(t, prepareTable(t.Context(), db, "test_migrations"))
		assert.NoError(t, prepareTable(t.Context(), db, "test_migrations"))
	})
}

func TestCurrentVersion(t *testing.T) {
	t.Run("should return empty version when no migrations applied", func(t *testing.T) {
		db := openSQLite(t)
		assert.NoError(t, prepareTable(t.Context(), db, defaultTableName))

		version, err := currentVersion(t.Context(), db, defaultTableName)
		assert.NoError(t, err)
		assert.Equal(t, "", version)
	})

	t.Run("should return the highest applied version", func(t *testing.T) {
		db := openSQLite(t)
		assert.NoError(t, prepareTable(t.Context(), db, defaultTableName))

		// applied in order, likely within the same second, so the result
		// must not depend on timestamp ordering
		for _, v := range []string{"001", "002", "003"} {
			_, err := db.Exec("INSERT INTO schema_migrations (version) VALUES ('" + v + "')")
			assert.NoError(t, err)
		}

		version, err := currentVersion(t.Context(), db, defaultTableName)
		assert.NoError(t, err)
		assert.Equal(t, "003", version)
	})

	t.Run("should return error when database query fails", func(t *testing.T) {
		db := openSQLite(t)

		_, err := currentVersion(t.Context(), db, "nonexistent_table")
		assert.Error(t, err)
	})
}

func TestPrepareOptions(t *testing.T) {
	t.Run("should fill in default values", func(t *testing.T) {
		opt, err := prepareOptions(Options{Source: "./migrations"})
		assert.NoError(t, err)
		assert.Equal(t, Options{
			TableName: defaultTableName,
			Source:    "./migrations",
			Version:   VersionUp,
			Timeout:   defaultTimeout,
		}, opt)
	})

	t.Run("should keep provided values", func(t *testing.T) {
		opt, err := prepareOptions(Options{TableName: "custom", Source: "./migrations", Version: "001", Timeout: time.Minute})
		assert.NoError(t, err)
		assert.Equal(t, Options{TableName: "custom", Source: "./migrations", Version: "001", Timeout: time.Minute}, opt)
	})

	t.Run("should return error when source is empty", func(t *testing.T) {
		_, err := prepareOptions()
		assert.ErrorIs(t, err, ErrNoSource)
	})
}

func TestMigrationFiles(t *testing.T) {
	t.Run("should return migration files with up suffix", func(t *testing.T) {
		files, err := migrationFiles("./test", suffixUp)
		assert.NoError(t, err)
		assert.Equal(t, []migrationFile{
			{Version: "001", Filename: "001_example.up.sql"},
			{Version: "002", Filename: "002_example.up.sql"},
		}, files)
	})

	t.Run("should return migration files with down suffix", func(t *testing.T) {
		files, err := migrationFiles("./test", suffixDown)
		assert.NoError(t, err)
		assert.Equal(t, []migrationFile{
			{Version: "001", Filename: "001_example.down.sql"},
			{Version: "002", Filename: "002_example.down.sql"},
		}, files)
	})

	t.Run("should skip files without the version separator", func(t *testing.T) {
		dir := t.TempDir()
		writeMigrationFile(t, dir, "001_example.up.sql", "SELECT 1;")
		writeMigrationFile(t, dir, "noseparator.up.sql", "SELECT 1;")

		files, err := migrationFiles(dir, suffixUp)
		assert.NoError(t, err)
		assert.Equal(t, []migrationFile{{Version: "001", Filename: "001_example.up.sql"}}, files)
	})

	t.Run("should return error when source directory does not exist", func(t *testing.T) {
		files, err := migrationFiles("./nonexistent", suffixUp)
		assert.Error(t, err)
		assert.Nil(t, files)
	})
}

func TestMigrateUpFiles(t *testing.T) {
	type testcase struct {
		name     string
		cur      string
		target   string
		expected []migrationFile
	}

	testcases := []testcase{
		{
			name:   "should return all up migration files when current version is empty",
			cur:    "",
			target: VersionUp,
			expected: []migrationFile{
				{Version: "001", Filename: "001_example.up.sql"},
				{Version: "002", Filename: "002_example.up.sql"},
				{Version: "003", Filename: "003_example.up.sql"},
			},
		},
		{
			name:   "should return up migration files after current version up to target version",
			cur:    "001",
			target: "003",
			expected: []migrationFile{
				{Version: "002", Filename: "002_example.up.sql"},
				{Version: "003", Filename: "003_example.up.sql"},
			},
		},
		{
			name:   "should return up migration files after current version when target is up",
			cur:    "001",
			target: VersionUp,
			expected: []migrationFile{
				{Version: "002", Filename: "002_example.up.sql"},
				{Version: "003", Filename: "003_example.up.sql"},
			},
		},
		{
			name:     "should return no files when current version is same as target version",
			cur:      "002",
			target:   "002",
			expected: []migrationFile{},
		},
		{
			name:     "should return no files when current version is the highest one",
			cur:      "003",
			target:   VersionUp,
			expected: []migrationFile{},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range []string{
				"001_example.up.sql",
				"002_example.up.sql",
				"003_example.up.sql",
			} {
				writeMigrationFile(t, dir, name, "SELECT 1;")
			}

			result, err := migrateUpFiles(tc.cur, Options{Source: dir, Version: tc.target})
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestMigrateDownFiles(t *testing.T) {
	type testcase struct {
		name     string
		cur      string
		target   string
		expected []migrationFile
	}

	testcases := []testcase{
		{
			name:     "should return no files when current version is empty",
			cur:      "",
			target:   VersionDown,
			expected: []migrationFile{},
		},
		{
			name:   "should return down migration files down to target version in reverse order",
			cur:    "003",
			target: "001",
			expected: []migrationFile{
				{Version: "003", Filename: "003_example.down.sql"},
				{Version: "002", Filename: "002_example.down.sql"},
			},
		},
		{
			name:   "should return all down migration files when target is down",
			cur:    "003",
			target: VersionDown,
			expected: []migrationFile{
				{Version: "003", Filename: "003_example.down.sql"},
				{Version: "002", Filename: "002_example.down.sql"},
				{Version: "001", Filename: "001_example.down.sql"},
			},
		},
		{
			name:     "should return no files when current version is same as target version",
			cur:      "002",
			target:   "002",
			expected: []migrationFile{},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range []string{
				"001_example.down.sql",
				"002_example.down.sql",
				"003_example.down.sql",
			} {
				writeMigrationFile(t, dir, name, "SELECT 1;")
			}

			result, err := migrateDownFiles(tc.cur, Options{Source: dir, Version: tc.target})
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestVersionStatements(t *testing.T) {
	opt := Options{TableName: defaultTableName}

	assert.Equal(t, "INSERT INTO schema_migrations (version) VALUES ('001');", addVersion("001", opt))
	assert.Equal(t, "DELETE FROM schema_migrations WHERE version = '001';", removeVersion("001", opt))
	assert.Equal(t, "INSERT INTO schema_migrations (version) VALUES ('a''b');", addVersion("a'b", opt))
}

func TestMigrate(t *testing.T) {
	t.Run("should return error when no source provided", func(t *testing.T) {
		db := openSQLite(t)

		err := Migrate(db)
		assert.ErrorIs(t, err, ErrNoSource)
	})

	t.Run("should return error when current version lookup fails", func(t *testing.T) {
		db := openSQLite(t)
		// a pre-existing object with the same name makes CREATE TABLE
		// IF NOT EXISTS a no-op, then the version query fails against it
		_, err := db.Exec("CREATE VIEW schema_migrations AS SELECT 1 AS x")
		assert.NoError(t, err)

		err = Migrate(db, Options{Source: "./test"})
		assert.Error(t, err)
	})

	t.Run("should return error when the run exceeds its timeout", func(t *testing.T) {
		db := openSQLite(t)

		err := Migrate(db, Options{Source: "./test", Timeout: time.Nanosecond})
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})
	t.Run("should return error when source directory does not exist", func(t *testing.T) {
		db := openSQLite(t)

		err := Migrate(db, Options{Source: "./missing"})
		assert.Error(t, err)
		assert.True(t, errors.Is(err, fs.ErrNotExist))
	})

	t.Run("should return error when database is closed", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		assert.NoError(t, db.Close())

		err = Migrate(db, Options{Source: "./test"})
		assert.Error(t, err)
	})

	t.Run("should return nil when current version matches target version", func(t *testing.T) {
		db := openSQLite(t)
		assert.NoError(t, prepareTable(t.Context(), db, defaultTableName))
		_, err := db.Exec("INSERT INTO schema_migrations (version) VALUES ('002')")
		assert.NoError(t, err)

		err = Migrate(db, Options{Source: "./test", Version: "002"})
		assert.NoError(t, err)
		assert.Equal(t, []string{"002"}, versionsInTable(t, db, defaultTableName))
	})

	t.Run("should apply all pending up migrations on a fresh database", func(t *testing.T) {
		db := openSQLite(t)
		dir := t.TempDir()
		writeMigrationFile(t, dir, "001_t1.up.sql", "CREATE TABLE t1 (id INTEGER);")
		writeMigrationFile(t, dir, "002_t2.up.sql", "CREATE TABLE t2 (id INTEGER);")
		writeMigrationFile(t, dir, "003_t3.up.sql", "CREATE TABLE t3 (id INTEGER);")

		assert.NoError(t, Migrate(db, Options{Source: dir}))

		// versions must be recorded verbatim, not coerced (unquoted 001 would become 1)
		assert.Equal(t, []string{"001", "002", "003"}, versionsInTable(t, db, defaultTableName))
		assert.True(t, tableExists(t, db, "t1"))
		assert.True(t, tableExists(t, db, "t2"))
		assert.True(t, tableExists(t, db, "t3"))
	})

	t.Run("should migrate down to the target version", func(t *testing.T) {
		db := openSQLite(t)
		dir := t.TempDir()
		writeMigrationFile(t, dir, "001_t1.up.sql", "CREATE TABLE t1 (id INTEGER);")
		writeMigrationFile(t, dir, "002_t2.up.sql", "CREATE TABLE t2 (id INTEGER);")
		writeMigrationFile(t, dir, "003_t3.up.sql", "CREATE TABLE t3 (id INTEGER);")
		assert.NoError(t, Migrate(db, Options{Source: dir}))

		writeMigrationFile(t, dir, "001_t1.down.sql", "DROP TABLE t1;")
		writeMigrationFile(t, dir, "002_t2.down.sql", "DROP TABLE t2;")
		writeMigrationFile(t, dir, "003_t3.down.sql", "DROP TABLE t3;")

		assert.NoError(t, Migrate(db, Options{Source: dir, Version: "001"}))

		assert.Equal(t, []string{"001"}, versionsInTable(t, db, defaultTableName))
		assert.True(t, tableExists(t, db, "t1"))
		assert.False(t, tableExists(t, db, "t2"))
		assert.False(t, tableExists(t, db, "t3"))
	})

	t.Run("should migrate all the way down with the down sentinel", func(t *testing.T) {
		db := openSQLite(t)
		dir := t.TempDir()
		writeMigrationFile(t, dir, "001_t1.up.sql", "CREATE TABLE t1 (id INTEGER);")
		assert.NoError(t, Migrate(db, Options{Source: dir}))
		writeMigrationFile(t, dir, "001_t1.down.sql", "DROP TABLE t1;")

		// regression: VERSION_DOWN must route down even though numeric
		// versions compare lower than the sentinel string
		assert.NoError(t, Migrate(db, Options{Source: dir, Version: VersionDown}))

		assert.Empty(t, versionsInTable(t, db, defaultTableName))
		assert.False(t, tableExists(t, db, "t1"))
	})

	t.Run("should rollback failed migration without recording its version", func(t *testing.T) {
		db := openSQLite(t)
		dir := t.TempDir()
		writeMigrationFile(t, dir, "001_ok.up.sql", "CREATE TABLE t1 (id INTEGER);")
		writeMigrationFile(t, dir, "002_bad.up.sql", "CREATE TABLE t2 (id INTEGER); INVALID SQL;")

		err := Migrate(db, Options{Source: dir})
		assert.Error(t, err)

		assert.Equal(t, []string{"001"}, versionsInTable(t, db, defaultTableName))
		assert.True(t, tableExists(t, db, "t1"))
		assert.False(t, tableExists(t, db, "t2"))
	})
}

func TestMigrateUp(t *testing.T) {
	t.Run("should return error when migration source directory does not exist", func(t *testing.T) {
		db := openSQLite(t)

		err := migrateUp(t.Context(), db, "", Options{Source: filepath.Join(t.TempDir(), "missing"), TableName: defaultTableName, Version: VersionUp})
		assert.Error(t, err)
	})

	t.Run("should apply up migration statements", func(t *testing.T) {
		db := openSQLite(t)
		assert.NoError(t, prepareTable(t.Context(), db, defaultTableName))

		dir := t.TempDir()
		content := "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);\n-- comment;\nINSERT INTO users (name) VALUES ('alice');"
		writeMigrationFile(t, dir, "001_example.up.sql", content)

		assert.NoError(t, migrateUp(t.Context(), db, "", Options{Source: dir, TableName: defaultTableName, Version: VersionUp}))

		var count int
		assert.NoError(t, db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count))
		assert.Equal(t, 1, count)
		assert.Equal(t, []string{"001"}, versionsInTable(t, db, defaultTableName))
	})

	t.Run("should return error when migration sql is invalid", func(t *testing.T) {
		db := openSQLite(t)
		assert.NoError(t, prepareTable(t.Context(), db, defaultTableName))

		dir := t.TempDir()
		writeMigrationFile(t, dir, "001_example.up.sql", "INVALID SQL;")

		err := migrateUp(t.Context(), db, "", Options{Source: dir, TableName: defaultTableName, Version: VersionUp})
		assert.Error(t, err)
	})

	t.Run("should return error when migration file cannot be read", func(t *testing.T) {
		db := openSQLite(t)
		assert.NoError(t, prepareTable(t.Context(), db, defaultTableName))

		dir := t.TempDir()
		assert.NoError(t, os.Symlink(filepath.Join(dir, "missing.sql"), filepath.Join(dir, "001_example.up.sql")))

		err := migrateUp(t.Context(), db, "", Options{Source: dir, TableName: defaultTableName, Version: VersionUp})
		assert.Error(t, err)
	})
}

func TestMigrateDown(t *testing.T) {
	t.Run("should return error when migration source directory does not exist", func(t *testing.T) {
		db := openSQLite(t)

		err := migrateDown(t.Context(), db, "001", Options{Source: filepath.Join(t.TempDir(), "missing"), TableName: defaultTableName, Version: VersionDown})
		assert.Error(t, err)
	})

	t.Run("should apply down migration statements and remove the version", func(t *testing.T) {
		db := openSQLite(t)
		assert.NoError(t, prepareTable(t.Context(), db, defaultTableName))
		_, err := db.Exec("INSERT INTO schema_migrations (version) VALUES ('001')")
		assert.NoError(t, err)

		dir := t.TempDir()
		content := "CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY, name TEXT);\nDELETE FROM users WHERE name = 'alice';"
		writeMigrationFile(t, dir, "001_example.down.sql", content)

		assert.NoError(t, migrateDown(t.Context(), db, "001", Options{Source: dir, TableName: defaultTableName, Version: VersionDown}))

		assert.Empty(t, versionsInTable(t, db, defaultTableName))
	})

	t.Run("should return error when migration sql is invalid", func(t *testing.T) {
		db := openSQLite(t)
		assert.NoError(t, prepareTable(t.Context(), db, defaultTableName))
		_, err := db.Exec("INSERT INTO schema_migrations (version) VALUES ('001')")
		assert.NoError(t, err)

		dir := t.TempDir()
		writeMigrationFile(t, dir, "001_example.down.sql", "INVALID SQL;")

		err = migrateDown(t.Context(), db, "001", Options{Source: dir, TableName: defaultTableName, Version: VersionDown})
		assert.Error(t, err)
		// version row must survive the failed rollback
		assert.Equal(t, []string{"001"}, versionsInTable(t, db, defaultTableName))
	})

	t.Run("should return error when migration file cannot be read", func(t *testing.T) {
		db := openSQLite(t)
		assert.NoError(t, prepareTable(t.Context(), db, defaultTableName))
		_, err := db.Exec("INSERT INTO schema_migrations (version) VALUES ('001')")
		assert.NoError(t, err)

		dir := t.TempDir()
		assert.NoError(t, os.Symlink(filepath.Join(dir, "missing.sql"), filepath.Join(dir, "001_example.down.sql")))

		err = migrateDown(t.Context(), db, "001", Options{Source: dir, TableName: defaultTableName, Version: VersionDown})
		assert.Error(t, err)
	})
}

func TestExecScripts(t *testing.T) {
	t.Run("should return begin error when db is closed", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		assert.NoError(t, db.Close())

		err = execScripts(t.Context(), db, []string{"SELECT 1"})
		assert.Error(t, err)
	})

	t.Run("should rollback when statement execution fails", func(t *testing.T) {
		db := openSQLite(t)

		err := execScripts(t.Context(), db, []string{
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)",
			"INVALID SQL",
		})
		assert.Error(t, err)

		_, err = db.Exec("SELECT COUNT(*) FROM users")
		assert.Error(t, err)
	})

	t.Run("should commit all statements successfully", func(t *testing.T) {
		db := openSQLite(t)

		assert.NoError(t, execScripts(t.Context(), db, []string{
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)",
			"INSERT INTO users (name) VALUES ('alice')",
		}))

		var count int
		assert.NoError(t, db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count))
		assert.Equal(t, 1, count)
	})
}
