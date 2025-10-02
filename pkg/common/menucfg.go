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
	Name             string   `toml:"name" desc:"name of the menu"`
	NamePretty       string   `toml:"name_pretty" desc:"prettier name you usually want to display to the user."`
	Description      string   `toml:"description" desc:"used as a subtext"`
	Icon             string   `toml:"icon" desc:"default icon"`
	Action           string   `toml:"action" desc:"default action"`
	GlobalSearch     bool     `toml:"global_search" desc:"sets if entries in this menu should be searchable globally without being in the menu"`
	Entries          []Entry  `toml:"entries" desc:"menu items"`
	Terminal         bool     `toml:"terminal" desc:"execute action in terminal or not"`
	Keywords         []string `toml:"keywords" desc:"searchable keywords"`
	FixedOrder       bool     `toml:"fixed_order" desc:"don't sort entries alphabetically"`
	History          bool     `toml:"history" desc:"make use of history for sorting"`
	HistoryWhenEmpty bool     `toml:"history_when_empty" desc:"consider history when query is empty"`
	MinScore         int32    `koanf:"min_score" desc:"minimum score for items to be displayed" default:"depends on provider"`
}

type Entry struct {
	Text     string   `toml:"text" desc:"text for entry"`
	Async    string   `toml:"async" desc:"if the text should be updated asynchronously based on the action"`
	Subtext  string   `toml:"subtext" desc:"sub text for entry"`
	Value    string   `toml:"value" desc:"value to be used for the action, defaults to the text if empty"`
	Action   string   `toml:"action" desc:"action to run"`
	Terminal bool     `toml:"terminal" desc:"runs action in terminal if true"`
	Icon     string   `toml:"icon" desc:"icon for entry"`
	SubMenu  string   `toml:"submenu" desc:"submenu to open, if has prefix 'dmenu:' it'll launch that dmenu"`
	Preview  string   `toml:"preview" desc:"filepath for the preview"`
	Keywords []string `toml:"keywords" desc:"searchable keywords"`

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

				if v.Value == "" && v.Async == "" {
					m.Entries[k].Value = v.Text
				}

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
