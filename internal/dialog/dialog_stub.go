//go:build !(windows || darwin || linux) || !cgo

package dialog

import "errors"

type FileInfo struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

var ErrNotSupported = errors.New("file dialogs not supported in this build")

func Init() {}

func OpenFileDialog(title string, filters []string) (*FileInfo, error) {
	return nil, ErrNotSupported
}

func SaveFileDialog(title, defaultName string, filters []string) (string, error) {
	return "", ErrNotSupported
}

func OpenFolderDialog(title string) (string, error) {
	return "", ErrNotSupported
}
