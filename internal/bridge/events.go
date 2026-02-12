package bridge

import (
	"encoding/json"

	"github.com/rs/zerolog/log"
)

// BridgeEvent is the message format sent to frontend clients when a bridge event occurs
type BridgeEvent struct {
	Type    string          `json:"type"`    // Always "bridge_event"
	Method  string          `json:"method"`  // e.g. "PortValueUpdated", "StatusChanged"
	Payload json.RawMessage `json:"payload"` // Raw bridge payload
}

// Broadcaster is an interface for sending messages to connected frontend clients
type Broadcaster interface {
	BroadcastRaw(data []byte)
}

// EventForwarder forwards bridge events to frontend WebSocket clients
type EventForwarder struct {
	broadcaster Broadcaster
}

// NewEventForwarder creates a new EventForwarder
func NewEventForwarder(broadcaster Broadcaster) *EventForwarder {
	return &EventForwarder{broadcaster: broadcaster}
}

// HandleBridgeEvent is called by the bridge client when an unsolicited event arrives.
// It wraps the event in a BridgeEvent envelope and broadcasts it to all frontend clients.
func (f *EventForwarder) HandleBridgeEvent(method string, payload json.RawMessage) {
	event := BridgeEvent{
		Type:    "bridge_event",
		Method:  method,
		Payload: payload,
	}

	data, err := json.Marshal(event)
	if err != nil {
		log.Error().Err(err).Str("method", method).Msg("Failed to marshal bridge event")
		return
	}

	log.Debug().Str("method", method).Msg("Forwarding bridge event to frontend")
	f.broadcaster.BroadcastRaw(data)
}
