package common

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

var explicitDir string

func SetExplicitDir(dir string) {
	explicitDir = dir
	slog.Info("common", "configdir", dir)
}

func TmpDir() string {
	return filepath.Join(os.TempDir())
}

func ConfigDirs() []string {
	res := []string{}

	dir, err := os.UserConfigDir()
	if err != nil {
		slog.Error("common", "files", err)
		os.Exit(1)
	}

	usrCfgDir := filepath.Join(dir, "elephant")

	if FileExists(usrCfgDir) {
		res = append(res, usrCfgDir)
	}

	for _, v := range xdg.ConfigDirs {
		path := filepath.Join(v, "elephant")
		if FileExists(path) {
			res = append(res, path)
		}
	}

	return res
}

func CacheFile(file string) string {
	d, _ := os.UserCacheDir()

	return filepath.Join(d, "elephant", file)
}

var ErrConfigNotExists = errors.New("provider config doesn't exist")

func ProviderConfig(provider string) (string, error) {
	provider = fmt.Sprintf("%s.toml", provider)

	for _, v := range ConfigDirs() {
		file := filepath.Join(v, provider)

		if FileExists(file) {
			return file, nil
		}
	}

	return "", ErrConfigNotExists
}

func FileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}
