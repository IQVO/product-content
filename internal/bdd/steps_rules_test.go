package bdd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/cucumber/godog"

	"github.com/IQVO/product-content/internal/application/usecases"
	"github.com/IQVO/product-content/internal/domain/category"
	"github.com/IQVO/product-content/internal/domain/content"
	"github.com/IQVO/product-content/internal/domain/shared"
)

// steps_rules_test.go — the vocabulary of the documentation-derived rule
// scenarios (features/*_rules.feature): content builders that spell the
// business case instead of a JSON body, tree assertions by path, event payload
// assertions and the consumer's poison-message and redelivery paths.

const staticHost = "https://static.example.invalid/"

// jsonBody marshals a request body; the {category:Name} placeholders inside it
// are expanded by w.call.
func jsonBody(m map[string]any) string {
	b, err := json.Marshal(m)
	if err != nil {
		panic(err) // a map of strings and slices always marshals
	}
	return string(b)
}

func medium(id, role string) map[string]any {
	return map[string]any{"mediaId": id, "url": staticHost + id + ".jpg", "altText": "picture " + id, "role": role}
}

// completeBody is a body that passes the publish gate.
func completeBody(id, categoryName string) map[string]any {
	return map[string]any{
		"catalogItemId": id, "description": "A steel bottle, item " + id + ".", "bullets": []string{"Double-wall"},
		"media": []map[string]any{medium("m1", "PRIMARY")}, "primaryCategoryId": "{category:" + categoryName + "}",
	}
}

// text returns n characters of trimmed text.
func text(n int) string { return strings.Repeat("a", n) }

// fieldBody puts n characters into the named field of an otherwise valid body.
func fieldBody(id, field string, n int) (map[string]any, error) {
	body := map[string]any{"catalogItemId": id, "description": "Valid."}
	switch field {
	case "description":
		body["description"] = text(n)
	case "bullet":
		body["bullets"] = []string{text(n)}
	case "attribute name":
		body["attributes"] = []map[string]any{{"name": text(n), "value": "v"}}
	case "attribute value":
		body["attributes"] = []map[string]any{{"name": "n", "value": text(n)}}
	case "attribute unit":
		body["attributes"] = []map[string]any{{"name": "n", "value": "v", "unit": text(n)}}
	case "media alt text":
		body["media"] = []map[string]any{{"mediaId": "m1", "url": staticHost + "m1.jpg", "altText": text(n), "role": "PRIMARY"}}
	default:
		return nil, fmt.Errorf("unknown field %q", field)
	}
	return body, nil
}

// countedBody puts n items into the named collection of an otherwise valid body.
func countedBody(id, kind string, n int) (map[string]any, error) {
	body := map[string]any{"catalogItemId": id, "description": "Valid."}
	switch kind {
	case "bullets":
		items := make([]string, n)
		for i := range items {
			items[i] = fmt.Sprintf("bullet %d", i+1)
		}
		body["bullets"] = items
	case "attributes":
		items := make([]map[string]any, n)
		for i := range items {
			items[i] = map[string]any{"name": fmt.Sprintf("attribute %d", i+1), "value": "v"}
		}
		body["attributes"] = items
	case "media":
		items := make([]map[string]any, n)
		for i := range items {
			items[i] = medium(fmt.Sprintf("m%d", i+1), "GALLERY")
		}
		body["media"] = items
	default:
		return nil, fmt.Errorf("unknown collection %q", kind)
	}
	return body, nil
}

func dash(s string) string {
	if s == "-" {
		return ""
	}
	return s
}

// draftBody spells a draft by its publish-gate ingredients: "-" is none.
func draftBody(id, description, categoryName, pictures string) map[string]any {
	body := map[string]any{"catalogItemId": id, "description": dash(description)}
	if c := dash(categoryName); c != "" {
		body["primaryCategoryId"] = "{category:" + c + "}"
	}
	if p := dash(pictures); p != "" {
		var media []map[string]any
		for i, role := range strings.Split(p, ",") {
			media = append(media, medium(fmt.Sprintf("m%d", i+1), strings.TrimSpace(role)))
		}
		body["media"] = media
	}
	return body
}

func registerRules(sc *godog.ScenarioContext, w *world) {
	registerRuleCalls(sc, w)
	registerRuleBuilders(sc, w)
	registerRuleTree(sc, w)
	registerRuleEvents(sc, w)
	registerRuleReads(sc, w)
}

// autoKey returns an Idempotency-Key no other call of the scenario used.
func (w *world) autoKey() string {
	w.seq++
	return fmt.Sprintf("auto-%d", w.seq)
}

func (w *world) mustAnswer(want int, what string) error {
	if w.apiStatus != want {
		return fmt.Errorf("%s: API answered %d (%s), want %d", what, w.apiStatus, truncate(w.apiBody), want)
	}
	return nil
}

// registerRuleCalls: generic calls with a fresh key and the Given forms that
// fail unless the fixture was accepted, so a broken fixture never looks like a
// failing assertion.
func registerRuleCalls(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the API posts to "([^"]*)" with a fresh Idempotency-Key the body:$`, func(path string, body *godog.DocString) error {
		return w.call("POST", path, w.autoKey(), body.Content)
	})
	sc.Step(`^the API posts to "([^"]*)" with a fresh Idempotency-Key$`, func(path string) error {
		return w.call("POST", path, w.autoKey(), "")
	})
	sc.Step(`^the API posts to "([^"]*)" with an Idempotency-Key of (\d+) characters the body:$`, func(path string, n int, body *godog.DocString) error {
		return w.call("POST", path, text(n), body.Content)
	})
	sc.Step(`^item "([^"]*)" has a complete draft in category "([^"]*)"$`, func(id, cat string) error {
		return w.completeDraft(id, cat)
	})
	sc.Step(`^item "([^"]*)" has published content in category "([^"]*)"$`, func(id, cat string) error {
		if err := w.completeDraft(id, cat); err != nil {
			return err
		}
		if err := w.call("POST", "/product-contents/"+id+"/publications", w.autoKey(), ""); err != nil {
			return err
		}
		return w.mustAnswer(200, "publish "+id)
	})
	sc.Step(`^the API revises "([^"]*)" with a complete body in category "([^"]*)" and description "([^"]*)"$`, func(id, cat, desc string) error {
		b := completeBody(id, cat)
		b["description"] = desc
		return w.call("POST", "/product-contents/"+id+"/revisions", w.autoKey(), jsonBody(b))
	})
	sc.Step(`^the API publishes "([^"]*)"$`, func(id string) error {
		return w.call("POST", "/product-contents/"+id+"/publications", w.autoKey(), "")
	})
	sc.Step(`^the API retires "([^"]*)"$`, func(id string) error {
		return w.call("POST", "/product-contents/"+id+"/retirements", w.autoKey(), "")
	})
	sc.Step(`^the API discards the draft of "([^"]*)"$`, func(id string) error {
		return w.call("DELETE", "/product-contents/"+id+"/draft", "", "")
	})
}

// completeDraft makes the item known and creates a complete DRAFT for it.
func (w *world) completeDraft(id, cat string) error {
	meta := w.meta(typeCreated)
	if err := w.consumer().ApplyCatalogItemCreated(context.Background(), meta, usecases.CatalogItemCreated{CatalogItemID: id}); err != nil {
		return err
	}
	if err := w.call("POST", "/product-contents", w.autoKey(), jsonBody(completeBody(id, cat))); err != nil {
		return err
	}
	return w.mustAnswer(201, "create content of "+id)
}

// registerRuleBuilders: content bodies spelled by the rule they exercise.
func registerRuleBuilders(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the API creates content for "([^"]*)" with a (description|bullet|attribute name|attribute value|attribute unit|media alt text) of (\d+) characters$`,
		func(id, field string, n int) error {
			body, err := fieldBody(id, field, n)
			if err != nil {
				return err
			}
			return w.call("POST", "/product-contents", w.autoKey(), jsonBody(body))
		})
	sc.Step(`^the API creates content for "([^"]*)" with (\d+) (bullets|attributes|media)$`, func(id string, n int, kind string) error {
		body, err := countedBody(id, kind, n)
		if err != nil {
			return err
		}
		return w.call("POST", "/product-contents", w.autoKey(), jsonBody(body))
	})
	sc.Step(`^the API revises "([^"]*)" with (\d+) bullets$`, func(id string, n int) error {
		body, err := countedBody(id, "bullets", n)
		if err != nil {
			return err
		}
		return w.call("POST", "/product-contents/"+id+"/revisions", w.autoKey(), jsonBody(body))
	})
	sc.Step(`^the API creates a draft for "([^"]*)" with description "([^"]*)", category "([^"]*)" and pictures "([^"]*)"$`,
		func(id, desc, cat, pictures string) error {
			return w.call("POST", "/product-contents", w.autoKey(), jsonBody(draftBody(id, desc, cat, pictures)))
		})
	sc.Step(`^the API creates content for "([^"]*)" with one medium "([^"]*)" "([^"]*)" "([^"]*)" "([^"]*)"$`,
		func(id, mediaID, mediaURL, alt, role string) error {
			body := map[string]any{"catalogItemId": id, "description": "Valid.", "media": []map[string]any{
				{"mediaId": mediaID, "url": mediaURL, "altText": alt, "role": role}}}
			return w.call("POST", "/product-contents", w.autoKey(), jsonBody(body))
		})
	sc.Step(`^the API creates content for "([^"]*)" with two media both named "([^"]*)"$`, func(id, mediaID string) error {
		body := map[string]any{"catalogItemId": id, "description": "Valid.", "media": []map[string]any{
			medium(mediaID, "PRIMARY"), medium(mediaID, "GALLERY")}}
		return w.call("POST", "/product-contents", w.autoKey(), jsonBody(body))
	})
}

// registerRuleTree: category commands that do not assert 201, and the tree read
// as the set of its paths.
func registerRuleTree(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the API creates a category "([^"]*)"$`, func(name string) error { return w.createCategory(name, "") })
	sc.Step(`^the API creates a category "([^"]*)" under "([^"]*)"$`, func(name, parent string) error {
		return w.createCategory(name, parent)
	})
	sc.Step(`^the API creates a root category named with (\d+) characters$`, func(n int) error {
		return w.call("POST", "/categories", w.autoKey(), jsonBody(map[string]any{"name": text(n)}))
	})
	sc.Step(`^the API moves category "([^"]*)" under "([^"]*)"$`, func(name, parent string) error {
		return w.call("PUT", "/categories/{category:"+name+"}/parent", "", jsonBody(map[string]any{"parentId": "{category:" + parent + "}"}))
	})
	sc.Step(`^the API moves category "([^"]*)" to the root$`, func(name string) error {
		return w.call("PUT", "/categories/{category:"+name+"}/parent", "", `{"parentId":null}`)
	})
	sc.Step(`^the API renames category "([^"]*)" to "([^"]*)"$`, func(name, to string) error {
		if err := w.call("PUT", "/categories/{category:"+name+"}/name", "", jsonBody(map[string]any{"name": to})); err != nil {
			return err
		}
		if w.apiStatus == 200 {
			w.categories[to] = w.categories[name]
		}
		return nil
	})
	sc.Step(`^the live category tree has the paths "([^"]*)"$`, func(want string) error {
		if err := w.call("GET", "/categories", "", ""); err != nil {
			return err
		}
		if err := w.mustAnswer(200, "read the tree"); err != nil {
			return err
		}
		var tree struct {
			Categories []struct {
				Path string `json:"path"`
			} `json:"categories"`
		}
		if err := json.Unmarshal([]byte(w.apiBody), &tree); err != nil {
			return err
		}
		got := make([]string, len(tree.Categories))
		for i, c := range tree.Categories {
			got[i] = c.Path
		}
		exp := splitList(want)
		sort.Strings(got)
		sort.Strings(exp)
		if strings.Join(got, ", ") != strings.Join(exp, ", ") {
			return fmt.Errorf("tree paths = [%s], want [%s]", strings.Join(got, ", "), strings.Join(exp, ", "))
		}
		return nil
	})
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" || s == "-" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// createCategory posts a category; it remembers the id only when it was created.
func (w *world) createCategory(name, parent string) error {
	body := map[string]any{"name": name}
	if parent != "" {
		body["parentId"] = "{category:" + parent + "}"
	}
	if err := w.call("POST", "/categories", w.autoKey(), jsonBody(body)); err != nil {
		return err
	}
	if w.apiStatus == 201 {
		m, err := w.apiJSON()
		if err != nil {
			return err
		}
		w.categories[name] = fmt.Sprint(m["categoryId"])
	}
	return nil
}

func (w *world) events(name string) []shared.Event {
	var out []shared.Event
	for _, e := range w.store.Outbox().Events() {
		if e.EventName() == name {
			out = append(out, e)
		}
	}
	return out
}

// registerRuleEvents: payload assertions on the outbox and the consumer paths.
func registerRuleEvents(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the latest "CategoryTreePublished" event is version (\d+) of (\d+) categories$`, w.latestTreeEventIs)
	sc.Step(`^the latest "ProductContentPublished" event of "([^"]*)" has revision (\d+) and description "([^"]*)"$`, w.latestPublicationIs)
	sc.Step(`^the latest "ProductContentPublished" event of "([^"]*)" carries (\d+) bullets, (\d+) media and category "([^"]*)"$`, w.latestPublicationCarries)
	sc.Step(`^the latest "ProductContentRetired" event of "([^"]*)" has revision (\d+)$`, w.latestRetirementIs)
	registerRuleConsumer(sc, w)
}

func (w *world) latestTreeEventIs(version int64, n int) error {
	evs := w.events("CategoryTreePublished")
	if len(evs) == 0 {
		return errors.New("no CategoryTreePublished event was published")
	}
	ev, ok := evs[len(evs)-1].(category.CategoryTreePublished)
	if !ok {
		return fmt.Errorf("unexpected event type %T", evs[len(evs)-1])
	}
	if ev.AggregateVersion() != version || len(ev.Categories) != n {
		return fmt.Errorf("latest tree event is version %d of %d categories, want version %d of %d",
			ev.AggregateVersion(), len(ev.Categories), version, n)
	}
	return nil
}

func (w *world) latestPublicationIs(id string, rev int, desc string) error {
	ev, err := w.latestPublished(id)
	if err != nil {
		return err
	}
	if ev.Revision != rev || ev.Description != desc {
		return fmt.Errorf("latest publication of %s is revision %d %q, want revision %d %q", id, ev.Revision, ev.Description, rev, desc)
	}
	return nil
}

func (w *world) latestPublicationCarries(id string, bullets, media int, cat string) error {
	ev, err := w.latestPublished(id)
	if err != nil {
		return err
	}
	if len(ev.Bullets) != bullets || len(ev.Media) != media || ev.PrimaryCategoryID != w.categories[cat] {
		return fmt.Errorf("publication of %s carries %d bullets, %d media, category %q; want %d, %d, %q",
			id, len(ev.Bullets), len(ev.Media), ev.PrimaryCategoryID, bullets, media, w.categories[cat])
	}
	return nil
}

func (w *world) latestRetirementIs(id string, rev int) error {
	var last *content.ProductContentRetired
	for _, e := range w.events("ProductContentRetired") {
		if r, ok := e.(content.ProductContentRetired); ok && string(r.CatalogItemID) == id {
			last = &r
		}
	}
	if last == nil {
		return fmt.Errorf("no ProductContentRetired event of %s", id)
	}
	if last.Revision != rev {
		return fmt.Errorf("retirement of %s is at revision %d, want %d", id, last.Revision, rev)
	}
	return nil
}

// poison applies a fact whose catalogItemId is not a valid id and requires the
// consumer to refuse it as a validation error (dead-lettered, never retried).
func (w *world) poison(kind, id string) error {
	var err error
	if kind == "created" {
		err = w.consumer().ApplyCatalogItemCreated(context.Background(), w.meta(typeCreated), usecases.CatalogItemCreated{CatalogItemID: id})
	} else {
		err = w.consumer().ApplyCatalogItemDiscontinued(context.Background(), w.meta(typeDiscontinued), usecases.CatalogItemDiscontinued{CatalogItemID: id})
	}
	if err == nil {
		return fmt.Errorf("a %s event for id %q was applied, want it refused as a poison message", kind, id)
	}
	if !errors.Is(err, shared.ErrValidation) {
		return fmt.Errorf("a %s event for id %q failed, want a validation error: %w", kind, id, err)
	}
	return nil
}

func (w *world) latestPublished(id string) (content.ProductContentPublished, error) {
	var last *content.ProductContentPublished
	for _, e := range w.events("ProductContentPublished") {
		if p, ok := e.(content.ProductContentPublished); ok && string(p.CatalogItemID) == id {
			last = &p
		}
	}
	if last == nil {
		return content.ProductContentPublished{}, fmt.Errorf("no ProductContentPublished event of %s", id)
	}
	return *last, nil
}

func registerRuleConsumer(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^offer-availability reports an item with an id of (\d+) characters created$`, func(n int) error {
		return w.consumer().ApplyCatalogItemCreated(context.Background(), w.meta(typeCreated), usecases.CatalogItemCreated{CatalogItemID: text(n)})
	})
	sc.Step(`^the API creates content for the item with an id of (\d+) characters$`, func(n int) error {
		return w.call("POST", "/product-contents", w.autoKey(), jsonBody(map[string]any{"catalogItemId": text(n), "description": "Valid."}))
	})
	sc.Step(`^offer-availability reports a (created|discontinued) item with an id of (\d+) characters which is refused as a poison message$`,
		func(kind string, n int) error { return w.poison(kind, text(n)) })
	sc.Step(`^offer-availability reports a (created|discontinued) item with the id "([^"]*)" which is refused as a poison message$`,
		func(kind, id string) error { return w.poison(kind, id) })
	sc.Step(`^offer-availability delivers the creation of item "([^"]*)" twice$`, func(id string) error {
		m := w.meta(typeCreated)
		for range 2 {
			if err := w.consumer().ApplyCatalogItemCreated(context.Background(), m, usecases.CatalogItemCreated{CatalogItemID: id}); err != nil {
				return err
			}
		}
		return nil
	})
	sc.Step(`^offer-availability delivers the discontinuation of item "([^"]*)" twice$`, func(id string) error {
		m := w.meta(typeDiscontinued)
		for range 2 {
			if err := w.consumer().ApplyCatalogItemDiscontinued(context.Background(), m, usecases.CatalogItemDiscontinued{CatalogItemID: id}); err != nil {
				return err
			}
		}
		return nil
	})
	sc.Step(`^the inbox holds (\d+) events$`, func(n int) error {
		if got := w.store.Counts().Inbox; got != n {
			return fmt.Errorf("the inbox holds %d events, want %d", got, n)
		}
		return nil
	})
	sc.Step(`^the item view knows (\d+) items$`, func(n int) error {
		if got := w.store.Counts().Items; got != n {
			return fmt.Errorf("the item view knows %d items, want %d", got, n)
		}
		return nil
	})
}

// registerRuleReads: dotted-path assertions and the cursor walk.
func registerRuleReads(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the API response path "([^"]*)" is "([^"]*)"$`, func(path, want string) error {
		got, ok := w.apiField(path)
		if !ok {
			return fmt.Errorf("no %q in body: %s", path, truncate(w.apiBody))
		}
		if fmt.Sprint(got) != want {
			return fmt.Errorf("%s = %q, want %q", path, fmt.Sprint(got), want)
		}
		return nil
	})
	sc.Step(`^the API response field "([^"]*)" is not "([^"]*)"$`, func(name, unwanted string) error {
		got, ok := w.apiField(name)
		if ok && fmt.Sprint(got) == unwanted {
			return fmt.Errorf("%s = %q, want anything else", name, unwanted)
		}
		return nil
	})
	sc.Step(`^the API response has a field "([^"]*)"$`, func(path string) error {
		if _, ok := w.apiField(path); !ok {
			return fmt.Errorf("field %q missing from body: %s", path, truncate(w.apiBody))
		}
		return nil
	})
	sc.Step(`^the API requests the next page of "([^"]*)"$`, func(path string) error {
		v, ok := w.apiField("nextCursor")
		if !ok {
			return fmt.Errorf("the last answer holds no nextCursor: %s", truncate(w.apiBody))
		}
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		return w.call("GET", path+sep+"cursor="+url.QueryEscape(fmt.Sprint(v)), "", "")
	})
}
