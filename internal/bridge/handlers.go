package bridge

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// Handlers provides HTTP handlers for Adapt Bridge operations
type Handlers struct {
	client *Client
}

// NewHandlers creates a new Handlers instance
func NewHandlers(client *Client) *Handlers {
	return &Handlers{client: client}
}

// writeJSON writes a JSON response
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeError writes a JSON error response
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// readBody reads and parses JSON request body into a map
func readBody(r *http.Request) (map[string]interface{}, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return map[string]interface{}{}, nil
	}
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	return data, nil
}

// extractPathParam extracts the last segment from a URL path
// e.g. /api/adapt/component/abc-123 → abc-123
func extractPathParam(path string, prefix string) string {
	trimmed := strings.TrimPrefix(path, prefix)
	trimmed = strings.TrimPrefix(trimmed, "/")
	// Handle nested paths like /component/{id}/port/{portId}
	parts := strings.Split(trimmed, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return trimmed
}

// HandleConnect handles POST /api/adapt/connect
func (h *Handlers) HandleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	data, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	url, _ := data["url"].(string)
	apiKey, _ := data["apiKey"].(string)

	if url == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}

	if err := h.client.Connect(url, apiKey); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"url":         url,
		"connectedAt": time.Now().UnixMilli(),
	})
}

// HandleDisconnect handles POST /api/adapt/disconnect
func (h *Handlers) HandleDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if err := h.client.Disconnect(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// HandleStatus handles GET /api/adapt/status
func (h *Handlers) HandleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !h.client.IsConnected() {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"connected": false,
		})
		return
	}

	result, err := h.client.SendAndWait("GetBridgeStatus", map[string]interface{}{}, "BridgeStatus", nil, 5*time.Second)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Merge connection info with bridge status
	var status map[string]interface{}
	json.Unmarshal(result, &status)
	if status == nil {
		status = map[string]interface{}{}
	}
	status["connected"] = true

	writeJSON(w, http.StatusOK, status)
}

// HandleCreateComponent handles POST /api/adapt/component
func (h *Handlers) HandleCreateComponent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	data, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	componentType, _ := data["componentType"].(string)
	if componentType == "" {
		writeError(w, http.StatusBadRequest, "componentType is required")
		return
	}

	filter := func(method string, payload json.RawMessage) bool {
		if method != "Created" {
			return false
		}
		var p map[string]interface{}
		json.Unmarshal(payload, &p)
		t, _ := p["Type"].(string)
		if t == "" {
			t, _ = p["type"].(string)
		}
		return t == componentType
	}

	result, err := h.client.SendAndWait("CreateComponent", data, "Created", filter, 10*time.Second)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, json.RawMessage(result))
}

// HandleUpdateComponent handles PUT /api/adapt/component/{id}
func (h *Handlers) HandleUpdateComponent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	componentId := extractPathParam(r.URL.Path, "/api/adapt/component")
	if componentId == "" {
		writeError(w, http.StatusBadRequest, "component ID is required")
		return
	}

	data, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	data["componentId"] = componentId

	result, err := h.client.SendAndWait("UpdateComponent", data, "Updated", nil, 5*time.Second)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, json.RawMessage(result))
}

// HandleDeleteComponent handles DELETE /api/adapt/component/{id}
func (h *Handlers) HandleDeleteComponent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	componentId := extractPathParam(r.URL.Path, "/api/adapt/component")
	if componentId == "" {
		writeError(w, http.StatusBadRequest, "component ID is required")
		return
	}

	payload := map[string]string{"componentId": componentId}
	result, err := h.client.SendAndWait("DeleteComponent", payload, "Deleted", nil, 5*time.Second)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, json.RawMessage(result))
}

// HandleGetComponentInfo handles GET /api/adapt/component/{id}
func (h *Handlers) HandleGetComponentInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	componentId := extractPathParam(r.URL.Path, "/api/adapt/component")
	if componentId == "" {
		writeError(w, http.StatusBadRequest, "component ID is required")
		return
	}

	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "details"
	}

	expectedResponse := "ComponentDetails"
	switch mode {
	case "ports":
		expectedResponse = "ComponentPorts"
	case "port-details":
		expectedResponse = "PortDetails"
	}

	payload := map[string]string{"componentId": componentId, "mode": mode}
	portId := r.URL.Query().Get("portId")
	if portId != "" {
		payload["portId"] = portId
	}

	result, err := h.client.SendAndWait("GetComponentInfo", payload, expectedResponse, nil, 5*time.Second)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, json.RawMessage(result))
}

// HandleGetConnectionInfo handles GET /api/adapt/component/{id}/connections
func (h *Handlers) HandleGetConnectionInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	containerId := extractPathParam(r.URL.Path, "/api/adapt/component")
	// Strip /connections suffix
	containerId = strings.TrimSuffix(containerId, "/connections")
	if containerId == "" {
		writeError(w, http.StatusBadRequest, "container ID is required")
		return
	}

	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "connections"
	}

	expectedResponse := "Connections"
	if mode == "available-ports" {
		expectedResponse = "AvailablePorts"
	}

	payload := map[string]string{"containerId": containerId, "mode": mode}
	result, err := h.client.SendAndWait("GetConnectionInfo", payload, expectedResponse, nil, 5*time.Second)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, json.RawMessage(result))
}

// HandleAddPort handles POST /api/adapt/component/{id}/port
func (h *Handlers) HandleAddPort(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Extract component ID from path: /api/adapt/component/{id}/port
	path := strings.TrimPrefix(r.URL.Path, "/api/adapt/component/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "component ID is required")
		return
	}
	componentId := parts[0]

	data, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	data["componentId"] = componentId

	filter := func(method string, payload json.RawMessage) bool {
		if method != "Created" {
			return false
		}
		var p map[string]interface{}
		json.Unmarshal(payload, &p)
		t, _ := p["Type"].(string)
		if t == "" {
			t, _ = p["type"].(string)
		}
		return t == "Port"
	}

	result, err := h.client.SendAndWait("AddPortToComponent", data, "Created", filter, 10*time.Second)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, json.RawMessage(result))
}

// HandleDeletePort handles DELETE /api/adapt/component/{id}/port/{portId}
func (h *Handlers) HandleDeletePort(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Extract IDs from path: /api/adapt/component/{id}/port/{portId}
	path := strings.TrimPrefix(r.URL.Path, "/api/adapt/component/")
	parts := strings.Split(path, "/")
	if len(parts) < 3 || parts[0] == "" || parts[2] == "" {
		writeError(w, http.StatusBadRequest, "component ID and port ID are required")
		return
	}
	componentId := parts[0]
	portId := parts[2]

	payload := map[string]string{"componentId": componentId, "portId": portId}

	filter := func(method string, payload json.RawMessage) bool {
		if method != "Deleted" {
			return false
		}
		var p map[string]interface{}
		json.Unmarshal(payload, &p)
		t, _ := p["Type"].(string)
		if t == "" {
			t, _ = p["type"].(string)
		}
		return t == "Port"
	}

	result, err := h.client.SendAndWait("DeletePortFromComponent", payload, "Deleted", filter, 5*time.Second)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, json.RawMessage(result))
}

// HandleUpdatePort handles PUT /api/adapt/port/{id}
func (h *Handlers) HandleUpdatePort(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	portId := extractPathParam(r.URL.Path, "/api/adapt/port")
	if portId == "" {
		writeError(w, http.StatusBadRequest, "port ID is required")
		return
	}

	data, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	data["portId"] = portId

	filter := func(method string, payload json.RawMessage) bool {
		if method != "PortUpdated" {
			return false
		}
		var p map[string]interface{}
		json.Unmarshal(payload, &p)
		id, _ := p["Id"].(string)
		if id == "" {
			id, _ = p["id"].(string)
		}
		return id == portId
	}

	result, err := h.client.SendAndWait("UpdatePort", data, "PortUpdated", filter, 2*time.Second)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, json.RawMessage(result))
}

// HandleConnectPorts handles POST /api/adapt/connect-ports
func (h *Handlers) HandleConnectPorts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	data, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	filter := func(method string, payload json.RawMessage) bool {
		if method != "Created" {
			return false
		}
		var p map[string]interface{}
		json.Unmarshal(payload, &p)
		t, _ := p["Type"].(string)
		if t == "" {
			t, _ = p["type"].(string)
		}
		return t == "Connector"
	}

	result, err := h.client.SendAndWait("ConnectPorts", data, "Created", filter, 10*time.Second)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, json.RawMessage(result))
}

// HandleExecute handles POST /api/adapt/execute/{id}
func (h *Handlers) HandleExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	componentId := extractPathParam(r.URL.Path, "/api/adapt/execute")
	if componentId == "" {
		writeError(w, http.StatusBadRequest, "component ID is required")
		return
	}

	data, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Determine component type and set the appropriate ID field
	componentType, _ := data["componentType"].(string)
	if componentType == "" {
		componentType = "Network"
	}
	key := strings.ToLower(componentType) + "Id"
	data[key] = componentId

	result, err := h.client.SendAndWait("Execute", data, "ExecutionCompleted", nil, 30*time.Second)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, json.RawMessage(result))
}

// HandleWarmUp handles POST /api/adapt/warmup/{id}
func (h *Handlers) HandleWarmUp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	networkId := extractPathParam(r.URL.Path, "/api/adapt/warmup")
	if networkId == "" {
		writeError(w, http.StatusBadRequest, "network ID is required")
		return
	}

	payload := map[string]string{"networkId": networkId}
	result, err := h.client.SendAndWait("WarmUp", payload, "ExecutionCompleted", nil, 30*time.Second)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, json.RawMessage(result))
}

// HandleExportComponent handles POST /api/adapt/export/{id}
func (h *Handlers) HandleExportComponent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	componentId := extractPathParam(r.URL.Path, "/api/adapt/export")
	if componentId == "" {
		writeError(w, http.StatusBadRequest, "component ID is required")
		return
	}

	data, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	componentType, _ := data["componentType"].(string)

	payload := map[string]string{"id": componentId, "componentType": componentType}
	result, err := h.client.SendAndWait("ExportComponent", payload, "Exported", nil, 5*time.Second)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, json.RawMessage(result))
}

// HandleLoadComponent handles POST /api/adapt/load
func (h *Handlers) HandleLoadComponent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	data, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	result, err := h.client.SendAndWait("Load", data, "Loaded", nil, 10*time.Second)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, json.RawMessage(result))
}

// HandleSaveComponent handles POST /api/adapt/save/{id}
func (h *Handlers) HandleSaveComponent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	componentId := extractPathParam(r.URL.Path, "/api/adapt/save")
	if componentId == "" {
		writeError(w, http.StatusBadRequest, "component ID is required")
		return
	}

	data, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	path, _ := data["path"].(string)
	payload := map[string]string{"componentId": componentId, "path": path}

	result, err := h.client.SendAndWait("Save", payload, "Saved", nil, 5*time.Second)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, json.RawMessage(result))
}

// HandleGetTemplateTypes handles GET /api/adapt/templates
func (h *Handlers) HandleGetTemplateTypes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	templateType := r.URL.Query().Get("type")
	payload := map[string]interface{}{}
	expectedResponse := "TemplateTypes"

	if templateType != "" {
		payload["templateType"] = templateType
		expectedResponse = "TemplateTypeDetails"
	}

	result, err := h.client.SendAndWait("GetTemplateTypes", payload, expectedResponse, nil, 10*time.Second)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, json.RawMessage(result))
}

// HandleAddComponentToContainer handles POST /api/adapt/container/{id}/add
func (h *Handlers) HandleAddComponentToContainer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	containerId := extractPathParam(r.URL.Path, "/api/adapt/container")
	// Strip /add suffix
	containerId = strings.TrimSuffix(containerId, "/add")
	if containerId == "" {
		writeError(w, http.StatusBadRequest, "container ID is required")
		return
	}

	data, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	childId, _ := data["childId"].(string)
	if childId == "" {
		writeError(w, http.StatusBadRequest, "childId is required")
		return
	}

	payload := map[string]string{"containerId": containerId, "childId": childId}
	result, err := h.client.SendAndWait("AddComponentToContainer", payload, "Created", nil, 5*time.Second)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, json.RawMessage(result))
}

// HandleSync handles POST /api/adapt/sync — returns bridge status for full component sync
func (h *Handlers) HandleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !h.client.IsConnected() {
		writeError(w, http.StatusServiceUnavailable, "Not connected to Adapt Bridge")
		return
	}

	result, err := h.client.SendAndWait("GetBridgeStatus", map[string]interface{}{}, "BridgeStatus", nil, 5*time.Second)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, json.RawMessage(result))
}

// RegisterRoutes registers all bridge REST endpoints on the given mux
func (h *Handlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/adapt/connect", h.HandleConnect)
	mux.HandleFunc("/api/adapt/disconnect", h.HandleDisconnect)
	mux.HandleFunc("/api/adapt/status", h.HandleStatus)
	mux.HandleFunc("/api/adapt/sync", h.HandleSync)
	mux.HandleFunc("/api/adapt/templates", h.HandleGetTemplateTypes)
	mux.HandleFunc("/api/adapt/connect-ports", h.HandleConnectPorts)
	mux.HandleFunc("/api/adapt/load", h.HandleLoadComponent)

	// Dynamic routes — use prefix matching
	mux.HandleFunc("/api/adapt/component/", h.routeComponent)
	mux.HandleFunc("/api/adapt/port/", h.HandleUpdatePort)
	mux.HandleFunc("/api/adapt/execute/", h.HandleExecute)
	mux.HandleFunc("/api/adapt/warmup/", h.HandleWarmUp)
	mux.HandleFunc("/api/adapt/export/", h.HandleExportComponent)
	mux.HandleFunc("/api/adapt/save/", h.HandleSaveComponent)
	mux.HandleFunc("/api/adapt/container/", h.HandleAddComponentToContainer)

	log.Info().Msg("Registered Adapt Bridge REST endpoints")
}

// routeComponent routes /api/adapt/component/* requests based on method and sub-path
func (h *Handlers) routeComponent(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/adapt/component/")

	// POST /api/adapt/component (no ID) — create
	if path == "" || r.URL.Path == "/api/adapt/component" || r.URL.Path == "/api/adapt/component/" {
		if r.Method == http.MethodPost {
			h.HandleCreateComponent(w, r)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	parts := strings.Split(path, "/")

	// /api/adapt/component/{id}/port — add port
	// /api/adapt/component/{id}/port/{portId} — delete port
	if len(parts) >= 2 && parts[1] == "port" {
		if r.Method == http.MethodPost {
			h.HandleAddPort(w, r)
			return
		}
		if r.Method == http.MethodDelete && len(parts) >= 3 {
			h.HandleDeletePort(w, r)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// /api/adapt/component/{id}/connections
	if len(parts) >= 2 && parts[1] == "connections" {
		h.HandleGetConnectionInfo(w, r)
		return
	}

	// /api/adapt/component/{id} — GET (info), PUT (update), DELETE (delete)
	switch r.Method {
	case http.MethodGet:
		h.HandleGetComponentInfo(w, r)
	case http.MethodPut:
		h.HandleUpdateComponent(w, r)
	case http.MethodDelete:
		h.HandleDeleteComponent(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
