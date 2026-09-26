package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/abenz1267/elephant/v2/pkg/common"
)

type PasswordPrompt interface {
	RequestPassword(ssid string) (string, error)
}

var presets = map[string][]string{
	"walker": {"walker", "-d", "-x", "--maxheight", "1", "-p", "%PROMPT%"},
	"rofi":   {"rofi", "-dmenu", "-password", "-p", "%PROMPT%"},
	"wofi":   {"wofi", "-d", "-P", "-L", "1", "-W", "300", "-b", "-p", "%PROMPT%"},
}

func detectPasswordPrompt(preference string) PasswordPrompt {
	// try the preference first
	if preference != "auto" {
		switch preference {
		case "terminal":
			if common.GetTerminal() != "" {
				slog.Info(Name, "detectPasswordPrompt", "terminal")
				return &TerminalPrompt{}
			}
		case "custom":
			if config.CustomPromptCommand == "" {
				slog.Warn(Name, "detectPasswordPrompt", "custom_prompt_command is empty")
				return nil
			}
			parts := strings.Fields(config.CustomPromptCommand)
			if p, err := exec.LookPath(parts[0]); p != "" && err == nil {
				slog.Info(Name, "detectPasswordPrompt", "custom: "+parts[0])
				return &CommandPrompt{command: parts}
			}
		default:
			if args, ok := presets[preference]; ok {
				if p, err := exec.LookPath(args[0]); p != "" && err == nil {
					slog.Info(Name, "detectPasswordPrompt", preference)
					return &CommandPrompt{command: args}
				}
			}
		}
		slog.Warn(Name, "detectPasswordPrompt", preference+" not available, trying others")
	}

	// try all presets
	for name, args := range presets {
		if name == preference {
			continue
		}
		if p, err := exec.LookPath(name); p != "" && err == nil {
			slog.Info(Name, "detectPasswordPrompt", name)
			return &CommandPrompt{command: args}
		}
	}

	// terminal as fallback
	if preference != "terminal" {
		if common.GetTerminal() != "" {
			slog.Warn(Name, "detectPasswordPrompt", "falling back to terminal")
			return &TerminalPrompt{}
		}
	}

	return nil
}

type CommandPrompt struct {
	command []string
}

func (c *CommandPrompt) RequestPassword(ssid string) (string, error) {
	prompt := fmt.Sprintf("Password for %s: ", ssid)

	args := make([]string, len(c.command)-1)
	for i, arg := range c.command[1:] {
		args[i] = strings.ReplaceAll(arg, "%PROMPT%", prompt)
	}

	cmd := exec.Command(c.command[0], args...)
	out, err := cmd.Output()
	if err != nil {
		return "", nil // user cancelled
	}

	return strings.TrimRight(string(out), "\n\r"), nil
}

type TerminalPrompt struct{}

func (a *TerminalPrompt) RequestPassword(ssid string) (string, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	defer r.Close()

	dot := "●"
	backspace := `printf '\b \b'`
	if !config.ShowPasswordDots {
		dot = ""
		backspace = ""
	}

	repl := strings.NewReplacer(
		"__DOT__", dot,
		"__BACKSPACE__", backspace,
		"__FD__", fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), w.Fd()),
	)

	script := repl.Replace(`
		printf 'Password for %s: ' "$WIFI_SSID"
		pw=""
		bs=$(printf '\177')
		while IFS= read -rsn1 c; do
			[ -z "$c" ] && break
			if [ "$c" = "$bs" ]; then
				if [ -n "$pw" ]; then
					pw="${pw%?}"
					__BACKSPACE__
				fi
			else
				pw="$pw$c"
				printf '__DOT__'
			fi
		done
		echo
		echo "$pw" > __FD__`,
	)

	terminal := common.GetTerminal()
	if terminal == "" {
		w.Close()
		return "", fmt.Errorf("no terminal found")
	}

	cmd := exec.Command(terminal, "-e", "bash", "-c", script)
	cmd.Env = append(os.Environ(), "WIFI_SSID="+ssid)
	if err := cmd.Start(); err != nil {
		w.Close()
		return "", err
	}

	if err := cmd.Wait(); err != nil {
		slog.Debug(Name, "terminal_RequestPassword", err)
	}
	w.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}

	return strings.TrimRight(string(data), "\n\r"), nil
}
