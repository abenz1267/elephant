package main

import (
    "bufio"
    "os/exec"
    "strings"
)

// Wayland implementation
type Wayland struct {
    name string
}

func NewWayland() *Wayland {
    return &Wayland{
        name: "Wayland",
    }
}

func (w *Wayland) GetName() string {
    return w.name
}

func (w *Wayland) IsAvailable() bool {
    return checkToolAvailable("wl-paste") && checkToolAvailable("wl-copy")
}

func (w *Wayland) GetContent() ([]byte, []string, error) {
    cmd := exec.Command("wl-paste", "-n")
    out, err := cmd.Output()
    if err != nil {
        if strings.Contains(string(out), "Nothing is copied") {
            return nil, nil, nil
        }
        return nil, nil, err
    }
    
    mimetypes := w.getMimetypes()
    return out, mimetypes, nil
}

func (w *Wayland) getMimetypes() []string {
    cmd := exec.Command("wl-paste", "--list-types")
    out, err := cmd.Output()
    if err != nil {
        return []string{"text/plain"}
    }
    return strings.Fields(string(out))
}

func (w *Wayland) StartMonitoring(changed chan<- bool) error {
    cmd := exec.Command("wl-paste", "--watch", "echo", "")
    
    stdout, err := cmd.StdoutPipe()
    if err != nil {
        return err
    }

    err = cmd.Start()
    if err != nil {
        return err
    }

    go func() {
        scanner := bufio.NewScanner(stdout)
        for scanner.Scan() {
            changed <- true
        }
        cmd.Wait()
    }()
    
    return nil
}

func (w *Wayland) CopyToClipboard(id string, content string) error {
    cmd := exec.Command("wl-copy")
    cmd.Stdin = strings.NewReader(content)
    return cmd.Run()
}