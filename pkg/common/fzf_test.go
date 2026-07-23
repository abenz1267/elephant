package common

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/junegunn/fzf/src/algo"
)

func TestFuzzyPositionsToInt32(t *testing.T) {
	if positions := FuzzyPositionsToInt32(nil); positions != nil {
		t.Fatalf("expected nil positions, got %v", positions)
	}

	input := []int{7, 3, 1}
	got := FuzzyPositionsToInt32(&input)
	want := []int32{7, 3, 1}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("positions mismatch: got %v, want %v", got, want)
	}
}

func TestFuzzyScoreWithSlab(t *testing.T) {
	pattern := algo.NormalizeRunes([]rune("ept"))
	const target = "Elephant"

	wantScore, wantPositions, wantStart := FuzzyScore(pattern, target, false, nil)

	slab := AcquireFuzzySlab()
	defer ReleaseFuzzySlab(slab)

	gotScore, gotPositions, gotStart := FuzzyScore(pattern, target, false, slab)

	if gotScore != wantScore || gotStart != wantStart {
		t.Fatalf(
			"result mismatch: got score/start %d/%d, want %d/%d",
			gotScore,
			gotStart,
			wantScore,
			wantStart,
		)
	}
	if !reflect.DeepEqual(gotPositions, wantPositions) {
		t.Fatalf("positions mismatch: got %v, want %v", gotPositions, wantPositions)
	}
}

func TestReusedFuzzySlabMatchesFreshAllocations(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	alphabet := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 /_-äöüß")
	slab := AcquireFuzzySlab()
	defer ReleaseFuzzySlab(slab)

	for iteration := 0; iteration < 5_000; iteration++ {
		targetRunes := make([]rune, 1+rng.Intn(180))
		for i := range targetRunes {
			targetRunes[i] = alphabet[rng.Intn(len(alphabet))]
		}

		queryRunes := make([]rune, rng.Intn(16))
		for i := range queryRunes {
			queryRunes[i] = alphabet[rng.Intn(len(alphabet))]
		}

		pattern := algo.NormalizeRunes([]rune(strings.ToLower(string(queryRunes))))
		target := string(targetRunes)

		wantScore, wantPositions, wantStart := FuzzyScore(pattern, target, false, nil)
		gotScore, gotPositions, gotStart := FuzzyScore(pattern, target, false, slab)

		if gotScore != wantScore || gotStart != wantStart || !reflect.DeepEqual(gotPositions, wantPositions) {
			t.Fatalf(
				"iteration %d query=%q target=%q: slab=(%d,%v,%d), nil=(%d,%v,%d)",
				iteration,
				string(queryRunes),
				target,
				gotScore,
				gotPositions,
				gotStart,
				wantScore,
				wantPositions,
				wantStart,
			)
		}
	}
}
