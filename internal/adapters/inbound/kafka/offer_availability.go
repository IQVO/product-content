package kafka

import (
	"context"
	"errors"
	"log/slog"

	ce "github.com/cloudevents/sdk-go/v2/event"

	"github.com/IQVO/product-content/internal/application/usecases"
	"github.com/IQVO/product-content/internal/domain/shared"
)

// Topic and types of the offer-availability events product-content consumes
// (ADR 0003). Dispatch is on the full type string.
const (
	OfferAvailabilityTopic = "retailnetwork.offer-availability.events"

	TypeCatalogItemCreated      = "com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemCreated"
	TypeCatalogItemDiscontinued = "com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemDiscontinued"
)

// catalogItemData is the only part of the consumed payloads that is used.
type catalogItemData struct {
	CatalogItemID string `json:"catalogItemId"`
}

// NewOfferAvailabilityConsumer builds the consumer of offer-availability's
// event topic under groupID.
func NewOfferAvailabilityConsumer(brokers []string, groupID string, c usecases.Consumer, logger *slog.Logger) *Consumer {
	return newConsumer("offer-availability", brokers, OfferAvailabilityTopic, groupID, offerAvailabilityHandlers(c), logger)
}

func offerAvailabilityHandlers(c usecases.Consumer) map[string]HandlerFunc {
	return map[string]HandlerFunc{
		TypeCatalogItemCreated: func(ctx context.Context, e ce.Event) error {
			d, err := decodeData[catalogItemData](e)
			if err != nil {
				return err
			}
			return classify(c.ApplyCatalogItemCreated(ctx, meta(e), usecases.CatalogItemCreated{CatalogItemID: d.CatalogItemID}))
		},
		TypeCatalogItemDiscontinued: func(ctx context.Context, e ce.Event) error {
			d, err := decodeData[catalogItemData](e)
			if err != nil {
				return err
			}
			return classify(c.ApplyCatalogItemDiscontinued(ctx, meta(e), usecases.CatalogItemDiscontinued{CatalogItemID: d.CatalogItemID}))
		},
	}
}

func meta(e ce.Event) usecases.Meta {
	return usecases.Meta{EventID: e.ID(), Type: e.Type(), At: e.Time()}
}

// classify turns a validation failure (an id that can never be valid) into a
// poison payload; every other error is transient.
func classify(err error) error {
	if errors.Is(err, shared.ErrValidation) {
		return errors.Join(errBadPayload, err)
	}
	return err
}
