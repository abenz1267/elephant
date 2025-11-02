package common

import (
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/abenz1267/elephant/pkg/common"
	"github.com/go-git/go-git/v6"
)

func SetupGit(provider, url string) (string, *git.Worktree, *git.Repository) {
	base := filepath.Base(url)
	folder := common.CacheFile(base)

	var r *git.Repository

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
			slog.Error(provider, "gitclone", err)
			return "", nil, nil
		}
	} else {
		var err error
		r, err = git.PlainOpen(folder)
		if err != nil {
			slog.Error(provider, "gitclone", err)
			return "", nil, nil
		}
	}

	w, err := r.Worktree()
	if err != nil {
		slog.Error(provider, "gitpull", err)
		return "", nil, nil
	}

	err = w.Pull(&git.PullOptions{RemoteName: "origin"})
	if err != nil {
		slog.Info(provider, "gitpull", err)
	}

	return folder, w, r
}

// TODO: this needs better commit messages somehow...
func GitPush(provider, file string, w *git.Worktree, r *git.Repository) {
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
