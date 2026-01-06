package main

import (
	_ "embed"
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

//go:embed README.md
var readme string

var (
	Name        = "bitwarden"
	NamePretty  = "Bitwarden"
	config      *Config
	cachedItems []RbwItem
)

type Config struct {
	common.Config   `koanf:",squash"`
	ClearAfter      int    `koanf:"clear_after" desc:"clipboard will be cleared after X seconds. 0 to disable." default:"5"`
	CopyCommand     string `koanf:"copy_command" desc:"clipboard copy command to be executed. supports %VALUE%." default:"wl-copy --"`
	ClearCommand    string `koanf:"clear_command" desc:"clipboard clear command to be executed." default:"wl-copy --clear"`
	AutoTypeSupport bool   `koanf:"autotype_support" desc:"enable autotype support" default:"false"`
	AutoTypeCommand string `koanf:"autotype_command" desc:"copy command to be executed. supports %VALUE%." default:"wtype -- %VALUE%"`
	AutoTypeDelay   int    `koanf:"autotype_delay" desc:"delay autotype for X milliseconds. 0 to disable." default:"500"`
}

func Setup() {
	config = &Config{
		Config: common.Config{
			Icon:     "bitwarden",
			MinScore: 20,
		},
		ClearAfter:      5,
		CopyCommand:     "wl-copy --",
		ClearCommand:    "wl-copy --clear",
		AutoTypeSupport: false,
		AutoTypeCommand: "wtype -- %VALUE%",
		AutoTypeDelay:   500,
	}

	common.LoadConfig(Name, config)

	initItems()
}

func executableExists(command string) bool {
	p, err := exec.LookPath(command)

	if p == "" || err != nil {
		slog.Info(Name, "available", fmt.Sprintf("%s not found. disabling.", command))
		return false
	}

	return true
}

func Available() bool {
	return executableExists("rbw")
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

func State(provider string) *pb.ProviderStateResponse {
	return &pb.ProviderStateResponse{}
}

func HideFromProviderlist() bool {
	return config.HideFromProviderlist
}

func Icon() string {
	return config.Icon
}
