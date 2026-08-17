package distribution

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMailStubHealthAndAcceptedSimulationRetainNoRequestBody(t *testing.T) {
	t.Parallel()
	stub, err := NewMailStub("accepted", 0)
	if err != nil {
		t.Fatal(err)
	}
	health := httptest.NewRecorder()
	stub.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d", health.Code)
	}

	payload, err := jsonMailPayloadForTest()
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v3/mail/send", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	stub.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Header().Get("X-Stub-Request-Id") == "" {
		t.Fatalf("response = %d %#v", response.Code, response.Header())
	}
	if stub.RequestCount() != 1 || len(stub.LastRequestSHA256()) != 64 {
		t.Fatalf("stub evidence = %d %q", stub.RequestCount(), stub.LastRequestSHA256())
	}
}

func TestMailStubRejectsAuthorizationAndInjectsThrottle(t *testing.T) {
	t.Parallel()
	stub, err := NewMailStub("throttle", 0)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := jsonMailPayloadForTest()
	unauthorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v3/mail/send", bytes.NewReader(payload))
	request.Header.Set("Authorization", "Bearer should-not-reach-stub")
	stub.ServeHTTP(unauthorized, request)
	if unauthorized.Code != http.StatusBadRequest || stub.RequestCount() != 0 {
		t.Fatalf("authorization response = %d count=%d", unauthorized.Code, stub.RequestCount())
	}

	throttled := httptest.NewRecorder()
	stub.ServeHTTP(throttled, httptest.NewRequest(http.MethodPost, "/v3/mail/send", bytes.NewReader(payload)))
	if throttled.Code != http.StatusTooManyRequests || throttled.Header().Get("Retry-After") != "1" {
		t.Fatalf("throttle response = %d %#v", throttled.Code, throttled.Header())
	}
}

func jsonMailPayloadForTest() ([]byte, error) {
	mailer, err := NewHTTPMailer(MailConfig{
		Mode: MailModeStub, FromEmail: "dar-poc@example.com", FromName: "DAR POC",
		AllowedRecipients: []string{"ava.example@example.com"},
		StubEndpoint:      "https://mail-stub.example.internal/v3/mail/send",
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(buildMailPayload(mailer, testNotification(), "ava.example@example.com"))
}
