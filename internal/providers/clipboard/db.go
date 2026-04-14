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

var db *sql.DB

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

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_clipboard_pinned ON clipboard(pinned)`)
	if err != nil {
		return fmt.Errorf("sql create index pinned: %v", err)
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

	_, err := db.Exec(
		"INSERT OR REPLACE INTO clipboard (hash, content, img, uri_list, time, state, pinned) VALUES (?, ?, ?, ?, ?, ?, ?)",
		hash, item.Content, item.Img, uriList, item.Time.Unix(), item.State, pinned,
	)
	if err != nil {
		slog.Error(Name, "putItem", err)
	}
}

func updateItemTime(hash string, t time.Time) {
	_, err := db.Exec("UPDATE clipboard SET time = ? WHERE hash = ?", t.Unix(), hash)
	if err != nil {
		slog.Error(Name, "updateItemTime", err)
	}
}

func getItem(hash string) *Item {
	var content, img, uriList, state string
	var ts int64
	var pinned int

	err := db.QueryRow(
		"SELECT content, img, uri_list, time, state, pinned FROM clipboard WHERE hash = ?", hash,
	).Scan(&content, &img, &uriList, &ts, &state, &pinned)
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

func getItemsByQuery(query string, mode string, limit int) []itemRow {
	path := common.CacheFile("clipboard.db")
	queryDB, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_temp_store=memory&_busy_timeout=5000&mode=ro")
	if err != nil {
		slog.Error(Name, "open query db", err)
		return nil
	}
	defer queryDB.Close()

	var modeFilter string
	switch mode {
	case ImagesOnly:
		modeFilter = " AND img != ''"
	case TextOnly:
		modeFilter = " AND img = ''"
	default:
		modeFilter = ""
	}

	var rows *sql.Rows
	if query != "" {
		likePattern := "%" + query + "%"
		rows, err = queryDB.Query(
			"SELECT hash, content, img, uri_list, time, state, pinned FROM clipboard WHERE content LIKE ?"+modeFilter+" ORDER BY time DESC LIMIT ?",
			likePattern, limit,
		)
	} else {
		rows, err = queryDB.Query(
			"SELECT hash, content, img, uri_list, time, state, pinned FROM clipboard WHERE 1=1"+modeFilter+" ORDER BY time DESC LIMIT ?",
			limit,
		)
	}
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
	_, err := db.Exec("DELETE FROM clipboard WHERE hash = ?", hash)
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

	_, err := db.Exec("UPDATE clipboard SET pinned = ? WHERE hash = ?", val, hash)
	if err != nil {
		slog.Error(Name, "updateItemPinned", err)
	}
}

func updateItemContent(hash string, content string) {
	_, err := db.Exec("UPDATE clipboard SET content = ? WHERE hash = ?", content, hash)
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

	rows, err := db.Query(
		"SELECT hash, img FROM clipboard WHERE pinned = 0 ORDER BY time ASC LIMIT ?",
		count-maxItems,
	)
	if err != nil {
		slog.Error(Name, "trimToMax query", err)
		return
	}

	type entry struct {
		hash string
		img  string
	}
	var toDelete []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.hash, &e.img); err == nil {
			toDelete = append(toDelete, e)
		}
	}
	rows.Close()

	for _, e := range toDelete {
		if e.img != "" {
			_ = os.Remove(e.img)
		}
		deleteItem(e.hash)
	}
}

func cleanupOldEntries(olderThanMinutes int) int {
	cutoff := time.Now().Add(-time.Duration(olderThanMinutes) * time.Minute).Unix()

	rows, err := db.Query("SELECT hash, img FROM clipboard WHERE pinned = 0 AND time < ?", cutoff)
	if err != nil {
		slog.Error(Name, "cleanupOldEntries query", err)
		return 0
	}

	type entry struct {
		hash string
		img  string
	}
	var toDelete []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.hash, &e.img); err == nil {
			toDelete = append(toDelete, e)
		}
	}
	rows.Close()

	for _, e := range toDelete {
		if e.img != "" {
			_ = os.Remove(e.img)
		}
		deleteItem(e.hash)
	}

	return len(toDelete)
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
