package common

import (
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/abenz1267/elephant/pkg/common"
	"github.com/go-git/go-git/v6"
)

var gitMu sync.Mutex

func SetupGit(provider, url string) (string, *git.Worktree, *git.Repository) {
	gitMu.Lock()
	defer gitMu.Unlock()

	x := 0
	base := filepath.Base(url)
	folder := common.CacheFile(base)
	var w *git.Worktree
	var r *git.Repository

	for x < 15 {
		x++

		time.Sleep(1 * time.Second)

		slog.Info(provider, "gitsetup", "trying to setup git...")

		// clone
		if !common.FileExists(folder) {
			var err error

			url := url
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
		}

		var err error

		w, err = r.Worktree()
		if err != nil {
			slog.Debug(provider, "gitpull", err)
			continue
		}

		err = w.Pull(&git.PullOptions{RemoteName: "origin"})
		if err != nil {
			slog.Info(provider, "gitpull", err)

			if err.Error() != "already up-to-date" {
				continue
			}
		}

		break
	}

	return folder, w, r
}

// TODO: this needs better commit messages somehow...
func GitPush(provider, file string, w *git.Worktree, r *git.Repository) {
	gitMu.Lock()
	defer gitMu.Unlock()

	_, err := w.Add(file)
	if err != nil {
		slog.Error(provider, "gitadd", err)
		return
	}

	_, err = w.Commit("elephant", &git.CommitOptions{})
	if err != nil {
		slog.Error(provider, "commit", err)
		return
	}

	err = r.Push(&git.PushOptions{})
	if err != nil {
		slog.Error(provider, "push", err)
		return
	}
}
