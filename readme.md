# Simple SQL Migrate

A minimal SQL migration library for Go's `database/sql` — one directory of plain `.sql` files, no code generation.

- Each migration file runs in its own transaction together with its version bookkeeping.
- Applied versions are tracked in a single table (`schema_migrations` by default), created automatically.

## Install

```sh
go get github.com/kongsakchai/simple-sql-migrate
```

## Usage

```go
package main

import (
	"database/sql"
	"log"

	migrate "github.com/kongsakchai/simple-sql-migrate"
	_ "modernc.org/sqlite"
)

func main() {
	db, err := sql.Open("sqlite", "app.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// migrate all the way up (the default)
	if err := migrate.Migrate(db, migrate.Options{Source: "./migrations"}); err != nil {
		log.Fatal(err)
	}

	// ... or up/down to a specific version
	_ = migrate.Migrate(db, migrate.Options{Source: "./migrations", Version: "002"})

	// ... or all the way down
	_ = migrate.Migrate(db, migrate.Options{Source: "./migrations", Version: migrate.VersionDown})
}
```

## Migration files

Files live in `Source`, named `{VERSION}_{NAME}.up.sql` / `{VERSION}_{NAME}.down.sql`:

```
migrations/
├── 001_create_users.up.sql
├── 001_create_users.down.sql
├── 002_add_orders.up.sql
└── 002_add_orders.down.sql
```

- `VERSION` is the filename prefix before the first `_`.
- Versions are compared as **strings**, so pad numeric versions (`001`, `002`, …) or use fixed-width timestamps (`20240101120000`). `"9"` would sort after `"10"`.
- Migrating up applies every `.up.sql` with `current < version <= target` and inserts its version row.
- Migrating down applies every `.down.sql` with `target < version <= current` in reverse order and deletes each rolled-back version row.
- A file may contain multiple SQL statements. Whether a driver executes them all in one call is driver-specific (for MySQL the DSN needs `multiStatements=true`).

## Options

| Field       | Default                | Description                                                        |
| ----------- | ---------------------- | ------------------------------------------------------------------ |
| `Source`    | — (required)           | directory containing the migration files                           |
| `TableName` | `schema_migrations`    | version tracking table                                             |
| `Version`   | `migrate.VersionUp`    | target version, or the `migrate.VersionUp` / `migrate.VersionDown` sentinel |
| `Timeout`   | `10m`                  | overall timeout for the whole migration run                    |

The tracking table:

```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
    version VARCHAR(100) PRIMARY KEY,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)
```

## Development

```sh
go test ./...
```
