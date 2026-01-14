package main

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/common/history"
	"github.com/abenz1267/elephant/v2/pkg/common/wlr"
	"github.com/neurlang/wayland/wl"
)

const (
	ActionPin         = "pin"
	ActionPinUp       = "pinup"
	ActionPinDown     = "pindown"
	ActionUnpin       = "unpin"
	ActionStart       = "start"
	ActionNewInstance = "new_instance"
)

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
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
	case ActionStart, ActionNewInstance:
		toRun := ""
		prefix := common.LaunchPrefix(config.LaunchPrefix)

		parts := strings.Split(identifier, ":")

		isAction := false

		if len(parts) == 2 {
			for _, v := range files[parts[0]].Actions {
				if v.Action == parts[1] {
					toRun = v.Exec
					isAction = true
					break
				}
			}
		} else {
			toRun = files[parts[0]].Exec
		}

		if args == "" && config.WindowIntegration && wlr.IsSetup && action != ActionNewInstance {
			if !isAction || !config.WindowIntegrationIgnoreActions {
				if id, ok := appHasWindow(files[parts[0]]); ok {
					if err := wlr.Activate(id); err == nil {

						if config.History {
							h.Save(query, identifier)
						}

						return
					} else {
						slog.Error(Name, "focus window", err)
					}
				}
			}
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

		if config.WMIntegration && wmi != nil {
			appid := files[parts[0]].StartupWMClass

			if !slices.Contains(config.SingleInstanceApps, appid) || !slices.Contains(wmi.GetCurrentWindows(), appid) {
				go wmi.MoveToWorkspace(wmi.GetWorkspace(), appid)
			}
		}

		slog.Debug(Name, "activate", cmd.String())

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

func movePin(identifier string, down bool) {
	pinsMu.Lock()
	defer pinsMu.Unlock()

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
	pinsMu.Lock()
	defer pinsMu.Unlock()

	if slices.Contains(pins, identifier) {
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

func appHasWindow(f *DesktopFile) (wl.ProxyId, bool) {
	w := wlr.Windows()
	bin := strings.Fields(f.Exec)[0]

	for k, v := range w {
		if v.AppID == f.StartupWMClass || v.AppID == f.Icon || v.AppID == bin || v.Title == bin {
			return k, true
		}
	}

	return wl.ProxyId(0), false
}
