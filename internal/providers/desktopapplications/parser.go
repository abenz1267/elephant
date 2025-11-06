package main

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"unicode"
)

type Data struct {
	NoDisplay      bool
	Hidden         bool
	Terminal       bool
	Action         string
	Exec           string
	Name           string
	Comment        string
	Path           string
	Parent         string
	GenericName    string
	StartupWMClass string
	Icon           string
	Categories     []string
	OnlyShowIn     []string
	NotShowIn      []string
	Keywords       []string
}

func parseFile(path, l, ll string) (*DesktopFile, error) {
	slog.Debug(Name, "parse", path)

	data, err := os.ReadFile(path)
	if err != nil {
		slog.Error(Name, "parseFile", err)
		os.Exit(1)
	}

	parts := splitIntoParsebles(data)

	f := &DesktopFile{}

	for i, v := range parts {
		data := parseData(v, l, ll)

		if i == 0 {
			f.Data = data

			if f.Icon == "" {
				f.Icon = config.IconPlaceholder
			}
		} else {
			f.Actions = append(f.Actions, data)
		}
	}

	for k, v := range f.Actions {
		if len(v.Categories) == 0 {
			f.Actions[k].Categories = f.Categories
		}

		if v.Comment == "" {
			f.Actions[k].Comment = f.Comment
		}

		if v.GenericName == "" {
			f.Actions[k].GenericName = f.GenericName
		}

		f.Actions[k].Parent = f.Name
		f.Actions[k].Hidden = f.Hidden
		f.Actions[k].NoDisplay = f.NoDisplay
		f.Actions[k].NotShowIn = f.NotShowIn
		f.Actions[k].NoDisplay = f.NoDisplay
		f.Actions[k].Path = f.Path
		f.Actions[k].Terminal = f.Terminal
		f.Actions[k].StartupWMClass = f.StartupWMClass

		if len(v.Keywords) == 0 {
			f.Actions[k].Keywords = f.Keywords
		}

		if v.Icon == "" {
			f.Actions[k].Icon = f.Icon
		}
	}

	shouldShow := (len(f.NotShowIn) == 0 || !slices.Contains(f.NotShowIn, desktop)) &&
		(len(f.OnlyShowIn) == 0 || slices.Contains(f.OnlyShowIn, desktop)) &&
		!f.Hidden && !f.NoDisplay

	if shouldShow && f.Name == "" {
		return f, fmt.Errorf("invalid desktop file: %s", path)
	}

	return f, nil
}

func parseData(in []byte, l, ll string) Data {
	res := Data{}

	for line := range bytes.Lines(in) {
		line = bytes.TrimSpace(line)

		if len(line) == 0 {
			continue
		}

		switch {

		case bytes.HasPrefix(line, []byte("Keywords=")) && res.Keywords == nil:
			res.Keywords = strings.Split(string(bytes.TrimPrefix(line, []byte("Keywords="))), ";")
		case bytes.HasPrefix(line, fmt.Appendf(nil, "Keywords[%s]=", l)):
			res.Keywords = strings.Split(string(bytes.TrimPrefix(line, fmt.Appendf(nil, "Keywords[%s]=", l))), ";")
		case bytes.HasPrefix(line, fmt.Appendf(nil, "Keywords[%s]=", ll)):
			res.Keywords = strings.Split(string(bytes.TrimPrefix(line, fmt.Appendf(nil, "Keywords[%s]=", ll))), ";")

		case bytes.HasPrefix(line, []byte("GenericName=")) && res.GenericName == "":
			res.GenericName = string(bytes.TrimPrefix(line, []byte("GenericName=")))
		case bytes.HasPrefix(line, fmt.Appendf(nil, "GenericName[%s]=", l)):
			res.GenericName = string(bytes.TrimPrefix(line, fmt.Appendf(nil, "GenericName[%s]=", l)))
		case bytes.HasPrefix(line, fmt.Appendf(nil, "GenericName[%s]=", ll)):
			res.GenericName = string(bytes.TrimPrefix(line, fmt.Appendf(nil, "GenericName[%s]=", ll)))

		case bytes.HasPrefix(line, []byte("Name=")) && res.Name == "":
			res.Name = string(bytes.TrimPrefix(line, []byte("Name=")))
		case bytes.HasPrefix(line, fmt.Appendf(nil, "Name[%s]=", l)):
			res.Name = string(bytes.TrimPrefix(line, fmt.Appendf(nil, "Name[%s]=", l)))
		case bytes.HasPrefix(line, fmt.Appendf(nil, "Name[%s]=", ll)):
			res.Name = string(bytes.TrimPrefix(line, fmt.Appendf(nil, "Name[%s]=", ll)))

		case bytes.HasPrefix(line, []byte("Comment=")) && res.Comment == "":
			res.Comment = string(bytes.TrimPrefix(line, []byte("Comment=")))
		case bytes.HasPrefix(line, fmt.Appendf(nil, "Comment[%s]=", l)):
			res.Comment = string(bytes.TrimPrefix(line, fmt.Appendf(nil, "Comment[%s]=", l)))
		case bytes.HasPrefix(line, fmt.Appendf(nil, "Comment[%s]=", ll)):
			res.Comment = string(bytes.TrimPrefix(line, fmt.Appendf(nil, "Comment[%s]=", ll)))

		case bytes.HasPrefix(line, []byte("NoDisplay=")):
			res.NoDisplay = strings.ToLower(string(bytes.TrimPrefix(line, []byte("NoDisplay=")))) == "true"
		case bytes.HasPrefix(line, []byte("Hidden=")):
			res.Hidden = strings.ToLower(string(bytes.TrimPrefix(line, []byte("Hidden=")))) == "true"
		case bytes.HasPrefix(line, []byte("Terminal=")):
			res.Terminal = strings.ToLower(string(bytes.TrimPrefix(line, []byte("Terminal=")))) == "true"
		case bytes.HasPrefix(line, []byte("Path=")):
			res.Path = string(bytes.TrimPrefix(line, []byte("Path=")))

		case bytes.HasPrefix(line, []byte("StartupWMClass=")):
			res.StartupWMClass = string(bytes.TrimPrefix(line, []byte("StartupWMClass=")))

		case bytes.HasPrefix(line, []byte("Icon=")):
			res.Icon = string(bytes.TrimPrefix(line, []byte("Icon=")))

		case bytes.HasPrefix(line, []byte("Categories=")):
			res.Categories = strings.Split(string(bytes.TrimPrefix(line, []byte("Categories="))), ";")

		case bytes.HasPrefix(line, []byte("OnlyShowIn=")):
			res.OnlyShowIn = strings.Split(string(bytes.TrimPrefix(line, []byte("OnlyShowIn="))), ";")

		case bytes.HasPrefix(line, []byte("NotShowIn=")):
			res.NotShowIn = strings.Split(string(bytes.TrimPrefix(line, []byte("NotShowIn="))), ";")

		case bytes.HasPrefix(line, []byte("Exec=")):
			exec, err := parseExec(string(bytes.TrimPrefix(line, []byte("Exec="))))
			if err != nil {
				slog.Error(Name, "parsing", err)
			}

			res.Exec = exec
		case bytes.Contains(line, []byte("[Desktop Action ")):
			res.Action = string(bytes.TrimPrefix(line, []byte("[Desktop Action ")))
			res.Action = strings.TrimSuffix(res.Action, "]")
		}

	}

	return res
}

var fieldCodes = []string{"%f", "%F", "%u", "%U", "%d", "%D", "%n", "%N", "%i", "%c", "%k", "%v", "%m"}

// parseExec converts an XDG desktop file Exec entry into a slice of strings
// suitable for exec.Command. It handles field codes and proper escaping according
// to the XDG Desktop Entry specification.
// See: https://specifications.freedesktop.org/desktop-entry-spec/latest/ar01s07.html
func parseExec(execLine string) (string, error) {
	if execLine == "" {
		return "", errors.New("empty exec line")
	}

	var (
		parts         []string
		current       strings.Builder
		inQuote       bool
		escaped       bool
		doubleEscaped bool
	)

	// Helper to append current token and reset builder
	appendCurrent := func() {
		if current.Len() > 0 {
			parts = append(parts, current.String())
			current.Reset()
		}
	}

	// Process each rune in the exec line
	for _, r := range execLine {
		switch {
		case doubleEscaped:
			// Handle double-escaped character
			current.WriteRune(r)
			doubleEscaped = false

		case escaped && r == '\\':
			// This is a double escape sequence
			current.WriteRune('\\')
			doubleEscaped = true
			escaped = false

		case escaped:
			// Handle escaped character
			if r == '"' {
				current.WriteRune('"')
			} else {
				current.WriteRune('\\')
				current.WriteRune(r)
			}
			escaped = false

		case r == '\\':
			escaped = true

		case r == '"':
			inQuote = !inQuote
			// Keep the quotes in the output for shell interpretation
			current.WriteRune('"')

		case unicode.IsSpace(r) && !inQuote:
			// Space outside quotes marks token boundary
			appendCurrent()

		default:
			current.WriteRune(r)
		}
	}

	// Append final token if any
	appendCurrent()

	// Remove field codes
	for k, v := range parts {
		if len(v) == 2 && slices.Contains(fieldCodes, v) {
			until := min(k+1, len(parts))

			parts = slices.Delete(parts, k, until)
		}
	}

	if len(parts) == 0 {
		return "", errors.New("no command found after parsing")
	}

	return strings.Join(parts, " "), nil
}

func splitIntoParsebles(in []byte) [][]byte {
	hasAction := bytes.Contains(in, []byte("Desktop Action"))

	if !hasAction {
		return [][]byte{in}
	}

	parts := [][]byte{}

	indexes := []int{}
	i := 0

	for i < len(in) {
		idx := bytes.Index(in[i:], []byte("[Desk"))
		if idx == -1 {
			break
		}
		indexes = append(indexes, i+idx)
		i += idx + len([]byte("[Desk"))
	}

	for k, v := range indexes {
		if k+1 < len(indexes) {
			parts = append(parts, in[v:indexes[k+1]])
		} else {
			parts = append(parts, in[v:])
		}
	}

	slices.SortFunc(parts, func(a, b []byte) int {
		if bytes.Contains(a, []byte("[Desktop Entry")) {
			return -1
		}

		if bytes.Contains(b, []byte("[Desktop Entry")) {
			return 1
		}

		return 0
	})

	return parts
}
