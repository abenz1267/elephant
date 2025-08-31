package common

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/charlievieth/fastwalk"
	"github.com/pelletier/go-toml/v2"
)

type MenuConfig struct {
	Config `koanf:",squash"`
	Paths  []string `koanf:"paths" desc:"additional paths to check for menu definitions." default:""`
}

type Menu struct {
	Name         string   `toml:"name"`
	NamePretty   string   `toml:"name_pretty"`
	Description  string   `toml:"description"`
	Icon         string   `toml:"icon"`
	Action       string   `toml:"action"`
	GlobalSearch bool     `toml:"global_search"`
	Entries      []Entry  `toml:"entries"`
	Terminal     bool     `toml:"terminal"`
	Keywords     []string `toml:"keywords"`
}

type Entry struct {
	Text     string   `toml:"text"`
	Async    string   `toml:"async"`
	Subtext  string   `toml:"subtext"`
	Value    string   `toml:"value"`
	Action   string   `toml:"action"`
	Terminal bool     `toml:"terminal"`
	Icon     string   `toml:"icon"`
	SubMenu  string   `toml:"submenu"`
	Preview  string   `toml:"preview"`
	Keywords []string `toml:"keywords"`

	Identifier string `toml:"-"`
	Menu       string `toml:"-"`
}

func (e Entry) CreateIdentifier() string {
	md5 := md5.Sum(fmt.Appendf([]byte(""), "%s%s%s", e.Menu, e.Text, e.Value))
	return hex.EncodeToString(md5[:])
}

var (
	MenuConfigLoaded MenuConfig
	menuname         = "menus"
	Menus            = make(map[string]Menu)
)

func LoadMenus() {
	MenuConfigLoaded = MenuConfig{
		Config: Config{
			MinScore: 10,
		},
		Paths: []string{},
	}

	LoadConfig(menuname, MenuConfigLoaded)

	path := filepath.Join(ConfigDir(), "menus")

	MenuConfigLoaded.Paths = append(MenuConfigLoaded.Paths, path)

	conf := fastwalk.Config{
		Follow: true,
	}

	for _, root := range MenuConfigLoaded.Paths {
		if _, err := os.Stat(root); err != nil {
			continue
		}

		if err := fastwalk.Walk(&conf, root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() {
				return nil
			}

			m := Menu{}

			b, err := os.ReadFile(path)
			if err != nil {
				slog.Error(menuname, "setup", err)
			}

			err = toml.Unmarshal(b, &m)
			if err != nil {
				slog.Error(menuname, "setup", err)
			}

			for k, v := range m.Entries {
				m.Entries[k].Menu = m.Name
				m.Entries[k].Identifier = v.CreateIdentifier()

				if v.SubMenu != "" {
					m.Entries[k].Identifier = fmt.Sprintf("keepopen:menus:%s", v.SubMenu)
				}
			}

			Menus[m.Name] = m

			return nil
		}); err != nil {
			slog.Error(menuname, "walk", err)
			os.Exit(1)
		}
	}
}
