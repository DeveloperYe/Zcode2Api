package gateway

// limits.go — request-size and concurrency limits for the chat endpoints.
//
// Both were unbounded: a 50 MB body was read fully into memory, and nothing
// capped how many of those could be in flight, so an authenticated client
// could exhaust a small box with a handful of requests. Bodies now default to
// 8 MB (MAX_BODY_MB) and in-flight chat requests to 32 (MAX_CONCURRENT; 0
// disables the cap).

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
)

var errBodyTooLarge = errors.New("request body is too large")

// readBody reads the request body up to the configured limit.
func (s *Server) readBody(r *http.Request) ([]byte, error) {
	limit := s.bodyLimit
	if limit <= 0 {
		limit = 8 << 20
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > limit {
		return nil, errBodyTooLarge
	}
	return body, nil
}

// withChatSlot bounds concurrent chat requests. Excess callers get 429 with a
// Retry-After rather than being queued forever (a queued request holds memory
// just like a running one).
func (s *Server) withChatSlot(next http.HandlerFunc) http.HandlerFunc {
	if s.chatSem == nil {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case s.chatSem <- struct{}{}:
			defer func() { <-s.chatSem }()
			next(w, r)
		default:
			log.Printf("[limits] concurrency cap reached (%d), rejecting %s", cap(s.chatSem), r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"too many concurrent requests; retry shortly","type":"rate_limit_error"}}`))
		}
	}
}

// failRequest logs the detail and returns a generic message to the client —
// internal error text (paths, SQL messages, upstream bodies) must not be a
// reconnaissance gift.
func failRequest(w http.ResponseWriter, r *http.Request, status int, public string, err error) {
	if err != nil {
		log.Printf("[error] %s %s: %v", r.Method, r.URL.Path, err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if public == "" {
		public = "request failed"
	}
	json.NewEncoder(w).Encode(map[string]any{"error": public})
}
