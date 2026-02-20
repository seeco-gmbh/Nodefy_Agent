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

type Message struct {
	Type      string   `json:"type"`
	Path      string   `json:"path,omitempty"`
	Name      string   `json:"name,omitempty"`
	Operation string   `json:"operation,omitempty"`
	Content   string   `json:"content,omitempty"`
	Recursive bool     `json:"recursive,omitempty"`
	Error     string   `json:"error,omitempty"`
	Connected bool     `json:"connected,omitempty"`
	Size      int64    `json:"size,omitempty"`
	Title     string   `json:"title,omitempty"`
	Filters   []string `json:"filters,omitempty"`
	RequestID string   `json:"request_id,omitempty"`
}

type MessageHandler func(msg Message)

type RouteRegistrar func(mux *http.ServeMux)

type wsClient struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *wsClient) writeMessage(messageType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}
	return c.conn.WriteMessage(messageType, data)
}

type LocalServer struct {
	port            string
	version         string
	upgrader        websocket.Upgrader
	clients         map[*wsClient]bool
	clientsMu       sync.RWMutex
	messageHandler  MessageHandler
	running         bool
	server          *http.Server
	routeRegistrars []RouteRegistrar
}

func NewLocalServer(port string, version string, handler MessageHandler) *LocalServer {
	return &LocalServer{
		port:    port,
		version: version,
		clients: make(map[*wsClient]bool),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				return origin == "" || isAllowedOrigin(origin)
			},
		},
		messageHandler: handler,
	}
}

func checkPortAvailable(port string) error {
	addr := "127.0.0.1:" + port
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	// Probe listener — close error is not actionable here.
	_ = ln.Close()
	return nil
}

func checkExistingAgent(port string) bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get("http://127.0.0.1:" + port + "/status")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (s *LocalServer) AddRouteRegistrar(registrar RouteRegistrar) {
	s.routeRegistrars = append(s.routeRegistrars, registrar)
}

func (s *LocalServer) Start() error {
	if err := checkPortAvailable(s.port); err != nil {
		if checkExistingAgent(s.port) {
			return fmt.Errorf("another Nodefy Agent is already running on port %s", s.port)
		}
		return fmt.Errorf("port %s is already in use: %w", s.port, err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/ws", s.handleWebSocket)

	for _, registrar := range s.routeRegistrars {
		registrar(mux)
	}

	s.server = &http.Server{
		Addr:    "127.0.0.1:" + s.port,
		Handler: s.corsMiddleware(mux),
	}

	errCh := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("failed to start server: %w", err)
	case <-time.After(100 * time.Millisecond):
	}

	s.running = true
	log.Info().Str("port", s.port).Msg("WebSocket server started")

	return nil
}

func (s *LocalServer) Stop() {
	s.running = false
	if s.server != nil {
		if err := s.server.Close(); err != nil {
			log.Warn().Err(err).Msg("Error closing HTTP server")
		}
	}

	s.clientsMu.Lock()
	for c := range s.clients {
		if err := c.conn.Close(); err != nil {
			log.Warn().Err(err).Msg("Error closing WebSocket client connection")
		}
	}
	s.clients = make(map[*wsClient]bool)
	s.clientsMu.Unlock()
}

func (s *LocalServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
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

var allowedOrigins = []string{
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

func isAllowedOrigin(origin string) bool {
	for _, allowed := range allowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}

func (s *LocalServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "running",
		"version": s.version,
		"clients": len(s.clients),
	}); err != nil {
		log.Warn().Err(err).Msg("Failed to write status response")
	}
}

func (s *LocalServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("WebSocket upgrade failed")
		return
	}

	client := &wsClient{conn: conn}

	s.clientsMu.Lock()
	s.clients[client] = true
	s.clientsMu.Unlock()

	log.Info().Str("remote", conn.RemoteAddr().String()).Msg("Client connected")

	s.sendToClient(client, Message{Type: TypeStatus, Connected: true})

	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, client)
		s.clientsMu.Unlock()
		if err := conn.Close(); err != nil {
			log.Warn().Err(err).Msg("Error closing WebSocket connection on disconnect")
		}
		log.Info().Str("remote", conn.RemoteAddr().String()).Msg("Client disconnected")
	}()

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

		s.sendToClient(client, Message{Type: TypeAck})

		if s.messageHandler != nil {
			s.messageHandler(msg)
		}
	}
}

func (s *LocalServer) sendToClient(client *wsClient, msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return client.writeMessage(websocket.TextMessage, data)
}

func (s *LocalServer) Broadcast(msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal broadcast message")
		return
	}

	s.BroadcastRaw(data)
}

func (s *LocalServer) BroadcastRaw(data []byte) {
	s.clientsMu.RLock()
	clients := make([]*wsClient, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.clientsMu.RUnlock()

	for _, c := range clients {
		if err := c.writeMessage(websocket.TextMessage, data); err != nil {
			log.Error().Err(err).Msg("Failed to send to client, removing")
			s.clientsMu.Lock()
			delete(s.clients, c)
			s.clientsMu.Unlock()
			if err := c.conn.Close(); err != nil {
				log.Warn().Err(err).Msg("Error closing unresponsive client connection")
			}
		}
	}
}

func (s *LocalServer) SendFileChanged(path, name, operation string) error {
	s.Broadcast(Message{
		Type:      TypeFileChanged,
		Path:      path,
		Name:      name,
		Operation: operation,
	})
	return nil
}

// Retry with backoff to handle file locking (e.g. Excel saves)
func (s *LocalServer) SendFileWithContent(path, name, operation string) error {
	var content []byte
	var err error

	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		content, err = os.ReadFile(path)
		if err == nil {
			break
		}

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
		return err
	}

	s.Broadcast(Message{
		Type:      TypeFileChanged,
		Path:      path,
		Name:      name,
		Operation: operation,
		Content:   base64.StdEncoding.EncodeToString(content),
		Size:      int64(len(content)),
	})
	return nil
}

func (s *LocalServer) SendWatchStarted(path string) error {
	s.Broadcast(Message{
		Type: TypeWatchStarted,
		Path: path,
	})
	return nil
}

func (s *LocalServer) SendFileSelected(path, name string, size int64, requestID string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		log.Error().Err(err).Str("path", path).Msg("Failed to read selected file")
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

func (s *LocalServer) ClientCount() int {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	return len(s.clients)
}

func (s *LocalServer) IsRunning() bool {
	return s.running
}
