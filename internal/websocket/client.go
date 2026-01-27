package websocket

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// Message types for communication with the Orchestrator
const (
	TypeFileChanged   = "file_changed"
	TypeFileAdded     = "file_added"
	TypeFileDeleted   = "file_deleted"
	TypeWatchStarted  = "watch_started"
	TypeWatchStopped  = "watch_stopped"
	TypeHeartbeat     = "heartbeat"
	TypeWatch         = "watch"
	TypeUnwatch       = "unwatch"
	TypeUploadRequest = "upload_request"
	TypeAck           = "ack"
	TypeError         = "error"
	TypeConnected     = "connected"
)

// Message represents a WebSocket message
type Message struct {
	Type      string      `json:"type"`
	Path      string      `json:"path,omitempty"`
	Name      string      `json:"name,omitempty"`
	Event     string      `json:"event,omitempty"`
	Content   string      `json:"content,omitempty"`   // Base64 encoded file content
	Size      int64       `json:"size,omitempty"`
	Recursive bool        `json:"recursive,omitempty"`
	Error     string      `json:"error,omitempty"`
	Data      interface{} `json:"data,omitempty"`
}

// MessageHandler is called when a message is received from the server
type MessageHandler func(msg Message)

// Client is a WebSocket client for communicating with the Orchestrator
type Client struct {
	url            string
	sessionKey     string
	conn           *websocket.Conn
	handler        MessageHandler
	reconnectDelay time.Duration
	mu             sync.Mutex
	done           chan struct{}
	connected      bool
}

// NewClient creates a new WebSocket client
func NewClient(url, sessionKey string, reconnectDelay int, handler MessageHandler) *Client {
	return &Client{
		url:            url,
		sessionKey:     sessionKey,
		handler:        handler,
		reconnectDelay: time.Duration(reconnectDelay) * time.Second,
		done:           make(chan struct{}),
	}
}

// Connect establishes the WebSocket connection
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Add session key to URL
	fullURL := fmt.Sprintf("%s/%s", c.url, c.sessionKey)

	header := http.Header{}
	header.Set("X-Session-Key", c.sessionKey)

	log.Info().Str("url", fullURL).Msg("Connecting to Orchestrator")

	conn, resp, err := websocket.DefaultDialer.Dial(fullURL, header)
	if err != nil {
		if resp != nil {
			log.Error().Int("status", resp.StatusCode).Msg("Connection failed")
		}
		return fmt.Errorf("failed to connect: %w", err)
	}

	c.conn = conn
	c.connected = true

	log.Info().Msg("Connected to Orchestrator")
	return nil
}

// Start begins the message handling loop with auto-reconnect
func (c *Client) Start() {
	go c.messageLoop()
	go c.heartbeatLoop()
}

// Stop closes the connection
func (c *Client) Stop() error {
	close(c.done)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// IsConnected returns whether the client is connected
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// Send sends a message to the server
func (c *Client) Send(msg Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil || !c.connected {
		return fmt.Errorf("not connected")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		c.connected = false
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

// SendFileChanged notifies the server that a file has changed
func (c *Client) SendFileChanged(path, name, event string) error {
	return c.Send(Message{
		Type:  TypeFileChanged,
		Path:  path,
		Name:  name,
		Event: event,
	})
}

// SendFileWithContent sends a file change notification with file content
func (c *Client) SendFileWithContent(path, name, event string) error {
	// Read file content
	content, size, err := c.readFileContent(path)
	if err != nil {
		log.Warn().Err(err).Str("path", path).Msg("Failed to read file content")
		// Send without content
		return c.SendFileChanged(path, name, event)
	}

	return c.Send(Message{
		Type:    TypeFileChanged,
		Path:    path,
		Name:    name,
		Event:   event,
		Content: content,
		Size:    size,
	})
}

// readFileContent reads file and returns base64 encoded content
func (c *Client) readFileContent(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	// Get file size
	info, err := file.Stat()
	if err != nil {
		return "", 0, err
	}

	// Limit file size to 10MB
	const maxSize = 10 * 1024 * 1024
	if info.Size() > maxSize {
		return "", info.Size(), fmt.Errorf("file too large: %d bytes (max %d)", info.Size(), maxSize)
	}

	// Read content
	data, err := io.ReadAll(file)
	if err != nil {
		return "", 0, err
	}

	// Base64 encode
	encoded := base64.StdEncoding.EncodeToString(data)
	return encoded, info.Size(), nil
}

// SendWatchStarted notifies the server that watching has started
func (c *Client) SendWatchStarted(path string) error {
	return c.Send(Message{
		Type: TypeWatchStarted,
		Path: path,
	})
}

// messageLoop handles incoming messages with auto-reconnect
func (c *Client) messageLoop() {
	for {
		select {
		case <-c.done:
			return
		default:
			if !c.IsConnected() {
				c.reconnect()
				continue
			}

			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()

			if conn == nil {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			_, data, err := conn.ReadMessage()
			if err != nil {
				log.Warn().Err(err).Msg("WebSocket read error")
				c.mu.Lock()
				c.connected = false
				c.mu.Unlock()
				continue
			}

			var msg Message
			if err := json.Unmarshal(data, &msg); err != nil {
				log.Warn().Err(err).Msg("Failed to parse message")
				continue
			}

			if c.handler != nil {
				c.handler(msg)
			}
		}
	}
}

// heartbeatLoop sends periodic heartbeats
func (c *Client) heartbeatLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			if c.IsConnected() {
				if err := c.Send(Message{Type: TypeHeartbeat}); err != nil {
					log.Warn().Err(err).Msg("Failed to send heartbeat")
				}
			}
		}
	}
}

// reconnect attempts to reconnect to the server
func (c *Client) reconnect() {
	log.Info().Dur("delay", c.reconnectDelay).Msg("Attempting to reconnect")

	select {
	case <-c.done:
		return
	case <-time.After(c.reconnectDelay):
		if err := c.Connect(); err != nil {
			log.Warn().Err(err).Msg("Reconnection failed")
		}
	}
}
