package distribution

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
)

const storageAudience = "https://storage.azure.com/"

// RunWorker assembles managed-identity Azure clients and consumes one Peek-Lock message at a time.
func RunWorker(ctx context.Context, logger *slog.Logger, config RuntimeConfig) error {
	credential, err := azidentity.NewManagedIdentityCredential(
		&azidentity.ManagedIdentityCredentialOptions{ID: azidentity.ClientID(config.IdentityClientID)},
	)
	if err != nil {
		return ErrWorkerConfig
	}
	storageClient, err := service.NewClient(
		"https://"+config.StorageAccount+".blob.core.windows.net/",
		credential,
		&service.ClientOptions{Audience: storageAudience},
	)
	if err != nil {
		return ErrWorkerConfig
	}
	blobs, err := NewAzureBlobStore(storageClient, config.ManifestsContainer, config.BatchesContainer)
	if err != nil {
		return err
	}
	receipts, err := NewAzureReceiptStore(storageClient.NewContainerClient(config.ReceiptsContainer))
	if err != nil {
		return err
	}
	mailer, err := NewHTTPMailer(config.MailerConfig())
	if err != nil {
		return err
	}
	worker := NewWorker(WorkerOptions{
		Blobs: blobs, Receipts: receipts, Mailer: mailer, Auditor: slogAuditor{logger: logger},
		HMACKey: config.HMACKey(), HMACKeyVersion: config.HMACKeyVersion,
		Provider: config.Provider(), Clock: func() time.Time { return time.Now().UTC() },
		MaxAttempts: config.MaxAttempts, ClaimTimeout: config.ClaimTimeout,
	})
	serviceBus, err := azservicebus.NewClient(
		config.ServiceBusNamespace,
		credential,
		&azservicebus.ClientOptions{
			ApplicationID: "dar-distribution-go/0.1",
			RetryOptions: azservicebus.RetryOptions{
				MaxRetries: 3, RetryDelay: time.Second, MaxRetryDelay: 10 * time.Second,
			},
		},
	)
	if err != nil {
		return ErrWorkerConfig
	}
	defer serviceBus.Close(context.Background())
	receiver, err := serviceBus.NewReceiverForQueue(
		config.ServiceBusQueue,
		&azservicebus.ReceiverOptions{ReceiveMode: azservicebus.ReceiveModePeekLock},
	)
	if err != nil {
		return ErrWorkerConfig
	}
	defer receiver.Close(context.Background())

	readiness := &atomic.Bool{}
	health := newWorkerHealthServer(config.HealthPort, readiness)
	healthErrors := make(chan error, 1)
	go func() { healthErrors <- health.ListenAndServe() }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = health.Shutdown(shutdownCtx)
	}()
	readiness.Store(true)
	logger.Info("worker starting", "event", "startup", "mail_mode", config.MailMode)

	for {
		select {
		case <-ctx.Done():
			logger.Info("worker stopped", "event", "shutdown")
			return nil
		case healthErr := <-healthErrors:
			if healthErr != nil && !errors.Is(healthErr, http.ErrServerClosed) {
				return errors.New("health server stopped")
			}
			return nil
		default:
		}
		receiveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		messages, receiveErr := receiver.ReceiveMessages(receiveCtx, 1, nil)
		cancel()
		if receiveErr != nil {
			if errors.Is(receiveErr, context.DeadlineExceeded) || errors.Is(receiveErr, context.Canceled) {
				continue
			}
			logger.Warn("queue receive unavailable", "event", "queue_receive_failed")
			if !waitContext(ctx, 2*time.Second) {
				return nil
			}
			continue
		}
		for _, message := range messages {
			if err := processReceivedMessage(ctx, receiver, worker, message); err != nil {
				logger.Warn("message processing interrupted", "event", "message_processing_interrupted")
			}
		}
	}
}

func processReceivedMessage(
	parent context.Context,
	receiver *azservicebus.Receiver,
	worker *Worker,
	message *azservicebus.ReceivedMessage,
) error {
	messageCtx, cancel := context.WithCancel(parent)
	renewDone := make(chan error, 1)
	go renewMessageLock(messageCtx, cancel, receiver, message, renewDone)
	result := worker.Process(messageCtx, message.MessageID, message.Body)
	if result.Disposition == Abandon && !waitContext(messageCtx, boundedDelay(result.RetryAfter)) {
		cancel()
		<-renewDone
		return context.Canceled
	}
	cancel()
	renewErr := <-renewDone
	if renewErr != nil {
		return renewErr
	}
	settleCtx, settleCancel := context.WithTimeout(parent, 15*time.Second)
	defer settleCancel()
	switch result.Disposition {
	case Complete:
		return receiver.CompleteMessage(settleCtx, message, nil)
	case DeadLetter:
		reason := result.ReasonCode
		if reason == "" {
			reason = "WORKER_REJECTED"
		}
		description := "DAR distribution worker rejected the bounded operation"
		return receiver.DeadLetterMessage(settleCtx, message, &azservicebus.DeadLetterOptions{
			Reason: &reason, ErrorDescription: &description,
		})
	default:
		return receiver.AbandonMessage(settleCtx, message, nil)
	}
}

func renewMessageLock(
	ctx context.Context,
	cancel context.CancelFunc,
	receiver *azservicebus.Receiver,
	message *azservicebus.ReceivedMessage,
	done chan<- error,
) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	deadline := time.NewTimer(15 * time.Minute)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case <-deadline.C:
			cancel()
			done <- errors.New("message lock renewal budget exhausted")
			return
		case <-ticker.C:
			renewCtx, renewCancel := context.WithTimeout(ctx, 10*time.Second)
			err := receiver.RenewMessageLock(renewCtx, message, nil)
			renewCancel()
			if err != nil {
				if ctx.Err() != nil {
					done <- nil
					return
				}
				cancel()
				done <- errors.New("message lock renewal failed")
				return
			}
		}
	}
}

type slogAuditor struct {
	logger *slog.Logger
}

func (auditor slogAuditor) Emit(event AuditEvent) {
	auditor.logger.Info(
		"notification state",
		"schema_version", "1.0",
		"event", event.EventName,
		"occurred_at", event.OccurredAt,
		"operation_id", event.OperationID,
		"release_id", event.ReleaseID,
		"release_version", event.ReleaseVersion,
		"recipient_id", event.RecipientID,
		"hmac_key_version", event.HMACKeyVersion,
		"queue_message_id", event.QueueMessageID,
		"provider", event.Provider,
		"receipt_status", event.ReceiptStatus,
		"receipt_state_version", event.ReceiptStateVersion,
		"attempt_count", event.AttemptCount,
		"provider_http_status", event.ProviderHTTPStatus,
		"reason_code", event.ReasonCode,
	)
}

func newWorkerHealthServer(port int, ready *atomic.Bool) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Content-Type", "application/json")
		if !ready.Load() {
			response.WriteHeader(http.StatusServiceUnavailable)
			_, _ = response.Write([]byte(`{"service":"dar-distribution-worker","status":"starting"}`))
			return
		}
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte(`{"service":"dar-distribution-worker","status":"ok"}`))
	})
	return &http.Server{
		Addr: ":" + strconv.Itoa(port), Handler: mux,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second,
		BaseContext: func(net.Listener) context.Context { return context.Background() },
	}
}

func waitContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
