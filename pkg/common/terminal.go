package common

import (
	"bytes"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/adrg/xdg"
	"github.com/charlievieth/fastwalk"
)

var terminal = ""

var terminalApps = make(map[string]struct{})

func init() {
	terminal = GetTerminal()
	findTerminalApps()
}

func GetTerminal() string {
	envVars := []string{"TERM", "TERMINAL"}

	for _, v := range envVars {
		term, ok := os.LookupEnv(v)
		if ok {
			path, _ := exec.LookPath(term)

			if path != "" {
				return path
			}
		}
	}

	t := []string{
		"xdg-terminal-exec",
		"kitty",
		"foot",
		"ghostty",
		"alacritty",
		"Eterm",
		"aterm",
		"gnome-terminal",
		"guake",
		"hyper",
		"konsole",
		"lilyterm",
		"lxterminal",
		"mate-terminal",
		"qterminal",
		"roxterm",
		"rxvt",
		"st",
		"terminator",
		"terminix",
		"terminology",
		"termit",
		"termite",
		"tilda",
		"tilix",
		"urxvt",
		"uxterm",
		"wezterm",
		"x-terminal-emulator",
		"xfce4-terminal",
		"xterm",
	}

	for _, v := range t {
		path, _ := exec.LookPath(v)

		if path != "" {
			return path
		}
	}

	return ""
}

func WrapWithTerminal(in string) string {
	if terminal == "" {
		return in
	}

	return fmt.Sprintf("%s -e %s", terminal, in)
}

func findTerminalApps() {
	conf := fastwalk.Config{
		Follow: true,
	}

	for _, root := range xdg.ApplicationDirs {
		if _, err := os.Stat(root); err != nil {
			continue
		}

		if err := fastwalk.Walk(&conf, root, func(path string, d fs.DirEntry, err error) error {
			if strings.HasSuffix(path, ".desktop") {
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}

				if bytes.Contains(b, []byte("Terminal=true")) {
					terminalApps[filepath.Base(path)] = struct{}{}
				}
			}
			return nil
		}); err != nil {
			slog.Error("terminal", "walk", err)
			os.Exit(1)
		}
	}
}

func ForceTerminalForFile(file string) bool {
	cmd := exec.Command("sh", "-c", fmt.Sprintf("xdg-mime query default $(xdg-mime query filetype %s)", file))
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	homedir, err := os.UserHomeDir()
	if err != nil {
		log.Panic(err)
	}

	cmd.Dir = homedir

	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Println(err)
		log.Println(string(out))
		return false
	}

	if _, ok := terminalApps[strings.TrimSpace(string(out))]; ok {
		return true
	}

	return false
}
