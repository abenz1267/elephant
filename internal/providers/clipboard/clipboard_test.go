package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestActivateRemoveDoesNotDeadlock(t *testing.T) {
	const identifier = "item"

	oldConfig := config
	oldFile := file
	oldHistory := clipboardhistory
	oldModes := availableModes
	oldMode := currentMode
	t.Cleanup(func() {
		config = oldConfig
		file = oldFile
		clipboardhistory = oldHistory
		availableModes = oldModes
		currentMode = oldMode
	})

	config = &Config{MaxItems: 100}
	file = filepath.Join(t.TempDir(), "clipboard.gob")
	clipboardhistory = map[string]*Item{
		identifier: {
			Content: "delete me",
			Time:    time.Now(),
		},
	}
	availableModes = []string{ActionCombined, ActionTextOnly}
	currentMode = ActionCombined

	done := make(chan struct{})
	go func() {
		Activate(false, identifier, ActionRemove, "", "", 0, nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("removing a clipboard item deadlocked")
	}

	if _, ok := clipboardhistory[identifier]; ok {
		t.Fatal("clipboard item was not removed")
	}
}
