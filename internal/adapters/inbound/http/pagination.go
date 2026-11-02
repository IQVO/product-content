package http

import (
	"net/http"

	"github.com/IQVO/product-content/internal/adapters/inbound/http/page"
	"github.com/IQVO/product-content/internal/application/views"
)

// pageParams reads the `limit` and `cursor` query parameters of GET
// /product-contents, which declares no filter: any other parameter (a retired
// `after`, `offset`, `nextToken`) is errUnknownQueryParameter. The list is
// ordered by catalogItemId, which is unique, so the opaque cursor is the
// page.Position whose sort key and id are both the last catalogItemId; a cursor
// that decodes to anything else was not issued by this service and is
// invalid-cursor. after is "" for the first page.
func pageParams(r *http.Request) (limit int, after string, err error) {
	if name := page.Unknown(r); name != "" {
		return 0, "", errUnknownQueryParameterNamed(name)
	}
	if limit, err = page.Limit(r); err != nil {
		return 0, "", err
	}
	pos, err := page.After(r)
	if err != nil || pos == nil {
		return limit, "", err
	}
	if pos.Sort != pos.ID {
		return 0, "", page.ErrInvalidCursor
	}
	return limit, pos.ID, nil
}

// contentPageOut renders one use case page as the {items,nextCursor} envelope.
func contentPageOut(p views.ContentPage) page.Page[contentResponse] {
	out := page.Page[contentResponse]{Items: make([]contentResponse, len(p.Items))}
	for i, v := range p.Items {
		out.Items[i] = contentDTO(v)
	}
	if p.Next != "" {
		out.NextCursor = page.Encode(page.Position{Sort: p.Next, ID: p.Next})
	}
	return out
}
