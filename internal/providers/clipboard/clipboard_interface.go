package main

import (
	"fmt"
	"os/exec"
)

// Clipboard interface che definisce il comportamento comune
type Clipboard interface {
	GetName() string
	GetContent() ([]byte, []string, error)
	StartMonitoring(changed chan<- bool) error
	CopyToClipboard(id string, content string) error
	IsAvailable() bool
}

// Factory function
func CreateClipboard() (Clipboard, error) {
	// Prova prima Wayland
	wayland := NewWayland()
	if wayland.IsAvailable() {
		return wayland, nil
	}

	// Fallback a GPaste
	gpaste := NewGPaste()
	if gpaste.IsAvailable() {
		return gpaste, nil
	}

	return nil, fmt.Errorf("nessuna clipboard disponibile")
}

// Funzione helper comune
func checkToolAvailable(tool string) bool {
	cmd := exec.Command("which", tool)
	err := cmd.Run()
	return err == nil
}
