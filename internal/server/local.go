package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// Message types (same as websocket client for compatibility)
const (
	TypeWatch        = "watch"
	TypeUnwatch      = "unwatch"
	TypeFileChanged  = "file_changed"
	TypeWatchStarted = "watch_started"
	TypeHeartbeat    = "heartbeat"
	TypeAck          = "ack"
	TypeError        = "error"
	TypeStatus       = "status"
)

// Message represents a WebSocket message
type Message struct {
	Type      string `json:"type"`
	Path      string `json:"path,omitempty"`
	Name      string `json:"name,omitempty"`
	Operation string `json:"operation,omitempty"`
	Content   string `json:"content,omitempty"`
	Recursive bool   `json:"recursive,omitempty"`
	Error     string `json:"error,omitempty"`
	Connected bool   `json:"connected,omitempty"`
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

// Start starts the local WebSocket server
func (s *LocalServer) Start() error {
	mux := http.NewServeMux()

	// Health/status endpoint
	mux.HandleFunc("/status", s.handleStatus)

	// WebSocket endpoint
	mux.HandleFunc("/ws", s.handleWebSocket)

	s.server = &http.Server{
		Addr:    "127.0.0.1:" + s.port,
		Handler: s.corsMiddleware(mux),
	}

	s.running = true

	log.Info().Str("port", s.port).Msg("Starting local WebSocket server")

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("Local server error")
		}
	}()

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
func (s *LocalServer) SendFileWithContent(path, name, operation string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		log.Error().Err(err).Str("path", path).Msg("Failed to read file for broadcast")
		return s.SendFileChanged(path, name, operation)
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
