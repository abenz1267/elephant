package main

import (
	_ "embed"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abenz1267/elephant/v2/pkg/common"
)

var (
	Name             = "clipboard"
	NamePretty       = "Clipboard"
	file             = common.CacheFile("clipboard.gob")
	imgTypes         = make(map[string]string)
	config           *Config
	clipboardhistory = make(map[string]*Item)
	mu               sync.Mutex
	availableModes   = []string{}
	currentMode      = ActionCombined
	hasLocalsend     bool
)

//go:embed README.md
var readme string

//go:embed data/UnicodeData.txt
var unicodedata string

//go:embed data/symbols.xml
var symbolsdata string

var (
	paused       atomic.Bool
	saveFileChan = make(chan struct{})
)

const StateEditable = "editable"

type Item struct {
	Content string
	Img     string
	URIList []string
	Time    time.Time
	State   string
	Pinned  bool
}

type Config struct {
	common.Config   `koanf:",squash"`
	MaxItems        int    `koanf:"max_items" desc:"max amount of clipboard history items" default:"100"`
	OCR             bool   `koanf:"ocr" desc:"extract text from images via tesseract" default:"false"`
	ImageEditorCmd  string `koanf:"image_editor_cmd" desc:"editor to use for images. use '%FILE%' as placeholder for file path." default:""`
	TextEditorCmd   string `koanf:"text_editor_cmd" desc:"editor to use for text, otherwise default for mimetype. use '%FILE%' as placeholder for file path." default:""`
	Command         string `koanf:"command" desc:"default command to be executed" default:"wl-copy"`
	IgnoreSymbols   bool   `koanf:"ignore_symbols" desc:"ignores symbols/unicode" default:"true"`
	PinnedOnTop     bool   `koanf:"pinned_on_top" desc:"put pinned items on top" default:"false"`
	AutoCleanup     int    `koanf:"auto_cleanup" desc:"will automatically cleanup entries entries older than X minutes" default:"0"`
	AutoTypeSupport bool   `koanf:"autotype_support" desc:"enable autotype support" default:"false"`
	AutoTypeCommand string `koanf:"autotype_command" desc:"command to type text. supports %VALUE% placeholder." default:"wtype -- %VALUE%"`
	AutoTypeDelay   int    `koanf:"autotype_delay" desc:"delay in ms before typing starts. 0 to disable." default:"500"`
}

var symbols = make(map[string]struct{})

const (
	ActionPause      = "pause"
	ActionPin        = "pin"
	ActionUnpin      = "unpin"
	ActionLocalsend  = "localsend"
	ActionUnpause    = "unpause"
	ActionCopy       = "copy"
	ActionEdit       = "edit"
	ActionRemove     = "remove"
	ActionRemoveAll  = "remove_all"
	ActionAutoType    = "autotype"
	ActionImagesOnly = "show_images_only"
	ActionTextOnly   = "show_text_only"
	ActionPinnedOnly = "show_pinned_only"
	ActionCombined   = "show_combined"
)

var ignoreMimetypes = []string{"x-kde-passwordManagerHint"}
