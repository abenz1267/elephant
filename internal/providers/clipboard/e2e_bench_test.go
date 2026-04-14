package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

func BenchmarkE2EQueryBrowse(b *testing.B) {
	for _, size := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("items_%d", size), func(b *testing.B) {
			setupTestDB(b)
			seedItems(b, size)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				Query(nil, "", false, false, 0)
			}
		})
	}
}

func BenchmarkE2EQuerySearch(b *testing.B) {
	for _, size := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("items_%d", size), func(b *testing.B) {
			setupTestDB(b)
			seedItems(b, size)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				Query(nil, "number 42", false, false, 0)
			}
		})
	}
}

func BenchmarkLifecycleInsertPersist(b *testing.B) {
	for _, size := range []int{100, 1000, 10000} {
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

func BenchmarkLifecycleInsertBrowse(b *testing.B) {
	for _, size := range []int{100, 1000, 10000} {
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
				Query(nil, "", false, false, 0)
			}
		})
	}
}
