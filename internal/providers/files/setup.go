package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/abenz1267/elephant/internal/common"
	"github.com/abenz1267/elephant/internal/providers"
	"github.com/abenz1267/elephant/internal/util"
	"github.com/adrg/xdg"
	"github.com/charlievieth/fastwalk"
	"github.com/djherbis/times"
	"github.com/fsnotify/fsnotify"
)

var (
	pm      sync.Mutex
	paths   = make(map[string]*file)
	results = providers.QueryData{}
)

type file struct {
	identifier string
	path       string
	changed    time.Time
}

var terminalApps = make(map[string]struct{})

var (
	Name       = "files"
	NamePretty = "Files"
	config     *Config
)

type Config struct {
	common.Config `koanf:",squash"`
	LaunchPrefix  string `koanf:"launch_prefix" desc:"overrides the default app2unit or uwsm prefix, if set. 'CLEAR' to not prefix." default:""`
}

func init() {
	start := time.Now()

	config = &Config{
		Config: common.Config{
			Icon:     "folder",
			MinScore: 50,
		},
		LaunchPrefix: "",
	}

	common.LoadConfig(Name, config)

	findTerminalApps()

	home, _ := os.UserHomeDir()
	cmd := exec.Command("fd", ".", home, "--ignore-vcs", "--type", "file", "--type", "directory")

	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error(Name, "files", err)
		os.Exit(1)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if event.Op == fsnotify.Remove || event.Op == fsnotify.Rename {
					pm.Lock()
					// this is ghetto, but paths aren't suffixed with `/`, so we can't just check for a path-prefix
					for k, v := range paths {
						if _, err := os.Stat(v.path); err != nil {
							delete(paths, k)
						}
					}
					pm.Unlock()
				}

				if info, err := times.Stat(event.Name); err == nil {
					pm.Lock()

					fileInfo, err := os.Stat(event.Name)
					if err == nil {
						path := event.Name

						if fileInfo.IsDir() {
							path = path + "/"
							watcher.Add(path)
						}

						md5 := md5.Sum([]byte(path))
						md5str := hex.EncodeToString(md5[:])

						if val, ok := paths[md5str]; ok {
							val.changed = info.ChangeTime()
						} else {
							paths[md5str] = &file{
								identifier: md5str,
								path:       path,
								changed:    info.ChangeTime(),
							}
						}
					}

					pm.Unlock()
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
				pm.Lock()

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

				paths[md5str] = &f

				pm.Unlock()
			}
		}
	}

	slog.Info(Name, "files", len(paths), "time", time.Since(start))
}

func PrintDoc() {
	fmt.Printf("### %s\n", NamePretty)
	fmt.Println("Search files and folders.")
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

func Cleanup(qid uint32) {
	slog.Info(Name, "cleanup", qid)
	results.Lock()
	delete(results.Queries, qid)
	results.Unlock()
}

func findTerminalApps() {
	conf := fastwalk.Config{
		Follow: true,
	}

	for _, root := range xdg.ApplicationDirs {
		if _, err := os.Stat(root); err != nil {
			continue
		}

		if err := fastwalk.Walk(&conf, root, func(path string, d fs.DirEntry, err error) error {
			if strings.HasSuffix(path, ".desktop") {
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}

				if bytes.Contains(b, []byte("Terminal=true")) {
					terminalApps[filepath.Base(path)] = struct{}{}
				}
			}
			return nil
		}); err != nil {
			slog.Error(Name, "walk", err)
			os.Exit(1)
		}
	}
}

func Icon() string {
	return config.Icon
}
