package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/abenz1267/elephant/v2/pkg/common"
)

// setupTestDB creates a temporary SQLite database for benchmarks.
func setupTestDB(b *testing.B) {
	b.Helper()

	tmpDir := b.TempDir()
	os.Setenv("XDG_CACHE_HOME", tmpDir)

	config = &Config{
		Config: common.Config{
			MinScore: 30,
		},
		MaxItems:      50000,
		Command:       "wl-copy",
		IgnoreSymbols: false,
	}

	if err := openDB(); err != nil {
		b.Fatal(err)
	}
}

// seedItems inserts n items into the database with varied content.
func seedItems(b *testing.B, n int) {
	b.Helper()

	tx, err := db.Begin()
	if err != nil {
		b.Fatal(err)
	}

	stmt, err := tx.Prepare(
		"INSERT OR REPLACE INTO clipboard (hash, content, img, uri_list, time, state, pinned) VALUES (?, ?, ?, ?, ?, ?, ?)",
	)
	if err != nil {
		b.Fatal(err)
	}
	defer stmt.Close()

	baseTime := time.Now().Add(-time.Duration(n) * time.Minute)

	for i := 0; i < n; i++ {
		content := fmt.Sprintf("clipboard entry number %d with some text content for searching purposes — item %d", i, i)
		hash := md5.Sum([]byte(content))
		hashStr := hex.EncodeToString(hash[:])
		ts := baseTime.Add(time.Duration(i) * time.Minute).Unix()

		pinned := 0
		if i%100 == 0 {
			pinned = 1
		}

		_, err = stmt.Exec(hashStr, content, "", "", ts, "editable", pinned)
		if err != nil {
			b.Fatal(err)
		}
	}

	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
}

func BenchmarkInsert(b *testing.B) {
	for _, size := range []int{1000, 10000, 50000} {
		b.Run(fmt.Sprintf("existing_%d", size), func(b *testing.B) {
			setupTestDB(b)
			seedItems(b, size)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				content := fmt.Sprintf("new clipboard entry %d at bench time", i)
				hash := md5.Sum([]byte(content))
				hashStr := hex.EncodeToString(hash[:])
				putItem(hashStr, &Item{
					Content: content,
					Time:    time.Now(),
					State:   StateEditable,
				})
			}
		})
	}
}

func BenchmarkQueryBrowse(b *testing.B) {
	for _, size := range []int{1000, 10000, 50000} {
		b.Run(fmt.Sprintf("items_%d", size), func(b *testing.B) {
			setupTestDB(b)
			seedItems(b, size)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				getItemsByQuery("", Combined, 256)
			}
		})
	}
}

func BenchmarkQuerySearch(b *testing.B) {
	for _, size := range []int{1000, 10000, 50000} {
		b.Run(fmt.Sprintf("items_%d", size), func(b *testing.B) {
			setupTestDB(b)
			seedItems(b, size)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rows := getItemsByQuery("number 42", Combined, 1000)
				// Simulate fzf scoring on results like Query() does
				for _, row := range rows {
					common.FuzzyScore("number 42", row.Item.Content, false)
				}
			}
		})
	}
}

func BenchmarkTrim(b *testing.B) {
	for _, size := range []int{1000, 10000, 50000} {
		b.Run(fmt.Sprintf("items_%d", size), func(b *testing.B) {
			setupTestDB(b)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				// Re-seed to size+10 each iteration so there's something to trim
				db.Exec("DELETE FROM clipboard")
				seedItems(b, size+10)
				b.StartTimer()

				trimToMax(size)
			}
		})
	}
}

func BenchmarkGetItem(b *testing.B) {
	setupTestDB(b)
	seedItems(b, 10000)

	// Get a known hash to look up
	content := "clipboard entry number 5000 with some text content for searching purposes — item 5000"
	hash := md5.Sum([]byte(content))
	hashStr := hex.EncodeToString(hash[:])

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		getItem(hashStr)
	}
}

func BenchmarkCountByType(b *testing.B) {
	for _, size := range []int{1000, 10000, 50000} {
		b.Run(fmt.Sprintf("items_%d", size), func(b *testing.B) {
			setupTestDB(b)
			seedItems(b, size)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				countByType()
			}
		})
	}
}
