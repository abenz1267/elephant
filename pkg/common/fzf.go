package common

import (
	"slices"
	"unicode"

	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

func init() {
	algo.Init("default")
}

func FuzzyScore(input, target string, exact bool) (int32, []int32, int32) {
	runes := []rune(input)
	chars := util.ToChars([]byte(target))

	var res algo.Result
	var pos *[]int

	if exact {
		res, pos = algo.ExactMatchNaive(slices.ContainsFunc(runes, unicode.IsUpper), true, true, &chars, runes, true, nil)
	} else {
		res, pos = algo.FuzzyMatchV2(slices.ContainsFunc(runes, unicode.IsUpper), true, true, &chars, runes, true, nil)
	}

	var int32Slice []int32

	if pos != nil {
		intSlice := *pos
		int32Slice = make([]int32, len(intSlice))

		for i, v := range intSlice {
			int32Slice[i] = int32(v)
		}
	} else {
		int32Slice = make([]int32, 0)
	}

	res.Score = res.Score - res.Start

	return int32(res.Score), int32Slice, int32(res.Start)
}
