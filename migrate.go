package migrate

import (
	"database/sql"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
)

type RepeatAction int

const (
	NoRepeat RepeatAction = iota
	RepeatAll
	RepeatLast

	VersionUp   = "VERSION_UP"
	VersionDown = "VERSION_DOWN"

	DefaultTableName = "schema_migrations"
	SuffixUp         = "up.sql"
	SuffixDown       = "down.sql"
	VersionSeparator = "_"
)

type Options struct {
	TableName        string
	Source           string
	Version          string
	Repeat           RepeatAction
	VersionSeparator string
}

var defaultOptions = Options{
	TableName:        DefaultTableName,
	Source:           "",
	Version:          VersionUp,
	Repeat:           NoRepeat,
	VersionSeparator: VersionSeparator,
}

func initTable(db *sql.DB, tableName string) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			version VARCHAR(100) PRIMARY KEY,
			timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`, tableName)
	_, err := db.Exec(query)
	return err
}

func getVersion(db *sql.DB, tableName string) (string, error) {
	query := fmt.Sprintf("SELECT version FROM %s ORDER BY timestamp DESC LIMIT 1", tableName)
	var version string
	err := db.QueryRow(query).Scan(&version)
	if err == sql.ErrNoRows {
		return "", nil
	} else if err != nil {
		return "", err
	}
	return version, nil
}

func Version(db *sql.DB, tableName ...string) (string, error) {
	tb := checkTableName(tableName...)
	if err := initTable(db, tb); err != nil {
		return "", err
	}

	return getVersion(db, tb)
}

func Migrate(db *sql.DB, opts ...Options) error {
	opt := checkOptions(opts...)
	if _, err := os.Stat(opt.Source); os.IsNotExist(err) {
		return err
	}

	curVersion, err := Version(db, opt.TableName)
	if err != nil {
		return err
	}

	if curVersion == opt.Version && opt.Repeat == NoRepeat {
		return nil
	}

	if curVersion <= opt.Version {
		return migrateUp(db, opt)
	}

	return migrateDown(db, opt)
}

// Helper

func GetRepeatAction(repeat string) RepeatAction {
	switch strings.ToLower(repeat) {
	case "all":
		return RepeatAll
	case "last":
		return RepeatLast
	default:
		return NoRepeat
	}
}

func checkTableName(tableName ...string) string {
	if len(tableName) > 0 && tableName[0] != "" {
		return tableName[0]
	}
	return defaultOptions.TableName
}

func checkOptions(opts ...Options) Options {
	opt := defaultOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	if opt.TableName == "" {
		opt.TableName = DefaultTableName
	}
	if opt.VersionSeparator == "" {
		opt.VersionSeparator = VersionSeparator
	}
	if opt.Version == "" {
		opt.Version = VersionUp
	}
	return opt
}

// migration file

func getMigrationFiles(source string, suffix string) ([]string, error) {
	files, err := os.ReadDir(source)
	if err != nil {
		return nil, err
	}

	var migrationFiles []string
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if strings.HasSuffix(file.Name(), suffix) {
			migrationFiles = append(migrationFiles, file.Name())
		}
	}

	return migrationFiles, nil
}

type migrationFile struct {
	Version  string
	Filename string
	Args     []string
}

func filterUpMigrationFiles(files []string, cur, target, separator string, repeat RepeatAction) []migrationFile {
	var result []migrationFile
	for _, file := range files {
		version := strings.Split(file, separator)[0]
		if version > target && target != VersionUp {
			continue
		}
		// skip if version file is less than or equal to current version, and repeat is not RepeatAll
		if version < cur && repeat != RepeatAll {
			continue
		}
		if version == cur && repeat == NoRepeat {
			continue
		}
		result = append(result, migrationFile{Version: version, Filename: file})
	}

	slices.SortFunc(result, func(a, b migrationFile) int {
		return strings.Compare(a.Version, b.Version)
	})
	return result
}

func filterDownMigrationFiles(files []string, cur, target, separator string, repeat RepeatAction) []migrationFile {
	var result []migrationFile
	for _, file := range files {
		version := strings.Split(file, separator)[0]
		if version < target && target != VersionDown {
			continue
		}
		// skip if version file is greater than or equal to current version, and repeat is not RepeatAll
		if version > cur && repeat != RepeatAll {
			continue
		}
		if version == cur && repeat == NoRepeat {
			continue
		}

		result = append(result, migrationFile{Version: version, Filename: file})
	}

	slices.SortFunc(result, func(a, b migrationFile) int {
		return strings.Compare(a.Version, b.Version)
	})
	slices.Reverse(result)
	return result
}

// execute

func migrateUp(db *sql.DB, opt Options) error {
	files, err := getMigrationFiles(opt.Source, SuffixUp)
	if err != nil {
		return err
	}
	migrations := filterUpMigrationFiles(files, opt.Version, opt.Version, opt.VersionSeparator, opt.Repeat)
	for _, m := range migrations {
		filename := path.Join(opt.Source, m.Filename)
		b, err := os.ReadFile(filename)
		if err != nil {
			return err
		}

		statements := splitSQLStatements(b)
		statements = append(statements, addMigrateUpStatements(m.Version, opt))
		if err := execScripts(db, statements); err != nil {
			return err
		}
	}
	return nil
}

func migrateDown(db *sql.DB, opt Options) error {
	files, err := getMigrationFiles(opt.Source, SuffixDown)
	if err != nil {
		return err
	}
	migrations := filterDownMigrationFiles(files, opt.Version, opt.Version, opt.VersionSeparator, opt.Repeat)
	for _, m := range migrations {
		filename := path.Join(opt.Source, m.Filename)
		b, err := os.ReadFile(filename)
		if err != nil {
			return err
		}

		statements := splitSQLStatements(b)
		statements = append(statements, addMigrateDownStatements(m.Version, opt))
		if err := execScripts(db, statements); err != nil {
			return err
		}
	}
	return nil
}

func addMigrateUpStatements(version string, opt Options) string {
	return fmt.Sprintf("INSERT INTO %s (version) VALUES ('%s') ON CONFLICT(version) DO UPDATE SET timestamp = CURRENT_TIMESTAMP", opt.TableName, version)
}

func addMigrateDownStatements(version string, opt Options) string {
	return fmt.Sprintf("DELETE FROM %s WHERE version = '%s'", opt.TableName, version)
}

func execScripts(db *sql.DB, statements []string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	commit := false
	defer func() {
		if commit {
			return
		}
		_ = tx.Rollback()
	}()

	for _, stmt := range statements {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}

	err = tx.Commit()
	commit = err == nil
	return err
}

func splitSQLStatements(query []byte) []string {
	script := string(query)
	statements := make([]string, 0)
	var builder strings.Builder

	inSingleQuote := false
	inDoubleQuote := false
	inBacktickQuote := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(script); i++ {
		ch := script[i]
		next := byte(0)
		if i+1 < len(script) {
			next = script[i+1]
		}

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
				builder.WriteByte(ch)
			}
			continue
		}

		if inBlockComment {
			if ch == '*' && next == '/' {
				i++
				inBlockComment = false
				builder.WriteByte(' ')
			}
			continue
		}

		if inSingleQuote {
			builder.WriteByte(ch)
			if ch == '\\' && next != 0 {
				builder.WriteByte(next)
				i++
				continue
			}
			if ch == '\'' {
				if next == '\'' {
					builder.WriteByte(next)
					i++
					continue
				}
				inSingleQuote = false
			}
			continue
		}

		if inDoubleQuote {
			builder.WriteByte(ch)
			if ch == '\\' && next != 0 {
				builder.WriteByte(next)
				i++
				continue
			}
			if ch == '"' {
				if next == '"' {
					builder.WriteByte(next)
					i++
					continue
				}
				inDoubleQuote = false
			}
			continue
		}

		if inBacktickQuote {
			builder.WriteByte(ch)
			if ch == '`' {
				if next == '`' {
					builder.WriteByte(next)
					i++
					continue
				}
				inBacktickQuote = false
			}
			continue
		}

		if ch == '-' && next == '-' {
			i++
			inLineComment = true
			continue
		}

		if ch == '/' && next == '*' {
			i++
			inBlockComment = true
			continue
		}

		switch ch {
		case '\'':
			inSingleQuote = true
			builder.WriteByte(ch)
		case '"':
			inDoubleQuote = true
			builder.WriteByte(ch)
		case '`':
			inBacktickQuote = true
			builder.WriteByte(ch)
		case ';':
			statement := strings.TrimSpace(builder.String())
			if statement != "" {
				statements = append(statements, statement)
			}
			builder.Reset()
		default:
			builder.WriteByte(ch)
		}
	}

	statement := strings.TrimSpace(builder.String())
	if statement != "" {
		statements = append(statements, statement)
	}

	return statements
}
