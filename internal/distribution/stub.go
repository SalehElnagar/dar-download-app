package distribution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/SalehElnagar/dar-download-app/internal/strictjson"
)

// MailStub is an internal-only, no-delivery SendGrid-compatible POC endpoint.
type MailStub struct {
	scenario     string
	timeoutDelay time.Duration
	mu           sync.Mutex
	requestCount int
	lastDigest   string
}

// NewMailStub creates one deterministic fault scenario.
func NewMailStub(scenario string, timeoutDelay time.Duration) (*MailStub, error) {
	switch scenario {
	case "accepted", "throttle", "reject", "unavailable", "timeout":
	default:
		return nil, ErrContract
	}
	if timeoutDelay == 0 {
		timeoutDelay = 15 * time.Second
	}
	if timeoutDelay < 100*time.Millisecond || timeoutDelay > 30*time.Second {
		return nil, ErrContract
	}
	return &MailStub{scenario: scenario, timeoutDelay: timeoutDelay}, nil
}

// ServeHTTP handles only health and the fixed mail-send path.
func (stub *MailStub) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	if request.Method == http.MethodGet && request.URL.Path == "/healthz" {
		body := []byte(`{"service":"dar-mail-stub","status":"ok"}`)
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusOK)
		// nosemgrep: go.lang.security.audit.xss.no-direct-write-to-responsewriter.no-direct-write-to-responsewriter -- compile-time JSON with an application/json content type
		_, _ = response.Write(body)
		return
	}
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if request.URL.Path != "/v3/mail/send" {
		response.WriteHeader(http.StatusNotFound)
		return
	}
	if request.Header.Get("Authorization") != "" {
		response.WriteHeader(http.StatusBadRequest)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 64*1024)
	body, err := io.ReadAll(request.Body)
	if err != nil || len(body) == 0 || len(body) > 64*1024 || !validStubPayload(body) {
		response.WriteHeader(http.StatusBadRequest)
		return
	}
	digest := sha256.Sum256(body)
	digestText := hex.EncodeToString(digest[:])
	stub.mu.Lock()
	stub.requestCount++
	stub.lastDigest = digestText
	stub.mu.Unlock()
	response.Header().Set("X-Stub-Request-Id", "stub-"+digestText[:24])
	switch stub.scenario {
	case "throttle":
		response.Header().Set("Retry-After", "1")
		response.WriteHeader(http.StatusTooManyRequests)
	case "reject":
		response.WriteHeader(http.StatusBadRequest)
	case "unavailable":
		response.Header().Set("Retry-After", "1")
		response.WriteHeader(http.StatusServiceUnavailable)
	case "timeout":
		if waitRequest(request.Context(), stub.timeoutDelay) {
			response.WriteHeader(http.StatusOK)
		}
	default:
		response.WriteHeader(http.StatusOK)
	}
}

// RequestCount is non-PII evidence for the POC only.
func (stub *MailStub) RequestCount() int {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return stub.requestCount
}

// LastRequestSHA256 returns only a body digest, never the mail payload.
func (stub *MailStub) LastRequestSHA256() string {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return stub.lastDigest
}

type stubPayload struct {
	Content []struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	} `json:"content"`
	From struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	} `json:"from"`
	Personalizations []struct {
		CustomArgs map[string]string `json:"custom_args"`
		To         []struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"to"`
	} `json:"personalizations"`
	Subject string `json:"subject"`
}

func validStubPayload(body []byte) bool {
	var payload stubPayload
	if strictjson.Decode(body, 64*1024, &payload) != nil || len(payload.Content) != 2 ||
		len(payload.Personalizations) != 1 || len(payload.Personalizations[0].To) != 1 ||
		payload.From.Email == "" || payload.Subject == "" || len(payload.Subject) > 256 {
		return false
	}
	for _, content := range payload.Content {
		if content.Value == "" || len(content.Value) > 16*1024 ||
			(content.Type != "text/plain" && content.Type != "text/html") {
			return false
		}
	}
	return true
}

func waitRequest(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
