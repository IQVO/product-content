package kafka_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	ce "github.com/cloudevents/sdk-go/v2/event"
	kafkago "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	kafkaconsumer "github.com/IQVO/product-content/internal/adapters/inbound/kafka"
)

const (
	publisherTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	probeType            = "com.retailnetwork.commerce.test.probe.Probed"
)

var errInjected = errors.New("injected transient database failure")

// probeReader hands out one message, then blocks until the context ends; the
// commit of that message is signalled on committed.
type probeReader struct {
	msg       kafkago.Message
	served    bool
	committed chan struct{}
}

func (r *probeReader) FetchMessage(ctx context.Context) (kafkago.Message, error) {
	if !r.served {
		r.served = true
		return r.msg, nil
	}
	<-ctx.Done()
	return kafkago.Message{}, ctx.Err()
}

func (r *probeReader) CommitMessages(context.Context, ...kafkago.Message) error {
	close(r.committed)
	return nil
}

func (r *probeReader) Close() error { return nil }

// tracedRun feeds one message with the given headers through Consumer.Run and
// returns the span context the handler saw and the consumer's ended spans. The
// handler fails with failWith when it is non-nil.
func tracedRun(t *testing.T, headers []kafkago.Header, failWith error) (trace.SpanContext, []sdktrace.ReadOnlySpan) {
	t.Helper()
	tp, prop := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() { otel.SetTracerProvider(tp); otel.SetTextMapPropagator(prop) })
	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	otel.SetTextMapPropagator(propagation.TraceContext{})

	var (
		mu   sync.Mutex
		seen trace.SpanContext
	)
	reader := &probeReader{
		msg:       kafkago.Message{Topic: "t", Value: message(t, "e1", probeType, map[string]any{}), Headers: headers},
		committed: make(chan struct{}),
	}
	consumer := &kafkaconsumer.Consumer{
		Name: "probe", Topic: "t", Reader: reader, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Retry: kafkaconsumer.RetryPolicy{Initial: time.Millisecond, Max: 4 * time.Millisecond},
		Handlers: map[string]kafkaconsumer.HandlerFunc{probeType: func(ctx context.Context, _ ce.Event) error {
			mu.Lock()
			defer mu.Unlock()
			seen = trace.SpanContextFromContext(ctx)
			return failWith
		}},
	}
	// A failing handler is retried forever without a DLQ: stop after its first
	// attempt ended a span. A succeeding one stops on the offset commit.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- consumer.Run(ctx) }()
	if failWith != nil {
		waitForEndedSpan(t, recorder)
	} else {
		select {
		case <-reader.committed:
		case <-time.After(5 * time.Second):
			t.Fatal("the message was never committed")
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	return seen, recorder.Ended()
}

func waitForEndedSpan(t *testing.T, r *tracetest.SpanRecorder) {
	t.Helper()
	for i := 0; i < 5000 && len(r.Ended()) == 0; i++ {
		time.Sleep(time.Millisecond)
	}
	if len(r.Ended()) == 0 {
		t.Fatal("no consumer span ended")
	}
}

// The consumer span is a consumer-kind span named "<topic> process" whose parent
// is the publisher's span carried in traceparent, and the handler runs inside it.
func TestConsumerSpanIsParentedToThePublisherTrace(t *testing.T) {
	seen, spans := tracedRun(t, []kafkago.Header{
		{Key: "content-type", Value: []byte("application/cloudevents+json; charset=UTF-8")},
		{Key: "traceparent", Value: []byte(publisherTraceparent)},
	}, nil)

	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	span := spans[0]
	if span.SpanKind() != trace.SpanKindConsumer || span.Name() != "t process" {
		t.Errorf("span = %v %q, want consumer \"t process\"", span.SpanKind(), span.Name())
	}
	if got := span.SpanContext().TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace id = %s, want the publisher's", got)
	}
	if got := span.Parent().SpanID().String(); got != "00f067aa0ba902b7" {
		t.Errorf("parent span id = %s, want the publish span's", got)
	}
	if seen.SpanID() != span.SpanContext().SpanID() {
		t.Errorf("handler ran under span %s, want the consumer span %s", seen.SpanID(), span.SpanContext().SpanID())
	}
}

func TestConsumerWithoutTraceHeadersStartsANewTrace(t *testing.T) {
	_, spans := tracedRun(t, nil, nil)
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	if spans[0].Parent().IsValid() {
		t.Errorf("parent = %v, want none for a message without traceparent", spans[0].Parent())
	}
}

func TestConsumerSpanRecordsAHandlerFailure(t *testing.T) {
	_, spans := tracedRun(t, []kafkago.Header{{Key: "traceparent", Value: []byte(publisherTraceparent)}}, errInjected)
	if len(spans) == 0 || spans[0].Status().Code != codes.Error {
		t.Fatalf("spans = %v, want an errored consumer span", spans)
	}
}
