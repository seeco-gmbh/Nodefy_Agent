package bridge

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// BridgeMessage represents a message sent to the Adapt Bridge
type BridgeMessage struct {
	Type      string      `json:"type"`
	Payload   interface{} `json:"payload"`
	RequestID string      `json:"requestId,omitempty"`
}

// BridgeResponse represents a message received from the Adapt Bridge
type BridgeResponse struct {
	Method    string          `json:"method"`
	Payload   json.RawMessage `json:"payload"`
	RequestID string          `json:"requestId,omitempty"`
}

// pendingRequest tracks an in-flight request waiting for a response
type pendingRequest struct {
	expectedMethod string
	filter         func(method string, payload json.RawMessage) bool
	ch             chan pendingResult
}

type pendingResult struct {
	payload json.RawMessage
	err     error
}

// EventHandler is called when the bridge sends an unsolicited event
type EventHandler func(method string, payload json.RawMessage)

// Client manages the WebSocket connection to the Adapt Bridge
type Client struct {
	ws                *websocket.Conn
	url               string
	apiKey            string
	isConnected       bool
	isAuthenticated   bool
	mu                sync.RWMutex
	pendingRequests   map[string]*pendingRequest
	pendingMu         sync.Mutex
	eventHandler      EventHandler
	done              chan struct{}
	reconnectAttempts int
	maxReconnect      int
	reconnectDelay    time.Duration
	reconnectBackoff  float64
}

// NewClient creates a new Bridge client
func NewClient() *Client {
	return &Client{
		pendingRequests:  make(map[string]*pendingRequest),
		maxReconnect:     5,
		reconnectDelay:   time.Second,
		reconnectBackoff: 2.0,
	}
}

// SetEventHandler sets the callback for unsolicited bridge events
func (c *Client) SetEventHandler(handler EventHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.eventHandler = handler
}

// IsConnected returns whether the client is connected to the bridge
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isConnected
}

// Connect establishes a WebSocket connection to the Adapt Bridge
func (c *Client) Connect(url string, apiKey string) error {
	c.mu.Lock()
	if c.isConnected {
		c.mu.Unlock()
		return nil
	}
	c.url = url
	c.apiKey = apiKey
	c.mu.Unlock()

	log.Info().Str("url", url).Msg("Connecting to Adapt Bridge")

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		log.Error().Err(err).Str("url", url).Msg("Failed to connect to Adapt Bridge")
		return fmt.Errorf("failed to connect to Adapt Bridge: %w", err)
	}

	c.mu.Lock()
	c.ws = conn
	c.isConnected = true
	c.reconnectAttempts = 0
	c.done = make(chan struct{})
	c.mu.Unlock()

	// Start read loop
	go c.readLoop()

	// Authenticate if API key is provided
	if apiKey != "" {
		if err := c.authenticate(); err != nil {
			log.Warn().Err(err).Msg("Bridge authentication failed")
			// Don't disconnect — some bridges work without auth
		}
	}

	log.Info().Str("url", url).Msg("Connected to Adapt Bridge")
	return nil
}

// Disconnect closes the WebSocket connection
func (c *Client) Disconnect() error {
	c.mu.Lock()

	if !c.isConnected || c.ws == nil {
		c.mu.Unlock()
		return nil
	}

	log.Info().Msg("Disconnecting from Adapt Bridge")

	// Signal readLoop to stop
	close(c.done)

	// Close WebSocket
	err := c.ws.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "User disconnected"),
	)
	if err != nil {
		log.Warn().Err(err).Msg("Error sending close message")
	}
	c.ws.Close()

	c.ws = nil
	c.isConnected = false
	c.isAuthenticated = false
	c.done = nil
	c.mu.Unlock()

	// Reject all pending requests (outside mu lock to avoid deadlock with pendingMu)
	c.pendingMu.Lock()
	for id, req := range c.pendingRequests {
		req.ch <- pendingResult{err: fmt.Errorf("connection closed")}
		delete(c.pendingRequests, id)
	}
	c.pendingMu.Unlock()

	log.Info().Msg("Disconnected from Adapt Bridge")
	return nil
}

// Send sends a message to the bridge without waiting for a response
func (c *Client) Send(msgType string, payload interface{}, requestID string) error {
	msg := BridgeMessage{
		Type:      msgType,
		Payload:   payload,
		RequestID: requestID,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isConnected || c.ws == nil {
		return fmt.Errorf("not connected to Adapt Bridge")
	}

	if err := c.ws.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

// SendAndWait sends a message and waits for a matching response
func (c *Client) SendAndWait(
	msgType string,
	payload interface{},
	expectedMethod string,
	filter func(method string, payload json.RawMessage) bool,
	timeout time.Duration,
) (json.RawMessage, error) {
	requestID := fmt.Sprintf("req-%d-%d", time.Now().UnixMilli(), time.Now().UnixNano()%1000000)

	ch := make(chan pendingResult, 1)
	req := &pendingRequest{
		expectedMethod: expectedMethod,
		filter:         filter,
		ch:             ch,
	}

	c.pendingMu.Lock()
	c.pendingRequests[requestID] = req
	c.pendingMu.Unlock()

	// Send the message
	if err := c.Send(msgType, payload, requestID); err != nil {
		c.pendingMu.Lock()
		delete(c.pendingRequests, requestID)
		c.pendingMu.Unlock()
		return nil, err
	}

	// Wait for response or timeout
	select {
	case result := <-ch:
		return result.payload, result.err
	case <-time.After(timeout):
		c.pendingMu.Lock()
		delete(c.pendingRequests, requestID)
		c.pendingMu.Unlock()
		return nil, fmt.Errorf("timeout waiting for %s (request: %s)", expectedMethod, requestID)
	}
}

// authenticate sends an authentication request to the bridge
func (c *Client) authenticate() error {
	payload := map[string]string{"apiKey": c.apiKey}
	_, err := c.SendAndWait("Authenticate", payload, "Authenticated", nil, 5*time.Second)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.isAuthenticated = true
	c.mu.Unlock()

	log.Info().Msg("Authenticated with Adapt Bridge")
	return nil
}

// readLoop reads messages from the WebSocket and routes them
func (c *Client) readLoop() {
	// Capture ws locally so we don't access c.ws after Disconnect sets it to nil
	c.mu.RLock()
	ws := c.ws
	c.mu.RUnlock()

	if ws == nil {
		return
	}

	defer func() {
		c.mu.Lock()
		wasConnected := c.isConnected
		c.isConnected = false
		c.isAuthenticated = false
		c.ws = nil
		c.mu.Unlock()

		// Reject all pending requests
		c.pendingMu.Lock()
		for id, req := range c.pendingRequests {
			req.ch <- pendingResult{err: fmt.Errorf("connection lost")}
			delete(c.pendingRequests, id)
		}
		c.pendingMu.Unlock()

		if wasConnected {
			// Emit disconnected event
			c.mu.RLock()
			handler := c.eventHandler
			c.mu.RUnlock()
			if handler != nil {
				disconnectPayload, _ := json.Marshal(map[string]string{"status": "disconnected"})
				handler("Disconnected", disconnectPayload)
			}

			// Attempt reconnect
			go c.reconnect()
		}
	}()

	for {
		select {
		case <-c.done:
			return
		default:
		}

		_, data, err := ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Error().Err(err).Msg("Bridge WebSocket read error")
			}
			return
		}

		var resp BridgeResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			log.Error().Err(err).Msg("Failed to parse bridge message")
			continue
		}

		// Skip heartbeats
		if resp.Method == "Heartbeat" {
			continue
		}

		// Check if this resolves a pending request
		if c.resolvePending(resp) {
			continue
		}

		// Otherwise, it's an unsolicited event — forward to handler
		c.mu.RLock()
		handler := c.eventHandler
		c.mu.RUnlock()
		if handler != nil {
			handler(resp.Method, resp.Payload)
		}
	}
}

// resolvePending checks if a response matches a pending request
func (c *Client) resolvePending(resp BridgeResponse) bool {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	isError := resp.Method == "Error"

	// First: try to match by requestId
	if resp.RequestID != "" {
		if req, ok := c.pendingRequests[resp.RequestID]; ok {
			delete(c.pendingRequests, resp.RequestID)
			if isError {
				var errPayload struct {
					Message string `json:"message"`
				}
				json.Unmarshal(resp.Payload, &errPayload)
				msg := errPayload.Message
				if msg == "" {
					msg = "Bridge error"
				}
				req.ch <- pendingResult{err: fmt.Errorf("%s", msg)}
			} else {
				req.ch <- pendingResult{payload: resp.Payload}
			}
			return true
		}
	}

	// If error, reject the first pending request
	if isError {
		for id, req := range c.pendingRequests {
			delete(c.pendingRequests, id)
			var errPayload struct {
				Message string `json:"message"`
			}
			json.Unmarshal(resp.Payload, &errPayload)
			msg := errPayload.Message
			if msg == "" {
				msg = "Bridge error"
			}
			req.ch <- pendingResult{err: fmt.Errorf("%s", msg)}
			return true
		}
	}

	// Match by expectedMethod or filter
	for id, req := range c.pendingRequests {
		matched := false
		if req.expectedMethod == resp.Method {
			matched = true
		}
		if !matched && req.filter != nil && req.filter(resp.Method, resp.Payload) {
			matched = true
		}
		if matched {
			delete(c.pendingRequests, id)
			req.ch <- pendingResult{payload: resp.Payload}
			return true
		}
	}

	return false
}

// reconnect attempts to reconnect to the bridge with exponential backoff
func (c *Client) reconnect() {
	c.mu.RLock()
	url := c.url
	apiKey := c.apiKey
	maxReconnect := c.maxReconnect
	c.mu.RUnlock()

	if url == "" {
		return
	}

	for attempt := 1; attempt <= maxReconnect; attempt++ {
		delay := c.reconnectDelay * time.Duration(math.Pow(c.reconnectBackoff, float64(attempt-1)))
		log.Info().Int("attempt", attempt).Dur("delay", delay).Msg("Reconnecting to Adapt Bridge")
		time.Sleep(delay)

		// Check if we were manually disconnected (done=nil means Disconnect was called)
		c.mu.RLock()
		manualDisconnect := c.done == nil
		alreadyConnected := c.isConnected
		c.mu.RUnlock()
		if manualDisconnect || alreadyConnected {
			return
		}

		if err := c.Connect(url, apiKey); err != nil {
			log.Warn().Err(err).Int("attempt", attempt).Msg("Reconnect failed")
			continue
		}

		log.Info().Int("attempt", attempt).Msg("Reconnected to Adapt Bridge")

		// Emit reconnected event
		c.mu.RLock()
		handler := c.eventHandler
		c.mu.RUnlock()
		if handler != nil {
			reconnectPayload, _ := json.Marshal(map[string]string{"status": "reconnected"})
			handler("Reconnected", reconnectPayload)
		}
		return
	}

	log.Error().Int("maxAttempts", maxReconnect).Msg("Failed to reconnect to Adapt Bridge")
}

// GetStatus returns the current connection status
func (c *Client) GetStatus() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]interface{}{
		"connected":     c.isConnected,
		"authenticated": c.isAuthenticated,
		"url":           c.url,
	}
}
