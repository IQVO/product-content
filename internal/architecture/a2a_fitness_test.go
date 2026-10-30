package architecture

// ADR 0006: the A2A inbound adapter is additive, never load-bearing, READ-ONLY
// and holds no outbound client.
//
//   - TestA2AAdapterDependencyRule: internal/adapters/inbound/a2a may depend
//     only on the application and domain layers (never on an outbound adapter,
//     never on cmd), and nothing else in internal/ may depend on it; cmd/a2a
//     wires it up and nothing else touches it.
//   - TestA2AAgentIsReadOnlyWithNoOutboundClient: an ALLOWLIST over the real Go
//     syntax tree (comments never count) of the non-test sources of the adapter
//     and of cmd/a2a: only the two read use cases (plus the Deps bundle that
//     carries their repositories) may be referenced, no write port, no outbox,
//     inbox, unit of work, Kafka or in-memory package, no Kafka variable and no
//     outbound A2A client. product-content has no MCP surface to derive a write
//     from, so the agent is read-only by construction.
//   - TestA2ASensorBites proves the detector flags each forbidden shape from
//     code fixtures, so the sensor above cannot pass vacuously.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"

	archgo "github.com/arch-go/arch-go/api"
	"github.com/arch-go/arch-go/api/configuration"
)

func TestA2AAdapterDependencyRule(t *testing.T) {
	moduleInfo := configuration.Load(modulePath)

	t.Run("a2a adapter depends only on application and domain", func(t *testing.T) {
		result := archgo.CheckArchitecture(moduleInfo, configuration.Config{
			DependenciesRules: []*configuration.DependenciesRule{
				{
					Package: "**.internal.adapters.inbound.a2a.**",
					ShouldOnlyDependsOn: &configuration.Dependencies{
						Internal: []string{
							"**.internal.adapters.inbound.a2a.**",
							"**.internal.application.**",
							"**.internal.domain.**",
						},
					},
				},
			},
		})

		assertArchGoPasses(t, result)
	})

	t.Run("nothing else depends on the a2a adapter", func(t *testing.T) {
		result := archgo.CheckArchitecture(moduleInfo, configuration.Config{
			DependenciesRules: []*configuration.DependenciesRule{
				{
					Package: "**.internal.**",
					ShouldNotDependsOn: &configuration.Dependencies{
						Internal: []string{"**.internal.adapters.inbound.a2a.**"},
					},
				},
			},
		})

		assertArchGoPasses(t, result)
	})
}

// allowedA2AUseCases are the only usecases.<Name> identifiers the A2A sources
// may reference: the two read use cases and the Deps bundle that carries the
// repositories they read through.
var allowedA2AUseCases = map[string]bool{
	"GetProductContent": true,
	"ListCategories":    true,
	"Deps":              true,
}

// forbiddenA2ASelectors are pkg.Name references a read-only agent never makes:
// write ports and the infrastructure constructors of the write side.
var forbiddenA2ASelectors = map[string]string{
	"ports.UnitOfWork":                  "a transaction: a read-only agent never writes",
	"ports.EventOutbox":                 "the outbox: a read-only agent publishes no fact",
	"ports.Inbox":                       "the inbox: a read-only agent consumes no event",
	"ports.IdempotencyStore":            "the Idempotency-Key store: a read-only agent has no POST",
	"postgres.NewOutboxRepo":            "the outbox: a read-only agent publishes no fact",
	"postgres.NewUnitOfWork":            "a transaction: a read-only agent never writes",
	"postgres.NewInbox":                 "the inbox: a read-only agent consumes no event",
	"postgres.NewIdempotencyStore":      "the Idempotency-Key store: a read-only agent has no POST",
	"postgres.NewCatalogItemRepository": "the catalogue read model: only the consumer needs it",
	"outboundkafka.NewEncoder":          "an event encoder: a read-only agent publishes no fact",
}

// forbiddenA2AImportParts are import-path fragments a read-only agent never
// imports: outbound A2A clients, Kafka, the outbox relay, the in-memory test
// doubles and the other inbound adapters.
var forbiddenA2AImportParts = map[string]string{
	"a2a-go/v2/a2aclient":               "an outbound A2A client (no call to a sibling context, ADR 0006)",
	"internal/adapters/outbound/kafka":  "Kafka: a read-only agent has no producer",
	"internal/adapters/outbound/outbox": "the outbox relay: only cmd/api drains outbox_events",
	"internal/adapters/outbound/memory": "the in-memory test doubles: the binary runs on real Postgres",
	"internal/adapters/inbound/kafka":   "Kafka: a read-only agent has no consumer",
	"internal/adapters/inbound/http":    "the REST adapter: the agent is its own adapter",
	"segmentio/kafka-go":                "Kafka",
}

// a2aViolations returns every way one non-test Go source breaks the read-only,
// no-outbound-client rule. src is the file content (nil reads filename).
func a2aViolations(filename string, src any) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution) // no ParseComments: comments never count
	if err != nil {
		return nil, err
	}
	var out []string
	for _, imp := range f.Imports {
		path, uerr := strconv.Unquote(imp.Path.Value)
		if uerr != nil {
			return nil, uerr
		}
		for part, why := range forbiddenA2AImportParts {
			if strings.Contains(path, part) {
				out = append(out, "imports "+path+": "+why)
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		out = append(out, nodeViolations(n)...)
		return true
	})
	sort.Strings(out)
	return out, nil
}

// nodeViolations checks one syntax node: a usecases.X outside the allowlist, a
// forbidden pkg.Name selector, or a KAFKA_ variable name in a string literal.
func nodeViolations(n ast.Node) []string {
	switch x := n.(type) {
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		if !ok {
			return nil
		}
		if id.Name == "usecases" && !allowedA2AUseCases[x.Sel.Name] {
			return []string{"references usecases." + x.Sel.Name + ": not a read use case of this agent (read-only like the REST GETs it mirrors)"}
		}
		if why, bad := forbiddenA2ASelectors[id.Name+"."+x.Sel.Name]; bad {
			return []string{"references " + id.Name + "." + x.Sel.Name + ": " + why}
		}
	case *ast.BasicLit:
		if x.Kind == token.STRING && strings.Contains(x.Value, "KAFKA_") {
			return []string{"reads a Kafka variable (" + x.Value + "): a read-only agent has no Kafka"}
		}
	}
	return nil
}

func TestA2AAgentIsReadOnlyWithNoOutboundClient(t *testing.T) {
	var files []string
	for _, dir := range []string{"../adapters/inbound/a2a", "../../cmd/a2a"} {
		found := goFilesUnder(t, dir, false)
		if len(found) == 0 {
			t.Fatalf("no sources under %s; the sensor is looking in the wrong place", dir)
		}
		files = append(files, found...)
	}
	for _, path := range files {
		violations, err := a2aViolations(path, nil)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, v := range violations {
			t.Errorf("%s %s", path, v)
		}
	}
}

// TestA2ASensorBites feeds the detector one fixture per forbidden shape and the
// legitimate read shapes, so the sensor cannot pass vacuously.
func TestA2ASensorBites(t *testing.T) {
	bad := map[string]string{
		"a write use case":       "package x\nvar _ = usecases.PublishContent{}\n",
		"another write use case": "package x\nvar _ = usecases.CreateCategory{}\n",
		"a consumer":             "package x\nvar _ = usecases.Consumer{}\n",
		"the unit of work port":  "package x\nvar _ ports.UnitOfWork\n",
		"the outbox port":        "package x\nvar _ ports.EventOutbox\n",
		"the outbox repository":  "package x\nvar _ = postgres.NewOutboxRepo\n",
		"an outbound A2A client": "package x\nimport _ \"github.com/a2aproject/a2a-go/v2/a2aclient\"\n",
		"a kafka writer":         "package x\nimport _ \"github.com/IQVO/product-content/internal/adapters/outbound/kafka\"\n",
		"a kafka client library": "package x\nimport _ \"github.com/segmentio/kafka-go\"\n",
		"the in-memory doubles":  "package x\nimport _ \"github.com/IQVO/product-content/internal/adapters/outbound/memory\"\n",
		"a Kafka variable":       "package x\nvar _ = os.Getenv(\"KAFKA_BROKERS\")\n",
	}
	for name, src := range bad {
		got, err := a2aViolations("fixture.go", src)
		if err != nil || len(got) == 0 {
			t.Errorf("the sensor does not flag %s (violations %v, err %v)", name, got, err)
		}
	}

	good := map[string]string{
		"the read use cases and the Deps bundle": "package x\nvar _ = usecases.GetProductContent{}\nvar _ = usecases.ListCategories{}\nvar _ = usecases.Deps{}\n",
		"the read repositories":                  "package x\nvar _ = postgres.NewContentRepository\nvar _ = postgres.NewTreeRepository\n",
		"comments naming forbidden things":       "package x\n// usecases.PublishContent ports.UnitOfWork a2aclient KAFKA_BROKERS\nvar _ = 1\n",
	}
	for name, src := range good {
		if got, err := a2aViolations("fixture.go", src); err != nil || len(got) != 0 {
			t.Errorf("false positive on %s: %v, %v", name, got, err)
		}
	}
}
