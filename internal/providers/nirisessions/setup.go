package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	_ "embed"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name       = "nirisessions"
	NamePretty = "Niri Sessions"
	config     *Config
)

//go:embed README.md
var readme string

const (
	ActionStart    = "start"
	ActionStartNew = "start_new"
)

type Config struct {
	common.Config `koanf:",squash"`
	Sessions      []Session `koanf:"sessions" desc:"define the sessions" default:""`
}

type Session struct {
	Name       string      `koanf:"name" desc:"name for the session" default:""`
	Workspaces []Workspace `koanf:"workspaces" desc:"set of workspaces" default:""`
}

type Workspace struct {
	Windows []Window `koanf:"windows" desc:"windows in this workspace group" default:""`
	After   []string `koanf:"after" desc:"commands to run after the workspace has been processed" default:""`
}

type Window struct {
	Command string   `koanf:"command" desc:"command to run" default:""`
	AppID   string   `koanf:"app_id" desc:"app_id to identify the window" default:""`
	After   []string `koanf:"after" desc:"commands to run after the window has been spawned" default:""`
}

func Setup() {
	config = &Config{
		Config: common.Config{
			Icon:     "view-grid",
			MinScore: 20,
		},
	}

	common.LoadConfig(Name, config)
}

func Available() bool {
	if os.Getenv("XDG_CURRENT_DESKTOP") == "niri" {
		return true
	}

	slog.Info(Name, "available", "not a niri session. disabling")
	return false
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
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

func monitor(appid string, res chan int) {
	cmd := exec.Command("niri", "msg", "-j", "event-stream")

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

	for scanner.Scan() {
		var e OpenedOrChangedEvent
		err := json.Unmarshal(scanner.Bytes(), &e)
		if err != nil {
			slog.Error(Name, "event unmarshal", err)
		}

		if e.WindowOpenedOrChanged != nil && e.WindowOpenedOrChanged.Window.AppID == appid && e.WindowOpenedOrChanged.Window.Layout.PosInScrollingLayout != nil {
			res <- e.WindowOpenedOrChanged.Window.ID
			return
		}
	}

	if err := scanner.Err(); err != nil {
		slog.Error(Name, "monitor", err)
		return
	}

	if err := cmd.Wait(); err != nil {
		slog.Error(Name, "monitor", err)
		return
	}
}

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	i, _ := strconv.Atoi(identifier)

	s := config.Sessions[i]

	res := make(chan int)

	if action == ActionStartNew {
		goWorkspaceDown()
	}

	for k, v := range s.Workspaces {
		for _, w := range v.Windows {
			go monitor(w.AppID, res)

			cmd := exec.Command("sh", "-c", w.Command)
			err := cmd.Start()
			if err != nil {
				slog.Error(Name, "activate", err)
				return
			} else {
				go func() {
					cmd.Wait()
				}()
			}

			id := <-res
			idStr := strconv.Itoa(id)

			for _, v := range w.After {
				toRun := strings.ReplaceAll(v, "%ID%", idStr)

				cmd := exec.Command("sh", "-c", toRun)

				err := cmd.Run()
				if err != nil {
					slog.Error(Name, "activate after", err)
					return
				}
			}
		}

		for _, c := range v.After {
			cmd := exec.Command("sh", "-c", c)

			err := cmd.Run()
			if err != nil {
				slog.Error(Name, "activate after", err)
				return
			}
		}

		if k < len(s.Workspaces)-1 {
			goWorkspaceDown()
		}
	}
}

func goWorkspaceDown() {
	cmd := exec.Command("niri", "msg", "action", "focus-workspace-down")
	err := cmd.Start()
	if err != nil {
		slog.Error(Name, "activate", err)
		return
	} else {
		go func() {
			cmd.Wait()
		}()
	}
}

func Query(conn net.Conn, query string, single bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	start := time.Now()

	entries := []*pb.QueryResponse_Item{}

	for k, v := range config.Sessions {
		e := &pb.QueryResponse_Item{
			Identifier: fmt.Sprintf("%d", k),
			Text:       v.Name,
			Icon:       config.Icon,
			Provider:   Name,
			Actions:    []string{ActionStart, ActionStartNew},
		}

		if query != "" {
			score, positions, start := common.FuzzyScore(query, v.Name, exact)

			e.Score = score
			e.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
				Start:     start,
				Field:     "text",
				Positions: positions,
			}
		}

		if query == "" || e.Score > config.MinScore {
			entries = append(entries, e)
		}
	}

	slog.Debug(Name, "query", time.Since(start))

	return entries
}

func Icon() string {
	return config.Icon
}

func HideFromProviderlist() bool {
	return config.HideFromProviderlist
}

func State(provider string) *pb.ProviderStateResponse {
	return &pb.ProviderStateResponse{}
}
