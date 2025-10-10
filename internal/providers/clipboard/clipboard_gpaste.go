package main

import (
    "bufio"
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

func (g *GPaste) IsAvailable() bool {
    return checkToolAvailable("gpaste-client")
}

func (g *GPaste) GetContent() ([]byte, []string, error) {
    cmd := exec.Command("gpaste-client", "get", "0")
    out, err := cmd.Output()
    if err != nil {
        return nil, nil, err
    }
    
    content := strings.TrimSpace(string(out))
    if content == "" {
        return nil, nil, fmt.Errorf("clipboard vuota")
    }
    
    return []byte(content), []string{"text/plain"}, nil
}

func (g *GPaste) StartMonitoring(changed chan<- bool) error {
    go func() {
        var lastContent string
        ticker := time.NewTicker(1 * time.Second)
        defer ticker.Stop()
        
        for range ticker.C {
            current, err := g.getCurrentContent()
            if err == nil && current != "" && current != lastContent {
                lastContent = current
                changed <- true
            }
        }
    }()
    return nil
}

func (g *GPaste) getCurrentContent() (string, error) {
    cmd := exec.Command("gpaste-client", "get", "0")
    out, err := cmd.Output()
    if err != nil {
        return "", err
    }
    return strings.TrimSpace(string(out)), nil
}

func (g *GPaste) CopyToClipboard(content string) error {
    cmd := exec.Command("gpaste-client", "set", content)
    return cmd.Run()
}