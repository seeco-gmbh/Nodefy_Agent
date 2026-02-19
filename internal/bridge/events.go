package bridge

import (
	"encoding/json"

	"github.com/rs/zerolog/log"
)

type BridgeEvent struct {
	Type    string          `json:"type"`
	Method  string          `json:"method"`
	Payload json.RawMessage `json:"payload"`
}

type Broadcaster interface {
	BroadcastRaw(data []byte)
}

type EventForwarder struct {
	broadcaster Broadcaster
}

func NewEventForwarder(broadcaster Broadcaster) *EventForwarder {
	return &EventForwarder{broadcaster: broadcaster}
}

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
