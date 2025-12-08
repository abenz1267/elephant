package main

import (
	"bufio"
	"crypto/md5"
	_ "embed"
	"encoding/hex"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
	"github.com/djherbis/times"
	"github.com/fsnotify/fsnotify"
)

//go:embed README.md
var readme string

var (
	Name         = "files"
	NamePretty   = "Files"
	config       *Config
	watcher      *fsnotify.Watcher
	ignoreRegexp []*regexp.Regexp
	hasLocalsend bool
)

type IgnoredPreview struct {
	Path        string `koanf:"path" desc:"path to ignore preview for" default:""`
	Placeholder string `koanf:"placeholder" desc:"text to display instead" default:""`
}

type Config struct {
	common.Config  `koanf:",squash"`
	LaunchPrefix   string           `koanf:"launch_prefix" desc:"overrides the default app2unit or uwsm prefix, if set." default:""`
	IgnoredDirs    []string         `koanf:"ignored_dirs" desc:"ignore these directories. regexp based." default:""`
	IgnorePreviews []IgnoredPreview `koanf:"ignore_previews" desc:"paths will not have a preview" default:""`
	IgnoreWatching []string         `koanf:"ignore_watching" desc:"paths will not be watched" default:""`
	SearchDirs     []string         `koanf:"search_dirs" desc:"directories to search for files" default:"$HOME"`
	FdFlags        []string         `koanf:"fd_flags" desc:"flags for fd" default:"['--ignore-vcs', '--type,' ,'file', '--type,' 'directory']"`
	WatchBuffer    int              `koanf:"watch_buffer" desc:"time in millisecnds elephant will gather changed paths before processing them" default:"2000"`
	WatchDirs      []string         `koanf:"watch_dirs" desc:"watch these dirs, even if watch = false" default:"[]"`
	Watch          bool             `koanf:"watch" desc:"watch indexed directories" default:"true"`
}

func Setup() {
	start := time.Now()

	err := openDB()
	if err != nil {
		slog.Error(Name, "setup", err)
		return
	}

	ls, err := exec.LookPath("localsend")
	if ls != "" && err == nil {
		hasLocalsend = true
	}

	config = &Config{
		Config: common.Config{
			Icon:     "folder",
			MinScore: 20,
		},
		LaunchPrefix: "",
		SearchDirs:   []string{},
		WatchBuffer:  2000,
		Watch:        true,
		FdFlags:      []string{"--ignore-vcs", "--type", "file", "--type", "directory"},
	}

	common.LoadConfig(Name, config)

	if config.NamePretty != "" {
		NamePretty = config.NamePretty
	}

	watcher, err = fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	deleteChan := make(chan string)
	regularChan := make(chan string)

	go handleDelete(deleteChan)
	go handleRegular(regularChan)

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					continue
				}

				if event.Op == fsnotify.Remove || event.Op == fsnotify.Rename {
					deleteChan <- event.Name
					continue
				}

				regularChan <- event.Name
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()

	go index()

	slog.Info(Name, "time", time.Since(start))
}

func index() {
	start := time.Now()
	dropAll()

	searchDirs := config.SearchDirs
	if len(searchDirs) == 0 {
		home, _ := os.UserHomeDir()
		searchDirs = []string{home}
	}

	for _, v := range config.IgnoredDirs {
		r, err := regexp.Compile(v)
		if err != nil {
			slog.Error(Name, "ignoredirs regexp", err)
			continue
		}

		ignoreRegexp = append(ignoreRegexp, r)
	}

	cmd := exec.Command("fd", ".")
	cmd.Args = append(cmd.Args, searchDirs...)
	cmd.Args = append(cmd.Args, config.FdFlags...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		slog.Error(Name, "files", err)
		return
	}

	if err := cmd.Start(); err != nil {
		slog.Error(Name, "files", err)
		return
	}

	for _, path := range config.SearchDirs {
		if shouldWatch(path) {
			watcher.Add(path)
		}

		if info, err := times.Stat(path); err == nil {
			diff := start.Sub(info.ChangeTime())

			md5 := md5.Sum([]byte(path))
			md5str := hex.EncodeToString(md5[:])

			f := File{
				Identifier: md5str,
				Path:       path,
				Changed:    time.Time{},
			}

			res := 3600 - diff.Seconds()
			if res > 0 {
				f.Changed = info.ChangeTime()
			}

			putFile(f)
		}
	}

	scanner := bufio.NewScanner(stdout)

	batch := make([]File, 0, 5000)

outer:
	for scanner.Scan() {
		path := strings.TrimSpace(scanner.Text())

		if len(path) > 0 {
			for _, v := range ignoreRegexp {
				if v.Match([]byte(path)) {
					continue outer
				}
			}

			if strings.HasSuffix(path, "/") {
				if shouldWatch(path) {
					watcher.Add(path)
				}
			}

			if info, err := times.Stat(path); err == nil {
				diff := start.Sub(info.ChangeTime())

				md5 := md5.Sum([]byte(path))
				md5str := hex.EncodeToString(md5[:])

				f := File{
					Identifier: md5str,
					Path:       path,
					Changed:    time.Time{},
				}

				res := 3600 - diff.Seconds()
				if res > 0 {
					f.Changed = info.ChangeTime()
				}

				batch = append(batch, f)

				if len(batch) >= 5000 {
					if err := putFileBatch(batch); err != nil {
						slog.Error(Name, "batch insert", err)
					}
					batch = batch[:0]
				}
			}
		}
	}

	if len(batch) > 0 {
		if err := putFileBatch(batch); err != nil {
			slog.Error(Name, "final batch insert", err)
		}
	}

	if err := cmd.Wait(); err != nil {
		slog.Error(Name, "cmd wait", err)
	}
}

func Available() bool {
	p, err := exec.LookPath("fd")

	if p == "" || err != nil {
		slog.Info(Name, "available", "fd not found. disabling.")
		return false
	}

	return true
}

func handleDelete(deleteChan chan string) {
	timer := time.NewTimer(time.Millisecond * time.Duration(config.WatchBuffer))
	do := false
	toDelete := []string{}

	for {
		select {
		case path := <-deleteChan:
			timer.Reset(time.Millisecond * time.Duration(config.WatchBuffer))
			toDelete = append(toDelete, path)
			do = true
		case <-timer.C:
			if do {
				slices.Sort(toDelete)
				toDelete = slices.Compact(toDelete)

				for _, path := range toDelete {
					deleteFileByPath(path)
				}

				toDelete = []string{}
				do = false
			}
		}
	}
}

func handleRegular(regularChan chan string) {
	timer := time.NewTimer(time.Millisecond * time.Duration(config.WatchBuffer))
	do := false
	data := []string{}

	for {
		select {
		case path := <-regularChan:
			timer.Reset(time.Millisecond * time.Duration(config.WatchBuffer))
			data = append(data, path)
			do = true
		case <-timer.C:
			if do {
				slices.Sort(data)
				data = slices.Compact(data)

				for _, path := range data {
					if info, err := times.Stat(path); err == nil {
						fileInfo, err := os.Stat(path)
						if err == nil {
							if fileInfo.IsDir() {
								path = path + "/"

								if shouldWatch(path) {
									watcher.Add(path)
								}
							}

							md5 := md5.Sum([]byte(path))
							md5str := hex.EncodeToString(md5[:])

							if val := getFile(md5str); val != nil {
								val.Changed = info.ChangeTime()
								putFile(*val)
							} else {
								putFile(File{
									Identifier: md5str,
									Path:       path,
									Changed:    info.ChangeTime(),
								})
							}
						}
					}
				}

				data = []string{}
				do = false
			}
		}
	}
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

func Icon() string {
	return config.Icon
}

func HideFromProviderlist() bool {
	return config.HideFromProviderlist
}

func State(provider string) *pb.ProviderStateResponse {
	actions := []string{}

	if !config.Watch {
		actions = append(actions, ActionReindex)
	}

	return &pb.ProviderStateResponse{
		Actions: actions,
	}
}

func shouldWatch(path string) bool {
	if (config.Watch || slices.Contains(config.WatchDirs, path)) && !slices.Contains(config.IgnoreWatching, path) {
		return true
	}

	return false
}
