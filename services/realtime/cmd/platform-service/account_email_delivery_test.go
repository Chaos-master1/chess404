package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chess404/realtime/internal/platform"
)

type stubAccountEmailSender struct {
	provider string
	results  []stubAccountEmailSendResult
	calls    int
}

type stubAccountEmailSendResult struct {
	messageID string
	err       error
}

func (s *stubAccountEmailSender) Provider() string { return s.provider }
func (s *stubAccountEmailSender) Enabled() bool    { return true }

func (s *stubAccountEmailSender) Send(ctx context.Context, delivery platform.AccountEmailDelivery) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	if s.calls >= len(s.results) {
		return "", errors.New("unexpected send call")
	}
	result := s.results[s.calls]
	s.calls++
	return result.messageID, result.err
}

func TestAccountEmailDispatcherMarksPreviewDeliveryDelivered(t *testing.T) {
	t.Parallel()

	outbox, err := platform.NewAccountEmailOutboxStore(filepath.Join(t.TempDir(), "outbox.json"))
	if err != nil {
		t.Fatalf("NewAccountEmailOutboxStore error = %v", err)
	}
	defer func() { _ = outbox.Close() }()

	delivery, err := outbox.QueueDelivery(platform.AccountEmailDeliveryRequest{
		AccountID: "acct_alpha",
		Email:     "alpha@example.com",
		Kind:      platform.AccountEmailDeliveryKindEmailVerification,
		Subject:   "Verify",
		TextBody:  "verify",
		HTMLBody:  "<p>verify</p>",
		ActionURL: "https://example.com/auth?auth=verify-email",
	})
	if err != nil {
		t.Fatalf("QueueDelivery error = %v", err)
	}

	sender := &stubAccountEmailSender{
		provider: "preview",
		results: []stubAccountEmailSendResult{
			{messageID: "preview:" + delivery.DeliveryID},
		},
	}
	now := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	dispatcher := newAccountEmailDispatcher(outbox, sender, func() time.Time { return now })
	dispatcher.processBatch(context.Background())

	overview := outbox.ListOverview("acct_alpha", 8)
	if len(overview.Deliveries) != 1 {
		t.Fatalf("ListOverview deliveries = %d, want 1", len(overview.Deliveries))
	}
	got := overview.Deliveries[0]
	if got.Status != platform.AccountEmailDeliveryStatusDelivered {
		t.Fatalf("delivery status = %q, want delivered", got.Status)
	}
	if got.AttemptCount != 1 {
		t.Fatalf("delivery attempts = %d, want 1", got.AttemptCount)
	}
	if got.Provider != "preview" {
		t.Fatalf("delivery provider = %q, want preview", got.Provider)
	}
	if sender.calls != 1 {
		t.Fatalf("sender calls = %d, want 1", sender.calls)
	}
}

func TestAccountEmailDispatcherRetriesThenFails(t *testing.T) {
	t.Parallel()

	outbox, err := platform.NewAccountEmailOutboxStore(filepath.Join(t.TempDir(), "outbox.json"))
	if err != nil {
		t.Fatalf("NewAccountEmailOutboxStore error = %v", err)
	}
	defer func() { _ = outbox.Close() }()

	_, err = outbox.QueueDelivery(platform.AccountEmailDeliveryRequest{
		AccountID: "acct_beta",
		Email:     "beta@example.com",
		Kind:      platform.AccountEmailDeliveryKindPasswordReset,
		Subject:   "Reset",
		TextBody:  "reset",
		HTMLBody:  "<p>reset</p>",
		ActionURL: "https://example.com/auth?auth=reset-password",
	})
	if err != nil {
		t.Fatalf("QueueDelivery error = %v", err)
	}

	sender := &stubAccountEmailSender{
		provider: "smtp",
		results: []stubAccountEmailSendResult{
			{err: errors.New("temporary smtp error")},
			{err: errors.New("final smtp error")},
		},
	}
	firstAttempt := time.Date(2026, 5, 16, 12, 30, 0, 0, time.UTC)
	dispatcher := newAccountEmailDispatcher(outbox, sender, func() time.Time { return firstAttempt })
	dispatcher.maxAttempts = 2
	dispatcher.baseRetry = 10 * time.Second
	dispatcher.maxRetry = 10 * time.Second
	dispatcher.processBatch(context.Background())

	overview := outbox.ListOverview("acct_beta", 8)
	if len(overview.Deliveries) != 1 {
		t.Fatalf("ListOverview deliveries = %d, want 1", len(overview.Deliveries))
	}
	if overview.Deliveries[0].Status != platform.AccountEmailDeliveryStatusQueued {
		t.Fatalf("after first attempt status = %q, want queued", overview.Deliveries[0].Status)
	}
	if overview.Deliveries[0].NextAttemptAt == nil {
		t.Fatalf("after first attempt nextAttemptAt is nil")
	}

	secondAttempt := firstAttempt.Add(15 * time.Second)
	dispatcher.now = func() time.Time { return secondAttempt }
	dispatcher.processBatch(context.Background())

	overview = outbox.ListOverview("acct_beta", 8)
	if len(overview.Deliveries) != 1 {
		t.Fatalf("ListOverview deliveries = %d, want 1", len(overview.Deliveries))
	}
	got := overview.Deliveries[0]
	if got.Status != platform.AccountEmailDeliveryStatusFailed {
		t.Fatalf("after second attempt status = %q, want failed", got.Status)
	}
	if got.AttemptCount != 2 {
		t.Fatalf("after second attempt attempts = %d, want 2", got.AttemptCount)
	}
	if sender.calls != 2 {
		t.Fatalf("sender calls = %d, want 2", sender.calls)
	}
}

func TestResendAccountEmailSenderSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test_key" {
			http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_12345"}`))
	}))
	defer server.Close()

	sender := resendAccountEmailSender{
		apiKey: "test_key",
		from:   "Chess404 <onboarding@resend.dev>",
		client: server.Client(),
	}

	// We test Send with a custom request pointing to our test server
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+sender.apiKey)
	resp, err := sender.client.Do(req)
	if err != nil {
		t.Fatalf("client.Do failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestResendAccountEmailSenderRateLimit(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		statusCode int
		body       string
	}{
		{
			name:       "HTTP 429 Status",
			statusCode: http.StatusTooManyRequests,
			body:       `{"message":"Too many requests","name":"rate_limit_exceeded"}`,
		},
		{
			name:       "Rate limit error message",
			statusCode: http.StatusUnprocessableEntity,
			body:       `{"message":"Daily quota exceeded for sending emails"}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			// Create a transport that redirects api.resend.com to test server
			sender := resendAccountEmailSender{
				apiKey: "test_key",
				from:   "Chess404 <onboarding@resend.dev>",
				client: &http.Client{
					Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
						r.URL.Scheme = "http"
						r.URL.Host = server.Listener.Addr().String()
						return http.DefaultTransport.RoundTrip(r)
					}),
				},
			}

			_, err := sender.Send(context.Background(), platform.AccountEmailDelivery{
				DeliveryID: "del_1",
				AccountID:  "acct_1",
				Email:      "user@example.com",
				Subject:    "Reset Password",
				HTMLBody:   "<p>reset</p>",
			})
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "email delivery limit reached. Please try again another time") {
				t.Fatalf("expected rate limit message, got: %v", err)
			}
		})
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

