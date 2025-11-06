// Package install provides the ability to install menus from elephant-community
package install

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/adrg/xdg"
)

var repo = filepath.Join(os.TempDir(), "elephant-community")

func Readme(menu string) {
	if menu == "" {
		fmt.Println("available:")
		fmt.Println("----------")

		List()
		return
	}

	dest := filepath.Join(xdg.DataHome, "elephant", "install")
	installed := filepath.Join(dest, menu, "README.md")

	if common.FileExists(installed) {
		b, err := os.ReadFile(installed)
		if err != nil {
			slog.Error("readme", "readfile", err)
			return
		}

		fmt.Println("Installed:")
		fmt.Println("----------")
		fmt.Println(string(b))
		return
	}

	cloneOrPull()

	available := filepath.Join(repo, menu, "README.md")

	if common.FileExists(available) {
		b, err := os.ReadFile(available)
		if err != nil {
			slog.Error("readme", "readfile", err)
			return
		}

		fmt.Println("Available:")
		fmt.Println("----------")
		fmt.Println(string(b))
		return
	}
}

func Remove(menus []string) {
	dest := filepath.Join(xdg.DataHome, "elephant", "install")

	if len(menus) == 0 {
		fmt.Println("installed:")
		fmt.Println("----------")

		filepath.WalkDir(dest, func(path string, d fs.DirEntry, err error) error {
			if strings.Contains(path, ".git") || path == dest {
				return nil
			}

			if d.IsDir() {
				fmt.Println(filepath.Base(path))
				return filepath.SkipDir
			}

			return nil
		})

		return
	}

	for _, v := range menus {
		path := filepath.Join(dest, v)

		if common.FileExists(path) {
			err := os.RemoveAll(path)
			if err != nil {
				slog.Error("remove", "delete", v)
			} else {
				slog.Info("remove", "delete", v)
			}
		}
	}
}

func List() {
	if err := cloneOrPull(); err != nil {
		slog.Error("list", "cloneOrPull", err)
		return
	}

	dest := filepath.Join(xdg.DataHome, "elephant", "install")

	filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if strings.Contains(path, ".git") || path == repo {
			return nil
		}

		if d.IsDir() {
			menuName := filepath.Base(path)
			installedPath := filepath.Join(dest, menuName)

			if common.FileExists(installedPath) {
				fmt.Printf("%s (installed)\n", menuName)
			} else {
				fmt.Println(menuName)
			}
			return filepath.SkipDir
		}

		return nil
	})
}

func Install(menus []string) {
	if len(menus) == 0 {
		fmt.Println("available:")
		fmt.Println("----------")

		List()
		return
	}

	if err := cloneOrPull(); err != nil {
		slog.Error("install", "cloneOrPull", err)
		return
	}

	dest := filepath.Join(xdg.DataHome, "elephant", "install")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		slog.Error("install", "mkdirs", err)
		return
	}

	for _, v := range menus {
		path := filepath.Join(repo, v)

		if common.FileExists(path) {
			cmd := exec.Command("cp", "-r", path, dest)
			if err := cmd.Run(); err != nil {
				slog.Error("install", "copy", err)
			} else {
				fmt.Printf("[%s] Done! Restart Elephant to see changes\n", v)
			}
		} else {
			slog.Error("install", "not found", v)
		}
	}
}

func cloneOrPull() error {
	if common.FileExists(repo) {
		if err := pull(repo); err != nil {
			slog.Error("install", "pull", "can't pull latest changes. re-cloning.")

			os.RemoveAll(repo)

			if err := clone(); err != nil {
				slog.Error("install", "clone", err)
				return errors.New("can't clone repository")
			}
		}
	} else {
		if err := clone(); err != nil {
			slog.Error("install", "clone", err)
			return errors.New("can't clone repository")
		}
	}

	return nil
}

func pull(path string) error {
	cmd := exec.Command("git", "pull")
	cmd.Dir = path
	return cmd.Run()
}

func clone() error {
	cmd := exec.Command("git", "clone", "https://github.com/abenz1267/elephant-community")
	cmd.Dir = os.TempDir()
	return cmd.Run()
}
