package desktopapplications

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

type Niri struct{}

type NiriWindow struct {
	AppID string `json:"app_id"`
}

func (Niri) GetCurrentWindows() []string {
	res := []string{}

	cmd := exec.Command("niri", "msg", "-j", "windows")

	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error(Name, "nirigetcurrentwindows", err)
		return res
	}

	var windows []NiriWindow

	err = json.Unmarshal(out, &windows)
	if err != nil {
		slog.Error(Name, "nirigetcurrentwindows", err)
		return res
	}

	for _, v := range windows {
		res = append(res, v.AppID)
	}

	return res
}

func (Niri) GetWorkspace() string {
	cmd := exec.Command("niri", "msg", "workspaces")

	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error(Name, "niriworkspaces", err)
		return ""
	}

	for line := range strings.Lines(string(out)) {
		line = strings.TrimSpace(line)

		if after, ok := strings.CutPrefix(line, "*"); ok {
			return strings.TrimSpace(after)
		}
	}

	return ""
}

type OpenedOrChangedEvent struct {
	WindowOpenedOrChanged *struct {
		Window struct {
			ID     int    `json:"id"`
			AppID  string `json:"app_id"`
			Layout struct {
				PosInScrollingLayout []int `json:"pos_in_scrolling_layout"`
			} `json:"layout"`
		} `json:"window"`
	} `json:"WindowOpenedOrChanged,omitempty"`
}

func (c Niri) MoveToWorkspace(workspace, initialWMClass string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "niri", "msg", "-j", "event-stream")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		slog.Error(Name, "monitor", err)
		return
	}

	if err := cmd.Start(); err != nil {
		slog.Error(Name, "monitor", err)
		return
	}

	scanner := bufio.NewScanner(stdout)
	done := make(chan bool, 1)

	go func() {
		defer func() { done <- true }()

		for scanner.Scan() {
			var e OpenedOrChangedEvent

			err := json.Unmarshal(scanner.Bytes(), &e)
			if err != nil {
				slog.Error(Name, "event unmarshal", err)
				continue
			}

			if e.WindowOpenedOrChanged != nil && e.WindowOpenedOrChanged.Window.AppID == initialWMClass {
				if c.GetWorkspace() == workspace {
					continue
				}

				cmd := exec.Command("niri", "msg", "action", "move-window-to-workspace", workspace, "--window-id", fmt.Sprintf("%d", e.WindowOpenedOrChanged.Window.ID), "--focus", "false")
				out, err := cmd.CombinedOutput()
				if err != nil {
					slog.Error(Name, "nirimovetoworkspace", out)
				}

				continue
			}
		}
	}()

	select {
	case <-ctx.Done():
		cmd.Process.Kill()
	case <-done:
		cmd.Process.Kill()
	}

	cmd.Wait()
}
