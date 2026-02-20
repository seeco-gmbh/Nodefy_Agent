package bridge

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"nodefy/agent/internal/httputil"

	"github.com/rs/zerolog/log"
)

type Handlers struct {
	client *Client
}

func NewHandlers(client *Client) *Handlers {
	return &Handlers{client: client}
}

func readBody(r *http.Request) (map[string]interface{}, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
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

func extractPathParam(path string, prefix string) string {
	trimmed := strings.TrimPrefix(path, prefix)
	trimmed = strings.TrimPrefix(trimmed, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return trimmed
}

func (h *Handlers) HandleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	data, err := readBody(r)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	url, _ := data["url"].(string)
	apiKey, _ := data["apiKey"].(string)

	if url == "" {
		httputil.WriteError(w, http.StatusBadRequest, "url is required")
		return
	}

	if err := h.client.Connect(url, apiKey); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"url":         url,
		"connectedAt": time.Now().UnixMilli(),
	})
}

func (h *Handlers) HandleDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if err := h.client.Disconnect(); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *Handlers) HandleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !h.client.IsConnected() {
		httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"connected": false,
		})
		return
	}

	result, err := h.client.SendAndWait("GetBridgeStatus", map[string]interface{}{}, "BridgeStatus", nil, 5*time.Second)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var status map[string]interface{}
	if err := json.Unmarshal(result, &status); err != nil {
		log.Warn().Err(err).Msg("Failed to parse bridge status response")
	}
	if status == nil {
		status = map[string]interface{}{}
	}
	status["connected"] = true

	httputil.WriteJSON(w, http.StatusOK, status)
}

func (h *Handlers) HandleCreateComponent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	data, err := readBody(r)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	componentType, _ := data["componentType"].(string)
	if componentType == "" {
		httputil.WriteError(w, http.StatusBadRequest, "componentType is required")
		return
	}

	filter := func(method string, payload json.RawMessage) bool {
		if method != "Created" {
			return false
		}
		var p map[string]interface{}
		if err := json.Unmarshal(payload, &p); err != nil {
			return false
		}
		t, _ := p["Type"].(string)
		if t == "" {
			t, _ = p["type"].(string)
		}
		return t == componentType
	}

	result, err := h.client.SendAndWait("CreateComponent", data, "Created", filter, 10*time.Second)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, json.RawMessage(result))
}

func (h *Handlers) HandleUpdateComponent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	componentId := extractPathParam(r.URL.Path, "/api/adapt/component")
	if componentId == "" {
		httputil.WriteError(w, http.StatusBadRequest, "component ID is required")
		return
	}

	data, err := readBody(r)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	data["componentId"] = componentId

	result, err := h.client.SendAndWait("UpdateComponent", data, "Updated", nil, 5*time.Second)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, json.RawMessage(result))
}

func (h *Handlers) HandleDeleteComponent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	componentId := extractPathParam(r.URL.Path, "/api/adapt/component")
	if componentId == "" {
		httputil.WriteError(w, http.StatusBadRequest, "component ID is required")
		return
	}

	payload := map[string]string{"componentId": componentId}
	result, err := h.client.SendAndWait("DeleteComponent", payload, "Deleted", nil, 5*time.Second)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, json.RawMessage(result))
}

func (h *Handlers) HandleGetComponentInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	componentId := extractPathParam(r.URL.Path, "/api/adapt/component")
	if componentId == "" {
		httputil.WriteError(w, http.StatusBadRequest, "component ID is required")
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
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, json.RawMessage(result))
}

func (h *Handlers) HandleGetConnectionInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	containerId := extractPathParam(r.URL.Path, "/api/adapt/component")
	containerId = strings.TrimSuffix(containerId, "/connections")
	if containerId == "" {
		httputil.WriteError(w, http.StatusBadRequest, "container ID is required")
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
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, json.RawMessage(result))
}

func (h *Handlers) HandleAddPort(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/adapt/component/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" {
		httputil.WriteError(w, http.StatusBadRequest, "component ID is required")
		return
	}
	componentId := parts[0]

	data, err := readBody(r)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	data["componentId"] = componentId

	filter := func(method string, payload json.RawMessage) bool {
		if method != "Created" {
			return false
		}
		var p map[string]interface{}
		if err := json.Unmarshal(payload, &p); err != nil {
			return false
		}
		t, _ := p["Type"].(string)
		if t == "" {
			t, _ = p["type"].(string)
		}
		return t == "Port"
	}

	result, err := h.client.SendAndWait("AddPortToComponent", data, "Created", filter, 10*time.Second)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, json.RawMessage(result))
}

func (h *Handlers) HandleDeletePort(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/adapt/component/")
	parts := strings.Split(path, "/")
	if len(parts) < 3 || parts[0] == "" || parts[2] == "" {
		httputil.WriteError(w, http.StatusBadRequest, "component ID and port ID are required")
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
		if err := json.Unmarshal(payload, &p); err != nil {
			return false
		}
		t, _ := p["Type"].(string)
		if t == "" {
			t, _ = p["type"].(string)
		}
		return t == "Port"
	}

	result, err := h.client.SendAndWait("DeletePortFromComponent", payload, "Deleted", filter, 5*time.Second)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, json.RawMessage(result))
}

func (h *Handlers) HandleUpdatePort(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	portId := extractPathParam(r.URL.Path, "/api/adapt/port")
	if portId == "" {
		httputil.WriteError(w, http.StatusBadRequest, "port ID is required")
		return
	}

	data, err := readBody(r)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	data["portId"] = portId

	filter := func(method string, payload json.RawMessage) bool {
		if method != "PortUpdated" {
			return false
		}
		var p map[string]interface{}
		if err := json.Unmarshal(payload, &p); err != nil {
			return false
		}
		id, _ := p["Id"].(string)
		if id == "" {
			id, _ = p["id"].(string)
		}
		return id == portId
	}

	result, err := h.client.SendAndWait("UpdatePort", data, "PortUpdated", filter, 2*time.Second)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, json.RawMessage(result))
}

func (h *Handlers) HandleConnectPorts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	data, err := readBody(r)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	filter := func(method string, payload json.RawMessage) bool {
		if method != "Created" {
			return false
		}
		var p map[string]interface{}
		if err := json.Unmarshal(payload, &p); err != nil {
			return false
		}
		t, _ := p["Type"].(string)
		if t == "" {
			t, _ = p["type"].(string)
		}
		return t == "Connector"
	}

	result, err := h.client.SendAndWait("ConnectPorts", data, "Created", filter, 10*time.Second)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, json.RawMessage(result))
}

func (h *Handlers) HandleExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	componentId := extractPathParam(r.URL.Path, "/api/adapt/execute")
	if componentId == "" {
		httputil.WriteError(w, http.StatusBadRequest, "component ID is required")
		return
	}

	data, err := readBody(r)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	componentType, _ := data["componentType"].(string)
	if componentType == "" {
		componentType = "Network"
	}
	key := strings.ToLower(componentType) + "Id"
	data[key] = componentId

	result, err := h.client.SendAndWait("Execute", data, "ExecutionCompleted", nil, 30*time.Second)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, json.RawMessage(result))
}

func (h *Handlers) HandleWarmUp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	networkId := extractPathParam(r.URL.Path, "/api/adapt/warmup")
	if networkId == "" {
		httputil.WriteError(w, http.StatusBadRequest, "network ID is required")
		return
	}

	payload := map[string]string{"networkId": networkId}
	result, err := h.client.SendAndWait("WarmUp", payload, "ExecutionCompleted", nil, 30*time.Second)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, json.RawMessage(result))
}

func (h *Handlers) HandleExportComponent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	componentId := extractPathParam(r.URL.Path, "/api/adapt/export")
	if componentId == "" {
		httputil.WriteError(w, http.StatusBadRequest, "component ID is required")
		return
	}

	data, err := readBody(r)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	componentType, _ := data["componentType"].(string)

	payload := map[string]string{"id": componentId, "componentType": componentType}
	result, err := h.client.SendAndWait("ExportComponent", payload, "Exported", nil, 5*time.Second)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, json.RawMessage(result))
}

func (h *Handlers) HandleLoadComponent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	data, err := readBody(r)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	result, err := h.client.SendAndWait("Load", data, "Loaded", nil, 10*time.Second)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, json.RawMessage(result))
}

func (h *Handlers) HandleSaveComponent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	componentId := extractPathParam(r.URL.Path, "/api/adapt/save")
	if componentId == "" {
		httputil.WriteError(w, http.StatusBadRequest, "component ID is required")
		return
	}

	data, err := readBody(r)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	path, _ := data["path"].(string)
	payload := map[string]string{"componentId": componentId, "path": path}

	result, err := h.client.SendAndWait("Save", payload, "Saved", nil, 5*time.Second)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, json.RawMessage(result))
}

func (h *Handlers) HandleGetTemplateTypes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
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
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, json.RawMessage(result))
}

func (h *Handlers) HandleAddComponentToContainer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	containerId := extractPathParam(r.URL.Path, "/api/adapt/container")
	containerId = strings.TrimSuffix(containerId, "/add")
	if containerId == "" {
		httputil.WriteError(w, http.StatusBadRequest, "container ID is required")
		return
	}

	data, err := readBody(r)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	childId, _ := data["childId"].(string)
	if childId == "" {
		httputil.WriteError(w, http.StatusBadRequest, "childId is required")
		return
	}

	payload := map[string]string{"containerId": containerId, "childId": childId}
	result, err := h.client.SendAndWait("AddComponentToContainer", payload, "Created", nil, 5*time.Second)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, json.RawMessage(result))
}

func (h *Handlers) HandleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !h.client.IsConnected() {
		httputil.WriteError(w, http.StatusServiceUnavailable, "Not connected to Adapt Bridge")
		return
	}

	result, err := h.client.SendAndWait("GetBridgeStatus", map[string]interface{}{}, "BridgeStatus", nil, 5*time.Second)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, json.RawMessage(result))
}

func (h *Handlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/adapt/connect", h.HandleConnect)
	mux.HandleFunc("/api/adapt/disconnect", h.HandleDisconnect)
	mux.HandleFunc("/api/adapt/status", h.HandleStatus)
	mux.HandleFunc("/api/adapt/sync", h.HandleSync)
	mux.HandleFunc("/api/adapt/templates", h.HandleGetTemplateTypes)
	mux.HandleFunc("/api/adapt/connect-ports", h.HandleConnectPorts)
	mux.HandleFunc("/api/adapt/load", h.HandleLoadComponent)

	mux.HandleFunc("/api/adapt/component/", h.routeComponent)
	mux.HandleFunc("/api/adapt/port/", h.HandleUpdatePort)
	mux.HandleFunc("/api/adapt/execute/", h.HandleExecute)
	mux.HandleFunc("/api/adapt/warmup/", h.HandleWarmUp)
	mux.HandleFunc("/api/adapt/export/", h.HandleExportComponent)
	mux.HandleFunc("/api/adapt/save/", h.HandleSaveComponent)
	mux.HandleFunc("/api/adapt/container/", h.HandleAddComponentToContainer)

	log.Info().Msg("Registered Adapt Bridge REST endpoints")
}

func (h *Handlers) routeComponent(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/adapt/component/")

	if path == "" || r.URL.Path == "/api/adapt/component" || r.URL.Path == "/api/adapt/component/" {
		if r.Method == http.MethodPost {
			h.HandleCreateComponent(w, r)
			return
		}
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	parts := strings.Split(path, "/")

	if len(parts) >= 2 && parts[1] == "port" {
		if r.Method == http.MethodPost {
			h.HandleAddPort(w, r)
			return
		}
		if r.Method == http.MethodDelete && len(parts) >= 3 {
			h.HandleDeletePort(w, r)
			return
		}
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if len(parts) >= 2 && parts[1] == "connections" {
		h.HandleGetConnectionInfo(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.HandleGetComponentInfo(w, r)
	case http.MethodPut:
		h.HandleUpdateComponent(w, r)
	case http.MethodDelete:
		h.HandleDeleteComponent(w, r)
	default:
		httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
