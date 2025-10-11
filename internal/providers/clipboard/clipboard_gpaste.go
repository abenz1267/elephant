package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// GPaste implementation
type GPaste struct {
	name string
}

func NewGPaste() *GPaste {
	return &GPaste{
		name: "GPaste",
	}
}

func (g *GPaste) GetName() string {
	return g.name
}

func (g *GPaste) GetCommand() string {
	return "gpaste-client"
}

func (g *GPaste) IsAvailable() bool {
	return checkToolAvailable("gpaste-client")
}

func (g *GPaste) GetContent() ([]byte, []string, error) {
	rawContent, err := g.getRawContent()
	if err != nil {
		return nil, nil, err
	}
	return []byte(rawContent), []string{"text/plain"}, nil
}

// Nuovo metodo implementato
func (g *GPaste) GetContentParsed() (itemID string, content string, mimetypes []string, err error) {
	rawContent, err := g.getRawContent()
	if err != nil {
		return "", "", nil, err
	}

	// Parsing specifico per GPaste: "UUID:contenuto"
	parts := strings.SplitN(rawContent, ":", 2)
	if len(parts) >= 2 {
		itemID = strings.TrimSpace(parts[0])
		content = strings.TrimSpace(parts[1])
	} else {
		// Fallback: usa MD5 come ID
		md5Hash := md5.Sum([]byte(rawContent))
		itemID = hex.EncodeToString(md5Hash[:])
		content = rawContent
	}

	return itemID, content, []string{"text/plain"}, nil
}

func (g *GPaste) getRawContent() (string, error) {
	cmd := exec.Command("gpaste-client", "history")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	content := strings.TrimSpace(string(out))
	if content == "" {
		return "", fmt.Errorf("clipboard vuota")
	}

	return content, nil
}

func (g *GPaste) StartMonitoring(changed chan<- bool) error {
	fmt.Println("=== StartMonitoring Clipboard Provider GPASTE ===")

	go func() {
		var lastContent string
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			current, err := g.getRawContent()
			if err == nil && current != "" && current != lastContent {
				lastContent = current
				changed <- true
			}
		}
	}()
	return nil
}

func (g *GPaste) CopyToClipboard(id string, content string) error {
	cmd := exec.Command("gpaste-client", "select", id)
	return cmd.Run()
}
