package main

import (
	"fmt"
	"testing"
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
