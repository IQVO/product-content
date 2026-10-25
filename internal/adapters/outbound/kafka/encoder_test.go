package kafka_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IQVO/product-content/internal/adapters/kafka/cloudevents"
	"github.com/IQVO/product-content/internal/adapters/outbound/kafka"
	"github.com/IQVO/product-content/internal/application/outbox"
	"github.com/IQVO/product-content/internal/domain/category"
	"github.com/IQVO/product-content/internal/domain/content"
	"github.com/IQVO/product-content/internal/domain/shared"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var t0 = time.Date(2026, 10, 23, 10, 15, 30, 123456000, time.UTC)

const fixedID = "3f6c2a9e-8b1d-4c7a-9e55-0d2f6b7a1c40"

func published() content.ProductContentPublished {
	return content.ProductContentPublished{
		Header:        shared.Header{Version: 3, At: t0},
		CatalogItemID: "item-100", Revision: 2, Description: "A sturdy steel water bottle.",
		Bullets:           []string{"Keeps drinks cold", "Dishwasher safe"},
		Attributes:        []content.Attribute{{Name: "capacity", Value: "750", Unit: "ml"}, {Name: "colour", Value: "blue"}},
		Media:             []content.Media{{MediaID: "m-1", URL: "https://cdn.example.test/m-1.jpg", AltText: "Front view", Role: content.RolePrimary}},
		PrimaryCategoryID: "cat-outdoor",
	}
}

func retired() content.ProductContentRetired {
	return content.ProductContentRetired{Header: shared.Header{Version: 4, At: t0.Add(time.Minute)}, CatalogItemID: "item-100", Revision: 2}
}

func tree() category.CategoryTreePublished {
	return category.CategoryTreePublished{
		Header: shared.Header{Version: 5, At: t0}, TreeID: category.TreeID,
		Categories: []category.Category{
			{ID: "cat-outdoor", Name: "Outdoor", Path: "Outdoor"},
			{ID: "cat-bottles", ParentID: "cat-outdoor", Name: "Bottles", Path: "Outdoor/Bottles"},
		},
	}
}

func encodeOne(t *testing.T, ev shared.Event) outbox.Message {
	t.Helper()
	enc := &kafka.Encoder{NewID: func() string { return fixedID }}
	msgs, err := enc.Encode(ev)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("Encode: %v (%d messages)", err, len(msgs))
	}
	return msgs[0]
}

// assertGolden compares the CloudEvents bytes with the golden file EXACTLY.
func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden.json")
	if *update {
		if err := os.WriteFile(path, append(got, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got)+"\n" != string(want) {
		t.Fatalf("%s differs from its golden file:\n got %s\nwant %s", name, got, want)
	}
}

func assertEnvelope(t *testing.T, msg outbox.Message, entity, name, subject string) {
	t.Helper()
	wantType := "com.retailnetwork.storefront.product-content." + entity + "." + name
	if msg.EventType != wantType || msg.EventID != fixedID || msg.Topic != "retailnetwork.product-content.events" {
		t.Fatalf("message = %+v", msg)
	}
	if msg.Subject != subject || string(msg.Key) != subject {
		t.Fatalf("subject/key = %q %q", msg.Subject, msg.Key)
	}
	if msg.DataSchema != "urn:retailnetwork:product-content:events:"+name+":v1" {
		t.Fatalf("dataschema = %q", msg.DataSchema)
	}
	if len(msg.Headers) != 1 || msg.Headers[0].Key != "content-type" || msg.Headers[0].Value != cloudevents.MediaType {
		t.Fatalf("headers = %+v", msg.Headers)
	}
	e, err := cloudevents.Decode(msg.Value)
	if err != nil || e.Type() != wantType || e.Source() != "/retailnetwork/product-content" || e.ID() != fixedID {
		t.Fatalf("decode: %v %v", err, e)
	}
}

func TestProductContentPublishedGolden(t *testing.T) {
	msg := encodeOne(t, published())
	assertGolden(t, "ProductContentPublished", msg.Value)
	assertEnvelope(t, msg, "productcontent", "ProductContentPublished", "item-100")
}

func TestProductContentRetiredGolden(t *testing.T) {
	msg := encodeOne(t, retired())
	assertGolden(t, "ProductContentRetired", msg.Value)
	assertEnvelope(t, msg, "productcontent", "ProductContentRetired", "item-100")
}

func TestCategoryTreePublishedGolden(t *testing.T) {
	msg := encodeOne(t, tree())
	assertGolden(t, "CategoryTreePublished", msg.Value)
	assertEnvelope(t, msg, "categorytree", "CategoryTreePublished", category.TreeID)
}

func TestPublishedWithNoBulletsOrMediaKeepsEmptyArrays(t *testing.T) {
	ev := published()
	ev.Bullets, ev.Attributes, ev.Media = nil, nil, nil
	raw := string(encodeOne(t, ev).Value)
	for _, want := range []string{`"bullets":[]`, `"attributes":[]`, `"media":[]`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("missing %s in %s", want, raw)
		}
	}
	var env struct{ Data map[string]any }
	if err := json.Unmarshal([]byte(raw), &env); err != nil || env.Data["catalogItemId"] != "item-100" {
		t.Fatalf("data = %v %v", env.Data, err)
	}
}
