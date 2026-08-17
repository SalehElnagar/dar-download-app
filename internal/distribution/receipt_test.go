package distribution

import (
	"testing"
	"time"
)

func TestReceiptSuccessTransitionsAreMonotonicAndTerminal(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 17, 19, 0, 0, 0, time.UTC)
	receipt, err := ClaimReceipt(ReceiptIntent{
		OperationID: repeatHex("a", 64), ReleaseID: "dar_receipt_test_01",
		ReleaseVersion: "1.0.0", RecipientID: repeatHex("b", 64),
		HMACKeyVersion: "v1", Provider: "stub", QueueMessageID: repeatHex("a", 64) + ":0",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	started, err := receipt.StartSend(now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := started.Simulate(now.Add(2*time.Second), 200, "stub-request-1")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.StateVersion != 1 || started.StateVersion != 2 || accepted.StateVersion != 3 {
		t.Fatalf("state versions = %d, %d, %d", receipt.StateVersion, started.StateVersion, accepted.StateVersion)
	}
	if accepted.Status != ReceiptSimulated || accepted.TerminalAt == nil {
		t.Fatalf("terminal receipt = %#v", accepted)
	}
	if _, err := accepted.StartSend(now.Add(3 * time.Second)); err == nil {
		t.Fatal("terminal receipt reopened")
	}
}

func TestStaleSendStartedBecomesUnknownWithoutResend(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 17, 19, 0, 0, 0, time.UTC)
	receipt, err := ClaimReceipt(ReceiptIntent{
		OperationID: repeatHex("a", 64), ReleaseID: "dar_receipt_test_01",
		ReleaseVersion: "1.0.0", RecipientID: repeatHex("b", 64),
		HMACKeyVersion: "v1", Provider: "stub", QueueMessageID: repeatHex("a", 64) + ":0",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	started, err := receipt.StartSend(now)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := started.RecoverStale(now.Add(6*time.Minute), 5*time.Minute, 5)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != ReceiptUnknown || recovered.ReasonCode == nil || *recovered.ReasonCode != "STALE_SEND_OUTCOME" {
		t.Fatalf("RecoverStale() = %#v", recovered)
	}
}
