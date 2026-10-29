package bdd

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"

	"github.com/cucumber/godog"

	httpadapter "github.com/IQVO/product-content/internal/adapters/inbound/http"
	"github.com/IQVO/product-content/internal/application/usecases"
)

// api serves the real router of apis/openapi.yaml over the scenario's world.
func (w *world) api() *httptest.Server {
	if w.srv == nil {
		d := w.deps
		w.srv = httptest.NewServer(httpadapter.NewRouter(&httpadapter.Server{
			CreateProductContent: usecases.CreateProductContent{Deps: d}, ReviseContent: usecases.ReviseContent{Deps: d},
			PublishContent: usecases.PublishContent{Deps: d}, RetireContent: usecases.RetireContent{Deps: d},
			DiscardDraft: usecases.DiscardDraft{Deps: d}, GetProductContent: usecases.GetProductContent{Deps: d},
			ListProductContents: usecases.ListProductContents{Deps: d}, CreateCategory: usecases.CreateCategory{Deps: d},
			MoveCategory: usecases.MoveCategory{Deps: d}, RenameCategory: usecases.RenameCategory{Deps: d},
			PublishCategoryTree: usecases.PublishCategoryTree{Deps: d}, ListCategories: usecases.ListCategories{Deps: d},
			IdempotencyStore: w.store.Idempotency(), Clock: d.Clock,
		}, nil, ""))
	}
	return w.srv
}

var placeholder = regexp.MustCompile(`\{category:([^}]*)\}`)

// expand replaces {category:Name} with the id the API minted for that name.
func (w *world) expand(s string) (string, error) {
	var missing string
	out := placeholder.ReplaceAllStringFunc(s, func(m string) string {
		name := placeholder.FindStringSubmatch(m)[1]
		id, ok := w.categories[name]
		if !ok {
			missing = name
		}
		return id
	})
	if missing != "" {
		return "", fmt.Errorf("no category named %q was created", missing)
	}
	return out, nil
}

func (w *world) call(method, path, key, body string) error {
	path, err := w.expand(path)
	if err != nil {
		return err
	}
	body, err = w.expand(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(method, w.api().URL+path, strings.NewReader(body))
	if err != nil {
		return err
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	w.apiStatus, w.apiBody, w.apiHeader = resp.StatusCode, string(b), resp.Header
	return nil
}

func registerHTTP(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the API requests "([^"]*)"$`, func(path string) error { return w.call("GET", path, "", "") })
	sc.Step(`^the API posts to "([^"]*)" with Idempotency-Key "([^"]*)" the body:$`, func(path, key string, body *godog.DocString) error {
		return w.call("POST", path, key, body.Content)
	})
	sc.Step(`^the API posts to "([^"]*)" with Idempotency-Key "([^"]*)"$`, func(path, key string) error {
		return w.call("POST", path, key, "")
	})
	sc.Step(`^the API posts to "([^"]*)" without an Idempotency-Key and body:$`, func(path string, body *godog.DocString) error {
		return w.call("POST", path, "", body.Content)
	})
	sc.Step(`^a category "([^"]*)" exists$`, func(name string) error {
		if err := w.call("POST", "/categories", "cat-"+name, fmt.Sprintf(`{"name":%q}`, name)); err != nil {
			return err
		}
		m, err := w.apiJSON()
		if err != nil || w.apiStatus != 201 {
			return fmt.Errorf("create category: %d %s (%v)", w.apiStatus, w.apiBody, err)
		}
		w.categories[name] = fmt.Sprint(m["categoryId"])
		return nil
	})
}
