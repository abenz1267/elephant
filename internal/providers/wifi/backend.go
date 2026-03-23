package main

import (
	"os/exec"
)

type Backend interface {
	CheckWifiState() bool
	SetWifiEnabled(enabled bool) error
	GetNetworks() []Network
	Connect(ssid string, password string) error
	Disconnect(ssid string) error
	Forget(ssid string) error
	Scan()
}

var backends = map[string]func() Backend{
	"nm": func() Backend { return &NmcliBackend{} },
}

func detectBackend(preference string) Backend {
	if preference != "auto" {
		if fn, ok := backends[preference]; ok {
			if p, err := exec.LookPath(preference); p != "" && err == nil {
				return fn()
			}
		}
		return nil
	}

	for name, fn := range backends {
		if p, err := exec.LookPath(name); p != "" && err == nil {
			return fn()
		}
	}

	return nil
}
