package outbox

import (
	"errors"
	"testing"
)

func TestSendFailureKeepsTheRunbookWording(t *testing.T) {
	cause := errors.New("broker down")
	f := &SendFailure{RowID: 7, Message: Message{EventType: "t.A"}, Attempts: 2, Err: cause}
	if got, want := f.Error(), "outbox row 7 (t.A): broker down"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(f, cause) {
		t.Fatal("Unwrap must expose the send error")
	}
}
