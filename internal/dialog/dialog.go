//go:build (windows || darwin || linux) && cgo

package dialog

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/sqweek/dialog"
)

type FileInfo struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// dialogRequest wraps a dialog call to be executed on the dedicated dialog thread.
type dialogRequest struct {
	fn     func() (interface{}, error)
	result chan dialogResult
}

type dialogResult struct {
	val interface{}
	err error
}

var (
	dialogCh chan dialogRequest
	initOnce sync.Once
)

const (
	maxRetries    = 3
	retryDelay    = 200 * time.Millisecond
	dialogTimeout = 120 * time.Second
)

// Init starts the dedicated dialog thread. Must be called once at startup.
// Safe to call multiple times; only the first call has effect.
func Init() {
	initOnce.Do(func() {
		dialogCh = make(chan dialogRequest, 4)
		go dialogLoop()
	})
}

// dialogLoop runs on a dedicated goroutine with the OS thread locked.
// All native dialog calls are dispatched here to avoid macOS main-thread issues.
func dialogLoop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	for req := range dialogCh {
		val, err := safeCall(req.fn)
		req.result <- dialogResult{val: val, err: err}
	}
}

// safeCall executes fn with panic recovery.
func safeCall(fn func() (interface{}, error)) (val interface{}, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("dialog panic: %v", r)
			log.Error().Msgf("Recovered from dialog panic: %v", r)
		}
	}()
	return fn()
}

// dispatch sends a dialog call to the dedicated thread and waits for the result.
func dispatch(fn func() (interface{}, error)) (interface{}, error) {
	Init()

	req := dialogRequest{
		fn:     fn,
		result: make(chan dialogResult, 1),
	}

	select {
	case dialogCh <- req:
	case <-time.After(dialogTimeout):
		return nil, fmt.Errorf("dialog dispatch timeout: another dialog may be open")
	}

	select {
	case res := <-req.result:
		return res.val, res.err
	case <-time.After(dialogTimeout):
		return nil, fmt.Errorf("dialog execution timeout")
	}
}

// dispatchWithRetry retries transient dialog failures.
func dispatchWithRetry(fn func() (interface{}, error)) (interface{}, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		val, err := dispatch(fn)
		if err == nil {
			return val, nil
		}
		lastErr = err
		log.Warn().Err(err).Int("attempt", i+1).Msg("Dialog call failed, retrying")
		time.Sleep(retryDelay)
	}
	return nil, fmt.Errorf("dialog failed after %d attempts: %w", maxRetries, lastErr)
}

func buildFileFilter(filters []string) []string {
	var extensions []string
	for _, f := range filters {
		ext := filepath.Ext(f)
		if ext != "" {
			extensions = append(extensions, ext[1:])
		}
	}
	return extensions
}

func OpenFileDialog(title string, filters []string) (*FileInfo, error) {
	extensions := buildFileFilter(filters)

	val, err := dispatchWithRetry(func() (interface{}, error) {
		builder := dialog.File().Title(title)
		if len(extensions) > 0 {
			builder = builder.Filter("Supported files", extensions...)
		}

		path, err := builder.Load()
		if err != nil {
			if err == dialog.ErrCancelled {
				return nil, nil
			}
			return nil, err
		}
		return path, nil
	})
	if err != nil {
		return nil, err
	}
	if val == nil {
		return nil, nil
	}

	path := val.(string)
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	return &FileInfo{
		Path: path,
		Name: info.Name(),
		Size: info.Size(),
	}, nil
}

func SaveFileDialog(title, defaultName string, filters []string) (string, error) {
	extensions := buildFileFilter(filters)

	val, err := dispatchWithRetry(func() (interface{}, error) {
		builder := dialog.File().Title(title)
		if defaultName != "" {
			builder = builder.SetStartFile(defaultName)
		}
		if len(extensions) > 0 {
			builder = builder.Filter("Supported files", extensions...)
		}

		path, err := builder.Save()
		if err != nil {
			if err == dialog.ErrCancelled {
				return nil, nil
			}
			return nil, err
		}
		return path, nil
	})
	if err != nil {
		return "", err
	}
	if val == nil {
		return "", nil
	}
	return val.(string), nil
}

func OpenFolderDialog(title string) (string, error) {
	val, err := dispatchWithRetry(func() (interface{}, error) {
		path, err := dialog.Directory().Title(title).Browse()
		if err != nil {
			if err == dialog.ErrCancelled {
				return nil, nil
			}
			return nil, err
		}
		return path, nil
	})
	if err != nil {
		return "", err
	}
	if val == nil {
		return "", nil
	}
	return val.(string), nil
}
