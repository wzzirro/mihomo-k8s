{{- define "mihomo-k8s.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "mihomo-k8s.fullname" -}}
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

{{- define "mihomo-k8s.selectorLabels" -}}
app.kubernetes.io/name: {{ include "mihomo-k8s.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "mihomo-k8s.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "mihomo-k8s.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "mihomo-k8s.image" -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) }}
{{- end }}

{{/* In-cluster DNS name of the gateway Service. */}}
{{- define "mihomo-k8s.gatewayHost" -}}
{{- printf "%s.%s.svc.%s" (include "mihomo-k8s.fullname" .) .Release.Namespace .Values.clusterDomain }}
{{- end }}

{{- define "mihomo-k8s.clientEnvName" -}}
{{- default (printf "%s-proxy-env" (include "mihomo-k8s.fullname" .)) .Values.clientEnv.name }}
{{- end }}

{{- define "mihomo-k8s.transparentClientName" -}}
{{- default (printf "%s-transparent-client" (include "mihomo-k8s.fullname" .)) .Values.transparentClient.name }}
{{- end }}

{{/* CIDRs intercepted by transparent clients. */}}
{{- define "mihomo-k8s.transparentCIDRs" -}}
{{- $cidrs := .Values.transparentClient.cidrs }}
{{- if not $cidrs }}{{- $cidrs = .Values.presets.telegram.ipCIDRs }}{{- end }}
{{- join " " $cidrs }}
{{- end }}

{{/* Environment shared by the gateway containers. */}}
{{- define "mihomo-k8s.env" -}}
{{- if and .Values.controller.enabled .Values.controller.existingSecret.name }}
- name: MIHOMO_CONTROLLER_SECRET
  valueFrom:
    secretKeyRef:
      name: {{ .Values.controller.existingSecret.name }}
      key: {{ .Values.controller.existingSecret.key }}
{{- end }}
{{- with .Values.env }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/* The gateway's mihomo configuration (may contain ${VAR} placeholders). */}}
{{- define "mihomo-k8s.config" -}}
{{- $group := .Values.group -}}
{{- $providers := .Values.providers | default dict -}}
{{- $proxies := .Values.proxies | default list -}}
{{- if and (empty $providers) (empty $proxies) -}}
{{- fail "mihomo-k8s: nothing to route through - set .Values.providers and/or .Values.proxies" -}}
{{- end -}}
{{- if not (has $group.type (list "url-test" "fallback")) -}}
{{- fail "mihomo-k8s: .Values.group.type must be url-test or fallback" -}}
{{- end -}}
{{- if not (has .Values.defaultAction (list "REJECT" "DIRECT")) -}}
{{- fail "mihomo-k8s: .Values.defaultAction must be REJECT or DIRECT" -}}
{{- end -}}

{{- $cfg := dict
    "mixed-port" (int .Values.gateway.port)
    "allow-lan" true
    "bind-address" "*"
    "mode" "rule"
    "log-level" .Values.gateway.logLevel
    "ipv6" false
    "find-process-mode" "off"
    "profile" (dict "store-selected" false "store-fake-ip" false) -}}

{{- if .Values.controller.enabled -}}
{{- $_ := set $cfg "external-controller" (printf "127.0.0.1:%d" (int .Values.controller.port)) -}}
{{- if .Values.controller.existingSecret.name -}}
{{- $_ := set $cfg "secret" "${MIHOMO_CONTROLLER_SECRET}" -}}
{{- end -}}
{{- end -}}

{{- $g := dict
    "name" $group.name
    "type" $group.type
    "url" $group.url
    "interval" (int $group.interval)
    "timeout" (int $group.timeout)
    "lazy" false -}}
{{- if eq $group.type "url-test" -}}{{- $_ := set $g "tolerance" (int $group.tolerance) -}}{{- end -}}
{{- with $group.filter -}}{{- $_ := set $g "filter" . -}}{{- end -}}
{{- with $group.excludeFilter -}}{{- $_ := set $g "exclude-filter" . -}}{{- end -}}

{{- if $providers -}}
{{- $rendered := dict -}}
{{- range $name, $p := $providers -}}
{{- $p = deepCopy $p -}}
{{- if not (hasKey $p "path") -}}{{- $_ := set $p "path" (printf "./proxy-providers/%s.yaml" $name) -}}{{- end -}}
{{- $_ := set $rendered $name $p -}}
{{- end -}}
{{- $_ := set $cfg "proxy-providers" $rendered -}}
{{- $_ := set $g "use" (keys $providers | sortAlpha) -}}
{{- end -}}

{{- if $proxies -}}
{{- $names := list -}}
{{- range $proxies -}}{{- $names = append $names .name -}}{{- end -}}
{{- $_ := set $cfg "proxies" $proxies -}}
{{- $_ := set $g "proxies" $names -}}
{{- end -}}

{{- $_ := set $cfg "proxy-groups" (list $g) -}}

{{- $rules := list -}}
{{- range .Values.extraRules -}}{{- $rules = append $rules . -}}{{- end -}}
{{- if .Values.allow.telegram -}}
{{- range .Values.presets.telegram.domainSuffixes -}}{{- $rules = append $rules (printf "DOMAIN-SUFFIX,%s,%s" . $group.name) -}}{{- end -}}
{{- range .Values.presets.telegram.ipCIDRs -}}{{- $rules = append $rules (printf "IP-CIDR,%s,%s,no-resolve" . $group.name) -}}{{- end -}}
{{- end -}}
{{- range .Values.allow.domains -}}{{- $rules = append $rules (printf "DOMAIN,%s,%s" . $group.name) -}}{{- end -}}
{{- range .Values.allow.domainSuffixes -}}{{- $rules = append $rules (printf "DOMAIN-SUFFIX,%s,%s" . $group.name) -}}{{- end -}}
{{- range .Values.allow.ipCIDRs -}}{{- $rules = append $rules (printf "IP-CIDR,%s,%s,no-resolve" . $group.name) -}}{{- end -}}
{{- /* The gateway must never connect to itself or to link-local addresses
       (its own API, cloud metadata) on a client's behalf. */ -}}
{{- range .Values.blockedCIDRs -}}{{- $rules = append $rules (printf "IP-CIDR,%s,REJECT" .) -}}{{- end -}}
{{- $rules = append $rules (printf "MATCH,%s" .Values.defaultAction) -}}
{{- $_ := set $cfg "rules" $rules -}}

{{- /* A value that is exactly one placeholder is double-quoted, so that any
       secret substitutes into valid YAML (render-config.awk escapes it). */ -}}
{{- regexReplaceAll "(?m)(: |- )(\\$\\{[A-Za-z_][A-Za-z0-9_]*\\})$" (toYaml $cfg) "${1}\"${2}\"" -}}
{{- end }}
