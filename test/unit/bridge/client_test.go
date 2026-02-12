package bridge_test

import (
	"encoding/json"
	"net/http/httptest"
	"sync"
	"time"

	"nodefy/agent/internal/bridge"
	"nodefy/agent/test/helpers"

	"github.com/gorilla/websocket"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Bridge Client", func() {

	Describe("NewClient", func() {
		It("should create a new disconnected client", func() {
			c := bridge.NewClient()
			Expect(c).NotTo(BeNil())
			Expect(c.IsConnected()).To(BeFalse())
		})
	})

	Describe("GetStatus", func() {
		It("should report disconnected for a new client", func() {
			c := bridge.NewClient()
			status := c.GetStatus()
			Expect(status["connected"]).To(BeFalse())
			Expect(status["url"]).To(Equal(""))
		})
	})

	Describe("Connect / Disconnect", func() {
		var (
			c      *bridge.Client
			server *httptest.Server
		)

		BeforeEach(func() {
			c = bridge.NewClient()
		})

		AfterEach(func() {
			c.Disconnect()
			if server != nil {
				server.Close()
			}
		})

		It("should connect to a WebSocket server", func() {
			server = helpers.MockBridgeServer(helpers.SilentHandler())
			err := c.Connect(helpers.WsURL(server), "")
			Expect(err).NotTo(HaveOccurred())
			Expect(c.IsConnected()).To(BeTrue())
		})

		It("should be a no-op when already connected", func() {
			server = helpers.MockBridgeServer(helpers.SilentHandler())
			err := c.Connect(helpers.WsURL(server), "")
			Expect(err).NotTo(HaveOccurred())

			err = c.Connect(helpers.WsURL(server), "")
			Expect(err).NotTo(HaveOccurred())
			Expect(c.IsConnected()).To(BeTrue())
		})

		It("should disconnect cleanly", func() {
			server = helpers.MockBridgeServer(helpers.SilentHandler())
			Expect(c.Connect(helpers.WsURL(server), "")).To(Succeed())

			err := c.Disconnect()
			Expect(err).NotTo(HaveOccurred())
			Expect(c.IsConnected()).To(BeFalse())
		})

		It("should be a no-op when disconnecting while not connected", func() {
			err := c.Disconnect()
			Expect(err).NotTo(HaveOccurred())
		})

		It("should fail to connect to an invalid URL", func() {
			err := c.Connect("ws://127.0.0.1:1", "")
			Expect(err).To(HaveOccurred())
			Expect(c.IsConnected()).To(BeFalse())
		})
	})

	Describe("Send", func() {
		It("should fail when not connected", func() {
			c := bridge.NewClient()
			err := c.Send("Test", map[string]string{"key": "value"}, "req-1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not connected"))
		})
	})

	Describe("SendAndWait", func() {
		var (
			c      *bridge.Client
			server *httptest.Server
		)

		AfterEach(func() {
			if c != nil {
				c.Disconnect()
			}
			if server != nil {
				server.Close()
			}
		})

		It("should receive a matching response by requestId", func() {
			server = helpers.MockBridgeServer(helpers.EchoHandler("Created", `{"id":"comp-123","type":"Module"}`))
			c = bridge.NewClient()
			Expect(c.Connect(helpers.WsURL(server), "")).To(Succeed())

			result, err := c.SendAndWait("CreateComponent", map[string]string{"componentType": "Module"}, "Created", nil, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var payload map[string]string
			Expect(json.Unmarshal(result, &payload)).To(Succeed())
			Expect(payload["id"]).To(Equal("comp-123"))
		})

		It("should timeout when no response arrives", func() {
			server = helpers.MockBridgeServer(helpers.SilentHandler())
			c = bridge.NewClient()
			Expect(c.Connect(helpers.WsURL(server), "")).To(Succeed())

			_, err := c.SendAndWait("CreateComponent", map[string]string{}, "Created", nil, 200*time.Millisecond)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("timeout"))
		})

		It("should return bridge errors", func() {
			server = helpers.MockBridgeServer(helpers.EchoHandler("Error", `{"message":"Component not found"}`))
			c = bridge.NewClient()
			Expect(c.Connect(helpers.WsURL(server), "")).To(Succeed())

			_, err := c.SendAndWait("DeleteComponent", map[string]string{"componentId": "abc"}, "Deleted", nil, 2*time.Second)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Component not found"))
		})

		It("should match by filter function", func() {
			server = helpers.MockBridgeServer(func(conn *websocket.Conn) {
				for {
					_, _, err := conn.ReadMessage()
					if err != nil {
						return
					}
					resp := map[string]interface{}{
						"method":  "Created",
						"payload": json.RawMessage(`{"Type":"Module","id":"mod-1"}`),
					}
					data, _ := json.Marshal(resp)
					conn.WriteMessage(websocket.TextMessage, data)
				}
			})
			c = bridge.NewClient()
			Expect(c.Connect(helpers.WsURL(server), "")).To(Succeed())

			filter := func(method string, payload json.RawMessage) bool {
				if method != "Created" {
					return false
				}
				var p map[string]interface{}
				json.Unmarshal(payload, &p)
				return p["Type"] == "Module"
			}

			result, err := c.SendAndWait("CreateComponent", map[string]string{"componentType": "Module"}, "Created", filter, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var payload map[string]string
			json.Unmarshal(result, &payload)
			Expect(payload["id"]).To(Equal("mod-1"))
		})

		It("should handle concurrent requests", func() {
			server = helpers.MockBridgeServer(helpers.ConcurrentEchoHandler("OK", `{"success":true}`))
			c = bridge.NewClient()
			Expect(c.Connect(helpers.WsURL(server), "")).To(Succeed())

			const numRequests = 5
			var wg sync.WaitGroup
			errCh := make(chan error, numRequests)

			for i := 0; i < numRequests; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, err := c.SendAndWait("Test", map[string]string{}, "OK", nil, 10*time.Second)
					if err != nil {
						errCh <- err
					}
				}()
			}

			wg.Wait()
			close(errCh)

			for err := range errCh {
				Fail("Concurrent SendAndWait failed: " + err.Error())
			}
		})
	})

	Describe("Authentication", func() {
		It("should authenticate with a valid API key", func() {
			server := helpers.MockBridgeServer(func(conn *websocket.Conn) {
				for {
					_, data, err := conn.ReadMessage()
					if err != nil {
						return
					}
					var msg struct {
						Type      string `json:"type"`
						RequestID string `json:"requestId"`
					}
					json.Unmarshal(data, &msg)

					if msg.Type == "Authenticate" {
						resp := map[string]interface{}{
							"method":    "Authenticated",
							"payload":   json.RawMessage(`{"success":true}`),
							"requestId": msg.RequestID,
						}
						respData, _ := json.Marshal(resp)
						conn.WriteMessage(websocket.TextMessage, respData)
					}
				}
			})
			defer server.Close()

			c := bridge.NewClient()
			err := c.Connect(helpers.WsURL(server), "test-api-key")
			Expect(err).NotTo(HaveOccurred())
			defer c.Disconnect()

			status := c.GetStatus()
			Expect(status["authenticated"]).To(BeTrue())
		})
	})

	Describe("Event Handling", func() {
		It("should forward unsolicited events to the event handler", func() {
			server := helpers.MockBridgeServer(func(conn *websocket.Conn) {
				time.Sleep(50 * time.Millisecond)
				event := map[string]interface{}{
					"method":  "PortValueUpdated",
					"payload": json.RawMessage(`{"portId":"port-1","value":42}`),
				}
				data, _ := json.Marshal(event)
				conn.WriteMessage(websocket.TextMessage, data)

				for {
					_, _, err := conn.ReadMessage()
					if err != nil {
						return
					}
				}
			})
			defer server.Close()

			c := bridge.NewClient()

			var receivedMethod string
			var mu sync.Mutex
			done := make(chan struct{})

			c.SetEventHandler(func(method string, payload json.RawMessage) {
				mu.Lock()
				receivedMethod = method
				mu.Unlock()
				close(done)
			})

			Expect(c.Connect(helpers.WsURL(server), "")).To(Succeed())
			defer c.Disconnect()

			Eventually(done, 2*time.Second).Should(BeClosed())

			mu.Lock()
			defer mu.Unlock()
			Expect(receivedMethod).To(Equal("PortValueUpdated"))
		})

		It("should skip heartbeat messages", func() {
			server := helpers.MockBridgeServer(func(conn *websocket.Conn) {
				hb := map[string]interface{}{"method": "Heartbeat", "payload": json.RawMessage(`{}`)}
				data, _ := json.Marshal(hb)
				conn.WriteMessage(websocket.TextMessage, data)

				time.Sleep(20 * time.Millisecond)

				ev := map[string]interface{}{"method": "StatusChanged", "payload": json.RawMessage(`{"status":"idle"}`)}
				data, _ = json.Marshal(ev)
				conn.WriteMessage(websocket.TextMessage, data)

				for {
					_, _, err := conn.ReadMessage()
					if err != nil {
						return
					}
				}
			})
			defer server.Close()

			c := bridge.NewClient()

			var receivedMethod string
			var mu sync.Mutex
			done := make(chan struct{})

			c.SetEventHandler(func(method string, payload json.RawMessage) {
				mu.Lock()
				receivedMethod = method
				mu.Unlock()
				select {
				case <-done:
				default:
					close(done)
				}
			})

			Expect(c.Connect(helpers.WsURL(server), "")).To(Succeed())
			defer c.Disconnect()

			Eventually(done, 2*time.Second).Should(BeClosed())

			mu.Lock()
			defer mu.Unlock()
			Expect(receivedMethod).To(Equal("StatusChanged"))
		})
	})

	Describe("Disconnect rejects pending", func() {
		It("should reject pending requests on disconnect", func() {
			server := helpers.MockBridgeServer(helpers.SilentHandler())
			defer server.Close()

			c := bridge.NewClient()
			Expect(c.Connect(helpers.WsURL(server), "")).To(Succeed())

			errCh := make(chan error, 1)
			go func() {
				_, err := c.SendAndWait("GetStatus", map[string]string{}, "BridgeStatus", nil, 5*time.Second)
				errCh <- err
			}()

			time.Sleep(50 * time.Millisecond)
			c.Disconnect()

			Eventually(errCh, 2*time.Second).Should(Receive(HaveOccurred()))
		})
	})
})
