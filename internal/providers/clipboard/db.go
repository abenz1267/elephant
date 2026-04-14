package main

import (
	"bytes"
	"database/sql"
	"encoding/gob"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/abenz1267/elephant/v2/pkg/common"
	_ "github.com/mattn/go-sqlite3"
)

var (
	db     *sql.DB // writer connection (serialized)
	readDB *sql.DB // persistent read-only connection for queries

	stmtPut           *sql.Stmt
	stmtGet           *sql.Stmt
	stmtUpdateTime    *sql.Stmt
	stmtDelete        *sql.Stmt
	stmtUpdatePinned  *sql.Stmt
	stmtUpdateContent *sql.Stmt

	// Read-path prepared statements (on readDB)
	stmtQueryCombined *sql.Stmt
	stmtQueryImages   *sql.Stmt
	stmtQueryText     *sql.Stmt
)

func openDB() error {
	path := common.CacheFile("clipboard.db")

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create cache dir: %v", err)
	}

	var err error
	db, err = sql.Open("sqlite3", path+"?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_temp_store=memory&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("sql open: %v", err)
	}

	db.SetMaxOpenConns(1)

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS clipboard (
		hash TEXT PRIMARY KEY,
		content TEXT NOT NULL DEFAULT '',
		img TEXT NOT NULL DEFAULT '',
		uri_list TEXT NOT NULL DEFAULT '',
		time INTEGER NOT NULL,
		state TEXT NOT NULL DEFAULT '',
		pinned INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		return fmt.Errorf("sql create table: %v", err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_clipboard_time ON clipboard(time DESC)`)
	if err != nil {
		return fmt.Errorf("sql create index time: %v", err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_clipboard_pinned_time ON clipboard(pinned, time DESC)`)
	if err != nil {
		return fmt.Errorf("sql create index pinned_time: %v", err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_clipboard_img ON clipboard(img)`)
	if err != nil {
		return fmt.Errorf("sql create index img: %v", err)
	}

	// Superseded by composite index
	db.Exec(`DROP INDEX IF EXISTS idx_clipboard_pinned`)

	// Prepare write statements
	stmtPut, err = db.Prepare("INSERT OR REPLACE INTO clipboard (hash, content, img, uri_list, time, state, pinned) VALUES (?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return fmt.Errorf("prepare put: %v", err)
	}

	stmtGet, err = db.Prepare("SELECT content, img, uri_list, time, state, pinned FROM clipboard WHERE hash = ?")
	if err != nil {
		return fmt.Errorf("prepare get: %v", err)
	}

	stmtUpdateTime, err = db.Prepare("UPDATE clipboard SET time = ? WHERE hash = ?")
	if err != nil {
		return fmt.Errorf("prepare updateTime: %v", err)
	}

	stmtDelete, err = db.Prepare("DELETE FROM clipboard WHERE hash = ?")
	if err != nil {
		return fmt.Errorf("prepare delete: %v", err)
	}

	stmtUpdatePinned, err = db.Prepare("UPDATE clipboard SET pinned = ? WHERE hash = ?")
	if err != nil {
		return fmt.Errorf("prepare updatePinned: %v", err)
	}

	stmtUpdateContent, err = db.Prepare("UPDATE clipboard SET content = ? WHERE hash = ?")
	if err != nil {
		return fmt.Errorf("prepare updateContent: %v", err)
	}

	// Persistent read-only connection for query path (no per-call open/close)
	readDB, err = sql.Open("sqlite3", path+"?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_temp_store=memory&_busy_timeout=5000&mode=ro")
	if err != nil {
		return fmt.Errorf("sql open read: %v", err)
	}

	// Prepare read-path statements (one per mode filter variant)
	const queryCols = "SELECT hash, content, img, uri_list, time, state, pinned FROM clipboard"

	stmtQueryCombined, err = readDB.Prepare(queryCols + " ORDER BY time DESC LIMIT ?")
	if err != nil {
		return fmt.Errorf("prepare queryCombined: %v", err)
	}

	stmtQueryImages, err = readDB.Prepare(queryCols + " WHERE img != '' ORDER BY time DESC LIMIT ?")
	if err != nil {
		return fmt.Errorf("prepare queryImages: %v", err)
	}

	stmtQueryText, err = readDB.Prepare(queryCols + " WHERE img = '' ORDER BY time DESC LIMIT ?")
	if err != nil {
		return fmt.Errorf("prepare queryText: %v", err)
	}

	return nil
}

func putItem(hash string, item *Item) {
	uriList := ""
	if len(item.URIList) > 0 {
		uriList = strings.Join(item.URIList, "\n")
	}

	pinned := 0
	if item.Pinned {
		pinned = 1
	}

	_, err := stmtPut.Exec(hash, item.Content, item.Img, uriList, item.Time.Unix(), item.State, pinned)
	if err != nil {
		slog.Error(Name, "putItem", err)
	}
}

func updateItemTime(hash string, t time.Time) {
	_, err := stmtUpdateTime.Exec(t.Unix(), hash)
	if err != nil {
		slog.Error(Name, "updateItemTime", err)
	}
}

func getItem(hash string) *Item {
	var content, img, uriList, state string
	var ts int64
	var pinned int

	err := stmtGet.QueryRow(hash).Scan(&content, &img, &uriList, &ts, &state, &pinned)
	if err != nil {
		return nil
	}

	var uris []string
	if uriList != "" {
		uris = strings.Split(uriList, "\n")
	}

	return &Item{
		Content: content,
		Img:     img,
		URIList: uris,
		Time:    time.Unix(ts, 0),
		State:   state,
		Pinned:  pinned == 1,
	}
}

type itemRow struct {
	Hash string
	Item *Item
}

// getItemsByQuery returns items ordered by time DESC. Filtering by search
// text is intentionally not done here — the caller applies fzf-style fuzzy
// scoring in Go, which has different (broader) match semantics than SQL LIKE.
func getItemsByQuery(mode string, limit int) []itemRow {
	var stmt *sql.Stmt
	switch mode {
	case ImagesOnly:
		stmt = stmtQueryImages
	case TextOnly:
		stmt = stmtQueryText
	default:
		stmt = stmtQueryCombined
	}

	rows, err := stmt.Query(limit)
	if err != nil {
		slog.Error(Name, "getItemsByQuery", err)
		return nil
	}
	defer rows.Close()

	var result []itemRow

	for rows.Next() {
		var hash, content, img, uriList, state string
		var ts int64
		var pinned int

		if err := rows.Scan(&hash, &content, &img, &uriList, &ts, &state, &pinned); err != nil {
			continue
		}

		var uris []string
		if uriList != "" {
			uris = strings.Split(uriList, "\n")
		}

		result = append(result, itemRow{
			Hash: hash,
			Item: &Item{
				Content: content,
				Img:     img,
				URIList: uris,
				Time:    time.Unix(ts, 0),
				State:   state,
				Pinned:  pinned == 1,
			},
		})
	}

	return result
}

func deleteItem(hash string) {
	_, err := stmtDelete.Exec(hash)
	if err != nil {
		slog.Error(Name, "deleteItem", err)
	}
}

func deleteAllUnpinned() []string {
	rows, err := db.Query("SELECT img FROM clipboard WHERE pinned = 0 AND img != ''")
	if err != nil {
		slog.Error(Name, "deleteAllUnpinned query", err)
		return nil
	}

	var imgs []string
	for rows.Next() {
		var img string
		if err := rows.Scan(&img); err == nil && img != "" {
			imgs = append(imgs, img)
		}
	}
	rows.Close()

	_, err = db.Exec("DELETE FROM clipboard WHERE pinned = 0")
	if err != nil {
		slog.Error(Name, "deleteAllUnpinned delete", err)
	}

	return imgs
}

func updateItemPinned(hash string, pinned bool) {
	val := 0
	if pinned {
		val = 1
	}

	_, err := stmtUpdatePinned.Exec(val, hash)
	if err != nil {
		slog.Error(Name, "updateItemPinned", err)
	}
}

func updateItemContent(hash string, content string) {
	_, err := stmtUpdateContent.Exec(content, hash)
	if err != nil {
		slog.Error(Name, "updateItemContent", err)
	}
}

func trimToMax(maxItems int) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM clipboard").Scan(&count)
	if err != nil || count <= maxItems {
		return
	}

	excess := count - maxItems

	// Clean up image files before batch delete
	rows, err := db.Query(
		"SELECT img FROM clipboard WHERE pinned = 0 AND img != '' ORDER BY time ASC LIMIT ?",
		excess,
	)
	if err != nil {
		slog.Error(Name, "trimToMax img query", err)
	} else {
		for rows.Next() {
			var img string
			if err := rows.Scan(&img); err == nil && img != "" {
				_ = os.Remove(img)
			}
		}
		rows.Close()
	}

	// Batch delete oldest unpinned entries in a single statement
	_, err = db.Exec(
		"DELETE FROM clipboard WHERE hash IN (SELECT hash FROM clipboard WHERE pinned = 0 ORDER BY time ASC LIMIT ?)",
		excess,
	)
	if err != nil {
		slog.Error(Name, "trimToMax delete", err)
	}
}

func cleanupOldEntries(olderThanMinutes int) int {
	cutoff := time.Now().Add(-time.Duration(olderThanMinutes) * time.Minute).Unix()

	// Clean up image files before batch delete
	rows, err := db.Query("SELECT img FROM clipboard WHERE pinned = 0 AND time < ? AND img != ''", cutoff)
	if err != nil {
		slog.Error(Name, "cleanupOldEntries query", err)
		return 0
	}

	for rows.Next() {
		var img string
		if err := rows.Scan(&img); err == nil && img != "" {
			_ = os.Remove(img)
		}
	}
	rows.Close()

	// Batch delete all old unpinned entries in a single statement
	result, err := db.Exec("DELETE FROM clipboard WHERE pinned = 0 AND time < ?", cutoff)
	if err != nil {
		slog.Error(Name, "cleanupOldEntries delete", err)
		return 0
	}

	n, _ := result.RowsAffected()
	return int(n)
}

func countByType() (bool, bool) {
	var hasText, hasImg bool

	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM clipboard WHERE img = '')").Scan(&hasText)
	if err != nil {
		slog.Error(Name, "countByType text", err)
	}

	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM clipboard WHERE img != '')").Scan(&hasImg)
	if err != nil {
		slog.Error(Name, "countByType img", err)
	}

	return hasText, hasImg
}

func itemCount() int {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM clipboard").Scan(&count)
	if err != nil {
		slog.Error(Name, "itemCount", err)
		return 0
	}
	return count
}

func migrateGobToSQLite() {
	gobFile := common.CacheFile("clipboard.gob")

	if !common.FileExists(gobFile) {
		return
	}

	slog.Info(Name, "migration", "migrating clipboard.gob to SQLite")

	f, err := os.ReadFile(gobFile)
	if err != nil {
		slog.Error(Name, "migration read", err)
		return
	}

	var history map[string]*Item
	decoder := gob.NewDecoder(bytes.NewReader(f))
	err = decoder.Decode(&history)
	if err != nil {
		slog.Error(Name, "migration decode", err)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error(Name, "migration tx begin", err)
		return
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		"INSERT OR REPLACE INTO clipboard (hash, content, img, uri_list, time, state, pinned) VALUES (?, ?, ?, ?, ?, ?, ?)",
	)
	if err != nil {
		slog.Error(Name, "migration prepare", err)
		return
	}
	defer stmt.Close()

	for hash, item := range history {
		uriList := ""
		if len(item.URIList) > 0 {
			uriList = strings.Join(item.URIList, "\n")
		}

		pinned := 0
		if item.Pinned {
			pinned = 1
		}

		_, err = stmt.Exec(hash, item.Content, item.Img, uriList, item.Time.Unix(), item.State, pinned)
		if err != nil {
			slog.Error(Name, "migration insert", err, "hash", hash)
		}
	}

	if err := tx.Commit(); err != nil {
		slog.Error(Name, "migration commit", err)
		return
	}

	os.Remove(gobFile)
	slog.Info(Name, "migration", fmt.Sprintf("migrated %d clipboard entries to SQLite", len(history)))
}
