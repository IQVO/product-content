// Package readmodel holds the plain data of the event-fed local read model
// CatalogItemView (ADR 0003). The ports package stays interfaces-only, so the
// struct lives here.
package readmodel

import "time"

// ItemState is the state of a catalogue item as product-content knows it.
type ItemState string

// The states. DISCONTINUED is terminal.
const (
	ItemActive       ItemState = "ACTIVE"
	ItemDiscontinued ItemState = "DISCONTINUED"
)

// Valid reports whether s is ACTIVE or DISCONTINUED.
func (s ItemState) Valid() bool { return s == ItemActive || s == ItemDiscontinued }

// CatalogItem is one row of CatalogItemView.
type CatalogItem struct {
	CatalogItemID string
	State         ItemState
	UpdatedAt     time.Time
}
