package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/IQVO/product-content/internal/adapters/outbound/memory"
	"github.com/IQVO/product-content/internal/application/ports"
	"github.com/IQVO/product-content/internal/application/usecases"
	"github.com/IQVO/product-content/internal/domain/category"
	"github.com/IQVO/product-content/internal/domain/content"
	"github.com/IQVO/product-content/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)

// harness builds Deps over the in-memory test doubles and seeds through the
// real use cases, with a fixed clock.
type harness struct {
	t     *testing.T
	store *memory.Store
	deps  usecases.Deps
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	clock := memory.NewClock(t0)
	s := memory.NewStore(clock)
	return &harness{t: t, store: s, deps: usecases.Deps{
		Contents: s.Contents(), Categories: s.Categories(), Items: s.Items(), Inbox: s.Inbox(), Outbox: s.Outbox(),
		UoW: s.UnitOfWork(), Clock: clock, IDs: &memory.SequentialIDs{},
	}}
}

func (h *harness) a2aDeps() Deps {
	return Deps{GetProductContent: &usecases.GetProductContent{Deps: h.deps}, ListCategories: &usecases.ListCategories{Deps: h.deps}}
}

func (h *harness) must(err error) {
	h.t.Helper()
	if err != nil {
		h.t.Fatalf("seed: %v", err)
	}
}

func (h *harness) category(parent, name string) string {
	h.t.Helper()
	c, err := usecases.CreateCategory{Deps: h.deps}.Execute(context.Background(), usecases.CreateCategoryInput{ParentID: parent, Name: name})
	h.must(err)
	return c.CategoryID
}

// draft makes the catalogue item known and creates its DRAFT content.
func (h *harness) draft(id, categoryID string) {
	h.t.Helper()
	ctx := context.Background()
	meta := usecases.Meta{EventID: "ev-" + id, Type: "type.A", At: t0}
	h.must(usecases.Consumer{Deps: h.deps}.ApplyCatalogItemCreated(ctx, meta, usecases.CatalogItemCreated{CatalogItemID: id}))
	_, err := usecases.CreateProductContent{Deps: h.deps}.Execute(ctx, usecases.ContentInput{
		CatalogItemID: content.ID(id),
		Body: content.Body{
			Description: "A 2-litre insulated steel bottle.", Bullets: []string{"Double-wall", "Leak-proof"},
			Attributes:        []content.Attribute{{Name: "capacity", Value: "2", Unit: "l"}, {Name: "colour", Value: "steel"}},
			Media:             []content.Media{{MediaID: "med-1", URL: "https://static.example.invalid/a.jpg", AltText: "side", Role: content.RolePrimary}},
			PrimaryCategoryID: categoryID,
		},
	})
	h.must(err)
}

func (h *harness) publish(id string) {
	h.t.Helper()
	_, err := usecases.PublishContent{Deps: h.deps}.Execute(context.Background(), content.ID(id))
	h.must(err)
}

// ---- the wire: JSON-RPC over HTTP, driven by the SDK's own client ----------

// wireEnvelope is a TEST-LOCAL copy of the answer contract. If the adapter's
// envelope drifts, this suite fails instead of silently following it.
type wireEnvelope struct {
	Skill string `json:"skill"`
	Data  *struct {
		ProductContent *contentView `json:"productContent"`
		CategoryTree   *struct {
			Version    int64 `json:"version"`
			Categories []struct {
				CategoryID string `json:"categoryId"`
				ParentID   string `json:"parentId"`
				Name       string `json:"name"`
				Path       string `json:"path"`
			} `json:"categories"`
		} `json:"categoryTree"`
	} `json:"data"`
	Problem *struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
		Status int    `json:"status"`
	} `json:"problem"`
}

type wire struct{ client *a2aclient.Client }

func newWire(t *testing.T, deps Deps) *wire {
	t.Helper()
	srv := httptest.NewServer(Handler(NewExecutor(deps)))
	t.Cleanup(srv.Close)
	card := Card(Config{AgentURL: srv.URL + "/", Version: "test"})
	client, err := a2aclient.NewFromCard(context.Background(), card)
	if err != nil {
		t.Fatalf("NewFromCard: %v", err)
	}
	return &wire{client: client}
}

// call sends one message part and returns the decoded envelope and the
// one-line text summary of the agent's reply.
func (w *wire) call(t *testing.T, part *a2a.Part) (wireEnvelope, string) {
	t.Helper()
	resp, err := w.client.SendMessage(context.Background(), &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, part)})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	msg, ok := resp.(*a2a.Message)
	if !ok || len(msg.Parts) < 2 {
		t.Fatalf("reply = %T, want a message with a data and a text part", resp)
	}
	data, ok := msg.Parts[0].Content.(a2a.Data)
	if !ok {
		t.Fatalf("first reply part is %T, want data", msg.Parts[0].Content)
	}
	raw, _ := json.Marshal(data.Value)
	var env wireEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("reply is not the skill envelope: %v (%s)", err, raw)
	}
	text, _ := msg.Parts[1].Content.(a2a.Text)
	return env, string(text)
}

func (w *wire) skill(t *testing.T, skill string, args map[string]any) (wireEnvelope, string) {
	t.Helper()
	return w.call(t, a2a.NewDataPart(map[string]any{"skill": skill, "arguments": args}))
}

func wantProblem(t *testing.T, env wireEnvelope, status int, slug string) {
	t.Helper()
	if env.Problem == nil {
		t.Fatalf("expected a %d problem, got data: %+v", status, env.Data)
	}
	if env.Problem.Status != status || env.Problem.Type != problemBase+slug {
		t.Fatalf("problem = %+v, want %d %s%s", env.Problem, status, problemBase, slug)
	}
	if env.Data != nil {
		t.Fatalf("a problem answer must carry no data, got %+v", env.Data)
	}
}

func TestWireGetProductContentAnswersWhatRESTReturns(t *testing.T) {
	h := newHarness(t)
	cat := h.category("", "Bottles")
	h.draft("item-1", cat)
	h.draft("item-2", cat)
	h.publish("item-2")
	w := newWire(t, h.a2aDeps())

	env, text := w.skill(t, skillGetProductContent, map[string]any{"catalogItemId": "item-1"})
	if env.Problem != nil || env.Skill != skillGetProductContent || env.Data == nil || env.Data.ProductContent == nil {
		t.Fatalf("envelope = %+v", env)
	}
	assertDraftContent(t, env.Data.ProductContent, cat)
	if text != "item-1: DRAFT, revision 1 (published revision 0), 2 bullets, 2 attributes, 1 media" {
		t.Fatalf("summary = %q", text)
	}

	env, _ = w.skill(t, skillGetProductContent, map[string]any{"catalogItemId": "item-2"})
	if env.Data == nil || env.Data.ProductContent == nil {
		t.Fatalf("published content = %+v", env)
	}
	if c := env.Data.ProductContent; c.State != "PUBLISHED" || c.PublishedRevision != 1 || c.PublishedAt == nil {
		t.Fatalf("published content = %+v", c)
	}
}

// contentView is the content part of the test-local envelope.
type contentView = struct {
	CatalogItemID string   `json:"catalogItemId"`
	State         string   `json:"state"`
	RetiredReason string   `json:"retiredReason"`
	Description   string   `json:"description"`
	Bullets       []string `json:"bullets"`
	Attributes    []struct {
		Name  string  `json:"name"`
		Value string  `json:"value"`
		Unit  *string `json:"unit"`
	} `json:"attributes"`
	Media []struct {
		MediaID string `json:"mediaId"`
		URL     string `json:"url"`
		AltText string `json:"altText"`
		Role    string `json:"role"`
	} `json:"media"`
	PrimaryCategoryID string     `json:"primaryCategoryId"`
	Revision          int        `json:"revision"`
	PublishedRevision int        `json:"publishedRevision"`
	CreatedAt         time.Time  `json:"createdAt"`
	PublishedAt       *time.Time `json:"publishedAt"`
}

func assertDraftContent(t *testing.T, c *contentView, cat string) {
	t.Helper()
	if c.CatalogItemID != "item-1" || c.State != "DRAFT" || c.Revision != 1 || c.PublishedRevision != 0 || c.PublishedAt != nil {
		t.Fatalf("content identity = %+v", c)
	}
	if c.PrimaryCategoryID != cat || !strings.HasPrefix(c.Description, "A 2-litre") || !c.CreatedAt.Equal(t0) {
		t.Fatalf("content body = %+v", c)
	}
	assertDraftLists(t, c)
}

func assertDraftLists(t *testing.T, c *contentView) {
	t.Helper()
	if len(c.Bullets) != 2 || len(c.Attributes) != 2 || len(c.Media) != 1 || c.Media[0].Role != "PRIMARY" {
		t.Fatalf("content lists = %+v", c)
	}
	if c.Attributes[0].Unit == nil || *c.Attributes[0].Unit != "l" || c.Attributes[1].Unit != nil {
		t.Fatalf("attribute units = %+v", c.Attributes)
	}
}

func TestWireListCategoriesAnswersTheTree(t *testing.T) {
	h := newHarness(t)
	w := newWire(t, h.a2aDeps())

	env, text := w.skill(t, skillListCategories, map[string]any{})
	if tree := env.Data.CategoryTree; env.Problem != nil || tree == nil || tree.Version != 0 || len(tree.Categories) != 0 {
		t.Fatalf("an empty tree is version 0 with no categories, got %+v", env)
	}
	if text != "category tree version 0: 0 categories" {
		t.Fatalf("summary = %q", text)
	}

	root := h.category("", "Home")
	child := h.category(root, "Bottles")
	env, text = w.call(t, a2a.NewDataPart(map[string]any{"skill": skillListCategories})) // arguments may be left out
	tree := env.Data.CategoryTree
	if tree == nil || tree.Version != 2 || len(tree.Categories) != 2 {
		t.Fatalf("tree = %+v", env)
	}
	got := map[string][3]string{}
	for _, c := range tree.Categories {
		got[c.CategoryID] = [3]string{c.ParentID, c.Name, c.Path}
	}
	if got[root] != [3]string{"", "Home", "Home"} || got[child] != [3]string{root, "Bottles", "Home/Bottles"} {
		t.Fatalf("categories = %v", got)
	}
	if text != "category tree version 2: 2 categories" {
		t.Fatalf("summary = %q", text)
	}
}

// The agent is read-only: no skill call changes the stored content, the tree
// or the outbox.
func TestWireReadsChangeNothing(t *testing.T) {
	h := newHarness(t)
	cat := h.category("", "Bottles")
	h.draft("item-1", cat)
	before := h.store.Outbox().Events()
	w := newWire(t, h.a2aDeps())

	for i := 0; i < 3; i++ {
		w.skill(t, skillGetProductContent, map[string]any{"catalogItemId": "item-1"})
		w.skill(t, skillGetProductContent, map[string]any{"catalogItemId": "ghost"})
		w.skill(t, skillListCategories, map[string]any{})
	}
	if after := h.store.Outbox().Events(); len(after) != len(before) {
		t.Fatalf("outbox grew from %d to %d events: a read skill wrote something", len(before), len(after))
	}
	v, err := usecases.GetProductContent{Deps: h.deps}.Execute(context.Background(), "item-1")
	if err != nil || v.Revision != 1 || v.State != "DRAFT" {
		t.Fatalf("content after reads = %+v, %v", v, err)
	}
}

func TestWireNotFoundIsAnsweredLikeREST(t *testing.T) {
	h := newHarness(t)
	w := newWire(t, h.a2aDeps())
	cases := map[string]string{
		"unknown id":    "ghost",
		"over-long id":  strings.Repeat("x", maxIDLength+1),
		"NUL in the id": "item\x001",
	}
	for name, id := range cases {
		t.Run(name, func(t *testing.T) {
			env, text := w.skill(t, skillGetProductContent, map[string]any{"catalogItemId": id})
			wantProblem(t, env, http.StatusNotFound, slugNotFound)
			if !strings.HasPrefix(text, "Not found (404): ") {
				t.Fatalf("summary = %q", text)
			}
		})
	}
	// An id of exactly the maximum length is a normal lookup.
	env, _ := w.skill(t, skillGetProductContent, map[string]any{"catalogItemId": strings.Repeat("x", maxIDLength)})
	wantProblem(t, env, http.StatusNotFound, slugNotFound)
}

// countingContents counts the reads that reach the repository.
type countingContents struct {
	ports.ContentRepository
	gets int
}

func (c *countingContents) Get(ctx context.Context, id content.ID) (*content.ProductContent, error) {
	c.gets++
	return c.ContentRepository.Get(ctx, id)
}

// An id that can never name a resource (over-long, or carrying a NUL that
// Postgres text cannot hold: SQLSTATE 22021, a 500 on the real runtime) is
// answered 404 WITHOUT reaching the repository, exactly as the REST route does.
func TestWireImpossibleIdsNeverReachTheRepository(t *testing.T) {
	h := newHarness(t)
	repo := &countingContents{ContentRepository: h.deps.Contents}
	h.deps.Contents = repo
	w := newWire(t, h.a2aDeps())

	for _, id := range []string{strings.Repeat("x", maxIDLength+1), "item\x00-1"} {
		env, _ := w.skill(t, skillGetProductContent, map[string]any{"catalogItemId": id})
		wantProblem(t, env, http.StatusNotFound, slugNotFound)
	}
	if repo.gets != 0 {
		t.Fatalf("%d impossible ids reached the repository", repo.gets)
	}
	env, _ := w.skill(t, skillGetProductContent, map[string]any{"catalogItemId": "ghost"})
	wantProblem(t, env, http.StatusNotFound, slugNotFound)
	if repo.gets != 1 {
		t.Fatalf("a well-formed unknown id must reach the repository once, got %d", repo.gets)
	}
}

func TestWireArgumentValidation(t *testing.T) {
	w := newWire(t, newHarness(t).a2aDeps())
	cases := map[string]struct {
		skill string
		args  map[string]any
	}{
		"get: missing id":           {skillGetProductContent, map[string]any{}},
		"get: blank id":             {skillGetProductContent, map[string]any{"catalogItemId": "   "}},
		"get: id of the wrong type": {skillGetProductContent, map[string]any{"catalogItemId": 7}},
		"get: unknown argument":     {skillGetProductContent, map[string]any{"catalogItemId": "item-1", "state": "PUBLISHED"}},
		"get: typo'd argument":      {skillGetProductContent, map[string]any{"catalogItemIds": "item-1"}},
		"list: unexpected argument": {skillListCategories, map[string]any{"parentId": "x"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			env, text := w.skill(t, tc.skill, tc.args)
			wantProblem(t, env, http.StatusBadRequest, slugInvalidArgument)
			if env.Skill != tc.skill || !strings.HasPrefix(text, "Invalid skill argument (400): ") {
				t.Fatalf("skill = %q, summary = %q", env.Skill, text)
			}
		})
	}
}

func TestWireRequestShapeProblems(t *testing.T) {
	h := newHarness(t)
	cat := h.category("", "Bottles")
	h.draft("item-1", cat)
	w := newWire(t, h.a2aDeps())

	env, _ := w.call(t, a2a.NewTextPart("not json at all"))
	wantProblem(t, env, http.StatusBadRequest, slugMalformedSkill)

	env, _ = w.call(t, a2a.NewDataPart(map[string]any{"arguments": map[string]any{}}))
	wantProblem(t, env, http.StatusBadRequest, slugMalformedSkill)

	env, _ = w.call(t, a2a.NewDataPart(map[string]any{"skill": skillListCategories, "arguments": map[string]any{}, "extra": 1}))
	wantProblem(t, env, http.StatusBadRequest, slugMalformedSkill)

	// Trailing data after the JSON document is refused, not ignored.
	env, _ = w.call(t, a2a.NewTextPart(`{"skill":"list_categories"} {"skill":"list_categories"}`))
	wantProblem(t, env, http.StatusBadRequest, slugMalformedSkill)

	env, _ = w.skill(t, "launch_rocket", map[string]any{})
	wantProblem(t, env, http.StatusBadRequest, slugUnknownSkill)

	// A write the REST surface offers is not a skill here.
	env, _ = w.skill(t, "publish_content", map[string]any{"catalogItemId": "item-1"})
	wantProblem(t, env, http.StatusBadRequest, slugUnknownSkill)

	// A text part holding the same JSON is accepted (documented contract).
	env, _ = w.call(t, a2a.NewTextPart(`{"skill":"get_product_content","arguments":{"catalogItemId":"item-1"}}`))
	if env.Problem != nil || env.Data == nil || env.Data.ProductContent == nil {
		t.Fatalf("a text part holding the skill JSON must be accepted, got %+v", env)
	}
}

// failingContents and failingTree make a read raise a chosen error.
type failingContents struct {
	ports.ContentRepository
	err error
}

func (f failingContents) Get(context.Context, content.ID) (*content.ProductContent, error) {
	return nil, f.err
}

type failingTree struct {
	ports.TreeRepository
	err error
}

func (f failingTree) Get(context.Context) (*category.Tree, error) { return nil, f.err }

// Errors the repositories can raise are ANSWERED in-band with the REST class,
// never raised as JSON-RPC errors; an unmapped one is a 500 that does not leak
// the internal message.
func TestWireUseCaseErrorsAreAnsweredInBandWithTheRESTClass(t *testing.T) {
	const secret = "pq: password authentication failed for user admin"
	cases := []struct {
		name   string
		err    error
		status int
		slug   string
	}{
		{"not found", ports.ErrNotFound, http.StatusNotFound, slugNotFound},
		{"validation", shared.Invalid("limit must be between 1 and 500"), http.StatusUnprocessableEntity, slugValidationFailed},
		{"internal", errors.New(secret), http.StatusInternalServerError, slugInternal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.deps.Contents = failingContents{err: tc.err}
			h.deps.Categories = failingTree{err: tc.err}
			w := newWire(t, h.a2aDeps())

			for skill, args := range map[string]map[string]any{
				skillGetProductContent: {"catalogItemId": "item-1"},
				skillListCategories:    {},
			} {
				env, text := w.skill(t, skill, args)
				if skill == skillListCategories && tc.err == ports.ErrNotFound {
					// The tree before its first category is the empty tree, as on REST.
					if env.Problem != nil || env.Data == nil || env.Data.CategoryTree == nil {
						t.Fatalf("%s: a missing tree is the empty tree, got %+v", skill, env)
					}
					continue
				}
				wantProblem(t, env, tc.status, tc.slug)
				if strings.Contains(env.Problem.Detail, "password") || strings.Contains(text, "password") {
					t.Fatalf("%s: the answer leaks the internal error: %q / %q", skill, env.Problem.Detail, text)
				}
			}
		})
	}
}

// TestCardIsServedAndDialable proves discovery end to end: the card handler
// serves the card at the well-known path and a client built from the FETCHED
// card reaches the agent.
func TestCardIsServedAndDialable(t *testing.T) {
	h := newHarness(t)
	mux := http.NewServeMux()
	mux.Handle("/", Handler(NewExecutor(h.a2aDeps())))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(Card(Config{AgentURL: srv.URL + "/", Version: "test"})))

	res, err := http.Get(srv.URL + a2asrv.WellKnownAgentCardPath) //nolint:gosec,noctx // test server URL
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("card fetch = %v / %v", res, err)
	}
	defer func() { _ = res.Body.Close() }()
	var card a2a.AgentCard
	if err := json.NewDecoder(res.Body).Decode(&card); err != nil {
		t.Fatal(err)
	}
	client, err := a2aclient.NewFromCard(context.Background(), &card)
	if err != nil {
		t.Fatal(err)
	}
	env, _ := (&wire{client: client}).skill(t, skillListCategories, map[string]any{})
	if env.Data == nil || env.Data.CategoryTree == nil {
		t.Fatalf("a client built from the served card could not call the agent: %+v", env)
	}
}
