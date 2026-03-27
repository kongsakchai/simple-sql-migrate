package migrate

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	_ "modernc.org/sqlite"
)

func TestInitTable(t *testing.T) {
	t.Run("should create migration table if it does not exist", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()

		err = initTable(db, "test_migrations")
		assert.NoError(t, err)
	})
}

func TestGetVersion(t *testing.T) {
	t.Run("should return empty version when no migrations applied", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()

		err = initTable(db, "test_migrations")
		assert.NoError(t, err)

		version, err := getVersion(db, "test_migrations")
		assert.NoError(t, err)
		assert.Equal(t, "", version)
	})

	t.Run("should return current version after applying migration", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()

		err = initTable(db, "test_migrations")
		assert.NoError(t, err)

		_, err = db.Exec("INSERT INTO test_migrations (version) VALUES ('001')")
		assert.NoError(t, err)

		version, err := getVersion(db, "test_migrations")
		assert.NoError(t, err)
		assert.Equal(t, "001", version)
	})

	t.Run("should return error when database query fails", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()

		_, err = getVersion(db, "nonexistent_table")
		assert.Error(t, err)
	})
}

func TestVersion(t *testing.T) {
	t.Run("should return empty version when no migrations applied", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()

		version, err := Version(db, "test_migrations")
		assert.NoError(t, err)
		assert.Equal(t, "", version)
	})

	t.Run("should return current version after applying migration", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()

		err = initTable(db, "test_migrations")
		assert.NoError(t, err)

		_, err = db.Exec("INSERT INTO test_migrations (version) VALUES ('001')")
		assert.NoError(t, err)

		version, err := Version(db, "test_migrations")
		assert.NoError(t, err)
		assert.Equal(t, "001", version)
	})

	t.Run("should return error when database query fails", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		db.Close()

		_, err = Version(db, "nonexistent_table")
		assert.Error(t, err)
	})
}

func TestHelper(t *testing.T) {
	t.Run("check table name", func(t *testing.T) {
		t.Run("should return default table name when table name is empty", func(t *testing.T) {
			assert.Equal(t, DefaultTableName, checkTableName())
		})

		t.Run("should return default table name when table name is empty string", func(t *testing.T) {
			assert.Equal(t, DefaultTableName, checkTableName(""))
		})

		t.Run("should return provided table name", func(t *testing.T) {
			assert.Equal(t, "custom_table", checkTableName("custom_table"))
		})
	})

	t.Run("check options", func(t *testing.T) {
		t.Run("should return default options when no options provided", func(t *testing.T) {
			assert.Equal(t, defaultOptions, checkOptions())
		})

		t.Run("should return provided options", func(t *testing.T) {
			opts := Options{
				TableName:        "custom_table",
				Source:           "custom_source",
				Version:          VersionDown,
				Repeat:           RepeatAll,
				VersionSeparator: "-",
			}
			assert.Equal(t, opts, checkOptions(opts))
		})

		t.Run("should fill in default values for missing fields", func(t *testing.T) {
			opts := Options{
				Source: "custom_source",
			}
			expected := defaultOptions
			expected.Source = "custom_source"
			assert.Equal(t, expected, checkOptions(opts))
		})
	})
}

func TestGetMigrationFiles(t *testing.T) {
	dir := "./test"
	t.Run("should return migration files with up suffix", func(t *testing.T) {
		// arrange
		expected := []string{
			"001_example.up.sql",
			"002_example.up.sql",
		}

		// act
		files, err := getMigrationFiles(dir, SuffixUp)

		// assert
		assert.NoError(t, err)
		assert.Equal(t, expected, files)
	})

	t.Run("should return migration files with down suffix", func(t *testing.T) {
		// arrange
		expected := []string{
			"001_example.down.sql",
			"002_example.down.sql",
		}

		// act
		files, err := getMigrationFiles(dir, SuffixDown)

		// assert
		assert.NoError(t, err)
		assert.Equal(t, expected, files)
	})

	t.Run("should return error when source directory does not exist", func(t *testing.T) {
		files, err := getMigrationFiles("./nonexistent", SuffixUp)
		assert.Error(t, err)
		assert.Nil(t, files)
	})
}

func TestFilterUpMigrationFiles(t *testing.T) {
	type testcase struct {
		name     string
		files    []string
		cur      string
		target   string
		repeat   RepeatAction
		expected []migrationFile
	}

	testcases := []testcase{
		{
			name: "should return all up migration files when current version is empty and target version is up",
			files: []string{
				"001_example.up.sql",
				"002_example.up.sql",
				"003_example.up.sql",
			},
			cur:      "",
			target:   VersionUp,
			repeat:   NoRepeat,
			expected: []migrationFile{{Version: "001", Filename: "001_example.up.sql"}, {Version: "002", Filename: "002_example.up.sql"}, {Version: "003", Filename: "003_example.up.sql"}},
		},
		{
			name: "should return up migration files after current version and before target version",
			files: []string{
				"001_example.up.sql",
				"002_example.up.sql",
				"003_example.up.sql",
			},
			cur:      "001",
			target:   "003",
			repeat:   NoRepeat,
			expected: []migrationFile{{Version: "002", Filename: "002_example.up.sql"}, {Version: "003", Filename: "003_example.up.sql"}},
		},
		{
			name: "should return up migration files after current version when target version is up",
			files: []string{
				"001_example.up.sql",
				"002_example.up.sql",
				"003_example.up.sql",
			},
			cur:      "001",
			target:   VersionUp,
			repeat:   NoRepeat,
			expected: []migrationFile{{Version: "002", Filename: "002_example.up.sql"}, {Version: "003", Filename: "003_example.up.sql"}},
		},
		{
			name: "should return empty list when current version is same as target version",
			files: []string{
				"001_example.up.sql",
				"002_example.up.sql",
				"003_example.up.sql",
			},
			cur:      "002",
			target:   "002",
			repeat:   NoRepeat,
			expected: nil,
		},
		{
			name: "should return all up migration files when version file after current version but repeat is RepeatAll",
			files: []string{
				"001_example.up.sql",
				"002_example.up.sql",
				"003_example.up.sql",
			},
			cur:      "001",
			target:   VersionUp,
			repeat:   RepeatAll,
			expected: []migrationFile{{Version: "001", Filename: "001_example.up.sql"}, {Version: "002", Filename: "002_example.up.sql"}, {Version: "003", Filename: "003_example.up.sql"}},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			// act
			result := filterUpMigrationFiles(tc.files, tc.cur, tc.target, VersionSeparator, tc.repeat)

			// assert
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestFilterDownMigrationFiles(t *testing.T) {
	type testcase struct {
		name     string
		files    []string
		cur      string
		target   string
		repeat   RepeatAction
		expected []migrationFile
	}

	testcases := []testcase{
		{
			name: "should return nil when current version is empty and target version is down",
			files: []string{
				"001_example.down.sql",
				"003_example.down.sql",
				"002_example.down.sql",
			},
			cur:      "",
			target:   VersionDown,
			repeat:   NoRepeat,
			expected: nil,
		},
		{
			name: "should return down migration files before current version and after target version",
			files: []string{
				"001_example.down.sql",
				"003_example.down.sql",
				"002_example.down.sql",
			},
			cur:      "003",
			target:   "001",
			repeat:   NoRepeat,
			expected: []migrationFile{{Version: "002", Filename: "002_example.down.sql"}, {Version: "001", Filename: "001_example.down.sql"}},
		},
		{
			name: "should return down migration files before current version when target version is down",
			files: []string{
				"001_example.down.sql",
				"003_example.down.sql",
				"002_example.down.sql",
			},
			cur:      "003",
			target:   VersionDown,
			repeat:   NoRepeat,
			expected: []migrationFile{{Version: "002", Filename: "002_example.down.sql"}, {Version: "001", Filename: "001_example.down.sql"}},
		},
		{
			name: "should return empty list when current version is same as target version",
			files: []string{
				"001_example.down.sql",
				"003_example.down.sql",
				"002_example.down.sql",
			},
			cur:      "002",
			target:   "002",
			repeat:   NoRepeat,
			expected: nil,
		},
		{
			name: "should return all down migration files when version file before current version but repeat is RepeatAll",
			files: []string{
				"001_example.down.sql",
				"003_example.down.sql",
				"002_example.down.sql",
			},
			cur:      "003",
			target:   VersionDown,
			repeat:   RepeatAll,
			expected: []migrationFile{{Version: "003", Filename: "003_example.down.sql"}, {Version: "002", Filename: "002_example.down.sql"}, {Version: "001", Filename: "001_example.down.sql"}},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			// act
			result := filterDownMigrationFiles(tc.files, tc.cur, tc.target, VersionSeparator, tc.repeat)

			// assert
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestSplitSQLStatements(t *testing.T) {
	type testcase struct {
		name     string
		query    string
		expected []string
	}

	testcases := []testcase{
		{
			name:     "should split simple SQL statements",
			query:    "SELECT * FROM users; SELECT * FROM orders;",
			expected: []string{"SELECT * FROM users", "SELECT * FROM orders"},
		},
		{
			name:     "should not split semicolons inside single quotes",
			query:    "INSERT INTO users (name) VALUES ('John; Doe'); SELECT * FROM users;",
			expected: []string{"INSERT INTO users (name) VALUES ('John; Doe')", "SELECT * FROM users"},
		},
		{
			name:     "should not split semicolons inside double quotes",
			query:    "INSERT INTO users (name) VALUES (\"John; Doe\"); SELECT * FROM users;",
			expected: []string{"INSERT INTO users (name) VALUES (\"John; Doe\")", "SELECT * FROM users"},
		},
		{
			name:     "should not split semicolons inside backticks",
			query:    "SELECT `column;name` FROM users; SELECT * FROM users;",
			expected: []string{"SELECT `column;name` FROM users", "SELECT * FROM users"},
		},
		{
			name:     "should not split semicolons inside line comments",
			query:    "SELECT * FROM users; -- This is a comment; with a semicolon\nSELECT * FROM orders;",
			expected: []string{"SELECT * FROM users", "SELECT * FROM orders"},
		},
		{
			name:     "should not split semicolons inside block comments",
			query:    "SELECT * FROM users; /* This is a block comment; with a semicolon */ SELECT * FROM orders;",
			expected: []string{"SELECT * FROM users", "SELECT * FROM orders"},
		},
		{
			name:     "should keep final statement without trailing semicolon",
			query:    "CREATE TABLE test (id INTEGER); INSERT INTO test (id) VALUES (1)",
			expected: []string{"CREATE TABLE test (id INTEGER)", "INSERT INTO test (id) VALUES (1)"},
		},
		{
			name:     "should not split escaped single quotes",
			query:    "INSERT INTO users (name) VALUES ('John\\'s; Doe'); SELECT * FROM users;",
			expected: []string{"INSERT INTO users (name) VALUES ('John\\'s; Doe')", "SELECT * FROM users"},
		},
		{
			name:     "should not split doubled single quotes",
			query:    "INSERT INTO users (name) VALUES ('John''; Doe'); SELECT * FROM users;",
			expected: []string{"INSERT INTO users (name) VALUES ('John''; Doe')", "SELECT * FROM users"},
		},
		{
			name:     "should not split escaped double quotes",
			query:    "INSERT INTO users (name) VALUES (\"John\\\"; Doe\"); SELECT * FROM users;",
			expected: []string{"INSERT INTO users (name) VALUES (\"John\\\"; Doe\")", "SELECT * FROM users"},
		},
		{
			name:     "should not split doubled double quotes",
			query:    "INSERT INTO users (name) VALUES (\"John\"\"; Doe\"); SELECT * FROM users;",
			expected: []string{"INSERT INTO users (name) VALUES (\"John\"\"; Doe\")", "SELECT * FROM users"},
		},
		{
			name:     "should not split doubled backticks",
			query:    "SELECT `column``;name` FROM users; SELECT * FROM users;",
			expected: []string{"SELECT `column``;name` FROM users", "SELECT * FROM users"},
		},
		{
			name:     "should ignore standalone comments between statements",
			query:    "-- heading comment;\nSELECT * FROM users; /* trailing comment; */",
			expected: []string{"SELECT * FROM users"},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			result := splitSQLStatements([]byte(tc.query))
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestMigrate(t *testing.T) {
	t.Run("should return error when source directory does not exist", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()

		err = Migrate(db, Options{Source: "./missing"})
		assert.Error(t, err)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("should return error when version lookup fails", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		assert.NoError(t, db.Close())

		err = Migrate(db, Options{Source: "./test"})
		assert.Error(t, err)
	})

	t.Run("should return nil when current version matches and repeat is disabled", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()

		assert.NoError(t, initTable(db, "test_migrations"))
		_, err = db.Exec("INSERT INTO test_migrations (version) VALUES ('001')")
		assert.NoError(t, err)

		err = Migrate(db, Options{Source: "./test", TableName: "test_migrations", Version: "001", Repeat: NoRepeat})
		assert.NoError(t, err)
	})

	t.Run("should return nil when current version matches and repeat is enabled", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()
		dir := t.TempDir()

		assert.NoError(t, initTable(db, "test_migrations"))
		_, err = db.Exec("INSERT INTO test_migrations (version) VALUES ('001')")
		assert.NoError(t, err)

		err = Migrate(db, Options{Source: dir, TableName: "test_migrations", Version: "001", Repeat: RepeatAll})
		assert.NoError(t, err)
	})

	t.Run("should call migrate up when current version is below target version", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()
		dir := t.TempDir()

		err = Migrate(db, Options{Source: dir, Version: "001"})
		assert.NoError(t, err)
	})

	t.Run("should call migrate down when current version is above target version", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()
		dir := t.TempDir()

		assert.NoError(t, initTable(db, "test_migrations"))
		_, err = db.Exec("INSERT INTO test_migrations (version) VALUES ('003')")
		assert.NoError(t, err)

		err = Migrate(db, Options{Source: dir, TableName: "test_migrations", Version: "001"})
		assert.NoError(t, err)
	})
}

func TestMigrateUp(t *testing.T) {
	t.Run("should return error when migration source directory does not exist", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()

		err = migrateUp(db, Options{Source: filepath.Join(t.TempDir(), "missing"), TableName: DefaultTableName, Version: "001", Repeat: RepeatAll, VersionSeparator: VersionSeparator})
		assert.Error(t, err)
	})

	t.Run("should apply up migration statements", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()
		assert.NoError(t, initTable(db, DefaultTableName))

		dir := t.TempDir()
		file := filepath.Join(dir, "001_example.up.sql")
		content := "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);\n-- comment;\nINSERT INTO users (name) VALUES ('alice');"
		assert.NoError(t, os.WriteFile(file, []byte(content), 0o644))

		assert.NoError(t, migrateUp(db, Options{Source: dir, TableName: DefaultTableName, Version: "001", Repeat: RepeatAll, VersionSeparator: VersionSeparator}))

		var count int
		assert.NoError(t, db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count))
		assert.Equal(t, 1, count)
	})

	t.Run("should return error when migration sql is invalid", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()
		assert.NoError(t, initTable(db, DefaultTableName))

		dir := t.TempDir()
		file := filepath.Join(dir, "001_example.up.sql")
		assert.NoError(t, os.WriteFile(file, []byte("INVALID SQL;"), 0o644))

		err = migrateUp(db, Options{Source: dir, TableName: DefaultTableName, Version: "001", Repeat: RepeatAll, VersionSeparator: VersionSeparator})
		assert.Error(t, err)
	})

	t.Run("should return error when migration file cannot be read", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()
		assert.NoError(t, initTable(db, DefaultTableName))

		dir := t.TempDir()
		file := filepath.Join(dir, "001_example.up.sql")
		assert.NoError(t, os.Symlink(filepath.Join(dir, "missing.sql"), file))

		err = migrateUp(db, Options{Source: dir, TableName: DefaultTableName, Version: "001", Repeat: RepeatAll, VersionSeparator: VersionSeparator})
		assert.Error(t, err)
	})
}

func TestMigrateDown(t *testing.T) {
	t.Run("should return error when migration source directory does not exist", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()

		err = migrateDown(db, Options{Source: filepath.Join(t.TempDir(), "missing"), TableName: DefaultTableName, Version: "001", Repeat: RepeatAll, VersionSeparator: VersionSeparator})
		assert.Error(t, err)
	})

	t.Run("should apply down migration statements", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()
		assert.NoError(t, initTable(db, DefaultTableName))
		_, err = db.Exec("INSERT INTO schema_migrations (version) VALUES ('001')")
		assert.NoError(t, err)

		dir := t.TempDir()
		file := filepath.Join(dir, "001_example.down.sql")
		content := "CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY, name TEXT);\nDELETE FROM users WHERE name = 'alice';"
		assert.NoError(t, os.WriteFile(file, []byte(content), 0o644))

		assert.NoError(t, migrateDown(db, Options{Source: dir, TableName: DefaultTableName, Version: "001", Repeat: RepeatAll, VersionSeparator: VersionSeparator}))

		var count int
		assert.NoError(t, db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = '001'").Scan(&count))
		assert.Equal(t, 0, count)
	})

	t.Run("should return error when migration sql is invalid", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()
		assert.NoError(t, initTable(db, DefaultTableName))
		_, err = db.Exec("INSERT INTO schema_migrations (version) VALUES ('001')")
		assert.NoError(t, err)

		dir := t.TempDir()
		file := filepath.Join(dir, "001_example.down.sql")
		assert.NoError(t, os.WriteFile(file, []byte("INVALID SQL;"), 0o644))

		err = migrateDown(db, Options{Source: dir, TableName: DefaultTableName, Version: "001", Repeat: RepeatAll, VersionSeparator: VersionSeparator})
		assert.Error(t, err)
	})

	t.Run("should return error when migration file cannot be read", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()
		assert.NoError(t, initTable(db, DefaultTableName))
		_, err = db.Exec("INSERT INTO schema_migrations (version) VALUES ('001')")
		assert.NoError(t, err)

		dir := t.TempDir()
		file := filepath.Join(dir, "001_example.down.sql")
		assert.NoError(t, os.Symlink(filepath.Join(dir, "missing.sql"), file))

		err = migrateDown(db, Options{Source: dir, TableName: DefaultTableName, Version: "001", Repeat: RepeatAll, VersionSeparator: VersionSeparator})
		assert.Error(t, err)
	})
}

func TestAddMigrateStatements(t *testing.T) {
	opt := Options{TableName: DefaultTableName}

	assert.Equal(t, "INSERT INTO schema_migrations (version) VALUES ('001') ON CONFLICT(version) DO UPDATE SET timestamp = CURRENT_TIMESTAMP", addMigrateUpStatements("001", opt))
	assert.Equal(t, "DELETE FROM schema_migrations WHERE version = '001'", addMigrateDownStatements("001", opt))
}

func TestExecScripts(t *testing.T) {
	t.Run("should return begin error when db is closed", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		assert.NoError(t, db.Close())

		err = execScripts(db, []string{"SELECT 1"})
		assert.Error(t, err)
	})

	t.Run("should rollback when statement execution fails", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()

		err = execScripts(db, []string{
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)",
			"INVALID SQL",
		})
		assert.Error(t, err)

		_, err = db.Exec("SELECT COUNT(*) FROM users")
		assert.Error(t, err)
	})

	t.Run("should commit all statements successfully", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		assert.NoError(t, err)
		defer db.Close()

		assert.NoError(t, execScripts(db, []string{
			"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)",
			"INSERT INTO users (name) VALUES ('alice')",
		}))

		var count int
		assert.NoError(t, db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count))
		assert.Equal(t, 1, count)
	})
}
