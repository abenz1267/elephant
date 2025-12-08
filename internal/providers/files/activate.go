package main

import (
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/abenz1267/elephant/v2/pkg/common"
)

const (
	ActionOpen      = "open"
	ActionOpenDir   = "opendir"
	ActionCopyPath  = "copypath"
	ActionCopyFile  = "copyfile"
	ActionLocalsend = "localsend"
	ActionReindex   = "refresh_index"
)

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	f := getFile(identifier)

	var path string

	if f == nil && action != ActionReindex {
		slog.Error(Name, "activate", "file not found")
		return
	}

	if f != nil {
		path = f.Path
	}

	if action == "" {
		action = ActionOpen
	}

	switch action {
	case ActionReindex:
		index()
	case ActionLocalsend:
		cmd := exec.Command("sh", "-c", strings.TrimSpace(fmt.Sprintf("%s %s %s", common.LaunchPrefix(""), "localsend", path)))

		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true,
		}

		err := cmd.Start()
		if err != nil {
			slog.Error(Name, "actionlocalsend", err)
		} else {
			go func() {
				cmd.Wait()
			}()
		}
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
