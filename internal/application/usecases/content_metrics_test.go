package usecases_test

import (
	"context"
	"sync"
	"testing"

	"github.com/IQVO/product-content/internal/application/ports"
	"github.com/IQVO/product-content/internal/application/usecases"
	"github.com/IQVO/product-content/internal/domain/content"
)

// recordingMetrics counts the ports.ContentMetrics calls of a use case.
type recordingMetrics struct {
	mu        sync.Mutex
	published int
	rejected  []string
}

var _ ports.ContentMetrics = (*recordingMetrics)(nil)

func (m *recordingMetrics) ContentPublished(context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.published++
}

func (m *recordingMetrics) ContentPublishRejected(_ context.Context, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rejected = append(m.rejected, reason)
}

func TestPublishCountsEachAttemptOnceByOutcome(t *testing.T) {
	f := newFx(t)
	m := &recordingMetrics{}
	f.deps.Metrics = m
	publish := usecases.PublishContent{Deps: f.deps}
	f.create("item-1")

	_, err := publish.Execute(bg(), "item-1")
	must(t, err)
	eq(t, "published after a publish", m.published, 1)

	// Nothing new to publish is not an attempt.
	_, err = publish.Execute(bg(), "item-1")
	must(t, err)
	eq(t, "published after a no-op", m.published, 1)
	eq(t, "rejected after a no-op", len(m.rejected), 0)

	_, err = publish.Execute(bg(), "item-none")
	wantErr(t, err, ports.ErrNotFound)
	eq(t, "rejected", len(m.rejected), 1)
	eq(t, "reason", m.rejected[0], "not-found")
	eq(t, "published after a refusal", m.published, 1)
}

func TestPublishRefusalReasonIsTheProblemSlug(t *testing.T) {
	f := newFx(t)
	m := &recordingMetrics{}
	f.deps.Metrics = m
	f.item("item-1")
	_, err := usecases.CreateProductContent{Deps: f.deps}.Execute(bg(), usecases.ContentInput{CatalogItemID: "item-1", Body: content.Body{Description: "only text"}})
	must(t, err)

	_, err = usecases.PublishContent{Deps: f.deps}.Execute(bg(), "item-1")
	wantErr(t, err, content.ErrIncompleteContent)
	eq(t, "rejections", len(m.rejected), 1)
	eq(t, "reason", m.rejected[0], "incomplete-content")
}
