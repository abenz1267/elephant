package main

import (
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/abenz1267/elephant/v2/pkg/common"
)

const (
	ActionOpen     = "open"
	ActionOpenDir  = "opendir"
	ActionCopyPath = "copypath"
	ActionCopyFile = "copyfile"
)

func Activate(identifier, action string, query string, args string) {
	f, _ := paths.Load(identifier)
	path := f.(*file).path

	if action == "" {
		action = ActionOpen
	}

	switch action {
	case ActionOpen, ActionOpenDir:
		if action == ActionOpenDir {
			path = filepath.Dir(path)
		}

		run := strings.TrimSpace(fmt.Sprintf("%s xdg-open '%s'", common.LaunchPrefix(config.LaunchPrefix), path))

		if common.ForceTerminalForFile(path) {
			run = common.WrapWithTerminal(run)
		}

		cmd := exec.Command("sh", "-c", run)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true,
		}

		err := cmd.Start()
		if err != nil {
			slog.Error(Name, "actionopen", err)
		} else {
			go func() {
				cmd.Wait()
			}()
		}
	case ActionCopyPath:
		cmd := exec.Command("wl-copy", path)

		err := cmd.Start()
		if err != nil {
			slog.Error(Name, "actioncopypath", err)
		} else {
			go func() {
				cmd.Wait()
			}()
		}

	case ActionCopyFile:
		cmd := exec.Command("wl-copy", "-t", "text/uri-list", fmt.Sprintf("file://%s", path))

		err := cmd.Start()
		if err != nil {
			slog.Error(Name, "actioncopyfile", err)
		} else {
			go func() {
				cmd.Wait()
			}()
		}
	default:
		slog.Error(Name, "activate", fmt.Sprintf("unknown action: %s", action))
		return
	}
}
