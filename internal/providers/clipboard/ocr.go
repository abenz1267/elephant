package main

import (
	"os/exec"
	"strings"
)

func runOCR(imgPath string) string {
	cmd := exec.Command("tesseract", imgPath, "stdout")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
