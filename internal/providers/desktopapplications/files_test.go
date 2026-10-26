package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestObsoleteSymlinkTargetRemovalKeepsCurrentEntry(t *testing.T) {
	// The provider keeps this state globally, so isolate the test and restore
	// the process state for any tests added to this package later.
	oldFiles := files
	oldWatchedDirs := watchedDirs
	oldSymlinkToReal := symlinkToReal
	oldRealToSymlink := realToSymlink
	oldWatcher := watcher

	var err error
	watcher, err = fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		watcher.Close()
		files = oldFiles
		watchedDirs = oldWatchedDirs
		symlinkToReal = oldSymlinkToReal
		realToSymlink = oldRealToSymlink
		watcher = oldWatcher
	})

	files = make(map[string]*DesktopFile)
	watchedDirs = make(map[string]bool)
	symlinkToReal = make(map[string]string)
	realToSymlink = make(map[string][]string)

	root := t.TempDir()
	applicationsDir := filepath.Join(root, "applications")
	oldTargetDir := filepath.Join(root, "target-old")
	newTargetDir := filepath.Join(root, "target-new")
	for _, dir := range []string{applicationsDir, oldTargetDir, newTargetDir} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	const identifier = "sleep.desktop"
	visiblePath := filepath.Join(applicationsDir, identifier)
	oldTarget := filepath.Join(oldTargetDir, identifier)
	newTarget := filepath.Join(newTargetDir, identifier)
	for _, target := range []string{oldTarget, newTarget} {
		if err := os.WriteFile(target, []byte("[Desktop Entry]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(oldTarget, visiblePath); err != nil {
		t.Fatal(err)
	}

	// Model the initial scan of a visible XDG entry backed by the old target.
	files[identifier] = &DesktopFile{Data: Data{SourceFile: visiblePath}}
	trackSymlinks(visiblePath)

	// Replace the visible symlink while preserving its path and desktop-entry
	// identifier.
	if err := os.Remove(visiblePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(newTarget, visiblePath); err != nil {
		t.Fatal(err)
	}
	trackSymlinks(visiblePath)

	if _, tracked := realToSymlink[oldTarget]; tracked {
		t.Fatalf("obsolete target %q is still tracked", oldTarget)
	}
	if target := symlinkToReal[visiblePath]; target != newTarget {
		t.Fatalf("visible entry target = %q, want %q", target, newTarget)
	}

	// Removing the old target must not remove the entry now backed by the new
	// target.
	if err := os.Remove(oldTarget); err != nil {
		t.Fatal(err)
	}
	handleFileRemove(oldTarget)
	if _, exists := files[identifier]; !exists {
		t.Fatal("removing the obsolete target deleted the current entry")
	}

	// Removing the active target must still remove its now-broken entry.
	if err := os.Remove(newTarget); err != nil {
		t.Fatal(err)
	}
	handleFileRemove(newTarget)
	if _, exists := files[identifier]; exists {
		t.Fatal("removing the current target kept the broken entry")
	}
}
