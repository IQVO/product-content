package kafka

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	kafkago "github.com/segmentio/kafka-go"

	"github.com/IQVO/product-content/internal/adapters/kafka/cloudevents"
	"github.com/IQVO/product-content/internal/application/outbox"
	"github.com/IQVO/product-content/internal/domain/category"
	"github.com/IQVO/product-content/internal/domain/content"
	"github.com/IQVO/product-content/internal/domain/shared"
)

// Topic is this service's integration-event topic.
const Topic = "retailnetwork.product-content.events"

// schemaVersion is the dataschema version of every payload below.
const schemaVersion = 1

// Wire payloads (CloudEvents `data`, camelCase JSON) pinned by
// apis/asyncapi.yaml. Arrays are always present; optional fields are omitted
// when absent, never null.

type attributeData struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
}

type mediaData struct {
	MediaID string `json:"mediaId"`
	URL     string `json:"url"`
	AltText string `json:"altText"`
	Role    string `json:"role"`
}

type publishedData struct {
	CatalogItemID     string          `json:"catalogItemId"`
	Revision          int             `json:"revision"`
	Description       string          `json:"description"`
	Bullets           []string        `json:"bullets"`
	Attributes        []attributeData `json:"attributes"`
	Media             []mediaData     `json:"media"`
	PrimaryCategoryID string          `json:"primaryCategoryId"`
}

type retiredData struct {
	CatalogItemID string `json:"catalogItemId"`
	Revision      int    `json:"revision"`
}

type categoryData struct {
	CategoryID string `json:"categoryId"`
	ParentID   string `json:"parentId,omitempty"`
	Name       string `json:"name"`
	Path       string `json:"path"`
}

type treeData struct {
	Version    int64          `json:"version"`
	Categories []categoryData `json:"categories"`
}

// Encoder implements outbox.Encoder: each domain event becomes one outbox
// message holding the CloudEvents bytes, the Kafka key (the subject) and the
// content-type header. The CloudEvents `id` is minted HERE, once, and persisted
// with the outbox row, so a relay retry republishes the same id.
type Encoder struct {
	// NewID mints a CloudEvents id; uuid.NewString when nil.
	NewID func() string
	// Topic overrides the destination topic (Topic when empty); tests use it
	// to isolate their topic on a shared broker.
	Topic string
}

// NewEncoder returns an Encoder minting random UUID v4 ids.
func NewEncoder() *Encoder { return &Encoder{NewID: uuid.NewString} }

var _ outbox.Encoder = (*Encoder)(nil)

// Encode encodes events in order. An event type it does not publish is a
// programming error and fails the whole call.
func (e *Encoder) Encode(events ...shared.Event) ([]outbox.Message, error) {
	newID := e.NewID
	if newID == nil {
		newID = uuid.NewString
	}
	out := make([]outbox.Message, 0, len(events))
	for _, ev := range events {
		data, err := payloadFor(ev)
		if err != nil {
			return nil, err
		}
		msg, err := buildMessage(ev, newID(), e.topic(), data)
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, nil
}

func (e *Encoder) topic() string {
	if e.Topic != "" {
		return e.Topic
	}
	return Topic
}

func buildMessage(ev shared.Event, id, topic string, data any) (outbox.Message, error) {
	value, err := cloudevents.New(cloudevents.Spec{
		ID: id, Entity: ev.Entity(), EventName: ev.EventName(), Subject: ev.SubjectID(), Time: ev.OccurredAt(),
		Stream: cloudevents.StreamEvents, Version: schemaVersion, Data: data,
	})
	if err != nil {
		return outbox.Message{}, fmt.Errorf("encode %s: %w", ev.EventName(), err)
	}
	ct := cloudevents.ContentTypeHeader()
	return outbox.Message{
		EventID: id, Topic: topic, EventType: cloudevents.Type(ev.Entity(), ev.EventName()), Subject: ev.SubjectID(),
		Key: []byte(ev.SubjectID()), DataSchema: cloudevents.DataSchema(cloudevents.StreamEvents, ev.EventName(), schemaVersion),
		Value: value, Headers: []outbox.Header{{Key: ct.Key, Value: string(ct.Value)}},
	}, nil
}

func payloadFor(ev shared.Event) (any, error) {
	switch e := ev.(type) {
	case content.ProductContentPublished:
		return publishedPayload(e), nil
	case content.ProductContentRetired:
		return retiredData{CatalogItemID: string(e.CatalogItemID), Revision: e.Revision}, nil
	case category.CategoryTreePublished:
		return treePayload(e), nil
	}
	return nil, fmt.Errorf("kafka encoder: %T is not a published event", ev)
}

func publishedPayload(e content.ProductContentPublished) publishedData {
	d := publishedData{
		CatalogItemID: string(e.CatalogItemID), Revision: e.Revision, Description: e.Description,
		Bullets:           append([]string{}, e.Bullets...),
		Attributes:        make([]attributeData, len(e.Attributes)),
		Media:             make([]mediaData, len(e.Media)),
		PrimaryCategoryID: e.PrimaryCategoryID,
	}
	for i, a := range e.Attributes {
		d.Attributes[i] = attributeData{Name: a.Name, Value: a.Value, Unit: a.Unit}
	}
	for i, m := range e.Media {
		d.Media[i] = mediaData{MediaID: m.MediaID, URL: m.URL, AltText: m.AltText, Role: string(m.Role)}
	}
	return d
}

func treePayload(e category.CategoryTreePublished) treeData {
	d := treeData{Version: e.Version, Categories: make([]categoryData, len(e.Categories))}
	for i, c := range e.Categories {
		d.Categories[i] = categoryData{CategoryID: c.ID, ParentID: c.ParentID, Name: c.Name, Path: c.Path}
	}
	return d
}

// Headers converts the stored outbox headers to Kafka headers.
func Headers(hs []outbox.Header) []kafkago.Header {
	out := make([]kafkago.Header, len(hs))
	for i, h := range hs {
		out[i] = kafkago.Header{Key: h.Key, Value: []byte(h.Value)}
	}
	return out
}

// Writer is the part of *kafkago.Writer the relay sink needs.
type Writer interface {
	WriteMessages(ctx context.Context, msgs ...kafkago.Message) error
}

// Sink writes outbox messages to Kafka; each message names its own topic, so
// the writer behind it has none (see NewWriter).
type Sink struct{ W Writer }

// Send writes msgs, in order, and returns when the broker acknowledged them.
func (s Sink) Send(ctx context.Context, msgs ...outbox.Message) error {
	out := make([]kafkago.Message, len(msgs))
	for i, m := range msgs {
		out[i] = kafkago.Message{Topic: m.Topic, Key: m.Key, Value: m.Value, Headers: Headers(m.Headers)}
	}
	if err := s.W.WriteMessages(ctx, out...); err != nil {
		return fmt.Errorf("write %d messages to kafka: %w", len(out), err)
	}
	return nil
}
