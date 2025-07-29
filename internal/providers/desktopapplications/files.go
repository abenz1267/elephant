package main

import (
	"github.com/adrg/xdg"
	"github.com/charlievieth/fastwalk"
	"github.com/fsnotify/fsnotify"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	files            map[string]*DesktopFile
	watchedDirs      map[string]bool
	symlinkToReal    map[string]string   // this should be [symlink]realfile
	realToSymlink    map[string][]string // this should be [realfile][]symlink
	filesMu          sync.RWMutex
	watcherDirsMu    sync.RWMutex
	symlinkTargetsMu sync.RWMutex // we use this for both symlink maps
	watcher          *fsnotify.Watcher
	regionLocale     = ""
	langLocale       = ""
	dirs             []string
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
			os.Exit(1)
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
		slog.Error(Name, "walk", err)
		os.Exit(1)
	}

	filesMu.RLock()
	_, exists := files[path]
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

	// setup two-way tracking
	symlinkTargetsMu.Lock()
	if realToSymlink[targetPath] == nil {
		realToSymlink[targetPath] = make([]string, 0)
		realToSymlink[targetPath] = append(realToSymlink[targetPath], filename)
	}

	symlinkToReal[filename] = targetPath
	symlinkTargetsMu.Unlock()

	addDirToWatcher(filepath.Dir(targetPath), watchedDirs)

	slog.Debug(Name, "symlink_tracked", filename, "target", targetPath)
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
	defer watcher.Close()

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			go handleFileEvent(event)

		case err, ok := <-watcher.Errors:
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
}

/*
handleFileCreate
we parse the desktop file
if a file is a symlink, we need to track it and set the desktop entry

if the file is not a symlink, then we check if any symlinks resolve to it
if any symlinks resolve to it, we set the desktop entry for each of those
if no symlinks resolve to it, we set the desktop entry for itself
*/
func handleFileCreate(path string) {
	symlinkTargetsMu.Lock()
	clone := realToSymlink[path]
	symlinkTargetsMu.Unlock()

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

/*
handleFileUpdate
we parse the desktop file
if a file is a symlink, we need to track it and set the desktop entry
if the file is not a symlink, then we check if any symlinks resolve to it
if any symlinks resolve to it, we set the desktop entry for each of those
if no symlinks resolve to it, we set the desktop entry for itself
*/
func handleFileUpdate(path string) {
	symlinkTargetsMu.Lock()
	clone := realToSymlink[path]
	symlinkTargetsMu.Unlock()

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

/*
handleFileUpdate
if a file is a symlink, we resolve it.
if the origin is referenced by more than this symlink, ignore it
if the origin is not referenced by more than this symlink, untrack it
*/
func handleFileRemove(path string) {
	symlinkTargetsMu.Lock()
	defer symlinkTargetsMu.Unlock()
	originPath, sym := isSymlink(path)
	defer slog.Debug(Name, "file_removed", path)
	if sym {
		filesMu.Lock()
		delete(files, path)
		filesMu.Unlock()

		delete(symlinkToReal, path)

		for i, s := range realToSymlink[originPath] {
			if s == path {
				realToSymlink[originPath] = append(realToSymlink[originPath][:i], realToSymlink[originPath][i+1:]...)
			}
		}
		if len(realToSymlink[originPath]) == 0 {
			delete(realToSymlink, originPath)
		}
	}

	if realToSymlink[path] != nil {
		for _, symedFile := range realToSymlink[path] {
			delete(symlinkToReal, symedFile)
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
	files[path] = parseFile(path, langLocale, regionLocale)
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
