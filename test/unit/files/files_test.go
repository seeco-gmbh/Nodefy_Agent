package files_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"

	"nodefy/agent/internal/files"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("File Handlers", func() {

	Describe("HandleExportFile", func() {
		It("should export data as pretty-printed JSON", func() {
			body := `{"data":{"name":"test","value":42},"filename":"export.json"}`
			req := httptest.NewRequest(http.MethodPost, "/api/files/export", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			files.HandleExportFile(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Header().Get("Content-Disposition")).To(ContainSubstring("export.json"))
			Expect(w.Header().Get("Content-Type")).To(Equal("application/json"))

			var parsed map[string]interface{}
			Expect(json.Unmarshal(w.Body.Bytes(), &parsed)).To(Succeed())
			Expect(parsed["name"]).To(Equal("test"))
			Expect(w.Body.String()).To(ContainSubstring("\n"))
		})

		It("should use default filename when not provided", func() {
			body := `{"data":{"key":"value"}}`
			req := httptest.NewRequest(http.MethodPost, "/api/files/export", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			files.HandleExportFile(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Header().Get("Content-Disposition")).To(ContainSubstring("export.json"))
		})

		It("should reject missing data field", func() {
			body := `{"filename":"test.json"}`
			req := httptest.NewRequest(http.MethodPost, "/api/files/export", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			files.HandleExportFile(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should reject invalid JSON", func() {
			req := httptest.NewRequest(http.MethodPost, "/api/files/export", strings.NewReader("not json"))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			files.HandleExportFile(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should reject wrong HTTP method", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/files/export", nil)
			w := httptest.NewRecorder()

			files.HandleExportFile(w, req)

			Expect(w.Code).To(Equal(http.StatusMethodNotAllowed))
		})

		It("should handle array data", func() {
			body := `{"data":[1,2,3],"filename":"array.json"}`
			req := httptest.NewRequest(http.MethodPost, "/api/files/export", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			files.HandleExportFile(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var parsed []interface{}
			Expect(json.Unmarshal(w.Body.Bytes(), &parsed)).To(Succeed())
			Expect(parsed).To(HaveLen(3))
		})

		It("should handle deeply nested data", func() {
			body := `{"data":{"deep":{"nested":{"value":true}}},"filename":"nested.json"}`
			req := httptest.NewRequest(http.MethodPost, "/api/files/export", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			files.HandleExportFile(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var parsed map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &parsed)
			deep := parsed["deep"].(map[string]interface{})
			nested := deep["nested"].(map[string]interface{})
			Expect(nested["value"]).To(BeTrue())
		})
	})

	Describe("HandleImportFile", func() {
		It("should import raw JSON", func() {
			body := `{"networks":[{"id":"net-1","name":"TestNetwork"}]}`
			req := httptest.NewRequest(http.MethodPost, "/api/files/import", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			files.HandleImportFile(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var resp map[string]interface{}
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["fileName"]).To(Equal("upload.json"))

			data := resp["data"].(map[string]interface{})
			networks := data["networks"].([]interface{})
			Expect(networks).To(HaveLen(1))
		})

		It("should import multipart file upload", func() {
			jsonContent := `{"modules":[{"id":"mod-1"}]}`

			var buf bytes.Buffer
			writer := multipart.NewWriter(&buf)
			part, err := writer.CreateFormFile("file", "test-module.json")
			Expect(err).NotTo(HaveOccurred())
			io.WriteString(part, jsonContent)
			writer.Close()

			req := httptest.NewRequest(http.MethodPost, "/api/files/import", &buf)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			w := httptest.NewRecorder()

			files.HandleImportFile(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var resp map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &resp)
			Expect(resp["fileName"]).To(Equal("test-module.json"))

			data := resp["data"].(map[string]interface{})
			modules := data["modules"].([]interface{})
			Expect(modules).To(HaveLen(1))
		})

		It("should reject invalid JSON", func() {
			req := httptest.NewRequest(http.MethodPost, "/api/files/import", strings.NewReader("this is not json"))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			files.HandleImportFile(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should reject wrong HTTP method", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/files/import", nil)
			w := httptest.NewRecorder()

			files.HandleImportFile(w, req)

			Expect(w.Code).To(Equal(http.StatusMethodNotAllowed))
		})
	})

	Describe("RegisterRoutes", func() {
		It("should register routes without panic", func() {
			mux := http.NewServeMux()
			Expect(func() { files.RegisterRoutes(mux) }).NotTo(Panic())
		})
	})
})
