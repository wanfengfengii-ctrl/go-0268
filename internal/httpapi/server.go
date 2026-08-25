package httpapi

import (
	"net/http"

	"verdant-leaf-fixation-gate/internal/device"
	"verdant-leaf-fixation-gate/internal/store"
)

// Server is the HTTP entry point for the service. It owns the router and the
// health/readiness handlers. Business handlers are wired onto the versioned
// API prefix by the caller.
type Server struct {
	mux *http.ServeMux
	app *App
}

// NewServer constructs a Server with an in-memory store and immediate-success
// device scripts. It is intended for tests and simple demos; production entry
// points should build the store explicitly via NewApp.
func NewServer() *Server {
	st, err := store.Open(":memory:")
	if err != nil {
		panic("open in-memory store: " + err.Error())
	}
	app := NewApp(st, device.NewAdapter(device.ImmediateScripts()))
	return NewServerWithApp(app)
}

// NewServerWithApp builds a Server around an already-constructed App and
// registers every versioned route.
func NewServerWithApp(app *App) *Server {
	s := &Server{mux: http.NewServeMux(), app: app}
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /readyz", s.handleHealth)
	s.registerAPI()
	return s
}

func (s *Server) registerAPI() {
	s.mux.HandleFunc("POST /api/v1/tasks", s.app.handleCreateTask)
	s.mux.HandleFunc("POST /api/v1/tasks/{id}/lock", s.app.handleLock)
	s.mux.HandleFunc("GET /api/v1/tasks/{id}", s.app.handleGetTask)
	s.mux.HandleFunc("POST /api/v1/tasks/{id}/receipts", s.app.handleReceipts)
	s.mux.HandleFunc("POST /api/v1/tasks/{id}/withering/start", s.app.handleWitheringStart)
	s.mux.HandleFunc("POST /api/v1/tasks/{id}/withering/readings", s.app.handleWitheringReadings)
	s.mux.HandleFunc("POST /api/v1/tasks/{id}/tenderness", s.app.handleTenderness)
	s.mux.HandleFunc("POST /api/v1/tasks/{id}/samples/seal", s.app.handleSeal)
	s.mux.HandleFunc("POST /api/v1/tasks/{id}/samples/reveal", s.app.handleReveal)
	s.mux.HandleFunc("POST /api/v1/tasks/{id}/assays/start", s.app.handleAssaysStart)
	s.mux.HandleFunc("POST /api/v1/tasks/{id}/assays/read", s.app.handleAssayRead)
	s.mux.HandleFunc("POST /api/v1/tasks/{id}/retests/read", s.app.handleRetestRead)
	s.mux.HandleFunc("POST /api/v1/tasks/{id}/rejudgments", s.app.handleRejudgment)
	s.mux.HandleFunc("POST /api/v1/tasks/{id}/reviews", s.app.handleReview)
	s.mux.HandleFunc("POST /api/v1/tasks/{id}/terminal", s.app.handleTerminal)
	s.mux.HandleFunc("POST /api/v1/tasks/{id}/fixation/confirm", s.app.handleConfirmFixation)
	s.mux.HandleFunc("POST /api/v1/device-attempts/{id}/retry", s.app.handleDeviceRetry)
}

// App returns the composed application for integration tests and recovery.
func (s *Server) App() *App { return s.app }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Handle registers an arbitrary handler under the given pattern.
func (s *Server) Handle(pattern string, h http.Handler) {
	s.mux.Handle(pattern, h)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
