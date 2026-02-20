package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
)

type Event struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Operation string `json:"operation"` // "create", "modify", "delete", "rename"
}

type EventHandler func(event Event)

type Watcher struct {
	watcher        *fsnotify.Watcher
	handler        EventHandler
	fileTypes      []string
	recursive      bool
	watchedDirs    map[string]bool
	watchedFiles   map[string]string
	mu             sync.RWMutex
	done           chan struct{}
	debounceTimers map[string]*time.Timer
	debounceMu     sync.Mutex
}

func New(fileTypes []string, recursive bool, handler EventHandler) (*Watcher, error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &Watcher{
		watcher:        fsWatcher,
		handler:        handler,
		fileTypes:      fileTypes,
		recursive:      recursive,
		watchedDirs:    make(map[string]bool),
		watchedFiles:   make(map[string]string),
		done:           make(chan struct{}),
		debounceTimers: make(map[string]*time.Timer),
	}, nil
}

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

	parentDir := filepath.Dir(absPath)
	fileName := filepath.Base(absPath)

	w.mu.Lock()
	if w.watchedFiles == nil {
		w.watchedFiles = make(map[string]string)
	}
	w.watchedFiles[absPath] = fileName
	w.mu.Unlock()

	if err := w.watchDirectory(parentDir); err != nil {
		return err
	}

	// Fire initial event so consumers get current file content immediately
	go w.handler(Event{
		Path:      absPath,
		Name:      fileName,
		Operation: "modify",
	})

	return nil
}

func (w *Watcher) watchDirectory(dir string) error {
	w.mu.Lock()
	alreadyWatched := w.watchedDirs[dir]
	if alreadyWatched {
		w.mu.Unlock()
		return nil
	}
	if err := w.watcher.Add(dir); err != nil {
		w.mu.Unlock()
		return err
	}
	w.watchedDirs[dir] = true
	w.mu.Unlock()

	if !w.recursive {
		go w.emitInitialDirectoryEvents(dir, false)
		return nil
	}

	var dirs []string
	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && path != dir {
			dirs = append(dirs, path)
		}
		return nil
	}); err != nil {
		log.Warn().Err(err).Str("path", dir).Msg("Failed to walk directory for recursive watch setup")
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	for _, d := range dirs {
		if !w.watchedDirs[d] {
			if err := w.watcher.Add(d); err != nil {
				log.Warn().Err(err).Str("path", d).Msg("Failed to watch subdirectory")
				continue
			}
			w.watchedDirs[d] = true
		}
	}

	go w.emitInitialDirectoryEvents(dir, true)

	return nil
}

func (w *Watcher) Unwatch(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	w.mu.Lock()
	if _, isFile := w.watchedFiles[absPath]; isFile {
		delete(w.watchedFiles, absPath)
		w.mu.Unlock()
		return nil
	}
	delete(w.watchedDirs, absPath)
	w.mu.Unlock()

	return w.watcher.Remove(absPath)
}

func (w *Watcher) Start() {
	go w.eventLoop()
}

func (w *Watcher) Stop() error {
	close(w.done)
	w.debounceMu.Lock()
	for _, timer := range w.debounceTimers {
		timer.Stop()
	}
	w.debounceTimers = make(map[string]*time.Timer)
	w.debounceMu.Unlock()
	return w.watcher.Close()
}

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

func (w *Watcher) handleEvent(event fsnotify.Event) {
	w.mu.RLock()
	_, isExplicitFileWatch := w.watchedFiles[event.Name]
	w.mu.RUnlock()

	if !isExplicitFileWatch && !w.isRelevantFile(event.Name) {
		return
	}

	var operation string
	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
		operation = "create"
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() && w.recursive {
			if err := w.watchDirectory(event.Name); err != nil {
				log.Warn().Err(err).Str("path", event.Name).Msg("Failed to watch new directory")
			}
		}
	case event.Op&fsnotify.Write == fsnotify.Write:
		operation = "modify"
	case event.Op&fsnotify.Remove == fsnotify.Remove:
		operation = "delete"
	case event.Op&fsnotify.Rename == fsnotify.Rename:
		operation = "rename"
	default:
		return
	}

	evt := Event{
		Path:      event.Name,
		Name:      filepath.Base(event.Name),
		Operation: operation,
	}

	w.debounceMu.Lock()
	if timer, exists := w.debounceTimers[event.Name]; exists {
		timer.Stop()
	}
	w.debounceTimers[event.Name] = time.AfterFunc(150*time.Millisecond, func() {
		w.debounceMu.Lock()
		delete(w.debounceTimers, event.Name)
		w.debounceMu.Unlock()
		log.Debug().
			Str("path", evt.Path).
			Str("op", evt.Operation).
			Msg("File event (debounced)")
		w.handler(evt)
	})
	w.debounceMu.Unlock()
}

// emitInitialDirectoryEvents walks a directory and fires synthetic events
// for all matching files so consumers get current content on watch start.
func (w *Watcher) emitInitialDirectoryEvents(dir string, recursive bool) {
	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if !recursive && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if !w.isRelevantFile(path) {
			return nil
		}
		log.Debug().
			Str("path", path).
			Msg("Emitting initial file event")
		w.handler(Event{
			Path:      path,
			Name:      filepath.Base(path),
			Operation: "create",
		})
		return nil
	}); err != nil {
		log.Warn().Err(err).Str("path", dir).Msg("Failed to walk directory for initial events")
	}
}

func (w *Watcher) isRelevantFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return false
		}
	}

	if len(w.fileTypes) == 0 {
		return true
	}

	for _, ft := range w.fileTypes {
		if strings.ToLower(ft) == ext {
			return true
		}
	}
	return false
}
