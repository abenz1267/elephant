package main

import (
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	_ "embed"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name        = "googlecalendar"
	NamePretty  = "Google Calendar"
	config      *Config
	teamsLinkRE *regexp.Regexp
)

//go:embed README.md
var readme string

type Config struct {
	common.Config `koanf:",squash"`
	Calendars     []string `koanf:"calendars" desc:"calendars to index" default:"[\"primary\"]"`
	Notify        bool     `koanf:"notify" desc:"notify before an event" default:"true"`
	TeamsForLinux bool     `koanf:"teams_for_linux" desc:"open teams meetings with teams-for-linux instead of browser" default:"false"`
	NotifyBuffer  int      `koanf:"notify_buffer" desc:"minutes before the event to notify" default:"5"`
	Credentials   string   `koanf:"credentials" desc:"location on disk for credentials.json" default:""`
}

func Setup() {
	config = &Config{
		Config: common.Config{
			Icon:     "google",
			MinScore: 20,
		},
		Calendars:     []string{"primary"},
		Notify:        true,
		TeamsForLinux: false,
	}

	common.LoadConfig(Name, config)

	if config.NamePretty != "" {
		NamePretty = config.NamePretty
	}

	if !common.FileExists(config.Credentials) {
		slog.Error(Name, "setup", "credentials file not found")
		return
	}

	teamsLinkRE = regexp.MustCompile(`https://teams\.microsoft\.com/l/meetup-join/[^>]+`)

	setupClient(config.Credentials)
}

func Available() bool {
	return true
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

const ActionJoinTeamsMeeting = "join_teams"

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	i := slices.IndexFunc(loadedEvents, func(e Event) bool {
		return e.Id == identifier
	})

	tool := "xdg-open"
	if config.TeamsForLinux {
		tool = "teams-for-linux"
	}

	cmd := exec.Command("sh", "-c", strings.TrimSpace(fmt.Sprintf("%s %s '%s'", common.LaunchPrefix(""), tool, loadedEvents[i].TeamsLink)))

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	err := cmd.Start()
	if err != nil {
		slog.Error(Name, "activate", err)
	} else {
		go func() {
			cmd.Wait()
		}()
	}
}

func Query(conn net.Conn, query string, single bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	start := time.Now()

	entries := []*pb.QueryResponse_Item{}
	getEvents()

	for _, v := range loadedEvents {
		a := []string{}

		if v.TeamsLink != "" {
			a = append(a, ActionJoinTeamsMeeting)
		}

		date := v.Start.DateTime
		if date == "" {
			date = v.Start.Date
		}

		e := &pb.QueryResponse_Item{
			Identifier: v.Id,
			Text:       v.Summary,
			Subtext:    date,
			Actions:    a,
			Icon:       "",
			Provider:   Name,
		}

		entries = append(entries, e)
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
