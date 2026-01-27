//go:build (windows || darwin || linux) && cgo

package dialog

import (
	"os"
	"path/filepath"

	"github.com/sqweek/dialog"
)

// FileInfo contains information about a selected file
type FileInfo struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// OpenFileDialog opens a native file dialog and returns the selected file info
func OpenFileDialog(title string, filters []string) (*FileInfo, error) {
	builder := dialog.File().Title(title)

	// Add filters if provided - combine all extensions into one filter
	if len(filters) > 0 {
		// Collect all extensions
		var extensions []string
		for _, f := range filters {
			ext := filepath.Ext(f)
			if ext != "" {
				extensions = append(extensions, ext[1:]) // Remove the dot
			}
		}
		if len(extensions) > 0 {
			// Add as single filter with all extensions
			builder = builder.Filter("Supported files", extensions...)
		}
	}

	// Open the dialog
	path, err := builder.Load()
	if err != nil {
		if err == dialog.ErrCancelled {
			return nil, nil // User cancelled, not an error
		}
		return nil, err
	}

	// Get file info
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

// OpenFolderDialog opens a native folder dialog and returns the selected path
func OpenFolderDialog(title string) (string, error) {
	path, err := dialog.Directory().Title(title).Browse()
	if err != nil {
		if err == dialog.ErrCancelled {
			return "", nil // User cancelled, not an error
		}
		return "", err
	}
	return path, nil
}
