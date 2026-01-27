package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// Message types (same as websocket client for compatibility)
const (
	TypeWatch          = "watch"
	TypeUnwatch        = "unwatch"
	TypeFileChanged    = "file_changed"
	TypeWatchStarted   = "watch_started"
	TypeHeartbeat      = "heartbeat"
	TypeAck            = "ack"
	TypeError          = "error"
	TypeStatus         = "status"
	TypeOpenFileDialog = "open_file_dialog"
	TypeFileSelected   = "file_selected"
	TypeDialogCanceled = "dialog_canceled"
)

// Message represents a WebSocket message
type Message struct {
	Type      string   `json:"type"`
	Path      string   `json:"path,omitempty"`
	Name      string   `json:"name,omitempty"`
	Operation string   `json:"operation,omitempty"`
	Content   string   `json:"content,omitempty"`
	Recursive bool     `json:"recursive,omitempty"`
	Error     string   `json:"error,omitempty"`
	Connected bool     `json:"connected,omitempty"`
	Size      int64    `json:"size,omitempty"`      // File size in bytes
	Title     string   `json:"title,omitempty"`     // Dialog title
	Filters   []string `json:"filters,omitempty"`   // File type filters (e.g., ["*.xlsx", "*.csv"])
	RequestID string   `json:"request_id,omitempty"` // To match request/response
}

// MessageHandler is called when a message is received from a client
type MessageHandler func(msg Message)

// FileEventSender is used to send file events to connected clients
type FileEventSender interface {
	SendFileChanged(path, name, operation string) error
	SendFileWithContent(path, name, operation string) error
	SendWatchStarted(path string) error
}

// LocalServer is a WebSocket server for local browser connections
type LocalServer struct {
	port           string
	upgrader       websocket.Upgrader
	clients        map[*websocket.Conn]bool
	clientsMu      sync.RWMutex
	messageHandler MessageHandler
	running        bool
	server         *http.Server
}

// NewLocalServer creates a new local WebSocket server
func NewLocalServer(port string, handler MessageHandler) *LocalServer {
	return &LocalServer{
		port:    port,
		clients: make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				// Allow localhost and nodefy.app
				allowedOrigins := []string{
					"http://localhost:3000",
					"http://localhost:3001",
					"http://localhost:5173",
					"http://127.0.0.1:3000",
					"http://127.0.0.1:3001",
					"http://127.0.0.1:5173",
					"https://nodefy.app",
					"https://www.nodefy.app",
					"https://app.nodefy.app",
				}
				for _, allowed := range allowedOrigins {
					if origin == allowed {
						return true
					}
				}
				// Also allow if no origin (direct connection)
				return origin == ""
			},
		},
		messageHandler: handler,
	}
}

// CheckPortAvailable checks if the port is available
func CheckPortAvailable(port string) error {
	addr := "127.0.0.1:" + port
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	ln.Close()
	return nil
}

// CheckExistingAgent checks if an agent is already running on the port
func CheckExistingAgent(port string) bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get("http://127.0.0.1:" + port + "/status")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Start starts the local WebSocket server
func (s *LocalServer) Start() error {
	// Check if port is available
	if err := CheckPortAvailable(s.port); err != nil {
		if CheckExistingAgent(s.port) {
			return fmt.Errorf("another Nodefy Agent is already running on port %s", s.port)
		}
		return fmt.Errorf("port %s is already in use: %w", s.port, err)
	}

	mux := http.NewServeMux()

	// Health/status endpoint
	mux.HandleFunc("/status", s.handleStatus)

	// WebSocket endpoint
	mux.HandleFunc("/ws", s.handleWebSocket)

	s.server = &http.Server{
		Addr:    "127.0.0.1:" + s.port,
		Handler: s.corsMiddleware(mux),
	}

	// Start server and wait for it to be ready
	errCh := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Give the server a moment to start or fail
	select {
	case err := <-errCh:
		return fmt.Errorf("failed to start server: %w", err)
	case <-time.After(100 * time.Millisecond):
		// Server started successfully
	}

	s.running = true
	log.Info().Str("port", s.port).Msg("WebSocket server started")

	return nil
}

// Stop stops the local server
func (s *LocalServer) Stop() {
	s.running = false
	if s.server != nil {
		s.server.Close()
	}

	s.clientsMu.Lock()
	for conn := range s.clients {
		conn.Close()
	}
	s.clients = make(map[*websocket.Conn]bool)
	s.clientsMu.Unlock()
}

// corsMiddleware adds CORS headers
func (s *LocalServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleStatus returns the server status
func (s *LocalServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "running",
		"version": "0.1.0",
		"clients": len(s.clients),
	})
}

// handleWebSocket handles WebSocket connections
func (s *LocalServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("WebSocket upgrade failed")
		return
	}

	s.clientsMu.Lock()
	s.clients[conn] = true
	s.clientsMu.Unlock()

	log.Info().Str("remote", conn.RemoteAddr().String()).Msg("Client connected")

	// Send connected status
	s.sendToClient(conn, Message{Type: TypeStatus, Connected: true})

	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, conn)
		s.clientsMu.Unlock()
		conn.Close()
		log.Info().Str("remote", conn.RemoteAddr().String()).Msg("Client disconnected")
	}()

	// Read messages
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Error().Err(err).Msg("WebSocket read error")
			}
			break
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Error().Err(err).Msg("Failed to parse message")
			continue
		}

		log.Debug().Str("type", msg.Type).Str("path", msg.Path).Msg("Received message from client")

		// Send ack
		s.sendToClient(conn, Message{Type: TypeAck})

		// Handle message
		if s.messageHandler != nil {
			s.messageHandler(msg)
		}
	}
}

// sendToClient sends a message to a specific client
func (s *LocalServer) sendToClient(conn *websocket.Conn, msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

// Broadcast sends a message to all connected clients
func (s *LocalServer) Broadcast(msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal broadcast message")
		return
	}

	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	for conn := range s.clients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Error().Err(err).Msg("Failed to send to client")
		}
	}
}

// SendFileChanged broadcasts a file changed event
func (s *LocalServer) SendFileChanged(path, name, operation string) error {
	s.Broadcast(Message{
		Type:      TypeFileChanged,
		Path:      path,
		Name:      name,
		Operation: operation,
	})
	return nil
}

// SendFileWithContent broadcasts a file changed event with content
// Uses retry logic to handle file locking (e.g., Excel saves)
func (s *LocalServer) SendFileWithContent(path, name, operation string) error {
	var content []byte
	var err error
	
	// Retry up to 5 times with increasing delay (handles file locking from Excel, etc.)
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		content, err = os.ReadFile(path)
		if err == nil {
			break
		}
		
		// Wait before retry (100ms, 200ms, 300ms, 400ms, 500ms)
		delay := time.Duration(100*(i+1)) * time.Millisecond
		log.Debug().
			Err(err).
			Str("path", path).
			Int("attempt", i+1).
			Dur("delay", delay).
			Msg("File locked, retrying...")
		time.Sleep(delay)
	}
	
	if err != nil {
		log.Error().Err(err).Str("path", path).Int("retries", maxRetries).Msg("Failed to read file after retries")
		// Don't send event without content - it would be ignored by frontend anyway
		return err
	}

	s.Broadcast(Message{
		Type:      TypeFileChanged,
		Path:      path,
		Name:      name,
		Operation: operation,
		Content:   base64.StdEncoding.EncodeToString(content),
	})
	return nil
}

// SendWatchStarted broadcasts a watch started event
func (s *LocalServer) SendWatchStarted(path string) error {
	s.Broadcast(Message{
		Type: TypeWatchStarted,
		Path: path,
	})
	return nil
}

// SendFileSelected broadcasts a file selected event with content
func (s *LocalServer) SendFileSelected(path, name string, size int64, requestID string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		log.Error().Err(err).Str("path", path).Msg("Failed to read selected file")
		// Send without content
		s.Broadcast(Message{
			Type:      TypeFileSelected,
			Path:      path,
			Name:      name,
			Size:      size,
			RequestID: requestID,
		})
		return err
	}

	s.Broadcast(Message{
		Type:      TypeFileSelected,
		Path:      path,
		Name:      name,
		Size:      size,
		Content:   base64.StdEncoding.EncodeToString(content),
		RequestID: requestID,
	})
	return nil
}

// ClientCount returns the number of connected clients
func (s *LocalServer) ClientCount() int {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	return len(s.clients)
}

// IsRunning returns whether the server is running
func (s *LocalServer) IsRunning() bool {
	return s.running
}
