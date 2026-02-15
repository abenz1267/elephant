package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"slices"

	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

type NiriWorkspace struct {
	Output    string `json:"output"`
	Name      string `json:"name"`
	Id        int    `json:"id"`
	Idx       int    `json:"idx"`
	IsFocused bool   `json:"is_focused"`
}

type NiriWindow struct {
	Title       string `json:"title"`
	AppID       string `json:"app_id"`
	WorkspaceID int    `json:"workspace_id"`
	Layout      struct {
		PosInScrollingLayout []int `json:"pos_in_scrolling_layout"`
	} `json:"layout"`
}

type NiriWorkspaceHandler struct{}

func (n NiriWorkspaceHandler) Focus(workspace string) {
	cmd := exec.Command("sh", "-c", fmt.Sprintf("niri msg action focus-workspace %s", workspace))

	err := cmd.Start()
	if err != nil {
		slog.Error(Name, "activate focus workspace", err)
	} else {
		go func() {
			cmd.Wait()
		}()
	}
}

func (n NiriWorkspaceHandler) GetWorkspaces(query string, exact bool) []*pb.QueryResponse_Item {
	entries := []*pb.QueryResponse_Item{}

	cmd := exec.Command("niri", "msg", "-j", "windows")
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error(Name, "getNiriWorkspaces cmd", string(out))
		return entries
	}

	var windows []NiriWindow

	err = json.Unmarshal(out, &windows)
	if err != nil {
		slog.Error(Name, "getNiriWorkspaces unmarshal windows", err)
		return entries
	}

	cmd = exec.Command("niri", "msg", "-j", "workspaces")
	out, err = cmd.CombinedOutput()
	if err != nil {
		slog.Error(Name, "getNiriWorkspaces cmd", string(out))
		return entries
	}

	windowsOnWorkspaces := make(map[int][]NiriWindow)

	for _, v := range windows {
		if _, ok := windowsOnWorkspaces[v.WorkspaceID]; ok {
			windowsOnWorkspaces[v.WorkspaceID] = append(windowsOnWorkspaces[v.WorkspaceID], v)
		} else {
			windowsOnWorkspaces[v.WorkspaceID] = []NiriWindow{v}
		}
	}

	var workspaces []NiriWorkspace

	err = json.Unmarshal(out, &workspaces)
	if err != nil {
		slog.Error(Name, "getNiriWorkspaces unmarshal", err)
		return entries
	}

	outs := make(map[string]struct{})

	for _, v := range workspaces {
		outs[v.Output] = struct{}{}
	}

	for _, v := range workspaces {
		text := fmt.Sprintf("Workspace: %d", v.Id)

		if v.Name != "" {
			text = fmt.Sprintf("Workspace: %s", v.Name)
		}

		subtext := "(empty)"

		if v.IsFocused {
			continue
		}

		if val, ok := windowsOnWorkspaces[v.Id]; ok {
			if len(val) > 0 {
				slices.SortFunc(val, func(i, j NiriWindow) int {
					if len(i.Layout.PosInScrollingLayout) == 0 || len(j.Layout.PosInScrollingLayout) == 0 {
						return 0
					}

					if i.Layout.PosInScrollingLayout[0] > j.Layout.PosInScrollingLayout[0] {
						return 1
					}

					if i.Layout.PosInScrollingLayout[0] < j.Layout.PosInScrollingLayout[0] {
						return -1
					}

					return 0
				})

				titles := fmt.Sprintf("%s - %s", val[0].AppID, val[0].Title)

				if len(val) > 1 {
					for _, v := range val[1:] {
						titles = fmt.Sprintf("%s, %s - %s", titles, v.AppID, v.Title)
					}
				}

				subtext = fmt.Sprintf("%s", titles)
			}
		}

		if len(outs) != 1 {
			text = fmt.Sprintf("%s (%s)", text, v.Output)
		}

		e := &pb.QueryResponse_Item{
			Identifier: fmt.Sprintf("%d", v.Idx),
			Text:       text,
			Subtext:    subtext,
			Actions:    []string{ActionFocusWorkspace},
			Provider:   Name,
			Icon:       config.Icon,
		}

		if query != "" {
			matched, score, pos, start, ok := calcScoreWorkspace(query, text, subtext, exact)

			if ok {
				field := "text"
				e.Score = score

				if matched != "Workspace" {
					field = "subtext"
				}

				e.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
					Start:     start,
					Field:     field,
					Positions: pos,
				}
			}
		}

		if query == "" || e.Score > config.MinScore {
			entries = append(entries, e)
		}
	}

	return entries
}
