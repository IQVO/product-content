package http_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/IQVO/product-content/internal/adapters/inbound/http/page"
	"github.com/IQVO/product-content/internal/application/usecases"
)

// seedItems makes every id a known catalogue item and creates its draft
// content, in the order given (the list is served in catalogItemId order, not
// in this one).
func (e *env) seedItems(ids ...string) {
	e.t.Helper()
	for _, id := range ids {
		meta := usecases.Meta{EventID: "ev-" + id, Type: "t", At: t0}
		if err := (usecases.Consumer{Deps: e.deps}).ApplyCatalogItemCreated(context.Background(), meta, usecases.CatalogItemCreated{CatalogItemID: id}); err != nil {
			e.t.Fatal(err)
		}
		e.wantStatus(e.post("/product-contents", fmt.Sprintf(`{"catalogItemId":%q,"description":"d"}`, id)), http.StatusCreated)
	}
}

// walkContents follows nextCursor from the first page until it is absent and
// returns every catalogItemId in the order served, and the page count.
func walkContents(e *env, limit int) (ids []string, pages int) {
	e.t.Helper()
	cursor := ""
	for {
		target := "/product-contents?limit=" + strconv.Itoa(limit)
		if cursor != "" {
			target += "&cursor=" + url.QueryEscape(cursor)
		}
		body := e.get(target)
		e.wantStatus(body, http.StatusOK)
		doc := body.json(e.t)
		if _, old := doc["next"]; old {
			e.t.Fatalf("the retired member `next` is back: %s", body.Body)
		}
		items, ok := doc["items"].([]any)
		if !ok || len(items) > limit {
			e.t.Fatalf("items = %v (limit %d)", doc["items"], limit)
		}
		for _, it := range items {
			ids = append(ids, it.(map[string]any)["catalogItemId"].(string))
		}
		pages++
		next, more := doc["nextCursor"].(string)
		if !more {
			return ids, pages
		}
		if pages > 100 {
			e.t.Fatal("the walk never ends")
		}
		cursor = next
	}
}

// More contents than the page size, ids that are prefixes of one another: every
// page is walked, in catalogItemId order, and no content is repeated or skipped.
// The sort key of this list is the catalogItemId itself (unique), so there are
// no ties to break.
func TestListContentsWalksEveryPage(t *testing.T) {
	e := newEnv(t)
	all := []string{"sku-1", "sku-10", "sku-1a", "sku-2", "sku-20", "sku-3", "sku-30"}
	e.seedItems("sku-3", "sku-1", "sku-30", "sku-1a", "sku-2", "sku-10", "sku-20")
	want := []string{"sku-1", "sku-10", "sku-1a", "sku-2", "sku-20", "sku-3", "sku-30"}
	for _, limit := range []int{1, 2, 3, 7, 8, 500} {
		ids, pages := walkContents(e, limit)
		if fmt.Sprint(ids) != fmt.Sprint(want) {
			t.Fatalf("limit %d: walked %v, want %v", limit, ids, want)
		}
		if wantPages := (len(all) + limit - 1) / limit; pages != wantPages {
			t.Fatalf("limit %d: %d pages, want %d", limit, pages, wantPages)
		}
	}
}

func TestListContentsLastPageHasNoCursorAndEmptyIsAnEmptyPage(t *testing.T) {
	e := newEnv(t)
	none := e.get("/product-contents").json(t)
	if items, ok := none["items"].([]any); !ok || len(items) != 0 || none["nextCursor"] != nil {
		t.Fatalf("no contents is {items: []}: %v", none)
	}
	e.seedItems("sku-1")
	one := e.get("/product-contents?limit=1").json(t)
	if _, has := one["nextCursor"]; has || len(one["items"].([]any)) != 1 {
		t.Fatalf("exactly limit contents is the last page: %v", one)
	}
}

func TestListContentsRejectsBadLimitAndCursor(t *testing.T) {
	e := newEnv(t)
	e.seedItems("sku-1")
	for _, limit := range []string{"0", "501", "-1", "abc", "1.5", "", "1&limit=2"} {
		e.wantProblem(e.get("/product-contents?limit="+limit), http.StatusBadRequest, "invalid-limit")
	}
	notOurs := page.Encode(page.Position{Sort: "other", ID: "sku-1"})
	for _, cursor := range []string{"%25%25", "bm90LWpzb24", "e30", url.QueryEscape(notOurs), "x&cursor=y"} {
		e.wantProblem(e.get("/product-contents?cursor="+cursor), http.StatusBadRequest, "invalid-cursor")
	}
	// The retired paging parameters fail loudly instead of reading page one forever.
	for _, retired := range []string{"after=sku-1", "offset=1", "nextToken=abc", "from=sku-1", "page=2", "shopperId=x"} {
		e.wantProblem(e.get("/product-contents?"+retired), http.StatusBadRequest, "unknown-query-parameter")
	}
}
