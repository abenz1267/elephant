package main

import "log/slog"

type Backend interface {
	Available() bool
	CheckWifiState() bool
	SetWifiEnabled(enabled bool) error
	GetNetworks() []Network
	Connect(ssid string, password string) error
	Disconnect(ssid string) error
	Forget(ssid string) error
	WaitForNetworks()
}

var backends = map[string]Backend{}

func detectBackend(preference string) Backend {
	if preference != "auto" {
		if b, ok := backends[preference]; ok {
			if b.Available() {
				slog.Info(Name, "detectBackend", preference)
				return b
			}
			slog.Warn(Name, "detectBackend", preference+" not available, trying others")
		}
	}

	for name, b := range backends {
		if name == preference {
			continue
		}
		if b.Available() {
			slog.Info(Name, "detectBackend", name)
			return b
		}
	}

	return nil
}
