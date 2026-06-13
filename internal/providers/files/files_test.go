package main

import (
	"crypto/md5"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
	_ "github.com/mattn/go-sqlite3"
)

const benchDir = "/home/user/docs"

func benchPaths(n int) []string {
	paths := make([]string, n)
	for i := range n {
		switch {
		case i%5 == 0:
			paths[i] = fmt.Sprintf("%s/document_%d.pdf", benchDir, i)
		case i%5 == 1:
			paths[i] = fmt.Sprintf("%s/image_%d.png", benchDir, i)
		case i%5 == 2:
			paths[i] = fmt.Sprintf("%s/notes_%d.txt", benchDir, i)
		case i%5 == 3:
			paths[i] = fmt.Sprintf("%s/downloads/file_%d.zip", benchDir, i)
		default:
			paths[i] = fmt.Sprintf("%s/projects/src/main_%d.go", benchDir, i)
		}
	}
	return paths
}

func setupBenchDB(b *testing.B, n int) (string, *sql.DB) {
	b.Helper()

	tmpDir, err := os.MkdirTemp("", "files-bench-*")
	if err != nil {
		b.Fatal(err)
	}

	path := tmpDir + "/files.db"
	dsn := path + "?" + dbPragmas

	database, err := sql.Open("sqlite3", dsn)
	if err != nil {
		b.Fatal(err)
	}

	_, err = database.Exec(`CREATE TABLE files (
		identifier TEXT PRIMARY KEY,
		path TEXT NOT NULL,
		changed INTEGER
	)`)
	if err != nil {
		b.Fatal(err)
	}

	_, err = database.Exec(`CREATE INDEX idx_files_path ON files(path)`)
	if err != nil {
		b.Fatal(err)
	}

	_, err = database.Exec(`CREATE INDEX idx_files_changed ON files(changed DESC)`)
	if err != nil {
		b.Fatal(err)
	}

	paths := benchPaths(n)
	tx, _ := database.Begin()
	stmt, _ := tx.Prepare("INSERT INTO files (identifier, path, changed) VALUES (?, ?, ?)")

	now := time.Now()
	for i, p := range paths {
		id := fmt.Sprintf("%x", i)
		changed := now.Add(-time.Duration(i) * time.Minute).Unix()
		stmt.Exec(id, p, changed)
	}

	stmt.Close()
	tx.Commit()

	return tmpDir, database
}

func BenchmarkBrowseQuery(b *testing.B) {
	sizes := []int{1000, 10000, 100000}

	b.Run("newconn", func(b *testing.B) {
		for _, size := range sizes {
			b.Run(fmt.Sprintf("%d", size), func(b *testing.B) {
				tmpDir, database := setupBenchDB(b, size)
				dsn := tmpDir + "/files.db?" + dbPragmas
				database.Close()

				b.ResetTimer()
				for range b.N {
					rd, _ := sql.Open("sqlite3", dsn)
					rows, _ := rd.Query("SELECT identifier, path, changed FROM files WHERE path NOT LIKE '%/' ORDER BY changed DESC LIMIT 100")
					if rows != nil {
						for rows.Next() {
							var id, path string
							var changed int64
							rows.Scan(&id, &path, &changed)
						}
						rows.Close()
					}
					rd.Close()
				}
				os.RemoveAll(tmpDir)
			})
		}
	})

	b.Run("persistent", func(b *testing.B) {
		for _, size := range sizes {
			b.Run(fmt.Sprintf("%d", size), func(b *testing.B) {
				tmpDir, database := setupBenchDB(b, size)
				dsn := tmpDir + "/files.db?" + dbPragmas

				rd, _ := sql.Open("sqlite3", dsn)
				defer rd.Close()
				database.Close()

				b.ResetTimer()
				for range b.N {
					rows, _ := rd.Query("SELECT identifier, path, changed FROM files WHERE path NOT LIKE '%/' ORDER BY changed DESC LIMIT 100")
					if rows != nil {
						for rows.Next() {
							var id, path string
							var changed int64
							rows.Scan(&id, &path, &changed)
						}
						rows.Close()
					}
				}
				os.RemoveAll(tmpDir)
			})
		}
	})
}

func BenchmarkSearchQuery(b *testing.B) {
	sizes := []int{1000, 10000, 100000}

	for _, q := range []string{"document", "main", "nonexistent_xyz"} {
		b.Run(fmt.Sprintf("query_%s", q), func(b *testing.B) {
			b.Run("newconn", func(b *testing.B) {
				for _, size := range sizes {
					b.Run(fmt.Sprintf("%d", size), func(b *testing.B) {
						tmpDir, database := setupBenchDB(b, size)
						dsn := tmpDir + "/files.db?" + dbPragmas
						database.Close()

						likePattern := "%" + q + "%"

						b.ResetTimer()
						for range b.N {
							rd, _ := sql.Open("sqlite3", dsn)
							rows, _ := rd.Query("SELECT identifier, path, changed FROM files WHERE path LIKE ? ORDER BY changed DESC LIMIT 1000", likePattern)
							if rows != nil {
								for rows.Next() {
									var id, path string
									var changed int64
									rows.Scan(&id, &path, &changed)
								}
								rows.Close()
							}
							rd.Close()
						}
						os.RemoveAll(tmpDir)
					})
				}
			})

			b.Run("persistent", func(b *testing.B) {
				for _, size := range sizes {
					b.Run(fmt.Sprintf("%d", size), func(b *testing.B) {
						tmpDir, database := setupBenchDB(b, size)
						dsn := tmpDir + "/files.db?" + dbPragmas

						rd, _ := sql.Open("sqlite3", dsn)
						defer rd.Close()
						database.Close()

						likePattern := "%" + q + "%"

						b.ResetTimer()
						for range b.N {
							rows, _ := rd.Query("SELECT identifier, path, changed FROM files WHERE path LIKE ? ORDER BY changed DESC LIMIT 1000", likePattern)
							if rows != nil {
								for rows.Next() {
									var id, path string
									var changed int64
									rows.Scan(&id, &path, &changed)
								}
								rows.Close()
							}
						}
						os.RemoveAll(tmpDir)
					})
				}
			})
		})
	}
}

func BenchmarkPutFile(b *testing.B) {
	tmpDir, database := setupBenchDB(b, 1000)
	dsn := tmpDir + "/files.db?" + dbPragmas
	database.Close()

	database, _ = sql.Open("sqlite3", dsn)
	defer database.Close()

	stmt, _ := database.Prepare("INSERT OR REPLACE INTO files (identifier, path, changed) VALUES (?, ?, ?)")
	defer stmt.Close()

	b.ResetTimer()

	for range b.N {
		id := fmt.Sprintf("new_%d", b.N)
		stmt.Exec(id, "/home/user/new_file.txt", time.Now().Unix())
	}
	os.RemoveAll(tmpDir)
}

func BenchmarkInsertBatch(b *testing.B) {
	tmpDir, database := setupBenchDB(b, 1000)
	dsn := tmpDir + "/files.db?" + dbPragmas
	database.Close()

	b.ResetTimer()

	for range b.N {
		database, _ = sql.Open("sqlite3", dsn)
		paths := benchPaths(5000)
		tx, _ := database.Begin()
		stmt, _ := tx.Prepare("INSERT OR REPLACE INTO files (identifier, path, changed) VALUES (?, ?, ?)")
		for i, p := range paths {
			stmt.Exec(fmt.Sprintf("batch_%d_%d", b.N, i), p, time.Now().Unix())
		}
		stmt.Close()
		tx.Commit()
		database.Close()
	}

	os.RemoveAll(tmpDir)
}

func BenchmarkHash(b *testing.B) {
	paths := benchPaths(1000)

	b.Run("md5", func(b *testing.B) {
		for range b.N {
			for _, p := range paths {
				h := md5.Sum([]byte(p))
				_ = fmt.Sprintf("%x", h)
			}
		}
	})

	b.Run("xxhash", func(b *testing.B) {
		for range b.N {
			for _, p := range paths {
				_ = fmt.Sprintf("%x", xxhash.Sum64String(p))
			}
		}
	})
}
