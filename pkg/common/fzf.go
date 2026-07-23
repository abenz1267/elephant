package common

import (
	"sync"

	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

const (
	fuzzySlab16Size = 100 * 1024
	fuzzySlab32Size = 2048
)

// FuzzySlab is reusable scratch space for fzf's scoring matrices.
type FuzzySlab = util.Slab

var fuzzySlabPool = sync.Pool{
	New: func() any {
		return util.MakeSlab(fuzzySlab16Size, fuzzySlab32Size)
	},
}

func init() {
	algo.Init("default")
}

// AcquireFuzzySlab borrows scratch space for one non-concurrent query.
func AcquireFuzzySlab() *FuzzySlab {
	slab := fuzzySlabPool.Get().(*FuzzySlab)
	clear(slab.I16)
	clear(slab.I32)
	return slab
}

// ReleaseFuzzySlab returns query scratch space to the pool.
func ReleaseFuzzySlab(slab *FuzzySlab) {
	fuzzySlabPool.Put(slab)
}

// FuzzyScore scores target using pre-normalized query runes and reusable scratch space.
func FuzzyScore(runes []rune, target string, exact bool, slab *FuzzySlab) (int32, *[]int, int32) {
	chars := util.ToChars([]byte(target))

	var res algo.Result
	var pos *[]int

	// Passing a slab makes fzf fall back to FuzzyMatchV1 when the scoring
	// matrix does not fit. Keep the existing V2 behavior for oversized inputs.
	matchSlab := slab
	if slab != nil && chars.Length()*len(runes) > cap(slab.I16) {
		matchSlab = nil
	}

	if exact {
		res, pos = algo.ExactMatchNaive(true, true, true, &chars, runes, true, matchSlab)
	} else {
		res, pos = algo.FuzzyMatchV2(false, true, true, &chars, runes, true, matchSlab)

		// FuzzyMatchV2 does not overwrite every matrix cell that its backtrace
		// can inspect. Clear the portion used by this match before the slab is
		// reused, otherwise stale cells can change positions and the start score.
		if matchSlab != nil && fuzzyMatchUsedSlab(&chars, runes, res) {
			clearFuzzySlab(matchSlab, chars.Length(), len(runes))
		}
	}

	if res.Start > -1 {
		res.Score = res.Score - res.Start
	}

	return int32(res.Score), pos, int32(res.Start)
}

func fuzzyMatchUsedSlab(chars *util.Chars, runes []rune, res algo.Result) bool {
	if len(runes) == 0 || len(runes) > chars.Length() {
		return false
	}
	if res.Start >= 0 || !chars.IsBytes() {
		return true
	}
	for _, r := range runes {
		if r >= 128 {
			return true
		}
	}
	return false
}

func clearFuzzySlab(slab *FuzzySlab, targetLen, patternLen int) {
	used := len(slab.I16)
	if targetLen <= len(slab.I16)/3 {
		used = 3 * targetLen
	}
	remaining := len(slab.I16) - used
	matrixLimit := remaining / 2

	if patternLen > 0 && targetLen <= matrixLimit/patternLen {
		used += 2 * targetLen * patternLen
	} else {
		used = len(slab.I16)
	}

	clear(slab.I16[:used])
}

// FuzzyPositionsToInt32 converts fzf's native positions at the protobuf boundary.
func FuzzyPositionsToInt32(pos *[]int) []int32 {
	if pos == nil {
		return nil
	}

	positions := make([]int32, len(*pos))
	for i, position := range *pos {
		positions[i] = int32(position)
	}

	return positions
}
