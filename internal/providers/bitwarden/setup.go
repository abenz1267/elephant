package main

import (
	_ "embed"
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

//go:embed README.md
var readme string

var (
	Name = "bitwarden"
	NamePretty = "Bitwarden"
	config *Config
	cachedItems []RbwItem
)

type Config struct {
	common.Config	`koanf:",squash"`
}

func checkExecutable(command string) bool {
	p, err := exec.LookPath(command)

	if p == "" || err != nil {
		slog.Info(Name, "available", fmt.Sprintf("%s not found. disabling.", command))
		return false
	}
	
	return true
}

func Available() bool {
	return checkExecutable("rbw") && checkExecutable("wl-copy")
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
}

func State(provider string) *pb.ProviderStateResponse {
	return &pb.ProviderStateResponse{}
}

func Setup() {
	config = &Config{
		Config: common.Config{
			Icon: "bitwarden",
			MinScore: 20,
		},
	}

	common.LoadConfig(Name, config)

	initItems()
}

func HideFromProviderlist() bool {
	return config.HideFromProviderlist
}

func Icon() string {
	return config.Icon
}
