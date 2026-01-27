package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
)

// Event represents a file system event
type Event struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Operation string `json:"operation"` // "create", "modify", "delete", "rename"
}

// EventHandler is called when a file event occurs
type EventHandler func(event Event)

// Watcher watches files and directories for changes
type Watcher struct {
	watcher      *fsnotify.Watcher
	handler      EventHandler
	fileTypes    []string
	recursive    bool
	watchedDirs  map[string]bool
	watchedFiles map[string]string // Maps file path to filename for individual file watches
	mu           sync.RWMutex
	done         chan struct{}
}

// New creates a new file watcher
func New(fileTypes []string, recursive bool, handler EventHandler) (*Watcher, error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &Watcher{
		watcher:      fsWatcher,
		handler:      handler,
		fileTypes:    fileTypes,
		recursive:    recursive,
		watchedDirs:  make(map[string]bool),
		watchedFiles: make(map[string]string),
		done:         make(chan struct{}),
	}, nil
}

// Watch adds a path to watch
func (w *Watcher) Watch(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return w.watchDirectory(absPath)
	}

	// For individual files, watch the parent directory instead
	// This is necessary because apps like Excel delete and recreate files on save,
	// which invalidates watchers on the file itself
	parentDir := filepath.Dir(absPath)
	fileName := filepath.Base(absPath)
	
	w.mu.Lock()
	// Store the specific file we're interested in
	if w.watchedFiles == nil {
		w.watchedFiles = make(map[string]string)
	}
	w.watchedFiles[absPath] = fileName
	w.mu.Unlock()

	return w.watchDirectory(parentDir)
}

// watchDirectory adds a directory and optionally its subdirectories
func (w *Watcher) watchDirectory(dir string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.watchedDirs[dir] {
		return nil // Already watching
	}

	if err := w.watcher.Add(dir); err != nil {
		return err
	}
	w.watchedDirs[dir] = true

	if !w.recursive {
		return nil
	}

	// Walk subdirectories
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if info.IsDir() && path != dir {
			if !w.watchedDirs[path] {
				if err := w.watcher.Add(path); err != nil {
					log.Warn().Err(err).Str("path", path).Msg("Failed to watch subdirectory")
					return nil
				}
				w.watchedDirs[path] = true
			}
		}
		return nil
	})
}

// Unwatch removes a path from watching
func (w *Watcher) Unwatch(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	w.mu.Lock()
	// Check if this was a specific file watch
	if _, isFile := w.watchedFiles[absPath]; isFile {
		delete(w.watchedFiles, absPath)
		w.mu.Unlock()
		// Don't remove the directory watch as other files might be watched
		return nil
	}
	delete(w.watchedDirs, absPath)
	w.mu.Unlock()

	return w.watcher.Remove(absPath)
}

// Start begins watching for events
func (w *Watcher) Start() {
	go w.eventLoop()
}

// Stop stops the watcher
func (w *Watcher) Stop() error {
	close(w.done)
	return w.watcher.Close()
}

// eventLoop processes file system events
func (w *Watcher) eventLoop() {
	for {
		select {
		case <-w.done:
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Error().Err(err).Msg("Watcher error")
		}
	}
}

// handleEvent processes a single file system event
func (w *Watcher) handleEvent(event fsnotify.Event) {
	// Check if we're watching this specific file (explicitly requested by frontend)
	w.mu.RLock()
	_, isExplicitFileWatch := w.watchedFiles[event.Name]
	w.mu.RUnlock()

	// For directory watches, apply file type filter to avoid noise (.DS_Store, etc.)
	if !isExplicitFileWatch && !w.isRelevantFile(event.Name) {
		return
	}

	// Convert fsnotify operation to our operation string
	var operation string
	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
		operation = "create"
		// If a new directory is created, watch it too
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() && w.recursive {
			w.watchDirectory(event.Name)
		}
	case event.Op&fsnotify.Write == fsnotify.Write:
		operation = "modify"
	case event.Op&fsnotify.Remove == fsnotify.Remove:
		operation = "delete"
	case event.Op&fsnotify.Rename == fsnotify.Rename:
		operation = "rename"
	default:
		return // Ignore other operations
	}

	log.Debug().
		Str("path", event.Name).
		Str("op", operation).
		Msg("File event")

	w.handler(Event{
		Path:      event.Name,
		Name:      filepath.Base(event.Name),
		Operation: operation,
	})
}

// isRelevantFile checks if the file matches our watched file types
func (w *Watcher) isRelevantFile(path string) bool {
	// Skip directories
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return false
	}

	// If no file types specified, watch all files
	if len(w.fileTypes) == 0 {
		return true
	}

	ext := strings.ToLower(filepath.Ext(path))
	for _, ft := range w.fileTypes {
		if strings.ToLower(ft) == ext {
			return true
		}
	}
	return false
}

// WatchedPaths returns all currently watched paths
func (w *Watcher) WatchedPaths() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	paths := make([]string, 0, len(w.watchedDirs))
	for path := range w.watchedDirs {
		paths = append(paths, path)
	}
	return paths
}
