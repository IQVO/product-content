---
id: testing
title: Testing
sidebar_label: Testing
sidebar_position: 1
---

# Testing

The test pyramid **as it is on this branch**: a small pure domain under a
mutation gate, use cases and adapters with unit and `httptest` tests, a godog
BDD suite over the real use cases and router, testcontainers integration tests
against real Postgres and Kafka, a repository contract suite shared by the
in-memory and Postgres adapters, golden CloudEvents bytes, architecture fitness
tests and spec linting.

| Layer | Count / threshold | Command |
| --- | --- | --- |
| Unit, domain | 55 top-level test functions in 3 packages (`shared` 5, `content` 28, `category` 22) | `make test` |
| Unit, application | 33 in `internal/application/{usecases,outbox,readmodel}` (`usecases` 31, `outbox` 1, `readmodel` 1) | `make test` |
| Unit and httptest, adapters and binaries | `inbound/http` 20, `inbound/kafka` 33, `inbound/a2a` 13, `inbound/requestlog` 4, `outbound/kafka` 16, `outbound/outbox` 5, `outbound/telemetry` 11, `outbound/postgres` 3 (outbox trace unit tests), `kafka/cloudevents` 6, `cmd/api` 16, `cmd/a2a` 9, `bootretry` 6 | `make test` |
| Coverage gate | 90% over `./internal/domain/...,./internal/application/...` (`COVERAGE_THRESHOLD` in the `Makefile`) | `make coverage` |
| BDD (godog / Gherkin) | 3 feature files, **13 scenarios** (5 + 4 + 4), run by `internal/bdd/features_test.go` with `Strict: true` | `make bdd` |
| Repository contract | one suite, six sub-suites (`contents`, `tree`, `items`, `inbox`, `unitofwork`, `idempotency`), run against the in-memory store and against Postgres | `make test` (memory), `make integration` (Postgres) |
| Integration (testcontainers) | 6 top-level tests behind `//go:build integration` in 8 files: `cmd/api` 2, `cmd/a2a` 1, `outbound/postgres` 3 | `make integration` |
| Golden bytes | one `*.golden.json` per published event type in `internal/adapters/outbound/kafka/testdata` (3 files), compared byte for byte | `make test` |
| Architecture fitness | 31 top-level test functions in `internal/architecture` | `make arch-test` |
| Mutation (gremlins) | efficacy 95, mutant coverage 95 (`.gremlins.yaml`); measured 100% / 100% on the three domain packages | `make mutation`, `make mutation-full` |
| Spec linting (Spectral) | OpenAPI and AsyncAPI, `--fail-severity=warn` | CI `api-lint` |
| Helm chart tests | `charts/product-content/tests/render_test.sh`, `test_service_selectors.py` | CI `chart-tests` |
| Contract against a running service (schemathesis) | **none**: no schemathesis configuration and no CI job | none |

Counts come from `grep '^func Test'` per file on this branch; many tests are
table-driven, so the number of cases is larger. Run `go test ./... -v` for the
cases.

## Make targets

Every target mirrors a CI job. The `Makefile` is the only definition of the
coverage scope and the mutation package list.

| Target | Runs | Notes |
| --- | --- | --- |
| `make help` | prints the target list | default target |
| `make build` | `go build ./...` | compiles `cmd/api` and `cmd/a2a` |
| `make vet` | `go vet ./...` | |
| `make fmt` / `make fmt-check` | `gofmt -w .` / fail if `gofmt -l .` is non-empty | |
| `make lint` | `golangci-lint run --allow-parallel-runners ./...` | pinned `v2.14.0`, config `.golangci.yml` |
| `make test` | `go test ./... -race` | unit, httptest and BDD (the godog suite is a normal `Test` function) |
| `make coverage` | `go test ./... -race -coverprofile=coverage.out -coverpkg=./internal/domain/...,./internal/application/...`, then the 90% gate | the CI `test` job runs exactly this |
| `make arch-test` | `go test ./internal/architecture/... -v` | |
| `make bdd` | `go test ./... -run TestFeatures -v` | the godog suite in `internal/bdd`, reading `../../features` |
| `make integration` | `go test -tags=integration ./... -race -count=1` | needs only Docker; each test boots its own Postgres and Kafka; deliberately outside `check` and `check-all` |
| `make mutation` | `gremlins unleash` on every package matched by `./internal/domain/*/` (`shared`, `content`, `category`), one at a time | the fast, blocking subset; a new domain package is gated as soon as it exists |
| `make mutation-full` | `gremlins unleash ./internal/domain --workers 1 --timeout-coefficient 30` | the exhaustive scheduled run |
| `make vuln` | `govulncheck ./...` | |
| `make check` | `fmt-check vet build lint test` | the pre-push hook (`lefthook.yml`) runs this |
| `make check-all` | `check coverage arch-test bdd` | run before pushing |
| `make check-fast` | `fmt-check vet arch-test`, then `go test` of the packages changed vs `HEAD` | the agent Stop hook |
| `make guide-lint` / `make harness-test` | `scripts/harness/guide_lint.py` / `scripts/harness/test_hook.py` | agent-guide checks, not product tests |

`lefthook.yml` also runs `fmt-check`, `vet` and `lint` on every commit. Hooks
are not tracked: run `lefthook install` once per clone.

## Unit tests

- **Domain** (`internal/domain/{shared,content,category}`): pure Go, no
  fixtures. They pin the content state machine (`DRAFT` to `PUBLISHED` to
  `RETIRED`), the publication gate (description, exactly one `PRIMARY` medium, a
  category of the live tree), revision numbering, the four-level category depth
  limit, cycle prevention on `MoveCategory`, restore-from-storage and the
  ordered domain events.
- **Use cases** (`internal/application/usecases`): run against the in-memory
  adapters (`internal/adapters/outbound/memory`), which are test doubles only.
  The shipped binaries never link them
  (`TestRuntimeBinaryHasNoInMemoryAdapters`). `content_metrics_test.go` checks
  the `content.products.published` counter.
- **HTTP adapter** (`internal/adapters/inbound/http`): `httptest` over the real
  router, split into `content_routes_test.go`, `category_routes_test.go`,
  `platform_test.go` (health, idempotency, problem details),
  `readiness_test.go` and `telemetry_test.go`.
- **Kafka consumer** (`internal/adapters/inbound/kafka`): a fake reader drives
  the run loop (`consume_loop_test.go`: retry, backoff, dead-letter, commit only
  after success), `deadletter_test.go`, `dispatch_test.go` (which event types
  change the read model), `topic_not_ready_test.go` and `tracing_test.go`.
- **A2A** (`internal/adapters/inbound/a2a`, `cmd/a2a`): the Agent Card, every
  skill mapping, the JSON-RPC router and the boot configuration.
- **Outbox relay and encoder**: relay ordering and attempt cap with a fake
  store, and the CloudEvents encoder checked against golden files. Refresh them
  deliberately with `go test ./internal/adapters/outbound/kafka -update`.
- **`cmd/api`**: boot configuration and defaults (`main_test.go`) and the
  telemetry bootstrap (`telemetry_test.go`).

Run one package or one test:

```sh
go test ./internal/domain/content -run TestPublish -v
go test ./internal/application/usecases -v
```

## BDD (godog)

`features/` holds three files and 13 scenarios:

| File | Scenarios | Covers |
| --- | --- | --- |
| `content_lifecycle.feature` | 4 | complete draft published with one event, incomplete draft refused (`incomplete-content`), discarded draft, content of an unknown item (`unknown-catalog-item`) |
| `catalogue_events.feature` | 4 | `CatalogItemDiscontinued` retires published content (event raised) and drafts (no event), redelivery changes nothing, content for a discontinued item is refused (`item-discontinued`) |
| `categories_and_api.feature` | 5 | nested categories and `CategoryTreePublished` count, `duplicate-sibling-name`, `idempotency-key-required`, replay, `idempotency-key-reused` |

The runner `internal/bdd/features_test.go` (`TestFeatures`, step files
`steps_http_test.go` and `steps_assertions_test.go`) wires the **real router and
use cases over the in-memory store** with a frozen clock, so the suite is fast
and needs no Docker. `Strict: true` fails the run on an undefined or pending
step. The module root holds no `.go` file (`TestNoGoFilesAtRepoRoot`).

## Integration (testcontainers)

All files carry `//go:build integration` and boot their own containers through
`internal/testing/pgtest` and `internal/testing/kafkatest`; `TestMain` in
`cmd/api`, `outbound/outbox` and `outbound/postgres` shuts them down. A missing
Docker is a failure, never a skip.

| Suite | Package | What it proves |
| --- | --- | --- |
| Real wiring | `cmd/api` (2) | with real Postgres and a real Kafka, a `CatalogItemCreated` event produced on the offer-availability topic makes content creatable, and after publish `ProductContentPublished` appears on the content topic keyed by the item id; a `CatalogItemDiscontinued` event retires the content with reason `DISCONTINUED` |
| A2A over Postgres | `cmd/a2a` (1) | the A2A agent reads real Postgres data and writes nothing |
| Repositories | `outbound/postgres` (1) | `TestPostgresHonoursTheRepositoryContract` runs the whole `repocontract` suite against Postgres |
| Migrations | `outbound/postgres` (1) | `TestMigrationsGoUpDownUp`: up, down, up on an empty database |
| Outbox trace | `outbound/postgres` (1) | the active `traceparent` is stored with the outbox row |

`make integration` and the CI `integration` job run the same command,
`go test -tags=integration ./... -race -count=1`. Two fitness tests fail the
build if an integration test gates on `DATABASE_URL` or `KAFKA_BROKERS` or
skips (`TestPostgresIntegrationTestsUseTestcontainers`,
`TestKafkaIntegrationTestsUseTestcontainers`).

## Mutation testing

`.gremlins.yaml` sets `workers: 1`, `timeout-coefficient: 30` and the
thresholds `efficacy: 95` and `mutant-coverage: 95`. Gremlins fails when the
measured value is less than or equal to the threshold, so a threshold sits
strictly below what the code achieves. The file records a measurement of
2026-10-25: `shared` 31 mutants, `category` 46, `content` 76, each at 100%
efficacy and 100% mutant coverage. Only the domain is mutated; use cases and
adapters are not.

The scheduled `mutation` job (Monday 06:00 UTC and on manual dispatch) mutates
`./internal/domain` as a whole; a failing scheduled run opens or refreshes a
`harness:red` issue (`scripts/harness/red_issue.py`).

## Architecture fitness tests

`internal/architecture` carries the dependency-direction rule
(`TestHexagonalArchitecture`) and source-scanning sensors. Notable ones:

| Test | Rule |
| --- | --- |
| `TestHexagonalArchitecture` | six arch-go rules: `domain` depends on no internal package except `domain`; `application` only on `domain` and `application`; inbound and outbound adapters never import each other; nothing under `internal/` imports `cmd/`; `internal/application/ports` holds only interfaces |
| `TestA2AAdapterDependencyRule`, `TestA2AAgentIsReadOnlyWithNoOutboundClient` | the A2A adapter is additive and read-only, with no outbound client (ADR 0006) |
| `TestRuntimeBinaryHasNoInMemoryAdapters` | `cmd/*` never imports `outbound/memory` |
| `TestEventCatalogueMatchesContract` | every full CloudEvents type string that `apis/asyncapi.yaml` declares for this service also appears in a `*cloudevents*.md` ADR (the type catalogue, ADR 0004) |
| `TestCloudEventsOnly`, `TestNoEventEnvelopeToggleOrFlatEnvelope` | every Kafka message is a structured-mode CloudEvent built through the cloudevents package |
| `TestKafkaWritersSetRequiredAcks`, `TestKafkaWriterTopicNotSetTwice` | writer settings |
| `TestDeadLetterCallsGuardCancellation`, `TestReplayConsumersSetCommitInterval` | consumer shutdown and commit behaviour |
| `TestOutboxRelaysPassAttemptCap` | every relay is built with an attempt cap |
| `TestNoAuthMiddlewareReintroduced` | the fleet removed REST and MCP bearer auth; it must not come back |
| `TestNoGoFilesAtRepoRoot` | the module root stays free of Go files |

Each source-scanning sensor has a companion that proves it can fail
(`...SensorBites`, `...SensorFailsOnBadFixtures`).

```sh
make arch-test
```

## CI jobs

`.github/workflows/ci.yml` runs on push and pull request to `main` and
`develop`, weekly (Monday 06:00 UTC) and on manual dispatch.

| Job | Runs | When |
| --- | --- | --- |
| `lint` | golangci-lint v2.14.0, `--timeout=5m` | always |
| `guide-lint` | `scripts/harness/guide_lint.py` and `test_hook.py`; advisory (`continue-on-error`) | always |
| `complexity` | golangci-lint with only `gocyclo,gocognit,cyclop,funlen,nestif` plus an informational gocyclo report | always |
| `test` | `go build ./...`, `go vet ./...`, then `make coverage` (90% gate); uploads `coverage.out` | always |
| `bdd` | `go test ./... -run TestFeatures -v` | always |
| `integration` | `go test -tags=integration ./... -race -count=1` | always |
| `mutation-fast` | gremlins v0.6.0 on each package under `internal/domain/*/` | always; blocking |
| `mutation` | `gremlins unleash ./internal/domain --workers 1 --timeout-coefficient 30`; harness:red issue on failure | schedule and dispatch only |
| `drift` | `deadcode -test ./...`, `go mod tidy -diff` and `scripts/coverage-quality.py` (high-coverage functions with a surviving mutant) written to the job summary; `harness:red` issue on a scheduled failure | schedule and dispatch only |
| `api-lint` | Spectral on `apis/openapi.yaml` and `apis/asyncapi.yaml`, `--fail-severity=warn` | always |
| `vuln` | `govulncheck ./...` | always |
| `docs-api-drift` | `npm ci`, `npm run clean-api-docs` and `npm run gen-api-docs` in `docs/`, then fails when `docs/docs/api-reference/rest` differs from what is committed | always |
| `helm-lint` | `ct lint` and `helm lint` with each `charts/product-content/ci/*.yaml` values file | pull requests only |
| `chart-tests` | every `charts/product-content/tests/*.sh` and `test_*.py`; a printed `SKIP` fails the job | always |
| `arch-test` | `go test ./internal/architecture/... -v` | always |
| `trivy-scan` | builds the image locally (not pushed), uploads a Trivy SARIF report (CRITICAL/HIGH/MEDIUM) and fails on a CRITICAL or HIGH OS/library finding that has a fix | pull requests only |
| `docker-publish` | build and push `ghcr.io/iqvo/product-content`, cosign signature, SBOM attestation; needs the jobs above | push to `main` only |
| `release` | tags the next patch version, retags the image, pushes the Helm chart, creates the GitHub release | push to `main`, after `docker-publish` |

`.github/workflows/docs.yml` builds this site (`npm ci`, `npm run build` in
`docs/`) on pull requests that touch `docs/**`, `apis/**` or the workflow, and
deploys to GitHub Pages only on push to `main`. `codeql.yml` runs code
scanning and `ai-review.yml` is an advisory architecture review.

Jobs shown as *skipping* on a pull request into `develop` (`mutation`,
`docker-publish`, `release`) are gated by their `if:` conditions; that is
expected.

`ci.yml` has no job that regenerates the REST reference and fails on a diff:
after any change to `apis/openapi.yaml`, regenerate it yourself with
`npm run gen-api-docs` in `docs/`.

## Reproducing the docs build

```sh
cd docs
npm ci
npx tsc --noEmit -p .
npm run build
```

The build fails on a broken link or anchor. Mermaid diagrams are checked with
`scripts/validate-mermaid.cjs` of the warehouse-docs repository, because
`docusaurus build` alone never catches a broken diagram.
