package bridge_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"nodefy/agent/internal/bridge"
	"nodefy/agent/test/helpers"

	"github.com/gorilla/websocket"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// connectClient creates a Client connected to a mock bridge server
func connectClient(handler func(conn *websocket.Conn)) (*bridge.Client, *httptest.Server) {
	server := helpers.MockBridgeServer(handler)
	c := bridge.NewClient()
	ExpectWithOffset(1, c.Connect(helpers.WsURL(server), "")).To(Succeed())
	return c, server
}

var _ = Describe("Bridge Handlers", func() {

	Describe("HandleConnect", func() {
		It("should connect to a bridge", func() {
			wsServer := helpers.MockBridgeServer(helpers.SilentHandler())
			defer wsServer.Close()

			c := bridge.NewClient()
			h := bridge.NewHandlers(c)
			defer c.Disconnect()

			body := strings.NewReader(`{"url":"` + helpers.WsURL(wsServer) + `"}`)
			req := httptest.NewRequest(http.MethodPost, "/api/adapt/connect", body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.HandleConnect(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			resp, err := helpers.ParseJSONResponse(w)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp["success"]).To(BeTrue())
		})

		It("should reject missing URL", func() {
			c := bridge.NewClient()
			h := bridge.NewHandlers(c)

			body := strings.NewReader(`{}`)
			req := httptest.NewRequest(http.MethodPost, "/api/adapt/connect", body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.HandleConnect(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should reject wrong HTTP method", func() {
			c := bridge.NewClient()
			h := bridge.NewHandlers(c)

			req := httptest.NewRequest(http.MethodGet, "/api/adapt/connect", nil)
			w := httptest.NewRecorder()

			h.HandleConnect(w, req)

			Expect(w.Code).To(Equal(http.StatusMethodNotAllowed))
		})
	})

	Describe("HandleDisconnect", func() {
		It("should disconnect a connected client", func() {
			client, server := connectClient(helpers.SilentHandler())
			defer server.Close()

			h := bridge.NewHandlers(client)

			req := httptest.NewRequest(http.MethodPost, "/api/adapt/disconnect", nil)
			w := httptest.NewRecorder()

			h.HandleDisconnect(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(client.IsConnected()).To(BeFalse())
		})
	})

	Describe("HandleStatus", func() {
		It("should return connected status with bridge info", func() {
			client, server := connectClient(helpers.EchoHandler("BridgeStatus", `{"version":"1.0"}`))
			defer server.Close()
			defer client.Disconnect()

			h := bridge.NewHandlers(client)

			req := httptest.NewRequest(http.MethodGet, "/api/adapt/status", nil)
			w := httptest.NewRecorder()

			h.HandleStatus(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			resp, err := helpers.ParseJSONResponse(w)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp["connected"]).To(BeTrue())
		})

		It("should return disconnected status", func() {
			c := bridge.NewClient()
			h := bridge.NewHandlers(c)

			req := httptest.NewRequest(http.MethodGet, "/api/adapt/status", nil)
			w := httptest.NewRecorder()

			h.HandleStatus(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			resp, err := helpers.ParseJSONResponse(w)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp["connected"]).To(BeFalse())
		})
	})

	Describe("HandleCreateComponent", func() {
		It("should create a component", func() {
			client, server := connectClient(helpers.EchoHandler("Created", `{"id":"mod-1","Type":"Module"}`))
			defer server.Close()
			defer client.Disconnect()

			h := bridge.NewHandlers(client)

			body := strings.NewReader(`{"componentType":"Module","name":"TestModule"}`)
			req := httptest.NewRequest(http.MethodPost, "/api/adapt/component", body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.HandleCreateComponent(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			resp, err := helpers.ParseJSONResponse(w)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp["id"]).To(Equal("mod-1"))
		})

		It("should reject missing componentType", func() {
			client, server := connectClient(helpers.EchoHandler("Created", `{}`))
			defer server.Close()
			defer client.Disconnect()

			h := bridge.NewHandlers(client)

			body := strings.NewReader(`{"name":"TestModule"}`)
			req := httptest.NewRequest(http.MethodPost, "/api/adapt/component", body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.HandleCreateComponent(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("HandleGetComponentInfo", func() {
		It("should return component details", func() {
			client, server := connectClient(helpers.EchoHandler("ComponentDetails", `{"id":"comp-1","name":"MyModule","ports":[]}`))
			defer server.Close()
			defer client.Disconnect()

			h := bridge.NewHandlers(client)

			req := httptest.NewRequest(http.MethodGet, "/api/adapt/component/comp-1?mode=details", nil)
			w := httptest.NewRecorder()

			h.HandleGetComponentInfo(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			resp, err := helpers.ParseJSONResponse(w)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp["id"]).To(Equal("comp-1"))
		})
	})

	Describe("HandleUpdateComponent", func() {
		It("should update a component", func() {
			client, server := connectClient(helpers.EchoHandler("Updated", `{"id":"comp-1","name":"Renamed"}`))
			defer server.Close()
			defer client.Disconnect()

			h := bridge.NewHandlers(client)

			body := strings.NewReader(`{"name":"Renamed"}`)
			req := httptest.NewRequest(http.MethodPut, "/api/adapt/component/comp-1", body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.HandleUpdateComponent(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
		})
	})

	Describe("HandleDeleteComponent", func() {
		It("should delete a component", func() {
			client, server := connectClient(helpers.EchoHandler("Deleted", `{"id":"comp-1"}`))
			defer server.Close()
			defer client.Disconnect()

			h := bridge.NewHandlers(client)

			req := httptest.NewRequest(http.MethodDelete, "/api/adapt/component/comp-1", nil)
			w := httptest.NewRecorder()

			h.HandleDeleteComponent(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
		})
	})

	Describe("HandleUpdatePort", func() {
		It("should update a port", func() {
			client, server := connectClient(helpers.EchoHandler("PortUpdated", `{"Id":"port-1","name":"renamed"}`))
			defer server.Close()
			defer client.Disconnect()

			h := bridge.NewHandlers(client)

			body := strings.NewReader(`{"name":"renamed"}`)
			req := httptest.NewRequest(http.MethodPut, "/api/adapt/port/port-1", body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.HandleUpdatePort(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
		})
	})

	Describe("HandleConnectPorts", func() {
		It("should connect two ports", func() {
			client, server := connectClient(helpers.EchoHandler("Created", `{"Type":"Connector","id":"conn-1"}`))
			defer server.Close()
			defer client.Disconnect()

			h := bridge.NewHandlers(client)

			body := strings.NewReader(`{"sourcePortId":"p1","targetPortId":"p2","containerId":"mod-1"}`)
			req := httptest.NewRequest(http.MethodPost, "/api/adapt/connect-ports", body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.HandleConnectPorts(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
		})
	})

	Describe("HandleExecute", func() {
		It("should execute a component", func() {
			client, server := connectClient(helpers.EchoHandler("ExecutionCompleted", `{"success":true,"duration":123}`))
			defer server.Close()
			defer client.Disconnect()

			h := bridge.NewHandlers(client)

			body := strings.NewReader(`{"componentType":"Network"}`)
			req := httptest.NewRequest(http.MethodPost, "/api/adapt/execute/net-1", body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.HandleExecute(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
		})
	})

	Describe("HandleExportComponent", func() {
		It("should export a component", func() {
			client, server := connectClient(helpers.EchoHandler("Exported", `{"xml":"<network/>","code":"abc"}`))
			defer server.Close()
			defer client.Disconnect()

			h := bridge.NewHandlers(client)

			body := strings.NewReader(`{"componentType":"Network"}`)
			req := httptest.NewRequest(http.MethodPost, "/api/adapt/export/net-1", body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.HandleExportComponent(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
		})
	})

	Describe("HandleLoadComponent", func() {
		It("should load a component from XML", func() {
			client, server := connectClient(helpers.EchoHandler("Loaded", `{"id":"loaded-1","type":"Module"}`))
			defer server.Close()
			defer client.Disconnect()

			h := bridge.NewHandlers(client)

			body := strings.NewReader(`{"xml":"<module/>","componentType":"Module"}`)
			req := httptest.NewRequest(http.MethodPost, "/api/adapt/load", body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.HandleLoadComponent(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
		})
	})

	Describe("HandleSaveComponent", func() {
		It("should save a component", func() {
			client, server := connectClient(helpers.EchoHandler("Saved", `{"success":true}`))
			defer server.Close()
			defer client.Disconnect()

			h := bridge.NewHandlers(client)

			body := strings.NewReader(`{"path":"/tmp/test.xml"}`)
			req := httptest.NewRequest(http.MethodPost, "/api/adapt/save/comp-1", body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.HandleSaveComponent(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
		})
	})

	Describe("HandleGetTemplateTypes", func() {
		It("should return template types", func() {
			client, server := connectClient(helpers.EchoHandler("TemplateTypes", `{"TemplateGroups":[{"Group":"Math"}]}`))
			defer server.Close()
			defer client.Disconnect()

			h := bridge.NewHandlers(client)

			req := httptest.NewRequest(http.MethodGet, "/api/adapt/templates", nil)
			w := httptest.NewRecorder()

			h.HandleGetTemplateTypes(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
		})
	})

	Describe("HandleSync", func() {
		It("should sync components", func() {
			client, server := connectClient(helpers.EchoHandler("BridgeStatus", `{"components":[]}`))
			defer server.Close()
			defer client.Disconnect()

			h := bridge.NewHandlers(client)

			req := httptest.NewRequest(http.MethodPost, "/api/adapt/sync", nil)
			w := httptest.NewRecorder()

			h.HandleSync(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
		})

		It("should return 503 when not connected", func() {
			c := bridge.NewClient()
			h := bridge.NewHandlers(c)

			req := httptest.NewRequest(http.MethodPost, "/api/adapt/sync", nil)
			w := httptest.NewRecorder()

			h.HandleSync(w, req)

			Expect(w.Code).To(Equal(http.StatusServiceUnavailable))
		})
	})

	Describe("RegisterRoutes", func() {
		It("should register routes without panic", func() {
			c := bridge.NewClient()
			h := bridge.NewHandlers(c)
			mux := http.NewServeMux()
			Expect(func() { h.RegisterRoutes(mux) }).NotTo(Panic())
		})
	})

	Describe("Handler bridge timeout", func() {
		It("should return 500 when bridge does not respond", func() {
			client, server := connectClient(helpers.SilentHandler())
			defer server.Close()
			defer client.Disconnect()

			h := bridge.NewHandlers(client)

			body := strings.NewReader(`{"componentType":"Module"}`)
			req := httptest.NewRequest(http.MethodPost, "/api/adapt/component", body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			start := time.Now()
			h.HandleCreateComponent(w, req)
			elapsed := time.Since(start)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			Expect(elapsed).To(BeNumerically("<", 15*time.Second))
		})
	})
})
