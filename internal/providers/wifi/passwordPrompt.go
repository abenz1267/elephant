package main

import "log/slog"

type PasswordPrompt interface {
	Available() bool
	RequestPassword(ssid string) (string, error)
}

var passwordPrompts = map[string]PasswordPrompt{}

func detectPasswordPrompt(preference string) PasswordPrompt {
	if preference != "auto" {
		if p, ok := passwordPrompts[preference]; ok {
			if p.Available() {
				slog.Info(Name, "detectPasswordPrompt", preference)
				return p
			}
			slog.Warn(Name, "detectPasswordPrompt", preference+" not available, trying others")
		}
	}

	for name, p := range passwordPrompts {
		if name == preference || name == "terminal" {
			continue
		}
		if p.Available() {
			slog.Info(Name, "detectPasswordPrompt", name)
			return p
		}
	}

	if preference != "terminal" {
		if p, ok := passwordPrompts["terminal"]; ok && p.Available() {
			slog.Warn(Name, "detectPasswordPrompt", "falling back to terminal")
			return p
		}
	}

	return nil
}
