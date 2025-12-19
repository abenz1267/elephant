package main

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"al.essio.dev/pkg/shellescape"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/common/history"
)

const (
	ActionSearch           = "search"
	ActionSearchSuggestion = "search_suggestion"
	ActionOpenURL          = "open_url"
)

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	switch action {
	case ActionOpenURL:
		address := query
		if !strings.Contains(address, "://") {
			address = fmt.Sprintf("https://%s", query)
		}

		openURL(address)

	case ActionSearch:
		engine := engineNameMap[identifier]

		if args == "" {
			args = query
		}

		_, args = splitEnginePrefix(args)

		address := expandSubstitutions(engine.URL, args)
		run(query, identifier, address)

	case ActionSearchSuggestion:
		currentSuggestionsMutex.RLock()
		s, ok := currentSuggestions[identifier]
		currentSuggestionsMutex.RUnlock()
		if !ok {
			slog.Error(Name, "activate", "missing suggestion", "id", identifier)
			return
		}

		// TODO: Add engine history instead of suggestion history since suggestions are temporary
		url := expandSubstitutions(s.Engine.URL, s.Content)
		run(query, identifier, url)

	case history.ActionDelete:
		h.Remove(identifier)

	default:
		q := ""

		if !config.EnginesAsActions {
			slog.Error(Name, "activate", fmt.Sprintf("unknown action: %s", action))
			return
		}

		q = engineNameMap[action].URL
		q = expandSubstitutions(q, args)
		run(query, identifier, q)
	}
}

func expandSubstitutions(format string, args string) string {
	result := format
	if strings.Contains(format, "%CLIPBOARD%") {
		clipboardText := common.ClipboardText()
		if clipboardText == "" {
			slog.Error(Name, "activate", "empty clipboard")
		}

		result = strings.ReplaceAll(os.ExpandEnv(format), "%CLIPBOARD%", url.QueryEscape(clipboardText))
	} else if strings.Contains(format, "%TERM%") {
		result = strings.ReplaceAll(os.ExpandEnv(format), "%TERM%", url.QueryEscape(args))
	}

	return result
}

func run(query, identifier, url string) {
	openURL(url)

	if config.History {
		h.Save(query, identifier)
	}
}

func openURL(url string) {
	cmdStr := fmt.Sprintf("%s %s %s", common.LaunchPrefix(""), config.Command, shellescape.Quote(url))
	cmd := exec.Command("sh", "-c", strings.TrimSpace(cmdStr))

	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		slog.Error(Name, "executeCommand", err)
		return
	}

	go cmd.Wait()
}
