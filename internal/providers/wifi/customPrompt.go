package main

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

func init() {
	passwordPrompts["custom"] = &CustomPrompt{}
}

type CustomPrompt struct {
	name string
	args []string
}

func (a *CustomPrompt) Available() bool {
	if a.name != "" {
		return true
	}

	if config == nil || config.CustomPromptCommand == "" {
		slog.Warn(Name, "custom_Available", "custom_prompt_command is empty")
		return false
	}

	parts := strings.Fields(config.CustomPromptCommand)
	if p, err := exec.LookPath(parts[0]); p == "" || err != nil {
		slog.Warn(Name, "custom_Available", parts[0]+" not found")
		return false
	}

	a.name = parts[0]
	a.args = parts[1:]
	return true
}

func (a *CustomPrompt) RequestPassword(ssid string) (string, error) {
	if a.name == "" {
		return "", fmt.Errorf("no custom prompt command configured")
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
