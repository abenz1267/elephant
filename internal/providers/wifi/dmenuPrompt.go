package main

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

var dmenuTools = map[string][]string{
	"walker": {"-d", "-x", "--maxheight", "1", "-p", "%PROMPT%"},
	"rofi":   {"-dmenu", "-password", "-p", "%PROMPT%"},
	"wofi":   {"-d", "-P", "-L", "1", "-W", "300", "-b", "-p", "%PROMPT%"},
}

func init() {
	passwordPrompts["dmenu"] = &DmenuPrompt{}
}

type DmenuPrompt struct {
	name string
	args []string
}

func (a *DmenuPrompt) Available() bool {
	if a.name != "" {
		return true
	}

	if config == nil {
		return false
	}

	tool := config.DmenuCommand

	if tool != "auto" {
		if args, ok := dmenuTools[tool]; ok {
			if p, err := exec.LookPath(tool); p != "" && err == nil {
				a.name = tool
				a.args = args
				return true
			}
		}
		slog.Warn(Name, "dmenu_Available", tool+" not available, trying auto-detect")
	}

	for name, args := range dmenuTools {
		if p, err := exec.LookPath(name); p != "" && err == nil {
			a.name = name
			a.args = args
			return true
		}
	}

	return false
}

func (a *DmenuPrompt) RequestPassword(ssid string) (string, error) {
	if a.name == "" {
		return "", fmt.Errorf("no dmenu-compatible tool found")
	}

	prompt := fmt.Sprintf("Password for %s: ", ssid)

	args := make([]string, len(a.args))
	for i, arg := range a.args {
		args[i] = strings.ReplaceAll(arg, "%PROMPT%", prompt)
	}

	cmd := exec.Command(a.name, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", nil // user cancelled
	}

	return strings.TrimRight(string(out), "\n\r"), nil
}
