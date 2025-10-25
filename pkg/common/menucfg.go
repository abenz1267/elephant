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

	lua "github.com/yuin/gopher-lua"
)

type MenuConfig struct {
	Config `koanf:",squash"`
	Paths  []string `koanf:"paths" desc:"additional paths to check for menu definitions." default:""`
}

type Menu struct {
	HideFromProviderlist bool     `toml:"hide_from_providerlist" desc:"hides a provider from the providerlist provider. provider provider." default:"false"`
	Name                 string   `toml:"name" desc:"name of the menu"`
	NamePretty           string   `toml:"name_pretty" desc:"prettier name you usually want to display to the user."`
	Description          string   `toml:"description" desc:"used as a subtext"`
	Icon                 string   `toml:"icon" desc:"default icon"`
	Action               string   `toml:"action" desc:"default menu action to use"`
	Lua                  string   `toml:"lua" desc:"path to Lua script"`
	Entries              []Entry  `toml:"entries" desc:"menu items"`
	Terminal             bool     `toml:"terminal" desc:"execute action in terminal or not"`
	Keywords             []string `toml:"keywords" desc:"searchable keywords"`
	FixedOrder           bool     `toml:"fixed_order" desc:"don't sort entries alphabetically"`
	History              bool     `toml:"history" desc:"make use of history for sorting"`
	HistoryWhenEmpty     bool     `toml:"history_when_empty" desc:"consider history when query is empty"`
	MinScore             int32    `toml:"min_score" desc:"minimum score for items to be displayed" default:"depends on provider"`
	Parent               string   `toml:"parent" desc:"defines the parent menu" default:""`
}

func (m *Menu) CreateLuaEntries() {
	res := []Entry{}

	l := lua.NewState()

outer:
	for _, v := range MenuConfigLoaded.Paths {
		s := filepath.Join(v, fmt.Sprintf("%s.lua", m.Lua))

		if FileExists(s) {
			if err := l.DoFile(s); err != nil {
				slog.Error(m.Name, "initLua", err)
				l.Close()
				return
			}

			break outer
		}
	}

	defer l.Close()

	if l == nil {
		slog.Error(m.Name, "GetLuaEntries", "lua state is nil")
		return
	}

	if err := l.CallByParam(lua.P{
		Fn:      l.GetGlobal("GetEntries"),
		NRet:    1,
		Protect: true,
	}); err != nil {
		slog.Error(m.Name, "GetLuaEntries", err)
		return
	}

	ret := l.Get(-1)
	l.Pop(1)

	var entries []LuaEntry
	if table, ok := ret.(*lua.LTable); ok {
		table.ForEach(func(key, value lua.LValue) {
			if item, ok := value.(*lua.LTable); ok {
				entry := LuaEntry{}

				if text := item.RawGetString("Text"); text != lua.LNil {
					entry.Text = string(text.(lua.LString))
				}

				if preview := item.RawGetString("Preview"); preview != lua.LNil {
					entry.Preview = string(preview.(lua.LString))
				}

				if subtext := item.RawGetString("Subtext"); subtext != lua.LNil {
					entry.Subtext = string(subtext.(lua.LString))
				}

				if val := item.RawGetString("Value"); val != lua.LNil {
					entry.Value = string(val.(lua.LString))
				}

				if icon := item.RawGetString("Icon"); icon != lua.LNil {
					entry.Icon = string(icon.(lua.LString))
				}

				entries = append(entries, entry)
			}
		})
	}

	for _, v := range entries {
		e := Entry{
			Text:       v.Text,
			Subtext:    v.Subtext,
			Value:      v.Value,
			Icon:       v.Icon,
			Preview:    v.Preview,
			Identifier: v.CreateIdentifier(),
			Menu:       m.Name,
		}

		res = append(res, e)
	}

	m.Entries = res
}

type Entry struct {
	Text     string            `toml:"text" desc:"text for entry"`
	Async    string            `toml:"async" desc:"if the text should be updated asynchronously based on the action"`
	Subtext  string            `toml:"subtext" desc:"sub text for entry"`
	Value    string            `toml:"value" desc:"value to be used for the action."`
	Actions  map[string]string `toml:"actions" desc:"actions items can use"`
	Terminal bool              `toml:"terminal" desc:"runs action in terminal if true"`
	Icon     string            `toml:"icon" desc:"icon for entry"`
	SubMenu  string            `toml:"submenu" desc:"submenu to open, if has prefix 'dmenu:' it'll launch that dmenu"`
	Preview  string            `toml:"preview" desc:"filepath for the preview"`
	Keywords []string          `toml:"keywords" desc:"searchable keywords"`

	Identifier string `toml:"-"`
	Menu       string `toml:"-"`
}

type LuaEntry struct {
	Text    string
	Preview string
	Subtext string
	Value   string
	Icon    string
	Menu    string
}

func (l LuaEntry) CreateIdentifier() string {
	md5 := md5.Sum(fmt.Appendf([]byte(""), "%s%s%s", l.Menu, l.Text, l.Value))
	return hex.EncodeToString(md5[:])
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

	for _, v := range ConfigDirs() {
		path := filepath.Join(v, "menus")
		MenuConfigLoaded.Paths = append(MenuConfigLoaded.Paths, path)
	}

	conf := fastwalk.Config{
		Follow: true,
	}

	for _, root := range MenuConfigLoaded.Paths {
		if _, err := os.Stat(root); err != nil {
			continue
		}

		if err := fastwalk.Walk(&conf, root, func(path string, d fs.DirEntry, err error) error {
			if filepath.Ext(path) != ".toml" {
				return nil
			}

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

			if m.Lua == "" {
				for k, v := range m.Entries {
					m.Entries[k].Menu = m.Name
					m.Entries[k].Identifier = m.Entries[k].CreateIdentifier()

					if v.SubMenu != "" {
						m.Entries[k].Identifier = fmt.Sprintf("menus:%s", v.SubMenu)
					}
				}
			} else {
				m.CreateLuaEntries()
			}

			Menus[m.Name] = m

			return nil
		}); err != nil {
			slog.Error(menuname, "walk", err)
			os.Exit(1)
		}
	}
}
