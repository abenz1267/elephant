package main

import (
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

type NmcliBackend struct{}

func (b *NmcliBackend) CheckWifiState() bool {
	cmd := exec.Command("nmcli", "radio", "wifi")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}

	return strings.TrimSpace(string(out)) == "enabled"
}

func (b *NmcliBackend) SetWifiEnabled(enabled bool) error {
	state := "off"
	if enabled {
		state = "on"
	}

	cmd := exec.Command("nmcli", "radio", "wifi", state)
	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Error(Name, "set wifi", string(out))
		return err
	}

	return nil
}

func (b *NmcliBackend) GetNetworks() []Network {
	var result []Network

	known := make(map[string]string)
	cmd := exec.Command("nmcli", "-t", "-f", "NAME,UUID,TYPE", "connection", "show")
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error(Name, "get connections", err)
	}

	for l := range strings.Lines(strings.TrimSpace(string(out))) {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		fields := strings.SplitN(l, ":", 3)
		if len(fields) == 3 && fields[2] == "802-11-wireless" {
			known[fields[0]] = fields[1]
		}
	}

	cmd = exec.Command("nmcli", "-t", "-f", "SSID,SIGNAL,SECURITY,IN-USE,FREQ", "device", "wifi", "list")
	out, err = cmd.CombinedOutput()
	if err != nil {
		slog.Error(Name, "get wifi list", err)
		return result
	}

	seen := make(map[string]struct{})

	for l := range strings.Lines(strings.TrimSpace(string(out))) {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}

		fields := strings.SplitN(l, ":", 5)
		if len(fields) < 5 {
			continue
		}

		ssid := fields[0]
		if ssid == "" {
			continue
		}

		if _, ok := seen[ssid]; ok {
			continue
		}
		seen[ssid] = struct{}{}

		n := Network{
			SSID:      ssid,
			Signal:    fields[1],
			Security:  fields[2],
			InUse:     strings.TrimSpace(fields[3]) == "*",
			Frequency: freqBand(fields[4]),
		}

		if _, ok := known[ssid]; ok {
			n.Known = true
			n.UUID = known[ssid]
		}

		result = append(result, n)
	}

	return result
}

func (b *NmcliBackend) Connect(ssid string, password string) error {
	cmd := exec.Command("nmcli", "device", "wifi", "connect", ssid)
	if password != "" {
		cmd.Args = append(cmd.Args, "password", password)
	}

	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Error(Name, "connect", string(out))
		return err
	}

	return nil
}

func (b *NmcliBackend) Disconnect(ssid string) error {
	cmd := exec.Command("nmcli", "connection", "down", ssid)
	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Error(Name, "disconnect", string(out))
		return err
	}

	return nil
}

func (b *NmcliBackend) Forget(ssid string) error {
	cmd := exec.Command("nmcli", "connection", "delete", ssid)
	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Error(Name, "forget", string(out))
		return err
	}

	return nil
}

func (b *NmcliBackend) Scan() {
	for {
		out, err := exec.Command("nmcli", "-t", "-f", "SSID", "device", "wifi", "list", "--rescan", "yes").CombinedOutput()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}
