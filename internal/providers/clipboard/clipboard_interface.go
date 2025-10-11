package main

import (
	"fmt"
	"os/exec"
)

// Clipboard interface che definisce il comportamento comune
type Clipboard interface {
	GetName() string
	GetCommand() string
	GetContent() ([]byte, []string, error)
	GetContentParsed() (itemID string, content string, mimetypes []string, err error) // Nuovo metodo aggiunto
	StartMonitoring(changed chan<- bool) error
	CopyToClipboard(id string, content string) error
	IsAvailable() bool
}

// Factory function
func CreateClipboard() (Clipboard, error) {
	// Try GPaste
	gpaste := NewGPaste()
	if gpaste.IsAvailable() {
		return gpaste, nil
	}

	// Fallback to Wayland
	wayland := NewWayland()
	if wayland.IsAvailable() {
		return wayland, nil
	}

	return nil, fmt.Errorf("nessuna clipboard disponibile")
}

// Funzione helper comune
func checkToolAvailable(tool string) bool {
	cmd := exec.Command("which", tool)
	err := cmd.Run()
	return err == nil
}
