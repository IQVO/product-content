package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/IQVO/product-content/internal/application/ports"
)

// meterName scopes this service's own instruments, keeping them distinct from
// the ones otelchi and the runtime collector register.
const meterName = "github.com/IQVO/product-content"

// productsPublishedName is the Tier 2 business metric of the context (ADR 0005,
// the fleet's standard metrics convention): every publish attempt, by outcome.
const productsPublishedName = "content.products.published"

const (
	outcomeKey = attribute.Key("outcome")
	reasonKey  = attribute.Key("reason")

	outcomePublished = "published"
	outcomeRejected  = "rejected"
)

// ContentMetrics implements ports.ContentMetrics against the global
// MeterProvider. Until Setup installs a real provider the global one is a
// no-op, so recording is cheap and safe in tests and local runs.
type ContentMetrics struct {
	counter metric.Int64Counter
}

var _ ports.ContentMetrics = (*ContentMetrics)(nil)

// NewContentMetrics registers the counter. It only fails if the instrument name
// is invalid, which is a programming error, not a runtime condition. Call it
// after Setup so the instrument binds to the real MeterProvider.
func NewContentMetrics() (*ContentMetrics, error) {
	counter, err := otel.Meter(meterName).Int64Counter(
		productsPublishedName,
		metric.WithDescription("Publish attempts of product content by outcome (published or rejected; reason says why a "+
			"rejection happened, as a problem slug such as incomplete-content or item-discontinued). "+
			"A falling published share with a rising incomplete-content means merchandisers are publishing drafts that lack a "+
			"description, a PRIMARY medium or a category, so shoppers keep seeing stale content; a rising item-discontinued "+
			"means content work is aimed at items that left the catalogue; a rising internal-error means the service itself is failing."),
		metric.WithUnit("{publication}"),
	)
	if err != nil {
		return nil, err
	}
	return &ContentMetrics{counter: counter}, nil
}

// ContentPublished counts a revision that was published.
func (m *ContentMetrics) ContentPublished(ctx context.Context) {
	m.counter.Add(ctx, 1, metric.WithAttributes(outcomeKey.String(outcomePublished)))
}

// ContentPublishRejected counts a publish that published nothing, with the
// reason why.
func (m *ContentMetrics) ContentPublishRejected(ctx context.Context, reason string) {
	m.counter.Add(ctx, 1, metric.WithAttributes(outcomeKey.String(outcomeRejected), reasonKey.String(reason)))
}
