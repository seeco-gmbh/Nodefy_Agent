package files

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"nodefy/agent/internal/dialog"
	"nodefy/agent/internal/httputil"

	"github.com/rs/zerolog/log"
)

func HandleExportFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}

	var req struct {
		Data     json.RawMessage `json:"data"`
		Filename string          `json:"filename"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	if len(req.Data) == 0 {
		httputil.WriteError(w, http.StatusBadRequest, "data field is required")
		return
	}
	if req.Filename == "" {
		req.Filename = "export.json"
	}

	var parsed interface{}
	if err := json.Unmarshal(req.Data, &parsed); err != nil {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, req.Filename))
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(req.Data); err != nil {
			log.Warn().Err(err).Msg("Failed to write export response body")
		}
		return
	}

	pretty, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "Failed to format JSON")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, req.Filename))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(pretty); err != nil {
		log.Warn().Err(err).Msg("Failed to write export response body")
	}
}

func HandleImportFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	contentType := r.Header.Get("Content-Type")

	var fileBytes []byte
	var fileName string

	if len(contentType) >= 19 && contentType[:19] == "multipart/form-data" {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			httputil.WriteError(w, http.StatusBadRequest, "Failed to parse multipart form: "+err.Error())
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			httputil.WriteError(w, http.StatusBadRequest, "No file field in form: "+err.Error())
			return
		}
		defer file.Close()

		fileName = header.Filename
		fileBytes, err = io.ReadAll(file)
		if err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, "Failed to read uploaded file")
			return
		}
	} else {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			httputil.WriteError(w, http.StatusBadRequest, "Failed to read request body")
			return
		}
		fileBytes = body
		fileName = "upload.json"
	}

	var parsed interface{}
	if err := json.Unmarshal(fileBytes, &parsed); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "Uploaded file is not valid JSON: "+err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data":     parsed,
		"fileName": fileName,
	})
}

func HandleSaveDialog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		DefaultName string   `json:"defaultName"`
		Filters     []string `json:"filters"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		httputil.WriteError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	if req.DefaultName == "" {
		req.DefaultName = "project.ndf"
	}

	path, err := dialog.SaveFileDialog("Save Project", req.DefaultName, req.Filters)
	if err != nil {
		log.Error().Err(err).Msg("Save dialog error")
		httputil.WriteError(w, http.StatusInternalServerError, "Save dialog failed: "+err.Error())
		return
	}

	if path == "" {
		httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"cancelled": true, "path": ""})
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"cancelled": false, "path": path})
}

func HandleSaveFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}

	var req struct {
		Path        string          `json:"path"`
		Data        json.RawMessage `json:"data"`
		DefaultName string          `json:"defaultName"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	if len(req.Data) == 0 {
		httputil.WriteError(w, http.StatusBadRequest, "data field is required")
		return
	}

	savePath := req.Path

	if savePath == "" {
		defaultName := req.DefaultName
		if defaultName == "" {
			defaultName = "project.ndf"
		}
		dialogPath, err := dialog.SaveFileDialog("Save Project", defaultName, []string{"*.ndf", "*.json"})
		if err != nil {
			log.Error().Err(err).Msg("Save dialog error")
			httputil.WriteError(w, http.StatusInternalServerError, "Save dialog failed: "+err.Error())
			return
		}
		if dialogPath == "" {
			httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"cancelled": true, "path": ""})
			return
		}
		savePath = dialogPath
	}

	if !strings.HasSuffix(strings.ToLower(savePath), ".ndf") && !strings.HasSuffix(strings.ToLower(savePath), ".json") {
		savePath += ".ndf"
	}

	var parsed interface{}
	var fileContent []byte
	if err := json.Unmarshal(req.Data, &parsed); err == nil {
		indented, err := json.MarshalIndent(parsed, "", "  ")
		if err != nil {
			log.Warn().Err(err).Msg("Failed to pretty-print JSON, using raw bytes")
			fileContent = req.Data
		} else {
			fileContent = indented
		}
	} else {
		fileContent = req.Data
	}

	dir := filepath.Dir(savePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "Failed to create directory: "+err.Error())
		return
	}

	if err := os.WriteFile(savePath, fileContent, 0644); err != nil {
		log.Error().Err(err).Str("path", savePath).Msg("Failed to write file")
		httputil.WriteError(w, http.StatusInternalServerError, "Failed to write file: "+err.Error())
		return
	}

	log.Info().Str("path", savePath).Int("bytes", len(fileContent)).Msg("File saved")
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"cancelled": false,
		"path":      savePath,
		"bytes":     len(fileContent),
	})
}

func HandleLoadDialog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Filters []string `json:"filters"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		httputil.WriteError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	if len(req.Filters) == 0 {
		req.Filters = []string{"*.ndf", "*.json"}
	}

	fileInfo, err := dialog.OpenFileDialog("Open Project", req.Filters)
	if err != nil {
		log.Error().Err(err).Msg("Open dialog error")
		httputil.WriteError(w, http.StatusInternalServerError, "Open dialog failed: "+err.Error())
		return
	}

	if fileInfo == nil {
		httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"cancelled": true})
		return
	}

	content, err := os.ReadFile(fileInfo.Path)
	if err != nil {
		log.Error().Err(err).Str("path", fileInfo.Path).Msg("Failed to read file")
		httputil.WriteError(w, http.StatusInternalServerError, "Failed to read file: "+err.Error())
		return
	}

	var parsed interface{}
	if err := json.Unmarshal(content, &parsed); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "File is not valid JSON: "+err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"cancelled": false,
		"data":      parsed,
		"fileName":  fileInfo.Name,
		"path":      fileInfo.Path,
	})
}

func HandleReadFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}
	if req.Path == "" {
		httputil.WriteError(w, http.StatusBadRequest, "path is required")
		return
	}

	absPath, err := filepath.Abs(req.Path)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "Invalid path: "+err.Error())
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		httputil.WriteError(w, http.StatusNotFound, "File not found: "+err.Error())
		return
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "Failed to read file: "+err.Error())
		return
	}

	encoded := base64.StdEncoding.EncodeToString(content)

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"path":    absPath,
		"name":    filepath.Base(absPath),
		"size":    info.Size(),
		"content": encoded,
	})
}

func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/files/export", HandleExportFile)
	mux.HandleFunc("/api/files/import", HandleImportFile)
	mux.HandleFunc("/api/files/save-dialog", HandleSaveDialog)
	mux.HandleFunc("/api/files/save", HandleSaveFile)
	mux.HandleFunc("/api/files/load-dialog", HandleLoadDialog)
	mux.HandleFunc("/api/files/read", HandleReadFile)
	log.Info().Msg("Registered file endpoints")
}
