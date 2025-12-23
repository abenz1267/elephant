package common

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v6"
)

var (
	gitMu      sync.Mutex
	setupRepos = make(map[string]Repo)
)

type Repo struct {
	w *git.Worktree
	r *git.Repository
}

type Gittable interface {
	SetLocation(string)
	URL() string
	SetWorktree(*git.Worktree)
	SetRepository(*git.Repository)
}

func SetupGit(provider string, cfg Gittable) {
	gitMu.Lock()
	defer gitMu.Unlock()

	x := 0
	base := filepath.Base(cfg.URL())
	folder := CacheFile(base)
	var w *git.Worktree
	var r *git.Repository
	var pull bool

	if val, ok := setupRepos[cfg.URL()]; !ok {
		for x < 15 {
			x++

			time.Sleep(1 * time.Second)

			slog.Info(provider, "gitsetup", "trying to setup git...")

			// clone
			if !FileExists(folder) {
				var err error

				url := cfg.URL()
				if strings.HasPrefix(url, "https://github.com/") {
					url = strings.Replace(url, "https://github.com/", "git@github.com:", 1)
				}

				r, err = git.PlainClone(folder, &git.CloneOptions{
					URL:               url,
					RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
				})
				if err != nil {
					slog.Debug(provider, "gitclone", err)
					continue
				}
			} else {
				var err error
				r, err = git.PlainOpen(folder)
				if err != nil {
					slog.Debug(provider, "gitclone", err)
					continue
				}

				pull = true
			}

			var err error

			w, err = r.Worktree()
			if err != nil {
				slog.Debug(provider, "gitpull", err)
				continue
			}

			if pull {
				err = w.Pull(&git.PullOptions{RemoteName: "origin"})
				if err != nil {
					if err.Error() != "already up-to-date" && err.Error() != "remote repository is empty" {
						slog.Info(provider, "gitpull", err)
						continue
					}
				}
			}

			setupRepos[cfg.URL()] = Repo{
				w: w,
				r: r,
			}

			break
		}
	} else {
		slog.Info(provider, "gitsetup", "repo already setup")

		r = val.r
		w = val.w
	}

	cfg.SetLocation(folder)
	cfg.SetRepository(r)
	cfg.SetWorktree(w)
}

type PushData struct {
	provider string
	file     string
	w        *git.Worktree
	r        *git.Repository
}

var pushChan chan PushData

func init() {
	pushChan = make(chan PushData)

	go func() {
		timer := time.NewTimer(time.Second * 5)
		do := false

		var mu sync.Mutex
		work := make(map[string]PushData)

		for {
			select {
			case data := <-pushChan:
				mu.Lock()
				work[fmt.Sprintf("%s%s", data.provider, data.file)] = data
				mu.Unlock()
				timer.Reset(time.Second * 5)
				do = true
			case <-timer.C:
				if do {
					mu.Lock()
					for k, v := range work {
						_, err := v.w.Add(v.file)
						if err != nil {
							slog.Error(v.provider, "gitadd", err)
							continue
						}

						_, err = v.w.Commit("elephant", &git.CommitOptions{})
						if err != nil {
							slog.Error(v.provider, "commit", err)
							continue
						}

						err = v.r.Push(&git.PushOptions{})
						if err != nil {
							slog.Error(v.provider, "push", err)
							continue
						}

						delete(work, k)
						slog.Info(v.provider, "git", "pushed to repository")
					}
					mu.Unlock()

					do = false
				}
			}
		}
	}()
}

// TODO: this needs better commit messages somehow...
func GitPush(provider, file string, w *git.Worktree, r *git.Repository) {
	gitMu.Lock()
	defer gitMu.Unlock()

	pushChan <- PushData{
		provider: provider,
		file:     file,
		w:        w,
		r:        r,
	}
}
