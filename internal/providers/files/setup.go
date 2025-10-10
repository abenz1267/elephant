package main

import (
	"bytes"
	"crypto/md5"
	_ "embed"
	"encoding/hex"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/djherbis/times"
	"github.com/fsnotify/fsnotify"
)

var paths sync.Map

//go:embed README.md
var readme string

type file struct {
	identifier string
	path       string
	changed    time.Time
}

var (
	Name       = "files"
	NamePretty = "Files"
	config     *Config
)

type Config struct {
	common.Config `koanf:",squash"`
	LaunchPrefix  string `koanf:"launch_prefix" desc:"overrides the default app2unit or uwsm prefix, if set." default:""`
}

func Setup() {
	start := time.Now()

	config = &Config{
		Config: common.Config{
			Icon:     "folder",
			MinScore: 50,
		},
		LaunchPrefix: "",
	}

	common.LoadConfig(Name, config)

	home, _ := os.UserHomeDir()
	cmd := exec.Command("fd", ".", home, "--ignore-vcs", "--type", "file", "--type", "directory")
	// cmd.Args = append(cmd.Args, "--hidden")

	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error(Name, "files", err)
		os.Exit(1)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	deleteChan := make(chan struct{})

	go func() {
		timer := time.NewTimer(time.Second * 5)
		do := false

		for {
			select {
			case <-deleteChan:
				timer.Reset(time.Second * 2)
				do = true
			case <-timer.C:
				if do {
					// this is ghetto, but paths aren't suffixed with `/`, so we can't just check for a path-prefix
					paths.Range(func(key, val any) bool {
						k := key.(string)
						v := val.(*file)

						if _, err := os.Stat(v.path); err != nil {
							paths.Delete(k)
						}

						return true
					})

					do = false
				}
			}
		}
	}()

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if event.Op == fsnotify.Remove || event.Op == fsnotify.Rename {
					deleteChan <- struct{}{}
				}

				if info, err := times.Stat(event.Name); err == nil {
					fileInfo, err := os.Stat(event.Name)
					if err == nil {
						path := event.Name

						if fileInfo.IsDir() {
							path = path + "/"
							watcher.Add(path)
						}

						md5 := md5.Sum([]byte(path))
						md5str := hex.EncodeToString(md5[:])

						if val, ok := paths.Load(md5str); ok {
							v := val.(*file)
							v.changed = info.ChangeTime()
						} else {
							paths.Store(md5str, &file{
								identifier: md5str,
								path:       path,
								changed:    info.ChangeTime(),
							})
						}
					}
				}
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()

	for v := range bytes.Lines(out) {
		if len(v) > 0 {
			path := strings.TrimSpace(string(v))

			if strings.HasSuffix(path, "/") {
				watcher.Add(path)
			}

			if info, err := times.Stat(path); err == nil {
				diff := start.Sub(info.ChangeTime())

				md5 := md5.Sum([]byte(path))
				md5str := hex.EncodeToString(md5[:])

				f := file{
					identifier: md5str,
					path:       path,
					changed:    time.Time{},
				}

				res := 3600 - diff.Seconds()
				if res > 0 {
					f.changed = info.ChangeTime()
				}

				paths.Store(md5str, &f)
			}
		}
	}

	slog.Info(Name, "time", time.Since(start))
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

func Icon() string {
	return config.Icon
}
