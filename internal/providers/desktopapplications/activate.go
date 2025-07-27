package main

import (
	"log/slog"
	"os/exec"
)

// uwsm app -- firefox-developer-edition.desktop:open-profile-manager
// app2unit firefox-developer-edition.desktop:new-private-window

func Activate(sid uint32, identifier, action string) {
	cmd := exec.Command(config.Runner, identifier)

	err := cmd.Start()
	if err != nil {
		slog.Error(Name, "activate", identifier, "error", err)
	}

	go func() {
		cmd.Wait()
	}()
}
