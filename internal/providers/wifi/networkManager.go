package main

import (
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func init() {
	backends["nm"] = &NmcliBackend{}
}

type NmcliBackend struct{}

func (b *NmcliBackend) Available() bool {
	p, err := exec.LookPath("nmcli")
	return p != "" && err == nil
}

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
		slog.Error(Name, "nmcli_SetWifiEnabled", string(out))
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
		slog.Error(Name, "nmcli_GetNetworks", err)
	}

	for l := range strings.Lines(strings.TrimSpace(string(out))) {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		fields := nmcliSplitFields(l, 3)
		if len(fields) == 3 && fields[2] == "802-11-wireless" {
			known[fields[0]] = fields[1]
		}
	}

	cmd = exec.Command("nmcli", "-t", "-f", "SSID,SIGNAL,SECURITY,IN-USE,FREQ", "device", "wifi", "list")
	out, err = cmd.CombinedOutput()
	if err != nil {
		slog.Error(Name, "nmcli_GetNetworks", err)
		return result
	}

	seen := make(map[string]struct{})

	for l := range strings.Lines(strings.TrimSpace(string(out))) {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}

		fields := nmcliSplitFields(l, 5)
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

		signal, _ := strconv.Atoi(strings.TrimSpace(fields[1]))
		freq, _ := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(fields[4]), " MHz")))

		n := Network{
			SSID:      ssid,
			Signal:    signal,
			Security:  fields[2],
			InUse:     strings.TrimSpace(fields[3]) == "*",
			Frequency: freq,
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
		slog.Error(Name, "nmcli_Connect", string(out))
		return err
	}

	return nil
}

func (b *NmcliBackend) Disconnect(ssid string) error {
	cmd := exec.Command("nmcli", "connection", "down", ssid)
	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Error(Name, "nmcli_Disconnect", string(out))
		return err
	}

	return nil
}

func (b *NmcliBackend) Forget(ssid string) error {
	cmd := exec.Command("nmcli", "connection", "delete", ssid)
	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Error(Name, "nmcli_Forget", string(out))
		return err
	}

	return nil
}

func (b *NmcliBackend) WaitForNetworks() {
	maxTime := 5 //sec
	delay := 500 //ms
	for range int(maxTime * 1000 / delay) {
		out, err := exec.Command("nmcli", "-t", "-f", "SSID", "device", "wifi", "list", "--rescan", "yes").CombinedOutput()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			return
		}
		time.Sleep(time.Duration(delay) * time.Millisecond)
	}
	slog.Warn(Name, "nmcli_WaitForNetworks", "max retries reached")
}

// nmcliSplitFields splits an nmcli terse-mode line on unescaped colons.
func nmcliSplitFields(line string, n int) []string {
	var fields []string
	var buf strings.Builder
	escaped := false

	for _, r := range line {
		if escaped {
			buf.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == ':' && (n <= 0 || len(fields) < n-1) {
			fields = append(fields, buf.String())
			buf.Reset()
			continue
		}
		buf.WriteRune(r)
	}

	fields = append(fields, buf.String())
	return fields
}
