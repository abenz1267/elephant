package main

import (
	_ "embed"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name       = "colors"
	NamePretty = "Color Converter"
	config     *Config
)

//go:embed README.md
var readme string

const (
	ActionCopy = "copy"
)

type Config struct {
	common.Config `koanf:",squash"`
	MinChars      int    `koanf:"min_chars" desc:"don't convert if query is shorter than min_chars" default:"2"`
	Command       string `koanf:"command" desc:"default command to be executed. supports %VALUE%." default:"wl-copy -n '%VALUE%'"`
}

type RGBA struct {
	R, G, B, A uint8
}

func Setup() {
	LoadConfig()

	if config.NamePretty != "" {
		NamePretty = config.NamePretty
	}
}

func LoadConfig() {
	config = &Config{
		Config: common.Config{
			Icon: "color-picker",
		},
		MinChars: 2,
		Command:  "wl-copy -n '%VALUE%'",
	}

	common.LoadConfig(Name, config)
}

func Available() bool {
	return true
}

func PrintDoc(write bool) {
	if !write {
		fmt.Println(readme)
		fmt.Println()
	}

	util.PrintConfig(config, Name, write)
}

func Icon() string {
	return config.Icon
}

func HideFromProviderlist() bool {
	return config.HideFromProviderlist
}

func State(provider string) *pb.ProviderStateResponse {
	return &pb.ProviderStateResponse{}
}

// ─── Color Parsing ───────────────────────────────────────────────────────────

func parseHex(s string) (RGBA, bool) {
	s = strings.TrimPrefix(s, "#")

	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}

	if len(s) != 6 && len(s) != 8 {
		return RGBA{}, false
	}

	v, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return RGBA{}, false
	}

	if len(s) == 6 {
		return RGBA{
			R: uint8(v >> 16),
			G: uint8(v >> 8),
			B: uint8(v),
			A: 255,
		}, true
	}

	return RGBA{
		R: uint8(v >> 24),
		G: uint8(v >> 16),
		B: uint8(v >> 8),
		A: uint8(v),
	}, true
}

func parseRGB(s string) (RGBA, bool) {
	// Strip rgb() / rgba() wrapper
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "rgba(")
	s = strings.TrimPrefix(s, "rgb(")
	s = strings.TrimSuffix(s, ")")

	parts := splitFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '/'
	})

	if len(parts) < 3 {
		return RGBA{}, false
	}

	r, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || r < 0 || r > 255 {
		return RGBA{}, false
	}

	g, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || g < 0 || g > 255 {
		return RGBA{}, false
	}

	b, err := strconv.Atoi(strings.TrimSpace(parts[2]))
	if err != nil || b < 0 || b > 255 {
		return RGBA{}, false
	}

	a := 255
	if len(parts) > 3 {
		av, err := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
		if err == nil && av >= 0 && av <= 1 {
			a = int(av * 255)
		}
	}

	return RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: uint8(a)}, true
}

type HSL struct {
	H, S, L float64
}

func parseHSL(s string) (HSL, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "hsla(")
	s = strings.TrimPrefix(s, "hsl(")
	s = strings.TrimSuffix(s, ")")

	parts := splitFunc(s, func(r rune) bool {
		return r == ',' || r == ' '
	})

	if len(parts) < 3 {
		return HSL{}, false
	}

	hStr := strings.TrimSpace(parts[0])
	sStr := strings.TrimSpace(parts[1])
	lStr := strings.TrimSpace(parts[2])

	h, err := strconv.ParseFloat(hStr, 64)
	if err != nil || h < 0 || h > 360 {
		return HSL{}, false
	}

	sVal := strings.TrimSuffix(sStr, "%")
	sF, err := strconv.ParseFloat(sVal, 64)
	if err != nil || sF < 0 || sF > 100 {
		return HSL{}, false
	}

	lVal := strings.TrimSuffix(lStr, "%")
	lF, err := strconv.ParseFloat(lVal, 64)
	if err != nil || lF < 0 || lF > 100 {
		return HSL{}, false
	}

	return HSL{H: h, S: sF / 100, L: lF / 100}, true
}

func parseNamedColor(s string) (RGBA, bool) {
	c, ok := namedColors[strings.ToLower(strings.TrimSpace(s))]
	if !ok {
		return RGBA{}, false
	}
	return c, true
}

// ─── Color Conversion ────────────────────────────────────────────────────────

func hslToRGB(hsl HSL) RGBA {
	h := hsl.H / 360
	s := hsl.S
	l := hsl.L

	var r, g, b float64

	if s == 0 {
		return RGBA{
			R: uint8(l * 255),
			G: uint8(l * 255),
			B: uint8(l * 255),
			A: 255,
		}
	}

	hue2rgb := func(p, q, t float64) float64 {
		if t < 0 {
			t += 1
		}
		if t > 1 {
			t -= 1
		}
		if t < 1.0/6 {
			return p + (q-p)*6*t
		}
		if t < 1.0/2 {
			return q
		}
		if t < 2.0/3 {
			return p + (q-p)*(2.0/3-t)*6
		}
		return p
	}

	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q

	r = hue2rgb(p, q, h+1.0/3)
	g = hue2rgb(p, q, h)
	b = hue2rgb(p, q, h-1.0/3)

	return RGBA{
		R: uint8(r * 255 + 0.5),
		G: uint8(g * 255 + 0.5),
		B: uint8(b * 255 + 0.5),
		A: 255,
	}
}

func rgbToHSL(c RGBA) HSL {
	r := float64(c.R) / 255
	g := float64(c.G) / 255
	b := float64(c.B) / 255

	mx := max(r, g, b)
	mn := min(r, g, b)
	df := mx - mn

	var h float64
	if df == 0 {
		h = 0
	} else if mx == r {
		h = (g - b) / df
		if h < 0 {
			h += 6
		}
	} else if mx == g {
		h = (b-r)/df + 2
	} else {
		h = (r-g)/df + 4
	}
	h = h * 60

	l := (mx + mn) / 2

	var s float64
	if df != 0 {
		s = df / (1 - abs(2*l-1))
	}

	return HSL{H: h, S: s, L: l}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// ─── Formatting ──────────────────────────────────────────────────────────────

func (c RGBA) hex() string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

func (c RGBA) hexWithAlpha() string {
	return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A)
}

func (c RGBA) rgb() string {
	return fmt.Sprintf("rgb(%d, %d, %d)", c.R, c.G, c.B)
}

func (c RGBA) rgba() string {
	return fmt.Sprintf("rgba(%d, %d, %d, %.2f)", c.R, c.G, c.B, float64(c.A)/255)
}

func (hsl HSL) hsl() string {
	return fmt.Sprintf("hsl(%.0f, %.0f%%, %.0f%%)", hsl.H, hsl.S*100, hsl.L*100)
}

// ─── Parsing entry point ─────────────────────────────────────────────────────

func detectInput(s string) string {
	s = strings.TrimSpace(s)

	if strings.HasPrefix(s, "#") || isHexBare(s) {
		return "hex"
	}

	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "rgb") {
		return "rgb"
	}
	if strings.HasPrefix(lower, "hsl") {
		return "hsl"
	}

	if _, ok := namedColors[lower]; ok {
		return "named"
	}

	// Try bare RGB: "255,0,0" or "255 0 0"
	if strings.Count(s, ",") == 2 || strings.Count(s, " ") >= 2 {
		if _, ok := parseRGB(s); ok {
			return "rgb"
		}
	}

	return ""
}

func isHexBare(s string) bool {
	if len(s) != 6 && len(s) != 8 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// ─── Provider interface ──────────────────────────────────────────────────────

func Query(conn net.Conn, query string, single bool, _ bool, format uint8) []*pb.QueryResponse_Item {
	start := time.Now()

	entries := []*pb.QueryResponse_Item{}

	if query == "" || len(query) < config.MinChars {
		return entries
	}

	inputType := detectInput(query)
	if inputType == "" {
		return entries
	}

	var c RGBA
	var ok bool
	var hsl HSL

	switch inputType {
	case "hex":
		c, ok = parseHex(query)
	case "rgb":
		c, ok = parseRGB(query)
	case "hsl":
		hsl, ok = parseHSL(query)
		if ok {
			c = hslToRGB(hsl)
		}
	case "named":
		c, ok = parseNamedColor(query)
	}

	if !ok {
		return entries
	}

	if inputType != "hsl" {
		hsl = rgbToHSL(c)
	}

	// Build entries for each format
	type conversion struct {
		label string
		value string
	}

	formats := []conversion{
		{"HEX", c.hex()},
		{"HEX (alpha)", c.hexWithAlpha()},
		{"RGB", c.rgb()},
		{"RGBA", c.rgba()},
		{"HSL", hsl.hsl()},
	}

	// Find named color if close
	if name := findNamedColor(c); name != "" {
		formats = append(formats, conversion{"Named", name})
	}

	id := c.hex()

	for _, f := range formats {
		entries = append(entries, &pb.QueryResponse_Item{
			Identifier: id + ":" + f.label,
			Text:       f.value,
			Subtext:    f.label,
			Icon:       config.Icon,
			Provider:   Name,
			Actions:    []string{ActionCopy},
			State:      []string{"current"},
			Score:      int32(1000 - len(entries)),
			Type:       pb.QueryResponse_REGULAR,
		})
	}

	slog.Debug(Name, "query", time.Since(start))

	return entries
}

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	switch action {
	case ActionCopy:
		parts := strings.SplitN(identifier, ":", 2)
		if len(parts) != 2 {
			return
		}

		// Reconstruct the color value from the identifier
		c, ok := parseHex(parts[0])
		if !ok {
			return
		}

		hsl := rgbToHSL(c)
		name := findNamedColor(c)

		formatMap := map[string]string{
			"HEX":     c.hex(),
			"HEX (alpha)": c.hexWithAlpha(),
			"RGB":     c.rgb(),
			"RGBA":    c.rgba(),
			"HSL":     hsl.hsl(),
			"Named":   name,
		}

		val, ok := formatMap[parts[1]]
		if !ok {
			return
		}

		cmd := common.ReplaceResultOrStdinCmd(config.Command, val)
		err := cmd.Start()
		if err != nil {
			slog.Error(Name, "copy", err)
		} else {
			go func() {
				cmd.Wait()
			}()
		}
	default:
		slog.Error(Name, "activate", fmt.Sprintf("unknown action: %s", action))
	}
}

// ─── Named Colors ────────────────────────────────────────────────────────────

var namedColors = map[string]RGBA{
	"aliceblue":            {R: 240, G: 248, B: 255, A: 255},
	"antiquewhite":         {R: 250, G: 235, B: 215, A: 255},
	"aqua":                 {R: 0, G: 255, B: 255, A: 255},
	"aquamarine":           {R: 127, G: 255, B: 212, A: 255},
	"azure":                {R: 240, G: 255, B: 255, A: 255},
	"beige":                {R: 245, G: 245, B: 220, A: 255},
	"bisque":               {R: 255, G: 228, B: 196, A: 255},
	"black":                {R: 0, G: 0, B: 0, A: 255},
	"blanchedalmond":       {R: 255, G: 235, B: 205, A: 255},
	"blue":                 {R: 0, G: 0, B: 255, A: 255},
	"blueviolet":           {R: 138, G: 43, B: 226, A: 255},
	"brown":                {R: 165, G: 42, B: 42, A: 255},
	"burlywood":            {R: 222, G: 184, B: 135, A: 255},
	"cadetblue":            {R: 95, G: 158, B: 160, A: 255},
	"chartreuse":           {R: 127, G: 255, B: 0, A: 255},
	"chocolate":            {R: 210, G: 105, B: 30, A: 255},
	"coral":                {R: 255, G: 127, B: 80, A: 255},
	"cornflowerblue":       {R: 100, G: 149, B: 237, A: 255},
	"cornsilk":             {R: 255, G: 248, B: 220, A: 255},
	"crimson":              {R: 220, G: 20, B: 60, A: 255},
	"cyan":                 {R: 0, G: 255, B: 255, A: 255},
	"darkblue":             {R: 0, G: 0, B: 139, A: 255},
	"darkcyan":             {R: 0, G: 139, B: 139, A: 255},
	"darkgoldenrod":        {R: 184, G: 134, B: 11, A: 255},
	"darkgray":             {R: 169, G: 169, B: 169, A: 255},
	"darkgreen":            {R: 0, G: 100, B: 0, A: 255},
	"darkkhaki":            {R: 189, G: 183, B: 107, A: 255},
	"darkmagenta":          {R: 139, G: 0, B: 139, A: 255},
	"darkolivegreen":       {R: 85, G: 107, B: 47, A: 255},
	"darkorange":           {R: 255, G: 140, B: 0, A: 255},
	"darkorchid":           {R: 153, G: 50, B: 204, A: 255},
	"darkred":              {R: 139, G: 0, B: 0, A: 255},
	"darksalmon":           {R: 233, G: 150, B: 122, A: 255},
	"darkseagreen":         {R: 143, G: 188, B: 143, A: 255},
	"darkslateblue":        {R: 72, G: 61, B: 139, A: 255},
	"darkslategray":        {R: 47, G: 79, B: 79, A: 255},
	"darkturquoise":        {R: 0, G: 206, B: 209, A: 255},
	"darkviolet":           {R: 148, G: 0, B: 211, A: 255},
	"deeppink":             {R: 255, G: 20, B: 147, A: 255},
	"deepskyblue":          {R: 0, G: 191, B: 255, A: 255},
	"dimgray":              {R: 105, G: 105, B: 105, A: 255},
	"dodgerblue":           {R: 30, G: 144, B: 255, A: 255},
	"firebrick":            {R: 178, G: 34, B: 34, A: 255},
	"floralwhite":          {R: 255, G: 250, B: 240, A: 255},
	"forestgreen":          {R: 34, G: 139, B: 34, A: 255},
	"fuchsia":              {R: 255, G: 0, B: 255, A: 255},
	"gainsboro":            {R: 220, G: 220, B: 220, A: 255},
	"ghostwhite":           {R: 248, G: 248, B: 255, A: 255},
	"gold":                 {R: 255, G: 215, B: 0, A: 255},
	"goldenrod":            {R: 218, G: 165, B: 32, A: 255},
	"gray":                 {R: 128, G: 128, B: 128, A: 255},
	"green":                {R: 0, G: 128, B: 0, A: 255},
	"greenyellow":          {R: 173, G: 255, B: 47, A: 255},
	"honeydew":             {R: 240, G: 255, B: 240, A: 255},
	"hotpink":              {R: 255, G: 105, B: 180, A: 255},
	"indianred":            {R: 205, G: 92, B: 92, A: 255},
	"indigo":               {R: 75, G: 0, B: 130, A: 255},
	"ivory":                {R: 255, G: 255, B: 240, A: 255},
	"khaki":                {R: 240, G: 230, B: 140, A: 255},
	"lavender":             {R: 230, G: 230, B: 250, A: 255},
	"lavenderblush":        {R: 255, G: 240, B: 245, A: 255},
	"lawngreen":            {R: 124, G: 252, B: 0, A: 255},
	"lemonchiffon":         {R: 255, G: 250, B: 205, A: 255},
	"lightblue":            {R: 173, G: 216, B: 230, A: 255},
	"lightcoral":           {R: 240, G: 128, B: 128, A: 255},
	"lightcyan":            {R: 224, G: 255, B: 255, A: 255},
	"lightgoldenrodyellow": {R: 250, G: 250, B: 210, A: 255},
	"lightgreen":           {R: 144, G: 238, B: 144, A: 255},
	"lightgrey":            {R: 211, G: 211, B: 211, A: 255},
	"lightpink":            {R: 255, G: 182, B: 193, A: 255},
	"lightsalmon":          {R: 255, G: 160, B: 122, A: 255},
	"lightseagreen":        {R: 32, G: 178, B: 170, A: 255},
	"lightskyblue":         {R: 135, G: 206, B: 250, A: 255},
	"lightslategray":       {R: 119, G: 136, B: 153, A: 255},
	"lightsteelblue":       {R: 176, G: 196, B: 222, A: 255},
	"lightyellow":          {R: 255, G: 255, B: 224, A: 255},
	"lime":                 {R: 0, G: 255, B: 0, A: 255},
	"limegreen":            {R: 50, G: 205, B: 50, A: 255},
	"linen":                {R: 250, G: 240, B: 230, A: 255},
	"magenta":              {R: 255, G: 0, B: 255, A: 255},
	"maroon":               {R: 128, G: 0, B: 0, A: 255},
	"mediumaquamarine":     {R: 102, G: 205, B: 170, A: 255},
	"mediumblue":           {R: 0, G: 0, B: 205, A: 255},
	"mediumorchid":         {R: 186, G: 85, B: 211, A: 255},
	"mediumpurple":         {R: 147, G: 112, B: 219, A: 255},
	"mediumseagreen":       {R: 60, G: 179, B: 113, A: 255},
	"mediumslateblue":      {R: 123, G: 104, B: 238, A: 255},
	"mediumspringgreen":    {R: 0, G: 250, B: 154, A: 255},
	"mediumturquoise":      {R: 72, G: 209, B: 204, A: 255},
	"mediumvioletred":      {R: 199, G: 21, B: 133, A: 255},
	"midnightblue":         {R: 25, G: 25, B: 112, A: 255},
	"mintcream":            {R: 245, G: 255, B: 250, A: 255},
	"mistyrose":            {R: 255, G: 228, B: 225, A: 255},
	"moccasin":             {R: 255, G: 228, B: 181, A: 255},
	"navajowhite":          {R: 255, G: 222, B: 173, A: 255},
	"navy":                 {R: 0, G: 0, B: 128, A: 255},
	"oldlace":              {R: 253, G: 245, B: 230, A: 255},
	"olive":                {R: 128, G: 128, B: 0, A: 255},
	"olivedrab":            {R: 107, G: 142, B: 35, A: 255},
	"orange":               {R: 255, G: 165, B: 0, A: 255},
	"orangered":            {R: 255, G: 69, B: 0, A: 255},
	"orchid":               {R: 218, G: 112, B: 214, A: 255},
	"palegoldenrod":        {R: 238, G: 232, B: 170, A: 255},
	"palegreen":            {R: 152, G: 251, B: 152, A: 255},
	"paleturquoise":        {R: 175, G: 238, B: 238, A: 255},
	"palevioletred":        {R: 219, G: 112, B: 147, A: 255},
	"papayawhip":           {R: 255, G: 239, B: 213, A: 255},
	"peachpuff":            {R: 255, G: 218, B: 185, A: 255},
	"peru":                 {R: 205, G: 133, B: 63, A: 255},
	"pink":                 {R: 255, G: 192, B: 203, A: 255},
	"plum":                 {R: 221, G: 160, B: 221, A: 255},
	"powderblue":           {R: 176, G: 224, B: 230, A: 255},
	"purple":               {R: 128, G: 0, B: 128, A: 255},
	"rebeccapurple":        {R: 102, G: 51, B: 153, A: 255},
	"red":                  {R: 255, G: 0, B: 0, A: 255},
	"rosybrown":            {R: 188, G: 143, B: 143, A: 255},
	"royalblue":            {R: 65, G: 105, B: 225, A: 255},
	"saddlebrown":          {R: 139, G: 69, B: 19, A: 255},
	"salmon":               {R: 250, G: 128, B: 114, A: 255},
	"sandybrown":           {R: 244, G: 164, B: 96, A: 255},
	"seagreen":             {R: 46, G: 139, B: 87, A: 255},
	"seashell":             {R: 255, G: 245, B: 238, A: 255},
	"sienna":               {R: 160, G: 82, B: 45, A: 255},
	"silver":               {R: 192, G: 192, B: 192, A: 255},
	"skyblue":              {R: 135, G: 206, B: 235, A: 255},
	"slateblue":            {R: 106, G: 90, B: 205, A: 255},
	"slategray":            {R: 112, G: 128, B: 144, A: 255},
	"snow":                 {R: 255, G: 250, B: 250, A: 255},
	"springgreen":          {R: 0, G: 255, B: 127, A: 255},
	"steelblue":            {R: 70, G: 130, B: 180, A: 255},
	"tan":                  {R: 210, G: 180, B: 140, A: 255},
	"teal":                 {R: 0, G: 128, B: 128, A: 255},
	"thistle":              {R: 216, G: 191, B: 216, A: 255},
	"tomato":               {R: 255, G: 99, B: 71, A: 255},
	"turquoise":            {R: 64, G: 224, B: 208, A: 255},
	"violet":               {R: 238, G: 130, B: 238, A: 255},
	"wheat":                {R: 245, G: 222, B: 179, A: 255},
	"white":                {R: 255, G: 255, B: 255, A: 255},
	"whitesmoke":           {R: 245, G: 245, B: 245, A: 255},
	"yellow":               {R: 255, G: 255, B: 0, A: 255},
	"yellowgreen":          {R: 154, G: 205, B: 50, A: 255},
}

func findNamedColor(c RGBA) string {
	closest := ""
	bestDist := int(^uint(0) >> 1) // max int

	for name, nc := range namedColors {
		// Simple Euclidean distance in RGB space
		dr := int(c.R) - int(nc.R)
		dg := int(c.G) - int(nc.G)
		db := int(c.B) - int(nc.B)
		dist := dr*dr + dg*dg + db*db

		if dist == 0 {
			return name
		}

		if dist < bestDist {
			bestDist = dist
			closest = name
		}
	}

	// Only return if very close (threshold: ~5 per channel squared)
	if bestDist < 75 {
		return closest
	}

	return ""
}

// ─── Utility ─────────────────────────────────────────────────────────────────

func splitFunc(s string, fn func(rune) bool) []string {
	var parts []string
	var cur strings.Builder

	for _, r := range s {
		if fn(r) {
			if cur.Len() > 0 {
				parts = append(parts, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteRune(r)
	}

	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}

	return parts
}
