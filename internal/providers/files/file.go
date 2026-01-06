package files

import "time"

//go:generate msgp
type File struct {
	Identifier string
	Path       string
	Changed    time.Time
}
