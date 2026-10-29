package bdd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"github.com/IQVO/product-content/internal/application/usecases"
)

const (
	typeCreated      = "com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemCreated"
	typeDiscontinued = "com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemDiscontinued"
)

func registerEvents(sc *godog.ScenarioContext, w *world) {
	var last usecases.Meta
	sc.Step(`^offer-availability reports item "([^"]*)" created$`, func(id string) error {
		last = w.meta(typeCreated)
		return w.consumer().ApplyCatalogItemCreated(context.Background(), last, usecases.CatalogItemCreated{CatalogItemID: id})
	})
	sc.Step(`^offer-availability reports item "([^"]*)" discontinued$`, func(id string) error {
		last = w.meta(typeDiscontinued)
		return w.consumer().ApplyCatalogItemDiscontinued(context.Background(), last, usecases.CatalogItemDiscontinued{CatalogItemID: id})
	})
	sc.Step(`^the discontinuation of "([^"]*)" is delivered again$`, func(id string) error {
		return w.consumer().ApplyCatalogItemDiscontinued(context.Background(), last, usecases.CatalogItemDiscontinued{CatalogItemID: id})
	})
}

func (w *world) apiJSON() (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(w.apiBody), &m); err != nil {
		return nil, fmt.Errorf("answer is not a JSON object: %q (%v)", w.apiBody, err)
	}
	return m, nil
}

func registerAssertions(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the API answers (\d+)$`, w.apiAnswers)
	sc.Step(`^the API answers (\d+) with problem "([^"]*)"$`, w.apiAnswersProblem)
	sc.Step(`^the API response field "([^"]*)" is "([^"]*)"$`, w.apiFieldIs)
	sc.Step(`^the response is marked replayed$`, func() error {
		if got := w.apiHeader.Get("Idempotent-Replayed"); got != "true" {
			return fmt.Errorf("Idempotent-Replayed = %q, want true", got)
		}
		return nil
	})
	sc.Step(`^(\d+) "([^"]*)" events were published$`, func(n int, name string) error {
		got := 0
		for _, e := range w.store.Outbox().Events() {
			if e.EventName() == name {
				got++
			}
		}
		if got != n {
			return fmt.Errorf("%d %s events were published, want %d", got, name, n)
		}
		return nil
	})
}

func (w *world) apiAnswers(want int) error {
	if w.apiStatus != want {
		return fmt.Errorf("API answered %d (%s), want %d", w.apiStatus, truncate(w.apiBody), want)
	}
	return nil
}

func (w *world) apiAnswersProblem(want int, slug string) error {
	if err := w.apiAnswers(want); err != nil {
		return err
	}
	if ct := w.apiHeader.Get("Content-Type"); ct != "application/problem+json" {
		return fmt.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
	m, err := w.apiJSON()
	if err != nil {
		return err
	}
	if got := strings.TrimPrefix(fmt.Sprint(m["type"]), "/problems/"); got != slug {
		return fmt.Errorf("problem type = %q, want %q (%s)", got, slug, truncate(w.apiBody))
	}
	return nil
}

func (w *world) apiFieldIs(name, want string) error {
	m, err := w.apiJSON()
	if err != nil {
		return err
	}
	got, ok := m[name]
	if !ok {
		return fmt.Errorf("answer has no field %q (%s)", name, truncate(w.apiBody))
	}
	if fmt.Sprint(got) != want {
		return fmt.Errorf("%s = %q, want %q", name, fmt.Sprint(got), want)
	}
	return nil
}

func truncate(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
