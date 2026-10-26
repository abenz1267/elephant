package main

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/abenz1267/elephant/v2/internal/comm/handlers"
	"github.com/adrg/xdg"
	"github.com/charlievieth/fastwalk"
	"github.com/fsnotify/fsnotify"
)

var (
	files         map[string]*DesktopFile
	watchedDirs   map[string]bool
	symlinkToReal map[string]string   // this should be [symlink]realfile
	realToSymlink map[string][]string // this should be [realfile][]symlink
	filesMu       sync.RWMutex
	watcherDirsMu sync.RWMutex
	watcher       *fsnotify.Watcher
	reinitMu      sync.Mutex // guards reinitializeWatcher to prevent concurrent reinits
	regionLocale  = ""
	langLocale    = ""
	dirs          []string
)

func loadFiles() {
	start := time.Now()
	setVars()
	conf := fastwalk.Config{
		Follow: true,
	}

	var err error
	watcher, err = fsnotify.NewWatcher()
	if err != nil {
		slog.Error(Name, "watcher_init", err)
		return
	}

	for _, root := range dirs {
		if _, err := os.Stat(root); err != nil {
			continue
		}

		if err := fastwalk.Walk(&conf, root, walkFunction); err != nil {
			slog.Error(Name, "walk", err)
			continue
		}
	}

	if mossIsActive() {
		// With moss package manager, /usr is atomically replaced on package changes.
		// Watch it directly so we can detect the swap and reinitialize inotify watches.
		if err := watcher.Add("/usr"); err != nil {
			slog.Warn(Name, "usr_watcher_add", err)
		}
	}

	fileCount := len(files)
	slog.Info(Name, "files", fileCount, "time", time.Since(start))

	slog.Info(Name, "watcher_dirs", len(watchedDirs))
	go watchFiles()
	slog.Info(Name, "watcher", "started")
}

func setVars() {
	files = make(map[string]*DesktopFile)
	watchedDirs = make(map[string]bool)
	symlinkToReal = make(map[string]string)
	realToSymlink = make(map[string][]string)

	getLocale()

	dirs = xdg.ApplicationDirs
}

func walkFunction(path string, d fs.DirEntry, err error) error {
	if err != nil {
		return err
	}

	if filepath.Ext(path) == ".desktop" {
		check := strings.TrimSuffix(filepath.Base(path), ".desktop")

		for _, v := range br {
			if v.MatchString(check) {
				return nil
			}
		}
	}

	filesMu.RLock()
	_, exists := files[filepath.Base(path)]
	filesMu.RUnlock()

	if exists {
		return nil
	}

	if !d.IsDir() && filepath.Ext(path) == ".desktop" {
		addNewEntry(path)
	}

	if d.IsDir() {
		addDirToWatcher(path, watchedDirs)
	}

	return err
}

func trackSymlinks(filename string) {
	// for all intents and purposes, filename is the symlink
	// targetPath is what it resolves to.
	targetPath, sym := isSymlink(filename)
	if !sym {
		return
	}

	// A symlink can be replaced with another symlink at the same path. Remove
	// its old reverse mapping before recording the new target, or a later
	// removal of the old target can affect this entry.
	if previousTarget, ok := symlinkToReal[filename]; ok && previousTarget != targetPath {
		untrackSymlink(filename, previousTarget)
	}

	// setup two-way tracking
	// Repeated create or write events for one symlink must not add duplicate
	// reverse mappings for the same target.
	tracked := false
	for _, symlink := range realToSymlink[targetPath] {
		if symlink == filename {
			tracked = true
			break
		}
	}
	if !tracked {
		realToSymlink[targetPath] = append(realToSymlink[targetPath], filename)
	}

	symlinkToReal[filename] = targetPath

	addDirToWatcher(filepath.Dir(targetPath), watchedDirs)

	slog.Debug(Name, "symlink_tracked", filename, "target", targetPath)
}

// untrackSymlink removes both sides of a known symlink-to-target relationship.
// The forward mapping is deleted only if it still points to targetPath, so a
// delayed event for an old target cannot erase a newer relationship.
func untrackSymlink(filename, targetPath string) {
	if currentTarget, ok := symlinkToReal[filename]; ok && currentTarget == targetPath {
		delete(symlinkToReal, filename)
	}

	symlinks := realToSymlink[targetPath]
	for i, symlink := range symlinks {
		if symlink == filename {
			symlinks = append(symlinks[:i], symlinks[i+1:]...)
			break
		}
	}
	if len(symlinks) == 0 {
		delete(realToSymlink, targetPath)
	} else {
		realToSymlink[targetPath] = symlinks
	}
}

func addDirToWatcher(dir string, watchedDirs map[string]bool) {
	watcherDirsMu.Lock()
	defer watcherDirsMu.Unlock()
	if watchedDirs[dir] {
		return
	}

	if err := watcher.Add(dir); err != nil {
		slog.Warn(Name, "watcher_add", err, "dir", dir)
		return
	}

	watchedDirs[dir] = true
}

func watchFiles() {
	// Capture watcher locally so reinitializeWatcher can replace the global
	// without affecting this goroutines event loop.
	w := watcher
	defer w.Close()

	for {
		select {
		case event, ok := <-w.Events:
			if !ok {
				return
			}
			handleFileEvent(event)

		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			slog.Error(Name, "watcher", err)
		}
	}
}

func checkSubdirOfXDG(subdir string) bool {
	for _, dir := range dirs {
		if strings.HasPrefix(subdir, dir) {
			return true
		}
	}
	return false
}

func handleFileEvent(event fsnotify.Event) {
	// moss replaces /usr atomically, which invalidates all inotify watches under it.
	// Detect this and reinitialize.
	if event.Name == "/usr" && (event.Op&fsnotify.Rename != 0 || event.Op&fsnotify.Remove != 0) {
		slog.Info(Name, "usr_replaced", "reinitializing inotify watches")
		reinitializeWatcher()
		return
	}

	slog.Debug(Name, "file_system_event", event)
	if filepath.Ext(event.Name) != ".desktop" {
		// Handle directory creation to watch new subdirectories

		if event.Op&fsnotify.Create == fsnotify.Create {
			if info, err := os.Stat(event.Name); err == nil && info.IsDir() {

				// Don't track new subdirs of a dir we are only tracking for origin files
				if !checkSubdirOfXDG(event.Name) {
					return
				}

				if err := watcher.Add(event.Name); err != nil {
					slog.Warn(Name, "watcher_add_new", err, "dir", event.Name)
				}
			}
		}
		return
	}

	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
		handleFileCreate(event.Name)
	case event.Op&fsnotify.Write == fsnotify.Write:
		handleFileUpdate(event.Name)
	case event.Op&fsnotify.Remove == fsnotify.Remove:
		handleFileRemove(event.Name)
	case event.Op&fsnotify.Rename == fsnotify.Rename:
		handleFileRemove(event.Name)
	}

	handlers.ProviderUpdated <- Name
}

func handleFileCreate(path string) {
	clone := realToSymlink[path]
	_, sym := isSymlink(path)
	defer slog.Debug(Name, "file_created", path)
	if !sym {
		if clone != nil {
			for _, symedFile := range clone {
				addNewEntry(symedFile)
			}
			return
		}
		if !checkSubdirOfXDG(path) {
			return
		}
	}

	addNewEntry(path)
}

func handleFileUpdate(path string) {
	clone := realToSymlink[path]

	defer slog.Debug(Name, "file_updated", path)

	_, sym := isSymlink(path)
	if !sym {
		if clone != nil {
			for _, symedFile := range clone {
				addNewEntry(symedFile)
			}
			return
		}
		if !checkSubdirOfXDG(path) {
			return
		}
	}
	addNewEntry(path)
}

// handleFileRemove reconciles the index after a watched .desktop path is
// removed or renamed. path can identify either:
//   - a visible file in an XDG applications directory, possibly a symlink, or
//   - the resolved target file in another watched directory.
//
// For a symlinked entry, those two paths use opposite sides of symlinkToReal
// and realToSymlink and therefore require different cleanup.
func handleFileRemove(path string) {
	defer slog.Debug(Name, "file_removed", path)

	// A visible file or resolved target can be replaced at the same path before
	// its queued remove event is handled. Reparse the path that now exists
	// instead of deleting the replacement's entry.
	if fileExists(path) {
		handleFileUpdate(path)
		return
	}

	// Remove an entry only if its current SourceFile is path itself or a visible
	// symlink that is still recorded as pointing to this removed target file.
	removeEntriesForPath(path)

	// If path is a visible symlink, remove its saved forward relationship and
	// its membership in the target file's reverse relationship. The symlink can
	// no longer be resolved from disk, so use the relationship saved earlier.
	if targetPath, tracked := symlinkToReal[path]; tracked {
		untrackSymlink(path, targetPath)
	}

	// If path is a resolved target file, remove the forward relationships of
	// visible symlinks that still point to it, then remove its reverse mapping.
	// A symlink retargeted earlier is deliberately left associated with its new
	// target.
	if symlinks := realToSymlink[path]; symlinks != nil {
		for _, symlink := range symlinks {
			if symlinkToReal[symlink] == path {
				delete(symlinkToReal, symlink)
			}
		}
		delete(realToSymlink, path)
	}
}

// removeEntriesForPath removes cached entries actually backed by path. If path
// is a visible XDG file, the first check matches its SourceFile directly. If
// path is a resolved target, the loop finds visible symlinks still pointing to
// it and matches their SourceFile values. Desktop-entry identifiers are only
// basenames, so deleting by basename alone could remove a newer entry backed
// by another path.
func removeEntriesForPath(path string) {
	filesMu.Lock()
	defer filesMu.Unlock()

	if current, ok := files[filepath.Base(path)]; ok && current.SourceFile == path {
		delete(files, filepath.Base(path))
	}

	// A resolved target is indexed under each visible symlink's basename, with
	// the visible symlink stored as SourceFile. Confirm that both relationships
	// are still current before removing that entry.
	for _, symlink := range realToSymlink[path] {
		if symlinkToReal[symlink] != path {
			continue
		}
		identifier := filepath.Base(symlink)
		if current, ok := files[identifier]; ok && current.SourceFile == symlink {
			delete(files, identifier)
		}
	}
}

func addNewEntry(path string) {
	if origin, sym := isSymlink(path); sym {
		// check the file the symlink points to actually exists
		// otherwise it'll panic if you point to a location that's invalid
		trackSymlinks(path)
		if !fileExists(origin) {
			return
		}
	}

	filesMu.Lock()
	if f, err := parseFile(path, langLocale, regionLocale); err == nil {
		files[filepath.Base(path)] = f
	} else {
		slog.Error(Name, "parsing", err)
	}
	filesMu.Unlock()
}

func getLocale() {
	regionLocale = config.Locale

	if regionLocale == "" {
		regionLocale = os.Getenv("LANG")

		langMessages := os.Getenv("LC_MESSAGES")
		if langMessages != "" {
			regionLocale = langMessages
		}

		langAll := os.Getenv("LC_ALL")
		if langAll != "" {
			regionLocale = langAll
		}

		regionLocale = strings.Split(regionLocale, ".")[0]
	}

	langLocale = strings.Split(regionLocale, "_")[0]
}

func isSymlink(filename string) (string, bool) {
	targetPath, err := os.Readlink(filename)
	if err != nil {
		return "", false
	}

	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(filepath.Dir(filename), targetPath)
	}

	if targetPath == filename { // probably not needed, but maybe?
		return "", false
	}
	return targetPath, true
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// mossIsActive checks whether the moss package manager is running by looking
// for its state file. Used to decide if /usr needs direct monitoring.
func mossIsActive() bool {
	_, err := os.Stat("/.moss/db/state")
	return err == nil
}

// reinitializeWatcher tears down the current watcher and rebuilds it from scratch.
// Needed after moss atomically replaces /usr, which invalidates existing inotify watches.
func reinitializeWatcher() {
	reinitMu.Lock()
	defer reinitMu.Unlock()

	if watcher != nil {
		watcher.Close()
		watcher = nil
	}

	loadFiles()
	handlers.ProviderUpdated <- Name
	slog.Info(Name, "watcher_reinitialized", len(files))
}
