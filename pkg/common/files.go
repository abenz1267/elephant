package common

import (
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

func ConfigDir() string {
	if explicitDir != "" {
		return explicitDir
	}

	dir, err := os.UserConfigDir()
	if err != nil {
		slog.Error("common", "files", err)
		os.Exit(1)
	}

	usrCfgDir := filepath.Join(dir, "elephant")

	if FileExists(usrCfgDir) {
		return usrCfgDir
	}

	for _, v := range xdg.ConfigDirs {
		if FileExists(v) {
			return filepath.Join(v, "elephant")
		}
	}

	return ""
}

func CacheFile(file string) string {
	d, _ := os.UserCacheDir()

	return filepath.Join(d, "elephant", file)
}

func ProviderConfig(provider string) string {
	provider = fmt.Sprintf("%s.toml", provider)

	file := filepath.Join(ConfigDir(), provider)

	if FileExists(file) {
		return file
	}

	for _, v := range xdg.ConfigDirs {
		if FileExists(v) {
			return filepath.Join(v, "elephant", provider)
		}
	}

	return ""
}

func FileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}
