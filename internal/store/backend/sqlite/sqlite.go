package sqlite

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

func OpenDatabase(path string, open func(driverName, dataSourceName string) (*sql.DB, error)) (*sql.DB, error) {
	db, err := open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("lore: open database: %w", err)
	}

	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("lore: pragma %q: %w", pragma, err)
		}
	}

	return db, nil
}
