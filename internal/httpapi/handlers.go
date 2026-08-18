package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/msaaqib20/orrery/internal/permission"
	"github.com/msaaqib20/orrery/internal/runtime"
	"github.com/msaaqib20/orrery/internal/session"
	"github.com/msaaqib20/orrery/internal/version"
)

// MessageRequest is the body of POST /v1/message.
type MessageRequest struct {
	SessionID string `json:"session_id,omitempty"`
	Text      string `json:"text"`
}

// MessageResponse is the reply to POST /v1/message.
type MessageResponse struct {
	SessionID string  `json:"session_id"`
	Text      string  `json:"text"`
	Source    string  `json:"source"`
	Skill     string  `json:"skill,omitempty"`
	Provider  string  `json:"provider,omitempty"`
	Score     float64 `json:"score,omitempty"`
	ElapsedMS int64   `json:"elapsed_ms"`
}

// HealthResponse is the body of GET /healthz.
type HealthResponse struct {
	Status   string `json:"status"`
	UptimeMS int64  `json:"uptime_ms"`
}

// ReadyResponse is the body of GET /readyz.
type ReadyResponse struct {
	Status    string   `json:"status"`
	Skills    int      `json:"skills"`
	Providers []string `json:"providers"`
	Active    string   `json:"active_provider"`
}

// SkillView is one entry in GET /v1/skills.
type SkillView struct {
	Name         string   `json:"name"`
	Summary      string   `json:"summary"`
	Capabilities []string `json:"capabilities"`
	Examples     []string `json:"examples,omitempty"`
}

// SkillsResponse is the body of GET /v1/skills.
type SkillsResponse struct {
	Skills []SkillView `json:"skills"`
}

// TurnView is one turn in GET /v1/sessions/{id}.
type TurnView struct {
	Role string `json:"role"`
	Text string `json:"text"`
	At   string `json:"at"`
}

// SessionResponse is the body of GET /v1/sessions/{id}.
type SessionResponse struct {
	ID        string     `json:"id"`
	CreatedAt string     `json:"created_at"`
	UpdatedAt string     `json:"updated_at"`
	Turns     []TurnView `json:"turns"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.log, http.StatusOK, HealthResponse{
		Status:   "ok",
		UptimeMS: s.now().Sub(s.started).Milliseconds(),
	})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.log, http.StatusOK, ReadyResponse{
		Status:    "ready",
		Skills:    len(s.rt.Skills()),
		Providers: s.rt.Providers(),
		Active:    s.rt.ProviderName(),
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.log, http.StatusOK, version.Get())
}

func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	descs := s.rt.Skills()
	out := make([]SkillView, 0, len(descs))
	for _, d := range descs {
		caps := make([]string, 0, len(d.Capabilities))
		for _, c := range d.Capabilities {
			caps = append(caps, string(c))
		}
		out = append(out, SkillView{
			Name:         d.Name,
			Summary:      d.Summary,
			Capabilities: caps,
			Examples:     d.Examples,
		})
	}
	writeJSON(w, s.log, http.StatusOK, SkillsResponse{Skills: out})
}

func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, s.maxBody)
	defer body.Close()

	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()

	var req MessageRequest
	if err := dec.Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			s.writeError(w, r, http.StatusRequestEntityTooLarge, "body_too_large", "request body exceeds the configured limit")
			return
		}
		if errors.Is(err, io.EOF) {
			s.writeError(w, r, http.StatusBadRequest, "empty_body", "request body is empty")
			return
		}
		s.writeError(w, r, http.StatusBadRequest, "bad_json", "request body is not valid JSON for this endpoint")
		return
	}

	reply, err := s.rt.Handle(r.Context(), runtime.Request{
		SessionID: req.SessionID,
		Text:      req.Text,
	})
	if err != nil {
		s.writeMessageError(w, r, err)
		return
	}

	writeJSON(w, s.log, http.StatusOK, MessageResponse{
		SessionID: reply.SessionID,
		Text:      reply.Text,
		Source:    reply.Source,
		Skill:     reply.Skill,
		Provider:  reply.Provider,
		Score:     reply.Score,
		ElapsedMS: reply.ElapsedMS,
	})
}

// writeMessageError maps runtime failures onto status codes. Each case is
// listed explicitly so a new failure mode shows up as a 500 and gets noticed,
// rather than being silently absorbed by a catch-all.
func (s *Server) writeMessageError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, runtime.ErrEmptyText):
		s.writeError(w, r, http.StatusBadRequest, "empty_text", "text must not be empty")
	case errors.Is(err, permission.ErrDenied):
		s.writeError(w, r, http.StatusForbidden, "permission_denied", err.Error())
	case r.Context().Err() != nil:
		s.writeError(w, r, http.StatusRequestTimeout, "client_gone", "the request was cancelled")
	default:
		s.log.Error("request failed",
			slog.String("request_id", RequestIDFrom(r.Context())),
			slog.String("error", err.Error()))
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", "the request could not be completed")
	}
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, r, http.StatusBadRequest, "missing_id", "a session id is required")
		return
	}

	sess, err := s.rt.Session(id)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "session_not_found", "no such session")
		return
	}

	writeJSON(w, s.log, http.StatusOK, sessionView(sess))
}

func sessionView(sess *session.Session) SessionResponse {
	turns := make([]TurnView, 0, len(sess.Turns))
	for _, t := range sess.Turns {
		turns = append(turns, TurnView{
			Role: string(t.Role),
			Text: t.Text,
			At:   t.At.Format(time.RFC3339),
		})
	}
	return SessionResponse{
		ID:        sess.ID,
		CreatedAt: sess.CreatedAt.Format(time.RFC3339),
		UpdatedAt: sess.UpdatedAt.Format(time.RFC3339),
		Turns:     turns,
	}
}
