package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/IQVO/product-content/internal/application/ports"
	"github.com/IQVO/product-content/internal/domain/content"
	"github.com/IQVO/product-content/internal/domain/shared"
)

// tracerName is the OTel instrumentation scope for A2A skill spans.
const tracerName = "github.com/IQVO/product-content/internal/adapters/inbound/a2a"

// Skill ids advertised on the Agent Card (see Card).
const (
	skillGetProductContent = "get_product_content"
	skillListCategories    = "list_categories"
)

// maxIDLength bounds a catalogue item id, as the REST adapter does: a longer
// one cannot name a resource.
const maxIDLength = 128

// skillRequest is the wire contract of every skill call: the first meaningful
// message part carries (as a Data part, or a Text part holding the same JSON)
// the skill id and its arguments. Strict on purpose: an agent client is
// machine-to-machine, so unknown fields are rejected, never ignored. (Go's
// encoding/json matches field names case-insensitively, so only genuinely
// unknown names are refused.)
type skillRequest struct {
	Skill     string          `json:"skill"`
	Arguments json.RawMessage `json:"arguments"`
}

// getProductContentArgs and listCategoriesArgs are the per-skill argument
// shapes; list_categories takes none.
type getProductContentArgs struct {
	CatalogItemID string `json:"catalogItemId"`
}

type listCategoriesArgs struct{}

// answer maps one incoming message to a skill and returns the agent's reply
// Message. It is the whole executor body: this is a message-only agent, so a
// skill run is exactly one request mapped to one answer.
func answer(ctx context.Context, deps Deps, ec *a2asrv.ExecutorContext) a2a.Event {
	req, p := parseSkillRequest(ec)
	if p != nil {
		return reply(ec, envelope{Problem: p}, p.summary())
	}

	ctx, span := otel.Tracer(tracerName).Start(ctx, "a2a.skill "+req.Skill)
	defer span.End()
	span.SetAttributes(attribute.String("a2a.skill", req.Skill))

	res, p := run(ctx, deps, req)
	if p != nil {
		span.SetStatus(codes.Error, p.Detail)
		return reply(ec, envelope{Skill: req.Skill, Problem: p}, p.summary())
	}
	return reply(ec, envelope{Skill: req.Skill, Data: &res.dto}, res.summary)
}

// run dispatches one validated request to its skill.
func run(ctx context.Context, deps Deps, req skillRequest) (result, *problem) {
	switch req.Skill {
	case skillGetProductContent:
		return deps.getProductContent(ctx, req.Arguments)
	case skillListCategories:
		return deps.listCategories(ctx, req.Arguments)
	default:
		return result{}, problemUnknownSkill(req.Skill)
	}
}

// reply builds the agent Message for one skill run: a Data part carrying the
// structured envelope and a Text part carrying the one-line summary, bound to
// the caller's context (and task, when the client referenced one).
func reply(ec *a2asrv.ExecutorContext, env envelope, summary string) a2a.Event {
	return a2a.NewMessageForTask(a2a.MessageRoleAgent, ec, a2a.NewDataPart(env), a2a.NewTextPart(summary))
}

// parseSkillRequest extracts the skillRequest from the message's first
// meaningful part. A Data part (or a Text part holding the same JSON object)
// is accepted; anything else is a request-shape problem, answered, not
// raised, because a malformed delegation is a business answer for the calling
// agent, not a transport failure.
func parseSkillRequest(ec *a2asrv.ExecutorContext) (skillRequest, *problem) {
	msg := ec.Message
	if msg == nil || len(msg.Parts) == 0 {
		return skillRequest{}, problemMalformedRequest("the message carries no parts")
	}
	raw, err := partJSON(msg.Parts[0])
	if err != nil {
		return skillRequest{}, problemMalformedRequest(fmt.Sprintf("the first message part is not a skill request: %v", err))
	}
	var req skillRequest
	if err := decodeStrict(raw, &req); err != nil {
		return skillRequest{}, problemMalformedRequest(fmt.Sprintf("the skill request is not valid: %v", err))
	}
	if req.Skill == "" {
		return skillRequest{}, problemMalformedRequest(`the skill request carries no "skill" field`)
	}
	return req, nil
}

// partJSON renders one part's content as the JSON bytes of a skill request.
func partJSON(part *a2a.Part) ([]byte, error) {
	switch c := part.Content.(type) {
	case a2a.Data:
		return json.Marshal(c.Value)
	case a2a.Text:
		return []byte(string(c)), nil
	default:
		return nil, errors.New("expected a data part (or a text part holding the skill JSON)")
	}
}

// decodeStrict decodes one JSON document into dst, rejecting unknown fields
// and trailing data. Absent arguments decode as the empty object so each
// skill's own required-field checks produce the specific problem.
func decodeStrict(raw []byte, dst any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("unexpected data after the JSON document")
	}
	return nil
}

// getProductContent is the read skill over GetProductContent. It mirrors the
// REST route's identifier rule: an id that can never name a resource (empty
// after trimming is a missing argument; over 128 characters or carrying a NUL,
// which Postgres text cannot hold, is a 404) never reaches the repository.
func (d Deps) getProductContent(ctx context.Context, rawArgs json.RawMessage) (result, *problem) {
	var args getProductContentArgs
	if err := decodeStrict(rawArgs, &args); err != nil {
		return result{}, problemInvalidArgument(fmt.Sprintf("arguments: %v", err))
	}
	if strings.TrimSpace(args.CatalogItemID) == "" {
		return result{}, problemInvalidArgument("catalogItemId is required")
	}
	if len(args.CatalogItemID) > maxIDLength || strings.ContainsRune(args.CatalogItemID, 0) {
		return result{}, problemNotFound("no product content matches the catalogItemId")
	}
	view, err := d.GetProductContent.Execute(ctx, content.ID(args.CatalogItemID))
	if err != nil {
		return result{}, problemFromUseCase(ctx, err)
	}
	return toProductContent(view), nil
}

// listCategories is the read skill over ListCategories; it takes no arguments.
func (d Deps) listCategories(ctx context.Context, rawArgs json.RawMessage) (result, *problem) {
	var args listCategoriesArgs
	if err := decodeStrict(rawArgs, &args); err != nil {
		return result{}, problemInvalidArgument(fmt.Sprintf("arguments: %v", err))
	}
	view, err := d.ListCategories.Execute(ctx)
	if err != nil {
		return result{}, problemFromUseCase(ctx, err)
	}
	return toCategoryTree(view), nil
}

// problemFromUseCase maps a use case error to a problem answer with the SAME
// classification as the REST adapter (internal/adapters/inbound/http
// problemCatalogue) for the errors a read can raise: not found is 404,
// validation is 422, anything else is 500 whose detail never leaks the
// internal message (it is logged instead). The REST 409 classes belong to
// commands and cannot be raised by these reads. Domain rejections are EXPECTED
// business outcomes and are answered as problems, never as JSON-RPC errors.
func problemFromUseCase(ctx context.Context, err error) *problem {
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return problemNotFound(err.Error())
	case errors.Is(err, shared.ErrValidation):
		return problemValidation(err.Error())
	default:
		slog.ErrorContext(ctx, "a2a skill failed", "error", err)
		return problemInternal()
	}
}
