package distribution

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestStubMailerUsesFixedInternalEndpointWithoutAuthorization(t *testing.T) {
	t.Parallel()
	var captured *http.Request
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		captured = request
		return response(http.StatusOK, map[string]string{"X-Stub-Request-Id": "stub-1"}), nil
	})}
	mailer, err := NewHTTPMailer(MailConfig{
		Mode: MailModeStub, FromEmail: "dar-poc@example.com", FromName: "DAR POC",
		AllowedRecipients: []string{"ava.example@example.com"},
		StubEndpoint:      "https://mail-stub.example.internal/v3/mail/send", Client: client,
	})
	if err != nil {
		t.Fatal(err)
	}

	result := mailer.Send(context.Background(), testNotification())

	if result.Outcome != MailSimulated || result.RequestID != "stub-1" {
		t.Fatalf("Send() = %#v", result)
	}
	if captured.URL.String() != "https://mail-stub.example.internal/v3/mail/send" ||
		captured.Header.Get("Authorization") != "" {
		t.Fatalf("request = %#v", captured)
	}
	body, _ := io.ReadAll(captured.Body)
	if !strings.Contains(string(body), "ava.example@example.com") ||
		strings.Contains(strings.ToLower(string(body)), "blob.core.windows.net") {
		t.Fatalf("body = %s", body)
	}
}

func TestSandboxMailerUsesOfficialEndpointAndSimulationFlag(t *testing.T) {
	t.Parallel()
	var captured *http.Request
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		captured = request
		return response(http.StatusOK, nil), nil
	})}
	mailer, err := NewHTTPMailer(MailConfig{
		Mode: MailModeSandbox, FromEmail: "verified@example.com", FromName: "DAR POC",
		AllowedRecipients: []string{"ava.example@example.com"}, APIKey: "unit-provider-value",
		Client: client,
	})
	if err != nil {
		t.Fatal(err)
	}

	result := mailer.Send(context.Background(), testNotification())

	if result.Outcome != MailSimulated || captured.URL.String() != SendGridEndpoint ||
		captured.Header.Get("Authorization") != "Bearer unit-provider-value" {
		t.Fatalf("result=%#v request=%#v", result, captured)
	}
	body, _ := io.ReadAll(captured.Body)
	if !strings.Contains(string(body), `"sandbox_mode":{"enable":true}`) {
		t.Fatalf("body = %s", body)
	}
}

func TestMailerRejectsRecipientOutsideAllowlistBeforeNetwork(t *testing.T) {
	t.Parallel()
	called := false
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return response(http.StatusAccepted, nil), nil
	})}
	mailer, err := NewHTTPMailer(MailConfig{
		Mode: MailModeLive, FromEmail: "verified@example.com", FromName: "DAR POC",
		AllowedRecipients: []string{"allowed@example.com"}, APIKey: "unit-provider-value",
		Client: client,
	})
	if err != nil {
		t.Fatal(err)
	}

	result := mailer.Send(context.Background(), testNotification())

	if result.Outcome != MailPermanent || result.ReasonCode != "RECIPIENT_NOT_ALLOWED" || called {
		t.Fatalf("result=%#v called=%v", result, called)
	}
}

func TestMailerTreatsMissingProviderResponseAsUnknown(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection reset")
	})}
	mailer, err := NewHTTPMailer(MailConfig{
		Mode: MailModeStub, FromEmail: "dar-poc@example.com", FromName: "DAR POC",
		AllowedRecipients: []string{"ava.example@example.com"},
		StubEndpoint:      "https://mail-stub.example.internal/v3/mail/send", Client: client,
	})
	if err != nil {
		t.Fatal(err)
	}

	result := mailer.Send(context.Background(), testNotification())

	if result.Outcome != MailUnknown || result.ReasonCode != "PROVIDER_OUTCOME_UNKNOWN" {
		t.Fatalf("Send() = %#v", result)
	}
}

func testNotification() Notification {
	return Notification{
		OperationID: repeatHex("a", 64), ReleaseID: "dar_mail_test_01", ReleaseVersion: "1.0.0",
		RecipientID: repeatHex("b", 64), FirstName: "Ava", LastName: "Example",
		Email:          "ava.example@example.com",
		ApplicationURL: "https://dar-poc.example.internal/v1/releases/dar_mail_test_01/download",
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func response(status int, headers map[string]string) *http.Response {
	header := make(http.Header)
	for name, value := range headers {
		header.Set(name, value)
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader("provider body is ignored")),
	}
}
