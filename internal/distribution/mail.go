package distribution

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// SendGridEndpoint is fixed so runtime configuration cannot redirect provider credentials.
const SendGridEndpoint = "https://api.sendgrid.com/v3/mail/send"

// MailMode separates no-delivery simulation from explicitly approved live delivery.
type MailMode string

const (
	MailModeStub    MailMode = "stub"
	MailModeSandbox MailMode = "sendgrid_sandbox"
	MailModeLive    MailMode = "sendgrid_live"
)

// MailConfig is immutable provider policy. APIKey must come from a runtime secret reference.
type MailConfig struct {
	Mode              MailMode
	FromEmail         string
	FromName          string
	AllowedRecipients []string
	APIKey            string
	StubEndpoint      string
	Timeout           time.Duration
	Client            *http.Client
}

// HTTPMailer sends one bounded SendGrid-compatible request.
type HTTPMailer struct {
	mode      MailMode
	fromEmail string
	fromName  string
	allowed   map[string]struct{}
	apiKey    string
	endpoint  string
	client    *http.Client
}

// NewHTTPMailer validates endpoints and allowlists before any request can be sent.
func NewHTTPMailer(config MailConfig) (*HTTPMailer, error) {
	fromEmail, ok := canonicalEmail(config.FromEmail)
	if !ok || config.FromName == "" || len(config.FromName) > 64 || !controlFree(config.FromName) {
		return nil, ErrContract
	}
	maximumRecipients := 2
	if config.Mode == MailModeStub {
		maximumRecipients = 500
	}
	if len(config.AllowedRecipients) < 1 || len(config.AllowedRecipients) > maximumRecipients {
		return nil, ErrContract
	}
	allowed := make(map[string]struct{}, len(config.AllowedRecipients))
	for _, raw := range config.AllowedRecipients {
		email, valid := canonicalEmail(raw)
		if !valid {
			return nil, ErrContract
		}
		if _, duplicate := allowed[email]; duplicate {
			return nil, ErrContract
		}
		allowed[email] = struct{}{}
	}
	endpoint := SendGridEndpoint
	switch config.Mode {
	case MailModeStub:
		if config.APIKey != "" || !validStubEndpoint(config.StubEndpoint) {
			return nil, ErrContract
		}
		endpoint = config.StubEndpoint
	case MailModeSandbox, MailModeLive:
		if config.APIKey == "" || config.StubEndpoint != "" {
			return nil, ErrContract
		}
	default:
		return nil, ErrContract
	}
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	if timeout < 100*time.Millisecond || timeout > 30*time.Second {
		return nil, ErrContract
	}
	client := config.Client
	if client == nil {
		client = &http.Client{
			Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &HTTPMailer{
		mode: config.Mode, fromEmail: fromEmail, fromName: config.FromName,
		allowed: allowed, apiKey: config.APIKey, endpoint: endpoint, client: client,
	}, nil
}

// Send produces no log output and never returns a provider response body.
func (mailer *HTTPMailer) Send(ctx context.Context, notification Notification) MailResult {
	email, valid := canonicalEmail(notification.Email)
	if !valid {
		return MailResult{Outcome: MailPermanent, ReasonCode: "RECIPIENT_INVALID"}
	}
	if _, allowed := mailer.allowed[email]; !allowed {
		return MailResult{Outcome: MailPermanent, ReasonCode: "RECIPIENT_NOT_ALLOWED"}
	}
	body, err := json.Marshal(buildMailPayload(mailer, notification, email))
	if err != nil || len(body) == 0 || len(body) > 64*1024 {
		return MailResult{Outcome: MailPermanent, ReasonCode: "MAIL_PAYLOAD_INVALID"}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, mailer.endpoint, bytes.NewReader(body))
	if err != nil {
		return MailResult{Outcome: MailPermanent, ReasonCode: "MAIL_PAYLOAD_INVALID"}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "dar-distribution-go/0.1")
	if mailer.mode != MailModeStub {
		request.Header.Set("Authorization", "Bearer "+mailer.apiKey)
	}
	response, err := mailer.client.Do(request)
	if err != nil {
		return MailResult{Outcome: MailUnknown, ReasonCode: "PROVIDER_OUTCOME_UNKNOWN"}
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
	return classifyMailResponse(mailer.mode, response)
}

func buildMailPayload(mailer *HTTPMailer, notification Notification, email string) map[string]any {
	displayName := strings.TrimSpace(notification.FirstName + " " + notification.LastName)
	plain := "Hello " + notification.FirstName + ",\n\nDAR release " + notification.ReleaseVersion +
		" is ready.\nSign in to download it: " + notification.ApplicationURL + "\n"
	htmlBody := "<p>Hello " + html.EscapeString(notification.FirstName) + "</p>" +
		"<p>DAR release " + html.EscapeString(notification.ReleaseVersion) + " is ready.</p>" +
		`<p><a href="` + html.EscapeString(notification.ApplicationURL) + `">Sign in to download the DAR</a></p>`
	payload := map[string]any{
		"content": []map[string]string{
			{"type": "text/plain", "value": plain},
			{"type": "text/html", "value": htmlBody},
		},
		"from": map[string]string{"email": mailer.fromEmail, "name": mailer.fromName},
		"personalizations": []any{map[string]any{
			"custom_args": map[string]string{
				"operation_id": notification.OperationID,
				"recipient_id": notification.RecipientID,
				"release_id":   notification.ReleaseID,
			},
			"to": []map[string]string{{"email": email, "name": displayName}},
		}},
		"subject": "DAR release " + notification.ReleaseVersion + " is ready",
	}
	if mailer.mode == MailModeSandbox {
		payload["mail_settings"] = map[string]any{
			"sandbox_mode": map[string]bool{"enable": true},
		}
	}
	return payload
}

func classifyMailResponse(mode MailMode, response *http.Response) MailResult {
	requestID := boundedHeader(response.Header, "X-Message-Id")
	if requestID == "" {
		requestID = boundedHeader(response.Header, "X-Stub-Request-Id")
	}
	if (mode == MailModeStub || mode == MailModeSandbox) && response.StatusCode == http.StatusOK {
		return MailResult{Outcome: MailSimulated, HTTPStatus: http.StatusOK, RequestID: requestID}
	}
	if mode == MailModeLive && response.StatusCode == http.StatusAccepted {
		return MailResult{Outcome: MailAccepted, HTTPStatus: http.StatusAccepted, RequestID: requestID}
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return MailResult{
			Outcome: MailRetryable, HTTPStatus: response.StatusCode, RequestID: requestID,
			ReasonCode: "PROVIDER_THROTTLED", RetryAfter: retryAfter(response.Header),
		}
	}
	if response.StatusCode >= 500 && response.StatusCode <= 599 {
		return MailResult{
			Outcome: MailRetryable, HTTPStatus: response.StatusCode, RequestID: requestID,
			ReasonCode: "PROVIDER_UNAVAILABLE", RetryAfter: retryAfter(response.Header),
		}
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return MailResult{Outcome: MailPermanent, HTTPStatus: response.StatusCode, RequestID: requestID, ReasonCode: "PROVIDER_AUTH_FAILED"}
	}
	if response.StatusCode >= 400 && response.StatusCode <= 499 {
		return MailResult{Outcome: MailPermanent, HTTPStatus: response.StatusCode, RequestID: requestID, ReasonCode: "PROVIDER_REJECTED"}
	}
	return MailResult{Outcome: MailUnknown, HTTPStatus: response.StatusCode, RequestID: requestID, ReasonCode: "PROVIDER_PROTOCOL_UNKNOWN"}
}

func canonicalEmail(value string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(value))
	return email, len(email) <= 254 && emailPattern.MatchString(email)
}

func validStubEndpoint(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.Fragment != "" || parsed.Path != "/v3/mail/send" || parsed.Port() != "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return strings.HasSuffix(host, ".azurecontainerapps.io") || strings.HasSuffix(host, ".example.internal")
}

func boundedHeader(headers http.Header, name string) string {
	value := headers.Get(name)
	if len(value) > 128 {
		return value[:128]
	}
	return value
}

func retryAfter(headers http.Header) time.Duration {
	seconds, err := strconv.Atoi(headers.Get("Retry-After"))
	if err != nil || seconds < 1 {
		return time.Second
	}
	if seconds > 300 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}
