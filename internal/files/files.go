package files

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/rs/zerolog/log"
)

// HandleExportFile handles POST /api/files/export
// Receives JSON data and returns it as a file download.
func HandleExportFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}

	var req struct {
		Data     json.RawMessage `json:"data"`
		Filename string          `json:"filename"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	if len(req.Data) == 0 {
		writeError(w, http.StatusBadRequest, "data field is required")
		return
	}
	if req.Filename == "" {
		req.Filename = "export.json"
	}

	// Pretty-print the JSON for the download
	var parsed interface{}
	if err := json.Unmarshal(req.Data, &parsed); err != nil {
		// If it's not valid JSON, just send raw
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, req.Filename))
		w.WriteHeader(http.StatusOK)
		w.Write(req.Data)
		return
	}

	pretty, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to format JSON")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, req.Filename))
	w.WriteHeader(http.StatusOK)
	w.Write(pretty)
}

// HandleImportFile handles POST /api/files/import
// Receives a multipart file upload or raw JSON body and returns the parsed JSON content.
func HandleImportFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	contentType := r.Header.Get("Content-Type")

	var fileBytes []byte
	var fileName string

	if len(contentType) >= 19 && contentType[:19] == "multipart/form-data" {
		// Multipart upload
		if err := r.ParseMultipartForm(32 << 20); err != nil { // 32MB max
			writeError(w, http.StatusBadRequest, "Failed to parse multipart form: "+err.Error())
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "No file field in form: "+err.Error())
			return
		}
		defer file.Close()

		fileName = header.Filename
		fileBytes, err = io.ReadAll(file)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to read uploaded file")
			return
		}
	} else {
		// Raw JSON body with file content
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Failed to read request body")
			return
		}
		fileBytes = body
		fileName = "upload.json"
	}

	// Validate it's valid JSON
	var parsed interface{}
	if err := json.Unmarshal(fileBytes, &parsed); err != nil {
		writeError(w, http.StatusBadRequest, "Uploaded file is not valid JSON: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data":     parsed,
		"fileName": fileName,
	})
}

// RegisterRoutes registers file endpoints on the given mux
func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/files/export", HandleExportFile)
	mux.HandleFunc("/api/files/import", HandleImportFile)
	log.Info().Msg("Registered file export/import endpoints")
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
