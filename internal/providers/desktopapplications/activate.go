package main

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/common/history"
)

const (
	ActionPin     = "pin"
	ActionPinUp   = "pinup"
	ActionPinDown = "pindown"
	ActionUnpin   = "unpin"
	ActionStart   = "start"
)

func Activate(identifier, action string, query string, args string) {
	switch action {
	case ActionPinUp:
		movePin(identifier, false)
	case ActionPinDown:
		movePin(identifier, true)
	case ActionPin, ActionUnpin:
		pinItem(identifier)
		return
	case history.ActionDelete:
		h.Remove(identifier)
		return
	case ActionStart:
		toRun := ""
		prefix := common.LaunchPrefix(config.LaunchPrefix)

		parts := strings.Split(identifier, ":")

		if len(parts) == 2 {
			for _, v := range files[parts[0]].Actions {
				if v.Action == parts[1] {
					toRun = v.Exec
					break
				}
			}
		} else {
			toRun = files[parts[0]].Exec
		}

		if files[parts[0]].Terminal {
			toRun = common.WrapWithTerminal(toRun)
		}

		cmd := exec.Command("sh", "-c", strings.TrimSpace(fmt.Sprintf("%s %s %s", prefix, toRun, args)))

		if files[parts[0]].Path != "" {
			cmd.Dir = files[parts[0]].Path
		}

		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true,
		}

		err := cmd.Start()
		if err != nil {
			slog.Error(Name, "activate", identifier, "error", err)
			return
		} else {
			go func() {
				cmd.Wait()
			}()
		}

		if config.History {
			h.Save(query, identifier)
		}

		slog.Info(Name, "activated", identifier)
	default:
		slog.Error(Name, "activate", fmt.Sprintf("unknown action: %s", action))
		return
	}
}

func isPinned(identifier string) bool {
	return slices.Contains(pins, identifier)
}

func movePin(identifier string, down bool) {
	index := -1
	for i, pin := range pins {
		if pin == identifier {
			index = i
			break
		}
	}

	if index == -1 {
		return
	}

	var newIndex int
	if down {
		newIndex = index + 1
		if newIndex >= len(pins) {
			return
		}
	} else {
		newIndex = index - 1
		if newIndex < 0 {
			return
		}
	}

	pins[index], pins[newIndex] = pins[newIndex], pins[index]
}

func pinItem(identifier string) {
	if isPinned(identifier) {
		i := slices.Index(pins, identifier)
		pins = append(pins[:i], pins[i+1:]...)
	} else {
		pins = append(pins, identifier)
	}

	var b bytes.Buffer
	encoder := gob.NewEncoder(&b)

	err := encoder.Encode(pins)
	if err != nil {
		slog.Error("pinned", "encode", err)
		return
	}

	err = os.MkdirAll(filepath.Dir(common.CacheFile(fmt.Sprintf("%s_pinned.gob", Name))), 0o755)
	if err != nil {
		slog.Error("pinned", "createdirs", err)
		return
	}

	err = os.WriteFile(common.CacheFile(fmt.Sprintf("%s_pinned.gob", Name)), b.Bytes(), 0o600)
	if err != nil {
		slog.Error("pinned", "writefile", err)
	}
}
