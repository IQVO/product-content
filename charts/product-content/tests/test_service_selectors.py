#!/usr/bin/env python3
"""Assert this chart's Service selectors isolate each component.

Why this exists (fleet-wide): `app.kubernetes.io/name` + `instance` are identical
on every pod a release creates. A Service that selects on those two alone selects
ALL of them, which turns into a wrong answer the day a second component is added.
Every Deployment here carries `app.kubernetes.io/component` in selector.matchLabels
and its pod labels, and every Service selects on it, so each Service selects
EXACTLY ONE Deployment. This test fails if that ever stops being true.

product-content has one always-on component, the api (REST, the outbox relay and
the offer-availability consumer in one process), and one optional, default-off
component: the read-only a2a agent (cmd/a2a, ADR 0006). There is no mcp and no
frontend: the chart renders nothing for binaries that do not exist. The checks
run with every optional template (HPA, Ingress, HTTPRoute, a2a) enabled together.

Run: python3 charts/product-content/tests/test_service_selectors.py
Needs: helm, PyYAML.
"""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path

CHART_DIR = Path(__file__).resolve().parents[1]
RELEASE = "product-content"

# Dummy values, no password: the chart refuses to render without a database
# source, the Kafka brokers and the offer-availability consumer group. None is
# ever dialled.
GROUPS = {
    "offerAvailabilityConsumerGroup": "product-content-offer-availability",
}


def runtime(skip: str | None = None) -> list[str]:
    """The mandatory values, minus the one named by skip (database, kafka or a group key)."""
    args: list[str] = []
    if skip != "database":
        args += ["--set", "database.url=postgres://u@example.invalid:5432/db"]
    if skip != "kafka":
        args += ["--set", "kafka.brokers=kafka.example.invalid:9092"]
    for key, value in GROUPS.items():
        if skip != key:
            args += ["--set", f"config.{key}={value}"]
    return args


BASE = runtime()
ENABLE_EVERYTHING = BASE + [
    "--set", "autoscaling.api.enabled=true",
    "--set", "gatewayApi.enabled=true",
    "--set", "ingress.enabled=true",
    "--set", "a2a.enabled=true",
    "--set", "a2a.route.enabled=true",
    "--set", "a2a.route.parentRefs[0].name=warehouse-gateway",
    "--set", "a2a.route.parentRefs[0].namespace=kong",
    "--set", "config.outboxRelayInterval=2s",
    "--set", "config.shutdownDrainDelay=5s",
    "--set", "database.migrationsUrl=postgres://u@example.invalid:5432/direct",
]

# Every variable cmd/api reads, with a dedicated chart value.
API_ENV = (
    "HTTP_ADDR", "LOG_LEVEL", "DATABASE_URL", "MIGRATIONS_DATABASE_URL", "KAFKA_BROKERS",
    "OFFER_AVAILABILITY_CONSUMER_GROUP",
    "OUTBOX_RELAY_INTERVAL", "OUTBOX_MAX_ATTEMPTS", "SHUTDOWN_DRAIN_DELAY",
)


A2A = f"{RELEASE}-a2a"

# Variables the read-only a2a agent renders, and the ones it must never get
# (no Kafka, no relay, no consumer: it writes nothing).
A2A_ENV = ("A2A_ADDR", "A2A_AGENT_URL", "LOG_LEVEL", "DATABASE_URL", "MIGRATIONS_DATABASE_URL")
A2A_ENV_ABSENT = (
    "KAFKA_BROKERS", "EVENT_PUBLISHER", "OFFER_AVAILABILITY_CONSUMER_GROUP",
    "OUTBOX_RELAY_INTERVAL", "OUTBOX_MAX_ATTEMPTS", "SHUTDOWN_DRAIN_DELAY", "HTTP_ADDR",
)


def helm_template(extra_args: list[str]) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["helm", "template", RELEASE, str(CHART_DIR), *extra_args],
        capture_output=True, text=True,
    )


def render(extra_args: list[str]) -> list[dict]:
    result = helm_template(extra_args)
    if result.returncode != 0:
        raise SystemExit(f"FAIL: helm template {' '.join(extra_args)}:\n{result.stderr}")
    try:
        import yaml  # type: ignore
    except ModuleNotFoundError:  # pragma: no cover - environment guard
        print("SKIP: PyYAML not available; cannot assert selectors", file=sys.stderr)
        raise SystemExit(0)
    return [d for d in yaml.safe_load_all(result.stdout) if d]


def selector_of(doc: dict) -> dict:
    return doc.get("spec", {}).get("selector") or {}


def pod_labels_of(doc: dict) -> dict:
    return doc.get("spec", {}).get("template", {}).get("metadata", {}).get("labels") or {}


def matches(selector: dict, labels: dict) -> bool:
    return bool(selector) and all(labels.get(k) == v for k, v in selector.items())


def env_of(dep: dict) -> list[dict]:
    return dep["spec"]["template"]["spec"]["containers"][0].get("env", [])


def env_names(dep: dict) -> list[str]:
    return [e["name"] for e in env_of(dep)]


def env_value(dep: dict, name: str) -> str | None:
    return next((e.get("value") for e in env_of(dep) if e["name"] == name), None)


def check_components(docs: list[dict], failures: list[str]) -> None:
    services = {d["metadata"]["name"]: d for d in docs if d.get("kind") == "Service"}
    deployments = {d["metadata"]["name"]: d for d in docs if d.get("kind") == "Deployment"}

    if set(deployments) != {RELEASE, A2A}:
        failures.append(f"the chart has the api and a2a Deployments, rendered: {sorted(deployments)}")
    if set(services) != {RELEASE, A2A}:
        failures.append(f"the chart has the api and a2a Services, rendered: {sorted(services)}")
    for name, component in ((RELEASE, "api"), (A2A, "a2a")):
        if name not in services:
            failures.append(f"the {component} Service was not rendered")
        elif selector_of(services[name]).get("app.kubernetes.io/component") != component:
            failures.append(f"the {component} Service selector must pin component={component}")

    # Every Deployment's own selector must pin a component too, and be
    # satisfied by its pod labels.
    for name, dep in deployments.items():
        match_labels = dep["spec"]["selector"].get("matchLabels") or {}
        if "app.kubernetes.io/component" not in match_labels:
            failures.append(f"Deployment {name} selector.matchLabels lacks app.kubernetes.io/component")
        if not matches(match_labels, pod_labels_of(dep)):
            failures.append(f"Deployment {name} pod labels do not satisfy its own selector")

    # The real invariant: each Service selects exactly one Deployment.
    for svc_name, svc in services.items():
        sel = selector_of(svc)
        hit = [d for d, dep in deployments.items() if matches(sel, pod_labels_of(dep))]
        if len(hit) != 1:
            failures.append(f"Service {svc_name} selects {len(hit)} Deployments {sorted(hit)}; expected exactly 1")

    # The Ingress and the api HTTPRoute route to the api Service only; the a2a
    # HTTPRoute routes to the a2a Service only; the HPA owns the api replicas.
    for d in docs:
        if d.get("kind") == "Ingress":
            backends = {p["backend"]["service"]["name"] for r in d["spec"]["rules"] for p in r["http"]["paths"]}
            if backends != {RELEASE}:
                failures.append(f"the Ingress must route to the api Service only, got {sorted(backends)}")
        if d.get("kind") == "HTTPRoute":
            backends = {b["name"] for r in d["spec"]["rules"] for b in r["backendRefs"]}
            want = {A2A} if d["metadata"]["name"] == A2A else {RELEASE}
            if backends != want:
                failures.append(f"the HTTPRoute {d['metadata']['name']} must route to {sorted(want)} only, got {sorted(backends)}")
    routes = sorted(d["metadata"]["name"] for d in docs if d.get("kind") == "HTTPRoute")
    if routes != sorted([RELEASE, A2A]):
        failures.append(f"expected the api and a2a HTTPRoutes, rendered: {routes}")
    hpa = next((d for d in docs if d.get("kind") == "HorizontalPodAutoscaler"), None)
    if hpa is None or hpa["spec"]["scaleTargetRef"]["name"] != RELEASE:
        failures.append("the api HPA was not rendered (or does not scale the api Deployment)")
    elif "replicas" in deployments.get(RELEASE, {}).get("spec", {}):
        failures.append("the api Deployment must omit replicas when its HPA owns them")

    check_a2a(deployments.get(A2A), failures)

    api = deployments.get(RELEASE)
    if api is None:
        failures.append("the api Deployment was not rendered")
        return
    api_env = env_names(api)
    for want in API_ENV:
        if want not in api_env:
            failures.append(f"the api Deployment does not render {want} from its dedicated value")
    duplicates = sorted({n for n in api_env if api_env.count(n) > 1})
    if duplicates:
        failures.append(f"the api Deployment renders env vars twice: {duplicates}")
    for name, want in (("OUTBOX_RELAY_INTERVAL", "2s"), ("SHUTDOWN_DRAIN_DELAY", "5s")):
        if env_value(api, name) != want:
            failures.append(f"the api Deployment renders {name}={env_value(api, name)!r}, want {want!r}")


def check_a2a(a2a: dict | None, failures: list[str]) -> None:
    if a2a is None:
        failures.append("the a2a Deployment was not rendered")
        return
    names = env_names(a2a)
    for want in A2A_ENV:
        if want not in names:
            failures.append(f"the a2a Deployment does not render {want}")
    for bad in A2A_ENV_ABSENT:
        if bad in names:
            failures.append(f"the read-only a2a Deployment must not render {bad}")
    container = a2a["spec"]["template"]["spec"]["containers"][0]
    if container.get("envFrom"):
        failures.append("the a2a Deployment must not mount the api ConfigMap (envFrom)")
    if container.get("command") != ["/app/a2a"]:
        failures.append(f"the a2a container must run /app/a2a, got {container.get('command')}")
    if env_value(a2a, "A2A_ADDR") != ":8093":
        failures.append(f"the a2a agent must listen on :8093, got {env_value(a2a, 'A2A_ADDR')!r}")
    if container["ports"][0]["containerPort"] != 8093:
        failures.append("the a2a container port must be 8093")
    if "replicas" not in a2a["spec"]:
        failures.append("the a2a Deployment sets its own replicas (no HPA)")


def check_refusals(failures: list[str]) -> None:
    cases: list[tuple[str, list[str], str]] = [
        ("without a database source", runtime("database"), "requires database.url or database.existingSecret"),
        ("without kafka.brokers", runtime("kafka"), "kafka.brokers"),
        ("a *_MODE through extraEnv",
         BASE + ["--set", "extraEnv[0].name=CACHE_MODE", "--set", "extraEnv[0].value=permissive"], "extraEnv sets CACHE_MODE"),
        ("a consumer group through extraEnv",
         BASE + ["--set", "extraEnv[0].name=OFFER_AVAILABILITY_CONSUMER_GROUP", "--set", "extraEnv[0].value=pod"],
         "extraEnv sets OFFER_AVAILABILITY_CONSUMER_GROUP"),
        ("a chart-owned variable through extraEnv",
         BASE + ["--set", "extraEnv[0].name=KAFKA_BROKERS", "--set", "extraEnv[0].value=x:9092"], "extraEnv sets KAFKA_BROKERS"),
        ("a grace period under the shutdown bounds", BASE + ["--set", "terminationGracePeriodSeconds=30"], "terminationGracePeriodSeconds"),
        ("a grace period equal to the shutdown bounds", BASE + ["--set", "terminationGracePeriodSeconds=35"], "must exceed 35"),
        ("an http address on another port than the container's", BASE + ["--set", "config.httpAddr=:9090"], "config.httpAddr"),
        ("an a2a address on another port than the a2a container's",
         BASE + ["--set", "a2a.enabled=true", "--set", "a2a.httpAddr=:9090"], "a2a.httpAddr"),
    ]
    for key in GROUPS:
        cases.append((f"without config.{key}", runtime(key), f"config.{key} is required"))
    for label, args, needle in cases:
        result = helm_template(args)
        if result.returncode == 0 or needle not in result.stderr:
            failures.append(f"chart rendered (or failed for another reason) {label}")


def main() -> int:
    failures: list[str] = []

    check_components(render(ENABLE_EVERYTHING), failures)

    # Default values must not deploy an HPA or a route, nor any optional env.
    defaults = render(BASE)
    stray = [d["kind"] for d in defaults if d.get("kind") in {"HorizontalPodAutoscaler", "Ingress", "HTTPRoute"}]
    if stray:
        failures.append(f"optional templates rendered with default values: {stray}")
    a2a_stray = [d["metadata"]["name"] for d in defaults if d["metadata"]["name"].endswith("-a2a")]
    if a2a_stray:
        failures.append(f"the a2a agent is off by default, but these rendered: {a2a_stray}")
    dep = next(d for d in defaults if d.get("kind") == "Deployment")
    names = env_names(dep)
    off = {"OUTBOX_RELAY_INTERVAL", "SHUTDOWN_DRAIN_DELAY"}
    if off & set(names):
        failures.append(f"these must be off with default values: {sorted(off & set(names))}")
    for want in ("KAFKA_BROKERS", "OUTBOX_MAX_ATTEMPTS", *(k for k in API_ENV if k.endswith("_CONSUMER_GROUP"))):
        if want not in names:
            failures.append(f"{want} is mandatory and must always be rendered")

    check_refusals(failures)

    if failures:
        for f in failures:
            print(f"FAIL: {f}")
        return 1

    print("PASS: each Service selects exactly one Deployment (the api, and the read-only a2a agent when enabled); the HPA, "
          "Ingress, HTTPRoute and a2a are off by default and route to their own Service only; every cmd/api env var has a dedicated value; the chart refuses to render without a "
          "database source, brokers or the consumer group, with a *_MODE or a consumer group in extraEnv, with a grace "
          "period that does not exceed the shutdown bounds, or with an http address off the container port")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
