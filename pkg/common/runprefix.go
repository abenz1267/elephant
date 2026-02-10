package common

import (
	"log/slog"
	"os"
	"os/exec"
)

var runPrefix = ""

func InitRunPrefix() {
	runPrefix = elephantConfig.LaunchPrefix

	if runPrefix != "" {
		return
	}

	if !elephantConfig.AutoDetectLaunchPrefix {
		return
	}

	app2unit, err := exec.LookPath("app2unit")
	if err == nil && app2unit != "" {
		xdgTerminalExec, err := exec.LookPath("xdg-terminal-exec")
		if err == nil && xdgTerminalExec != "" {
			runPrefix = "app2unit"
			slog.Info("config", "runprefix autodetect", runPrefix)
			return
		}
	}

	uwsm, err := exec.LookPath("uwsm")
	if err == nil {
		cmd := exec.Command(uwsm, "check", "is-active")
		err := cmd.Run()
		if err == nil {
			runPrefix = "uwsm-app --"
			slog.Info("config", "runprefix autodetect", runPrefix)
			return
		}
	}

	if os.Getenv("XDG_CURRENT_DESKTOP") == "niri" {
		niri, err := exec.LookPath("niri")
		if err == nil && niri != "" {
			runPrefix = "niri msg action spawn --"
			slog.Info("config", "runprefix autodetect", runPrefix)
			return
		}
	}

	spid := os.Getenv("SYSTEMD_EXEC_PID")
	if spid != "" {
		systemdrun, err := exec.LookPath("systemd-run")
		if err == nil && systemdrun != "" {
			runPrefix = "systemd-run --user --scope"
			slog.Info("config", "runprefix autodetect", runPrefix)
			return
		}
	}

	slog.Info("config", "runprefix autodetect", "<empty>")
}

func LaunchPrefix() string {
	return runPrefix
}
