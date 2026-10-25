package memory

import (
	"fmt"
	"sync"
	"time"
)

// Clock is a settable ports.Clock.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// NewClock returns a clock reading t.
func NewClock(t time.Time) *Clock { return &Clock{now: t} }

// Now returns the current reading.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Set moves the clock to t.
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// Advance moves the clock forward by d.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// SequentialIDs is a ports.IDGenerator minting cat-1, cat-2, ...
type SequentialIDs struct {
	mu         sync.Mutex
	categories int
}

// NewCategoryID returns the next category id.
func (g *SequentialIDs) NewCategoryID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.categories++
	return fmt.Sprintf("cat-%d", g.categories)
}
