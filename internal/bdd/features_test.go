package bdd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cucumber/godog"

	"github.com/IQVO/product-content/internal/adapters/outbound/memory"
	"github.com/IQVO/product-content/internal/application/usecases"
)

var t0 = time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)

// TestFeatures runs the Gherkin scenarios of ../../features against the real
// router and use cases wired to the in-memory doubles.
func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: initializeScenario,
		Options:             &godog.Options{Format: "pretty", Paths: []string{"../../features"}, TestingT: t, Strict: true},
	}
	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run feature tests")
	}
}

// world is the state of one scenario.
type world struct {
	store *memory.Store
	deps  usecases.Deps
	seq   int
	// categories maps a category name to the id the API minted for it.
	categories map[string]string

	srv       *httptest.Server
	apiStatus int
	apiBody   string
	apiHeader http.Header
}

func newWorld() *world {
	clock := memory.NewClock(t0)
	s := memory.NewStore(clock)
	return &world{
		store: s, categories: map[string]string{},
		deps: usecases.Deps{
			Contents: s.Contents(), Categories: s.Categories(), Items: s.Items(), Inbox: s.Inbox(), Outbox: s.Outbox(),
			UoW: s.UnitOfWork(), Clock: clock, IDs: &memory.SequentialIDs{},
		},
	}
}

func (w *world) consumer() usecases.Consumer { return usecases.Consumer{Deps: w.deps} }

func (w *world) meta(typ string) usecases.Meta {
	w.seq++
	return usecases.Meta{EventID: fmt.Sprintf("ev-%d", w.seq), Type: typ, At: t0.Add(time.Duration(w.seq) * time.Minute)}
}

func initializeScenario(sc *godog.ScenarioContext) {
	w := newWorld()
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		if w.srv != nil {
			w.srv.Close()
		}
		*w = *newWorld()
		return ctx, nil
	})
	registerEvents(sc, w)
	registerHTTP(sc, w)
	registerAssertions(sc, w)
}
