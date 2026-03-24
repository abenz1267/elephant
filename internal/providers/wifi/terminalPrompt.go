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

func init() {
	passwordPrompts["terminal"] = &TerminalPrompt{}
}

type TerminalPrompt struct{}

func (a *TerminalPrompt) Available() bool {
	return common.GetTerminal() != ""
}

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
