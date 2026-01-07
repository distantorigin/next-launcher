package testing

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// MockGitHubServer provides a mock GitHub API server for testing
type MockGitHubServer struct {
	*httptest.Server
	Responses map[string]MockResponse
	Requests  []MockRequest
}

// MockResponse holds response data for a path
type MockResponse struct {
	StatusCode int
	Body       []byte
	Headers    map[string]string
}

// MockRequest records a request made to the mock server
type MockRequest struct {
	Method string
	Path   string
	Query  map[string][]string
}

// NewMockGitHubServer creates a new mock GitHub API server
func NewMockGitHubServer(t *testing.T) *MockGitHubServer {
	t.Helper()

	mock := &MockGitHubServer{
		Responses: make(map[string]MockResponse),
		Requests:  make([]MockRequest, 0),
	}

	mock.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record the request
		mock.Requests = append(mock.Requests, MockRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
		})

		// Look up response
		response, ok := mock.Responses[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{
				"message": "Not Found",
			})
			return
		}

		// Set custom headers
		for key, value := range response.Headers {
			w.Header().Set(key, value)
		}

		// Default to JSON content type if not specified
		if response.Headers["Content-Type"] == "" {
			w.Header().Set("Content-Type", "application/json")
		}

		// Write status code
		if response.StatusCode != 0 {
			w.WriteHeader(response.StatusCode)
		}

		// Write body
		w.Write(response.Body)
	}))

	t.Cleanup(func() {
		mock.Server.Close()
	})

	return mock
}

// SetResponse sets the response for a given path
func (m *MockGitHubServer) SetResponse(path string, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	m.Responses[path] = MockResponse{
		StatusCode: http.StatusOK,
		Body:       jsonData,
	}
	return nil
}

// SetJSONResponse sets a JSON response with custom status code
func (m *MockGitHubServer) SetJSONResponse(path string, statusCode int, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	m.Responses[path] = MockResponse{
		StatusCode: statusCode,
		Body:       jsonData,
	}
	return nil
}

// SetRawResponse sets a raw response
func (m *MockGitHubServer) SetRawResponse(path string, statusCode int, body []byte, headers map[string]string) {
	m.Responses[path] = MockResponse{
		StatusCode: statusCode,
		Body:       body,
		Headers:    headers,
	}
}

// SetError sets an error response
func (m *MockGitHubServer) SetError(path string, statusCode int, message string) error {
	return m.SetJSONResponse(path, statusCode, map[string]string{
		"message": message,
	})
}

// GetRequestCount returns the number of requests made to a path
func (m *MockGitHubServer) GetRequestCount(path string) int {
	count := 0
	for _, req := range m.Requests {
		if req.Path == path {
			count++
		}
	}
	return count
}

// ClearRequests clears the recorded requests
func (m *MockGitHubServer) ClearRequests() {
	m.Requests = make([]MockRequest, 0)
}
