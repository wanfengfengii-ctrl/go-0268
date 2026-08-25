package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(t *testing.T, srv *Server) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts
}

func postJSON(t *testing.T, ts *httptest.Server, path, body string) string {
	t.Helper()
	resp, err := http.Post(ts.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func getJSON(t *testing.T, ts *httptest.Server, path string) string {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
