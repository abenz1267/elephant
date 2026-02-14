package main

import (
	"fmt"

	"github.com/abenz1267/elephant/v2/internal/util"
)

func PrintDoc(write bool) {
	if !write {
		fmt.Println(readme)
		fmt.Println()
	}
	util.PrintConfig(config, Name, write)
}
