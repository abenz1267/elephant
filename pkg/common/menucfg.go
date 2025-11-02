package common

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/adrg/xdg"
	"github.com/charlievieth/fastwalk"
	"github.com/pelletier/go-toml/v2"

	lua "github.com/yuin/gopher-lua"
)

type MenuConfig struct {
	Config `koanf:",squash"`
	Paths  []string `koanf:"paths" desc:"additional paths to check for menu definitions." default:""`
}

type Menu struct {
	HideFromProviderlist bool              `toml:"hide_from_providerlist" desc:"hides a provider from the providerlist provider. provider provider." default:"false"`
	Name                 string            `toml:"name" desc:"name of the menu"`
	NamePretty           string            `toml:"name_pretty" desc:"prettier name you usually want to display to the user."`
	Description          string            `toml:"description" desc:"used as a subtext"`
	Icon                 string            `toml:"icon" desc:"default icon"`
	Action               string            `toml:"action" desc:"default menu action to use"`
	Actions              map[string]string `toml:"actions" desc:"global actions"`
	SearchName           bool              `toml:"search_name" desc:"wether to search for the menu name as well when searching globally" default:"false"`
	Cache                bool              `toml:"cache" desc:"will cache the results of the lua script on startup"`
	Entries              []Entry           `toml:"entries" desc:"menu items"`
	Terminal             bool              `toml:"terminal" desc:"execute action in terminal or not"`
	Keywords             []string          `toml:"keywords" desc:"searchable keywords"`
	FixedOrder           bool              `toml:"fixed_order" desc:"don't sort entries alphabetically"`
	History              bool              `toml:"history" desc:"make use of history for sorting"`
	HistoryWhenEmpty     bool              `toml:"history_when_empty" desc:"consider history when query is empty"`
	MinScore             int32             `toml:"min_score" desc:"minimum score for items to be displayed" default:"depends on provider"`
	Parent               string            `toml:"parent" desc:"defines the parent menu" default:""`
	SubMenu              string            `toml:"submenu" desc:"defines submenu to trigger on activation" default:""`

	// internal
	LuaString string
	IsLua     bool `toml:"-"`
}

func NewLuaState(name, data string) *lua.LState {
	l := lua.NewState()

	if err := l.DoString(data); err != nil {
		slog.Error(name, "newLuaState", err)
		l.Close()
		return nil
	}

	if l == nil {
		slog.Error(name, "newLuaState", "lua state is nil")
		return nil
	}

	return l
}

var (
	LastMenuValue    = make(map[string]string)
	LastMenuValueMut sync.Mutex
)

func GetLastMenuValue(L *lua.LState) int {
	str := L.CheckString(1)

	LastMenuValueMut.Lock()
	if result, ok := LastMenuValue[str]; ok {
		L.Push(lua.LString(result))
	} else {
		L.Push(lua.LString(""))
	}
	LastMenuValueMut.Unlock()

	return 1
}

func (m *Menu) CreateLuaEntries() {
	state := NewLuaState(m.Name, m.LuaString)

	state.SetGlobal("lastMenuValue", state.NewFunction(GetLastMenuValue))

	if state == nil {
		slog.Error(m.Name, "CreateLuaEntries", "no lua state")
		return
	}

	if err := state.CallByParam(lua.P{
		Fn:      state.GetGlobal("GetEntries"),
		NRet:    1,
		Protect: true,
	}); err != nil {
		slog.Error(m.Name, "GetLuaEntries", err)
		return
	}

	res := []Entry{}

	ret := state.Get(-1)
	state.Pop(1)

	if table, ok := ret.(*lua.LTable); ok {
		table.ForEach(func(key, value lua.LValue) {
			if item, ok := value.(*lua.LTable); ok {
				entry := Entry{}

				if text := item.RawGetString("Text"); text != lua.LNil {
					entry.Text = string(text.(lua.LString))
				}

				if preview := item.RawGetString("Preview"); preview != lua.LNil {
					entry.Preview = string(preview.(lua.LString))
				}

				if preview := item.RawGetString("PreviewType"); preview != lua.LNil {
					entry.PreviewType = string(preview.(lua.LString))
				}

				if subtext := item.RawGetString("Subtext"); subtext != lua.LNil {
					entry.Subtext = string(subtext.(lua.LString))
				}

				if submenu := item.RawGetString("SubMenu"); submenu != lua.LNil {
					entry.SubMenu = string(submenu.(lua.LString))
				}

				if val := item.RawGetString("Value"); val != lua.LNil {
					entry.Value = string(val.(lua.LString))
				}

				if icon := item.RawGetString("Icon"); icon != lua.LNil {
					entry.Icon = string(icon.(lua.LString))
				}

				if actions := item.RawGet(lua.LString("Actions")); actions != lua.LNil {
					if actionsTable, ok := actions.(*lua.LTable); ok {
						entry.Actions = make(map[string]string)
						actionsTable.ForEach(func(key, value lua.LValue) {
							if keyStr, keyOk := key.(lua.LString); keyOk {
								if valueStr, valueOk := value.(lua.LString); valueOk {
									entry.Actions[string(keyStr)] = string(valueStr)
								}
							}
						})
					}
				}

				if state := item.RawGet(lua.LString("State")); state != lua.LNil {
					if stateTable, ok := state.(*lua.LTable); ok {
						entry.State = make([]string, 0)
						stateTable.ForEach(func(key, value lua.LValue) {
							if str, ok := value.(lua.LString); ok {
								entry.State = append(entry.State, string(str))
							}
						})
					}
				}

				identifier := entry.CreateIdentifier()

				entry.Menu = m.Name

				if entry.SubMenu != "" {
					entry.Identifier = fmt.Sprintf("menus:%s:%s:%s", entry.SubMenu, entry.Menu, identifier)
				} else if m.SubMenu != "" {
					entry.Identifier = fmt.Sprintf("menus:%s:%s:%s", m.SubMenu, entry.Menu, identifier)
				} else {
					entry.Identifier = fmt.Sprintf("%s:%s", entry.Menu, identifier)
				}

				if entry.Preview != "" && entry.PreviewType == "" {
					entry.PreviewType = "file"
				}

				res = append(res, entry)
			}
		})
	}

	m.Entries = res
}

type Entry struct {
	Text        string            `toml:"text" desc:"text for entry"`
	Async       string            `toml:"async" desc:"if the text should be updated asynchronously based on the action"`
	Subtext     string            `toml:"subtext" desc:"sub text for entry"`
	Value       string            `toml:"value" desc:"value to be used for the action."`
	Actions     map[string]string `toml:"actions" desc:"actions items can use"`
	Terminal    bool              `toml:"terminal" desc:"runs action in terminal if true"`
	Icon        string            `toml:"icon" desc:"icon for entry"`
	SubMenu     string            `toml:"submenu" desc:"submenu to open, if has prefix 'dmenu:' it'll launch that dmenu"`
	Preview     string            `toml:"preview" desc:"filepath for the preview"`
	PreviewType string            `toml:"preview_type" desc:"type of the preview: text, file [default], command"`
	Keywords    []string          `toml:"keywords" desc:"searchable keywords"`
	State       []string          `toml:"state" desc:"state of an item, can be used to f.e. mark it as current"`

	Identifier string `toml:"-"`
	Menu       string `toml:"-"`
}

func (e Entry) CreateIdentifier() string {
	md5 := md5.Sum(fmt.Appendf([]byte(""), "%s%s%s%s", e.Menu, e.Text, e.Value, e.Subtext))
	return hex.EncodeToString(md5[:])
}

var (
	MenuConfigLoaded MenuConfig
	menuname         = "menus"
	Menus            = make(map[string]*Menu)
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

	installed := filepath.Join(xdg.DataHome, "elephant", "install")
	MenuConfigLoaded.Paths = append(MenuConfigLoaded.Paths, installed)

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

			switch filepath.Ext(path) {
			case ".toml":
				createTomlMenu(path)
			case ".lua":
				createLuaMenu(path)
			}

			return nil
		}); err != nil {
			slog.Error(menuname, "walk", err)
			os.Exit(1)
		}
	}
}

func createLuaMenu(path string) {
	m := Menu{}
	m.IsLua = true

	b, err := os.ReadFile(path)
	if err != nil {
		slog.Error(m.Name, "lua read", err)
		return
	}

	m.LuaString = string(b)

	state := NewLuaState("", string(b))

	if val := state.GetGlobal("Name"); val != lua.LNil {
		m.Name = string(val.(lua.LString))
	}

	if val := state.GetGlobal("NamePretty"); val != lua.LNil {
		m.NamePretty = string(val.(lua.LString))
	}

	if val := state.GetGlobal("HideFromProviderlist"); val != lua.LNil {
		m.HideFromProviderlist = bool(val.(lua.LBool))
	}

	if val := state.GetGlobal("Description"); val != lua.LNil {
		m.Description = string(val.(lua.LString))
	}

	if val := state.GetGlobal("Icon"); val != lua.LNil {
		m.Icon = string(val.(lua.LString))
	}

	if val := state.GetGlobal("Action"); val != lua.LNil {
		m.Action = string(val.(lua.LString))
	}

	if val := state.GetGlobal("Actions"); val != lua.LNil {
		if table, ok := val.(*lua.LTable); ok {
			m.Actions = make(map[string]string)
			table.ForEach(func(key, value lua.LValue) {
				if keyStr, keyOk := key.(lua.LString); keyOk {
					if valueStr, valueOk := value.(lua.LString); valueOk {
						m.Actions[string(keyStr)] = string(valueStr)
					}
				}
			})
		}
	}

	if val := state.GetGlobal("SearchName"); val != lua.LNil {
		m.SearchName = bool(val.(lua.LBool))
	}

	if val := state.GetGlobal("Cache"); val != lua.LNil {
		m.Cache = bool(val.(lua.LBool))
	}

	if val := state.GetGlobal("Terminal"); val != lua.LNil {
		m.Terminal = bool(val.(lua.LBool))
	}

	if val := state.GetGlobal("Keywords"); val != lua.LNil {
		if table, ok := val.(*lua.LTable); ok {
			m.Keywords = make([]string, 0)
			table.ForEach(func(key, value lua.LValue) {
				if str, ok := value.(lua.LString); ok {
					m.Keywords = append(m.Keywords, string(str))
				}
			})
		}
	}

	if val := state.GetGlobal("FixedOrder"); val != lua.LNil {
		m.FixedOrder = bool(val.(lua.LBool))
	}

	if val := state.GetGlobal("History"); val != lua.LNil {
		m.History = bool(val.(lua.LBool))
	}

	if val := state.GetGlobal("HistoryWhenEmpty"); val != lua.LNil {
		m.HistoryWhenEmpty = bool(val.(lua.LBool))
	}

	if val := state.GetGlobal("MinScore"); val != lua.LNil {
		m.MinScore = int32(val.(lua.LNumber))
	}

	if val := state.GetGlobal("Parent"); val != lua.LNil {
		m.Parent = string(val.(lua.LString))
	}

	if val := state.GetGlobal("SubMenu"); val != lua.LNil {
		m.SubMenu = string(val.(lua.LString))
	}

	if m.Cache {
		m.CreateLuaEntries()
	}

	if m.Name == "" || m.NamePretty == "" {
		slog.Error("menus", "path", path, "error", "missing Name or NamePretty")
		return
	}

	Menus[m.Name] = &m
}

func createTomlMenu(path string) {
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
		identifier := m.Entries[k].CreateIdentifier()

		if v.SubMenu != "" {
			m.Entries[k].Identifier = fmt.Sprintf("menus:%s:%s:%s", v.SubMenu, v.Menu, identifier)
		} else if m.SubMenu != "" {
			m.Entries[k].Identifier = fmt.Sprintf("menus:%s:%s:%s", m.SubMenu, v.Menu, identifier)
		} else {
			m.Entries[k].Identifier = fmt.Sprintf("%s:%s", m.Name, identifier)
		}
	}

	Menus[m.Name] = &m
}
