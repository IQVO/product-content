//go:build integration

package main

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IQVO/product-content/internal/testing/kafkatest"
	"github.com/IQVO/product-content/internal/testing/pgtest"
)

const fullContent = `{"catalogItemId":"sku-e2e","description":"A steel bottle.","bullets":["Double-wall"],
 "attributes":[{"name":"material","value":"steel"}],
 "media":[{"mediaId":"m1","url":"https://static.example.invalid/a.jpg","altText":"side","role":"PRIMARY"}],
 "primaryCategoryId":%q}`

func bootReal(t *testing.T) *service {
	kafkatest.CreateTopic(t, topicEvents)
	kafkatest.CreateTopic(t, topicAvailability)
	return boot(t, map[string]string{
		"DATABASE_URL": pgtest.URL(t), "KAFKA_BROKERS": strings.Join(kafkatest.Brokers(t), ","),
		"OFFER_AVAILABILITY_CONSUMER_GROUP": "product-content.e2e", "OUTBOX_RELAY_INTERVAL": "100ms",
		"SHUTDOWN_DRAIN_DELAY": "0",
	})
}

func TestRealWiringPublishesContentOfAConsumedItem(t *testing.T) {
	s := bootReal(t)
	cat := s.call(t, "POST", "/categories", "k-cat", `{"name":"Bottles"}`)
	if cat.status != 201 {
		t.Fatalf("create category: %d %s", cat.status, cat.body)
	}
	catID, _ := cat.field(t, "categoryId").(string)
	kafkatest.Produce(t, topicAvailability, availabilityEvent("evt-created", "com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemCreated", "sku-e2e"))
	body := strings.Replace(fullContent, "%q", `"`+catID+`"`, 1)
	var created reply
	eventually(t, 60*time.Second, "the consumed item to be known", func() bool {
		created = s.call(t, "POST", "/product-contents", nextKey(), body)
		return created.status == 201
	}, func() string { return created.body })
	if pub := s.call(t, "POST", "/product-contents/sku-e2e:publish", "k-pub", ""); pub.status != 200 || pub.field(t, "state") != "PUBLISHED" {
		t.Fatalf("publish: %d %s", pub.status, pub.body)
	}
	msgs := kafkatest.ReadKeyed(t, topicEvents, "sku-e2e", 1, 60*time.Second)
	if !strings.Contains(string(msgs[0].Value), "com.retailnetwork.storefront.product-content.productcontent.ProductContentPublished") {
		t.Fatalf("published event = %s", msgs[0].Value)
	}
}

func TestRealWiringDiscontinuedItemRetiresItsContent(t *testing.T) {
	s := bootReal(t)
	cat := s.call(t, "POST", "/categories", "k-cat2", `{"name":"Cups"}`)
	catID, _ := cat.field(t, "categoryId").(string)
	kafkatest.Produce(t, topicAvailability, availabilityEvent("evt-c2", "com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemCreated", "sku-e2e"))
	body := strings.Replace(strings.Replace(fullContent, "%q", `"`+catID+`"`, 1), "sku-e2e", "sku-e2e", 1)
	eventually(t, 60*time.Second, "the item to be known", func() bool {
		return s.call(t, "POST", "/product-contents", nextKey(), body).status == 201
	})
	kafkatest.Produce(t, topicAvailability, availabilityEvent("evt-d2", "com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemDiscontinued", "sku-e2e"))
	eventually(t, 60*time.Second, "the content to be retired", func() bool {
		return s.call(t, "GET", "/product-contents/sku-e2e", "", "").field(t, "retiredReason") == "DISCONTINUED"
	})
}

var keySeq atomic.Int64

// nextKey is a fresh Idempotency-Key: a retried key would replay the first reply.
func nextKey() string { return fmt.Sprintf("key-%d", keySeq.Add(1)) }
