package bdd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cucumber/godog"
)

// steps_wave2_test.go — the wave-2 additions: collection-size and
// dotted-path assertions for the read-side scenarios.
func registerWave2(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a category "([^"]*)" exists under "([^"]*)"$`, func(name, parent string) error {
		if err := w.call("POST", "/categories", "cat-"+name, fmt.Sprintf(`{"parentId":"{category:%s}","name":%q}`, parent, name)); err != nil {
			return err
		}
		m, err := w.apiJSON()
		if err != nil || w.apiStatus != 201 {
			return fmt.Errorf("create category under %s: %d %s (%w)", parent, w.apiStatus, w.apiBody, err)
		}
		w.categories[name] = fmt.Sprint(m["categoryId"])
		return nil
	})
	sc.Step(`^the API response list "([^"]*)" has (\d+) items$`, func(path string, want int) error {
		v, ok := w.apiField(path)
		if !ok {
			return fmt.Errorf("no list %q in body: %s", path, w.apiBody)
		}
		list, isList := v.([]any)
		if !isList {
			return fmt.Errorf("field %q is not a list: %s", path, w.apiBody)
		}
		if len(list) != want {
			return fmt.Errorf("list %q has %d items, want %d (body: %s)", path, len(list), want, w.apiBody)
		}
		return nil
	})
	sc.Step(`^the API response has no field "([^"]*)"$`, func(path string) error {
		if _, ok := w.apiField(path); ok {
			return fmt.Errorf("field %q present in body: %s", path, w.apiBody)
		}
		return nil
	})
}

// apiField reads a dotted path ("a.b.0.c") of the last API response body.
func (w *world) apiField(path string) (any, bool) {
	var v any
	if json.Unmarshal([]byte(w.apiBody), &v) != nil {
		return nil, false
	}
	cur := v
	for _, seg := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[seg]
			if !ok {
				return nil, false
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}
			cur = node[i]
		default:
			return nil, false
		}
	}
	return cur, true
}
