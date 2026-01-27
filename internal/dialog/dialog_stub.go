//go:build !(windows || darwin || linux) || !cgo

package dialog

import "errors"

// FileInfo contains information about a selected file
type FileInfo struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

var ErrNotSupported = errors.New("file dialogs not supported in this build")

// OpenFileDialog is a stub for non-CGO builds
func OpenFileDialog(title string, filters []string) (*FileInfo, error) {
	return nil, ErrNotSupported
}

// OpenFolderDialog is a stub for non-CGO builds
func OpenFolderDialog(title string) (string, error) {
	return "", ErrNotSupported
}
