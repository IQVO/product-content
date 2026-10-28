{{/*
Expand the name of the chart.
*/}}
{{- define "product-content.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "product-content.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Chart name and version as used by the chart label.
*/}}
{{- define "product-content.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "product-content.labels" -}}
helm.sh/chart: {{ include "product-content.chart" . }}
{{ include "product-content.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels. Every Deployment and Service also pins
app.kubernetes.io/component (api): a Service selecting on these two alone would
select every component a future change adds.
*/}}
{{- define "product-content.selectorLabels" -}}
app.kubernetes.io/name: {{ include "product-content.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "product-content.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "product-content.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Name of the Secret holding DATABASE_URL: the operator's own
(database.existingSecret) or the one this chart creates from database.url.
*/}}
{{- define "product-content.databaseSecretName" -}}
{{- if .Values.database.existingSecret }}
{{- .Values.database.existingSecret }}
{{- else }}
{{- include "product-content.fullname" . }}-database
{{- end }}
{{- end }}

{{/*
Image reference. The release job publishes the image as v<version> (and packages
the chart with appVersion <version>), so an empty image.tag means "v" + appVersion.
*/}}
{{- define "product-content.image" -}}
{{ .Values.image.repository }}:{{ .Values.image.tag | default (printf "v%s" .Chart.AppVersion) }}
{{- end }}

{{/*
MANDATORY: cmd/api exits at boot without DATABASE_URL (real Postgres only, no
in-memory mode). Fails the render naming the values instead of crash-looping.
*/}}
{{- define "product-content.requireDatabase" -}}
{{- if not (or .Values.database.url .Values.database.existingSecret) -}}
{{- fail "product-content requires database.url or database.existingSecret to be set (DATABASE_URL): the service runs on real Postgres only and exits at boot without it." -}}
{{- end -}}
{{- end -}}

{{/*
MANDATORY: cmd/api exits at boot without KAFKA_BROKERS (outbox relay, the
offer-availability consumer, /readyz). No default broker and no way to switch
Kafka off.
*/}}
{{- define "product-content.requireKafka" -}}
{{- $_ := required "product-content requires kafka.brokers to be set (KAFKA_BROKERS, comma-separated): the service runs on real Kafka only and exits at boot without it." .Values.kafka.brokers -}}
{{- end -}}

{{/*
The offer-availability consumer always runs under a STABLE group id that must
come from values (cmd/api exits at boot without OFFER_AVAILABILITY_CONSUMER_GROUP).
A stable id is what lets a rescheduled pod resume from its committed offsets; it
is never derived from the pod name.
*/}}
{{- define "product-content.requireConsumerGroups" -}}
{{- range $c := list (dict "key" "offerAvailabilityConsumerGroup" "env" "OFFER_AVAILABILITY_CONSUMER_GROUP") -}}
{{- if not (index $.Values.config $c.key) -}}
{{- fail (printf "config.%s is required (%s): the consumer always runs on real Kafka and cmd/api exits at boot without a group. Use a STABLE, service-prefixed id, never the pod name." $c.key $c.env) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
extraEnv may not bring back what the chart owns: no *_MODE variable (this
service has no mode: Postgres and Kafka are the only runtime), no
*_CONSUMER_GROUP (the group id is a config.* value), and none of the variables
the chart renders itself (the same variable is never set twice).
*/}}
{{- define "product-content.requireCleanExtraEnv" -}}
{{- $owned := list "HTTP_ADDR" "LOG_LEVEL" "DATABASE_URL" "MIGRATIONS_DATABASE_URL" "KAFKA_BROKERS" "OUTBOX_RELAY_INTERVAL" "OUTBOX_MAX_ATTEMPTS" "SHUTDOWN_DRAIN_DELAY" "OFFER_AVAILABILITY_CONSUMER_GROUP" -}}
{{- range .Values.extraEnv -}}
{{- $name := toString .name -}}
{{- if hasSuffix "_MODE" $name -}}
{{- fail (printf "extraEnv sets %s: *_MODE variables do not exist here - product-content runs on real Postgres and real Kafka only, with no permissive, off or in-memory mode." $name) -}}
{{- end -}}
{{- if hasSuffix "_CONSUMER_GROUP" $name -}}
{{- fail (printf "extraEnv sets %s: the consumer group id is the stable value config.offerAvailabilityConsumerGroup." $name) -}}
{{- end -}}
{{- if has $name $owned -}}
{{- fail (printf "extraEnv sets %s, which the chart renders from its own values: use that value instead." $name) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
SHUTDOWN_DRAIN_DELAY in whole seconds (the binary accepts Go durations; the chart
accepts "Ns", "Nm" and "0" because it needs the number to size the grace
period). Unset = the binary's default, 5s.
*/}}
{{- define "product-content.drainSeconds" -}}
{{- $raw := toString .Values.config.shutdownDrainDelay -}}
{{- if eq $raw "" -}}5
{{- else if eq $raw "0" -}}0
{{- else if regexMatch "^[0-9]+s$" $raw -}}{{ trimSuffix "s" $raw }}
{{- else if regexMatch "^[0-9]+m$" $raw -}}{{ mul 60 (int (trimSuffix "m" $raw)) }}
{{- else -}}
{{- fail (printf "config.shutdownDrainDelay is %q - want whole seconds (\"5s\"), minutes (\"1m\") or \"0\"." $raw) -}}
{{- end -}}
{{- end -}}

{{/*
The graceful shutdown is sequential (cmd/api/main.go): SHUTDOWN_DRAIN_DELAY,
the HTTP drain (10s), then the offer-availability consumer awaited (10s) and
the outbox relay awaited last (10s). A grace period that does not exceed
drain + 30s can SIGKILL the consumer or the relay mid-drain, stranding an
uncommitted offset or an in-flight outbox batch.
*/}}
{{- define "product-content.requireGracePeriod" -}}
{{- $need := add (int (include "product-content.drainSeconds" .)) 30 -}}
{{- if le (int .Values.terminationGracePeriodSeconds) $need -}}
{{- fail (printf "terminationGracePeriodSeconds is %v - it must exceed %v (SHUTDOWN_DRAIN_DELAY %vs + HTTP 10s + consumer 10s + relay 10s): a shorter period SIGKILLs a worker mid-drain." .Values.terminationGracePeriodSeconds $need (include "product-content.drainSeconds" .)) -}}
{{- end -}}
{{- end -}}

{{/*
The port of HTTP_ADDR is the container port the probes and the Service target.
*/}}
{{- define "product-content.requireHttpPort" -}}
{{- $port := trimPrefix ":" (regexFind ":[0-9]+$" (toString .Values.config.httpAddr)) -}}
{{- if ne $port (toString .Values.service.targetPort) -}}
{{- fail (printf "config.httpAddr is %q but service.targetPort is %v: the container port, the probes and the Service must agree." .Values.config.httpAddr .Values.service.targetPort) -}}
{{- end -}}
{{- end -}}

{{/*
Every render-time requirement of the real runtime, in one include. The api
Deployment (always rendered) includes it, so a mis-set release fails
`helm template` / `helm install`.
*/}}
{{- define "product-content.requireRealRuntime" -}}
{{- include "product-content.requireDatabase" . -}}
{{- include "product-content.requireKafka" . -}}
{{- include "product-content.requireConsumerGroups" . -}}
{{- include "product-content.requireCleanExtraEnv" . -}}
{{- include "product-content.requireGracePeriod" . -}}
{{- include "product-content.requireHttpPort" . -}}
{{- end -}}
