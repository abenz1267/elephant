package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunOCR(t *testing.T) {
	imgPath := filepath.Join(t.TempDir(), "ocr_test.png")

	cmd := exec.Command("convert", "-size", "300x50", "xc:white",
		"-pointsize", "20", "-fill", "black",
		"-draw", "text 10,30 'hello elephant'",
		imgPath)
	if err := cmd.Run(); err != nil {
		t.Skip("imagemagick not available:", err)
	}

	text := runOCR(imgPath)

	if !strings.Contains(strings.ToLower(text), "elephant") {
		t.Errorf("expected OCR to extract 'elephant', got: %q", text)
	}
}

func TestRunOCR_NoFile(t *testing.T) {
	text := runOCR(filepath.Join(t.TempDir(), "nonexistent_ocr_test.png"))
	if text != "" {
		t.Errorf("expected empty string for missing file, got: %q", text)
	}
}
