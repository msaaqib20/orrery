// Package httpapi exposes the runtime over HTTP.
//
// The transport layer is intentionally thin: it decodes, delegates to the
// runtime, and encodes. No routing, permission or session logic lives here, so
// a second transport (a Unix socket, a gRPC service) can be added without
// touching any decision-making code.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/msaaqib20/orrery/internal/runtime"
)

// DefaultMaxBodyBytes bounds an inbound request body.
const DefaultMaxBodyBytes int64 = 1 << 20

// Server adapts a Runtime to net/http.
type Server struct {
	rt      *runtime.Runtime
	log     *slog.Logger
	maxBody int64
	started time.Time
	now     func() time.Time
}

// Options configures a Server.
type Options struct {
	Logger       *slog.Logger
	MaxBodyBytes int64
	Now          func() time.Time
}

// New builds a Server around rt.
func New(rt *runtime.Runtime, opts Options) *Server {
	s := &Server{
		rt:      rt,
		log:     opts.Logger,
		maxBody: opts.MaxBodyBytes,
		now:     opts.Now,
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.maxBody <= 0 {
		s.maxBody = DefaultMaxBodyBytes
	}
	if s.now == nil {
		s.now = func() time.Time { return time.Now().UTC() }
	}
	s.started = s.now()
	return s
}

// Handler returns the fully wrapped HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /v1/version", s.handleVersion)
	mux.HandleFunc("GET /v1/skills", s.handleSkills)
	mux.HandleFunc("POST /v1/message", s.handleMessage)
	mux.HandleFunc("GET /v1/sessions/{id}", s.handleSession)

	// Order matters: requestID is outermost so every inner layer can log the
	// same id, accessLog sits above recoverer so a recovered panic is still
	// recorded with its 500 status.
	return chain(mux,
		requestID,
		accessLog(s.log),
		recoverer(s.log),
	)
}

// writeJSON encodes v as the response body.
func writeJSON(w http.ResponseWriter, log *slog.Logger, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		log.Error("encode response", slog.String("error", err.Error()))
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		// A hand-written fallback: if marshalling the real payload failed,
		// marshalling an error struct might fail for the same reason.
		_, _ = w.Write([]byte(`{"error":{"code":"encode_failed","message":"could not encode response"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// ErrorBody is the shape of every error response.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail carries a stable machine-readable code alongside the message.
type ErrorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	writeJSON(w, s.log, status, ErrorBody{Error: ErrorDetail{
		Code:      code,
		Message:   msg,
		RequestID: RequestIDFrom(r.Context()),
	}})
}
