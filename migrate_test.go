package migrate

import (
	"database/sql"
	"slices"
	"testing"

	_ "modernc.org/sqlite"
)

func setupDatabase() *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		panic(err)
	}

	return db
}

func TestMigrationUp(t *testing.T) {
	db := setupDatabase()
	defer db.Close()

	m := New(db, "example")

	// Run the Up migration
	if err := m.Up(); err != nil {
		t.Fatalf("Up migration failed: %v", err)
	}

	// Verify the migration was applied
	version, err := m.Version()
	if err != nil {
		t.Fatalf("Failed to get migration version: %v", err)
	}

	if version != "002" {
		t.Errorf("Expected version 0002, got %s", version)
	}
}

func TestMigrationDown(t *testing.T) {
	db := setupDatabase()
	defer db.Close()

	m := New(db, "example")

	// Run the Up migration first
	if err := m.Up(); err != nil {
		t.Fatalf("Up migration failed: %v", err)
	}

	// Now run the Down migration
	if err := m.Down(); err != nil {
		t.Fatalf("Down migration failed: %v", err)
	}

	// Verify the migration was rolled back
	version, err := m.Version()
	if err != nil {
		t.Fatalf("Failed to get migration version: %v", err)
	}

	if version != "" {
		t.Errorf("Expected no migration, got %s", version)
	}
}

func TestSetVersion(t *testing.T) {
	t.Run("should return error for empty version", func(t *testing.T) {
		db := setupDatabase()
		defer db.Close()

		m := New(db, "example")

		err := m.SetVersion("")
		if err == nil {
			t.Error("Expected error for empty version, got nil")
		}
	})

	t.Run("should set version successfully", func(t *testing.T) {
		db := setupDatabase()
		defer db.Close()

		m := New(db, "example")

		// Run the Up migration
		if err := m.Up(); err != nil {
			t.Fatalf("Up migration failed: %v", err)
		}

		// Set a specific version
		err := m.SetVersion("001")
		if err != nil {
			t.Fatalf("SetVersion failed: %v", err)
		}

		// Verify the migration version was set
		version, err := m.Version()
		if err != nil {
			t.Fatalf("Failed to get migration version: %v", err)
		}

		if version != "001" {
			t.Errorf("Expected version 0001, got %s", version)
		}
	})

	t.Run("should set version to current version", func(t *testing.T) {
		db := setupDatabase()
		defer db.Close()

		m := New(db, "example")

		// Run the Up migration
		if err := m.Up(); err != nil {
			t.Fatalf("Up migration failed: %v", err)
		}

		// Get current version
		currentVersion, err := m.Version()
		if err != nil {
			t.Fatalf("Failed to get current version: %v", err)
		}

		// Set the version to the current version
		err = m.SetVersion(currentVersion)
		if err != nil {
			t.Fatalf("SetVersion failed: %v", err)
		}

		// Verify the migration version was set
		version, err := m.Version()
		if err != nil {
			t.Fatalf("Failed to get migration version: %v", err)
		}

		if version != currentVersion {
			t.Errorf("Expected version %s, got %s", currentVersion, version)
		}
	})
}

func TestSplitSQLStatements(t *testing.T) {
	t.Run("should keep semicolons inside quoted strings", func(t *testing.T) {
		query := []byte(`
INSERT INTO example (payload) VALUES ('a:2:{i:1;s:7:"January";i:2;s:8:"February";}');
INSERT INTO example (payload) VALUES ('ok');
`)

		statements := splitSQLStatements(query)

		expected := []string{
			`INSERT INTO example (payload) VALUES ('a:2:{i:1;s:7:"January";i:2;s:8:"February";}')`,
			`INSERT INTO example (payload) VALUES ('ok')`,
		}

		if !slices.Equal(statements, expected) {
			t.Fatalf("expected statements %v, got %v", expected, statements)
		}
	})

	t.Run("should keep final statement without trailing semicolon", func(t *testing.T) {
		query := []byte("CREATE TABLE test (id INTEGER); INSERT INTO test (id) VALUES (1)")

		statements := splitSQLStatements(query)

		expected := []string{
			"CREATE TABLE test (id INTEGER)",
			"INSERT INTO test (id) VALUES (1)",
		}

		if !slices.Equal(statements, expected) {
			t.Fatalf("expected statements %v, got %v", expected, statements)
		}
	})
}
