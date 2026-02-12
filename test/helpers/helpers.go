package helpers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// MockBridgeServer creates a test WebSocket server with a custom handler
func MockBridgeServer(handler func(conn *websocket.Conn)) *httptest.Server {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		handler(conn)
	}))
	return server
}

// WsURL converts an httptest.Server URL to a WebSocket URL
func WsURL(server *httptest.Server) string {
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

// EchoHandler creates a mock bridge handler that responds with a fixed method/payload
// matching the requestId from the incoming message
func EchoHandler(method string, payload string) func(conn *websocket.Conn) {
	return func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				RequestID string `json:"requestId"`
			}
			json.Unmarshal(data, &msg)

			resp := map[string]interface{}{
				"method":    method,
				"payload":   json.RawMessage(payload),
				"requestId": msg.RequestID,
			}
			respData, _ := json.Marshal(resp)
			conn.WriteMessage(websocket.TextMessage, respData)
		}
	}
}

// ConcurrentEchoHandler is like EchoHandler but safe for concurrent writes
func ConcurrentEchoHandler(method string, payload string) func(conn *websocket.Conn) {
	var mu sync.Mutex
	return func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				RequestID string `json:"requestId"`
			}
			json.Unmarshal(data, &msg)

			resp := map[string]interface{}{
				"method":    method,
				"payload":   json.RawMessage(payload),
				"requestId": msg.RequestID,
			}
			respData, _ := json.Marshal(resp)
			mu.Lock()
			conn.WriteMessage(websocket.TextMessage, respData)
			mu.Unlock()
		}
	}
}

// SilentHandler keeps the connection open without responding
func SilentHandler() func(conn *websocket.Conn) {
	return func(conn *websocket.Conn) {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}
}

// AssertStatusCode checks the HTTP status code of a recorder
func AssertStatusCode(rr *httptest.ResponseRecorder, expected int) error {
	if rr.Code != expected {
		return fmt.Errorf("expected status %d, got %d: %s", expected, rr.Code, rr.Body.String())
	}
	return nil
}

// ParseJSONResponse parses a recorder's body into a map
func ParseJSONResponse(rr *httptest.ResponseRecorder) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}
	return result, nil
}

// WaitForCondition polls a condition with timeout
func WaitForCondition(condition func() bool, timeout, interval time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(interval)
	}
	return false
}

// GenerateTestToken returns a unique test token
func GenerateTestToken() string {
	return fmt.Sprintf("test-token-%d", time.Now().UnixNano())
}
