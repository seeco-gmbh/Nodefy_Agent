package bridge_test

import (
	"encoding/json"
	"sync"

	"nodefy/agent/internal/bridge"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// mockBroadcaster records all broadcast calls
type mockBroadcaster struct {
	mu       sync.Mutex
	messages [][]byte
}

func (m *mockBroadcaster) BroadcastRaw(data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	m.messages = append(m.messages, cp)
}

func (m *mockBroadcaster) getMessages() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages
}

var _ = Describe("EventForwarder", func() {

	Describe("NewEventForwarder", func() {
		It("should create a new forwarder", func() {
			broadcaster := &mockBroadcaster{}
			forwarder := bridge.NewEventForwarder(broadcaster)
			Expect(forwarder).NotTo(BeNil())
		})
	})

	Describe("HandleBridgeEvent", func() {
		It("should broadcast a single event", func() {
			broadcaster := &mockBroadcaster{}
			forwarder := bridge.NewEventForwarder(broadcaster)

			payload := json.RawMessage(`{"portId":"port-1","value":42}`)
			forwarder.HandleBridgeEvent("PortValueUpdated", payload)

			msgs := broadcaster.getMessages()
			Expect(msgs).To(HaveLen(1))

			var event map[string]interface{}
			Expect(json.Unmarshal(msgs[0], &event)).To(Succeed())
			Expect(event["type"]).To(Equal("bridge_event"))
			Expect(event["method"]).To(Equal("PortValueUpdated"))
		})

		It("should broadcast multiple events in order", func() {
			broadcaster := &mockBroadcaster{}
			forwarder := bridge.NewEventForwarder(broadcaster)

			forwarder.HandleBridgeEvent("StatusChanged", json.RawMessage(`{"status":"running"}`))
			forwarder.HandleBridgeEvent("PortUpdated", json.RawMessage(`{"Id":"p1"}`))
			forwarder.HandleBridgeEvent("Error", json.RawMessage(`{"message":"something failed"}`))

			msgs := broadcaster.getMessages()
			Expect(msgs).To(HaveLen(3))

			methods := make([]string, 3)
			for i, msg := range msgs {
			var event map[string]interface{}
			Expect(json.Unmarshal(msg, &event)).To(Succeed())
				methods[i] = event["method"].(string)
			}

			Expect(methods).To(Equal([]string{"StatusChanged", "PortUpdated", "Error"}))
		})

		It("should preserve the payload", func() {
			broadcaster := &mockBroadcaster{}
			forwarder := bridge.NewEventForwarder(broadcaster)

			forwarder.HandleBridgeEvent("PortValueUpdated", json.RawMessage(`{"portId":"port-1","value":42}`))

			msgs := broadcaster.getMessages()
			Expect(msgs).To(HaveLen(1))

			var event map[string]json.RawMessage
			Expect(json.Unmarshal(msgs[0], &event)).To(Succeed())

			var payload map[string]interface{}
			Expect(json.Unmarshal(event["payload"], &payload)).To(Succeed())
			Expect(payload["portId"]).To(Equal("port-1"))
			Expect(payload["value"]).To(BeNumerically("==", 42))
		})
	})
})
