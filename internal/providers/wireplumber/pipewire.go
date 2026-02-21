package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type PipewireDumpNode []struct {
	ID      int    `json:"id"`
	Type    string `json:"type"`
	Version int    `json:"version"`
	Info    struct {
		Props struct {
			NodeName        string `json:"node.name"`
			NodeDescription string `json:"node.description"`
			MediaClass      string `json:"media.class"`
		} `json:"props"`
	} `json:"info"`
}

type PipewireDumpMetadata []struct {
	ID      int    `json:"id"`
	Type    string `json:"type"`
	Version int    `json:"version"`
	Props   struct {
		MetadataName string `json:"metadata.name"`
		ObjectSerial int    `json:"object.serial"`
	} `json:"props"`
	Metadata []struct {
		Subject int    `json:"subject"`
		Key     string `json:"key"`
		Type    string `json:"type"`
		Value   struct {
			Name string `json:"name"`
		} `json:"value"`
	} `json:"metadata"`
}

const (
	WpCtlCommand  = "wpctl"
	PwDumpCommand = "pw-dump"
)

const (
	PipewireTypeSource = "Audio/Source"
	PipewireTypeSink   = "Audio/Sink"
)

type PipewireDevice struct {
	ID           int
	Description  string
	Selected     bool
	Volume       int
	Muted        bool
	PipewireType string
}

func runCommand(cmd *exec.Cmd) error {
	err := cmd.Start()

	if err != nil {
		return err
	}

	go func() {
		cmd.Wait()
	}()

	return nil
}

func getVolumeState(deviceId int) (int, bool, error) {
	cmd := exec.Command(WpCtlCommand, "get-volume", strconv.Itoa(deviceId))
	output, err := cmd.Output()
	if err != nil {
		return 0, false, err
	}

	// e.g. "Volume: 40.0 [MUTED]"
	outputString := string(output)
	muted := strings.Contains(outputString, "MUTED")

	// extract volume from output string (i.e. see example output above)
	_, volStr, _ := strings.Cut(outputString, ": ")
	volStr, _, _ = strings.Cut(volStr, " ")
	volStr = strings.TrimSpace(volStr)

	volume, err := strconv.ParseFloat(volStr, 64)
	if err != nil {
		volume = -1
	}

	return int(volume * 100), muted, nil
}

const (
	PipewireDefaultInputKey  = "default.configured.audio.source"
	PipewireDefaultOutputKey = "default.configured.audio.sink"
)

func getDefaultSinkAndSource() (defaultSourceName, defaultSinkName string, err error) {
	cmd := exec.Command(PwDumpCommand, "Metadata")
	output, err := cmd.Output()
	if err != nil {
		return "", "", err
	}

	dump := PipewireDumpMetadata{}
	err = json.Unmarshal(output, &dump)

	for _, metadata := range dump {
		for _, entry := range metadata.Metadata {
			switch entry.Key {
			case PipewireDefaultInputKey:
				defaultSourceName = entry.Value.Name
			case PipewireDefaultOutputKey:
				defaultSinkName = entry.Value.Name
			}
		}
	}

	return defaultSourceName, defaultSinkName, nil
}

func GetDevices() ([]PipewireDevice, error) {
	cmd := exec.Command(PwDumpCommand, "Node")
	output, err := cmd.Output()
	if err != nil {
		return []PipewireDevice{}, err
	}

	dump := PipewireDumpNode{}
	err = json.Unmarshal(output, &dump)
	if err != nil {
		return []PipewireDevice{}, err
	}

	defaultSinkName, defaultSourceName, err := getDefaultSinkAndSource()
	if err != nil {
		return []PipewireDevice{}, err
	}

	var devices []PipewireDevice
	for _, node := range dump {
		if node.Info.Props.MediaClass == PipewireTypeSink || node.Info.Props.MediaClass == PipewireTypeSource {
			var volume int
			var muted bool
			volume, muted, err = getVolumeState(node.ID)

			device := PipewireDevice{
				ID:           node.ID,
				Description:  node.Info.Props.NodeDescription,
				PipewireType: node.Info.Props.MediaClass,
				Volume:       volume,
				Muted:        muted,
				Selected:     node.Info.Props.NodeName == defaultSinkName || node.Info.Props.NodeName == defaultSourceName,
			}

			devices = append(devices, device)
		}
	}

	return devices, nil
}

func SetDefaultDevice(deviceId int) error {
	cmd := exec.Command(WpCtlCommand, "set-default", strconv.Itoa(deviceId))
	return runCommand(cmd)
}

func ToggleMute(deviceId int) error {
	cmd := exec.Command(WpCtlCommand, "set-mute", strconv.Itoa(deviceId), "toggle")
	return runCommand(cmd)
}

func SetVolume(deviceId int, volumeDiffPercentage int) error {
	var sign string
	if volumeDiffPercentage >= 0 {
		sign = "+"
	} else {
		sign = "-"
	}
	cmd := exec.Command(WpCtlCommand, "set-volume", strconv.Itoa(deviceId), fmt.Sprintf("%d%%%s", volumeDiffPercentage, sign))
	return runCommand(cmd)
}
