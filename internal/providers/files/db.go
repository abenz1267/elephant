package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/abenz1267/elephant/v2/pkg/common"
	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func openDB() error {
	path := common.CacheFile("files.db")
	os.Remove(path)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create cache dir: %v", err)
	}

	os.Create(path)

	for !common.FileExists(path) {
		time.Sleep(time.Millisecond * 10)
	}

	var err error

	db, err = sql.Open("sqlite3", path+"?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_temp_store=memory")
	if err != nil {
		return fmt.Errorf("sql open: %v", err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS files (
		identifier TEXT PRIMARY KEY,
		path TEXT NOT NULL,
		changed INTEGER
	)`)
	if err != nil {
		return fmt.Errorf("sql create table: %v", err)
	}

	// Create indexes for query performance
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_files_path ON files(path)`)
	if err != nil {
		return fmt.Errorf("sql create index path: %v", err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_files_changed ON files(changed DESC)`)
	if err != nil {
		return fmt.Errorf("sql create index changed: %v", err)
	}

	return nil
}

func putFileBatch(files []File) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("INSERT OR REPLACE INTO files (identifier, path, changed) VALUES (?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, f := range files {
		changedUnix := int64(0)
		if !f.Changed.IsZero() {
			changedUnix = f.Changed.Unix()
		}
		_, err = stmt.Exec(f.Identifier, f.Path, changedUnix)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func dropAll() {
	_, err := db.Exec("DELETE FROM files")
	if err != nil {
		slog.Error(Name, "delete", err)
	}
}

func putFile(f File) {
	changedUnix := int64(0)
	if !f.Changed.IsZero() {
		changedUnix = f.Changed.Unix()
	}

	_, err := db.Exec("INSERT OR REPLACE INTO files (identifier, path, changed) VALUES (?, ?, ?)",
		f.Identifier, f.Path, changedUnix)
	if err != nil {
		slog.Error(Name, "put", err)
	}
}

func getFile(identifier string) *File {
	var f File
	var changedUnix int64

	err := db.QueryRow("SELECT identifier, path, changed FROM files WHERE identifier = ?", identifier).
		Scan(&f.Identifier, &f.Path, &changedUnix)
	if err != nil {
		return nil
	}

	if changedUnix > 0 {
		f.Changed = time.Unix(changedUnix, 0)
	}

	return &f
}

func getFilesByQuery(query string, _ bool) []File {
	var result []File

	path := common.CacheFile("files.db")
	queryDB, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_temp_store=memory")
	if err != nil {
		slog.Error(Name, "open query db", err)
		return nil
	}
	defer queryDB.Close()

	var rows *sql.Rows

	if query != "" {
		likePattern := "%" + query + "%"
		rows, err = queryDB.Query("SELECT identifier, path, changed FROM files WHERE path LIKE ? ORDER BY changed DESC LIMIT 1000", likePattern)
	} else {
		rows, err = queryDB.Query("SELECT identifier, path, changed FROM files WHERE path NOT LIKE '%/' ORDER BY changed DESC LIMIT 100")
	}

	if err != nil {
		slog.Error(Name, "read", err)
		return nil
	}
	defer rows.Close()

	for rows.Next() {
		var f File
		var changedUnix int64

		if err := rows.Scan(&f.Identifier, &f.Path, &changedUnix); err != nil {
			continue
		}

		if changedUnix > 0 {
			f.Changed = time.Unix(changedUnix, 0)
		}

		result = append(result, f)
	}

	return result
}

func deleteFileByPath(path string) {
	_, err := db.Exec("DELETE FROM files WHERE path LIKE ?", path+"%")
	if err != nil {
		slog.Error(Name, "delete", err)
	}
}
