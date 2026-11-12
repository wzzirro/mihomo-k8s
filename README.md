# mihomo-k8s

A rule-based egress gateway for Kubernetes workloads, built on the
[mihomo](https://github.com/MetaCubeX/mihomo) (Clash.Meta) core.

It brings the model of desktop Clash clients such as
[FlClash](https://github.com/chen08209/FlClash) into a cluster: nodes come from
a subscription, an automatic group keeps a working node selected, rules decide
what may leave, and a panel shows live connections and logs. The difference is
who the client is — not a person's laptop, but your pods.

The typical use: services in the cluster (bots, alerting, integrations) need to
reach an API that is not reachable from where the cluster runs, Telegram being
the built-in preset.

> Status: early. This is a Helm chart today; an operator and a purpose-built UI
> are planned once the model has proven itself in production.

## What you get

- **Gateway** — two mihomo replicas behind one Service, speaking HTTP and
  SOCKS5 on a single port.
- **Subscriptions** — mihomo `proxy-providers` passed through as is, refreshed
  by the core. Secrets (subscription URL, headers) stay in Kubernetes Secrets
  and are substituted at pod start; they never land in a ConfigMap.
- **Automatic node selection** — one `url-test` or `fallback` group with
  include/exclude filters by node name. There is no manual selection on
  purpose: replicas share no state, so they must reach the same decision alone.
- **Rules** — the Telegram preset plus your own domains and CIDRs go through
  the proxies; everything else goes out directly from the gateway
  (`defaultAction: DIRECT`), or is refused if you set `defaultAction: REJECT`.
- **Two ways in for workloads**
  - *explicit*: a ConfigMap with `HTTPS_PROXY` / `NO_PROXY`, created in the
    namespaces you list, consumed with `envFrom`;
  - *transparent*: an init container and a sidecar for workloads that cannot
    use a proxy (for example a local Telegram Bot API server, which talks
    MTProto to hardcoded addresses). See
    [examples/transparent-client.yaml](examples/transparent-client.yaml).
- **Reachability probe** — checks the target through the proxy group and POSTs
  to a webhook when it starts failing and when it recovers. The webhook is
  called directly, not through the gateway.
- **Panel** — [metacubexd](https://github.com/MetaCubeX/metacubexd) as a
  sidecar. The panel and the mihomo API listen on `127.0.0.1` inside the pod:
  reachable with `kubectl port-forward`, invisible to the rest of the cluster.

## Install

```sh
kubectl create namespace egress
kubectl -n egress create secret generic mihomo-subscription \
  --from-literal=url='https://subscription.example/your-token'
kubectl -n egress create secret generic mihomo-controller \
  --from-literal=secret="$(openssl rand -hex 24)"

helm install egress ./charts/mihomo-k8s -n egress \
  -f examples/values-telegram.yaml
```

[examples/values-telegram.yaml](examples/values-telegram.yaml) is a commented
starting point; [charts/mihomo-k8s/values.yaml](charts/mihomo-k8s/values.yaml)
documents every option.

## Connecting workloads

**Explicit proxy.** List the namespace in `clientEnv.namespaces`, then:

```yaml
envFrom:
  - configMapRef:
      name: egress-mihomo-k8s-proxy-env
```

All of the workload's outgoing HTTP(S) now goes to the gateway. Listed
destinations leave through the proxies; the rest is forwarded directly by the
gateway, so it reaches the internet from the gateway pod's node rather than the
workload's. Hosts that must not touch the gateway at all belong in
`clientEnv.extraNoProxy`.

**Transparent.** List the namespace in `transparentClient.namespaces` and add
the two containers from
[examples/transparent-client.yaml](examples/transparent-client.yaml) to the pod.
Only connections to `transparentClient.cidrs` (the Telegram subnets by default)
are intercepted; the rest of the pod's traffic is untouched. If the sidecar is
down those connections are refused rather than sent directly.

## Panel

```sh
kubectl -n egress port-forward deploy/egress-mihomo-k8s 9090:9090 8080:8080
```

Open <http://127.0.0.1:8080> and enter the API secret. You are looking at one
replica; pick a specific pod instead of `deploy/...` to see another.

## Probe webhook

```json
{
  "source": "mihomo-k8s",
  "status": "firing",
  "target": "https://api.telegram.org",
  "group": "auto",
  "namespace": "egress",
  "pod": "egress-mihomo-k8s-5d8c7f9b6-abcde",
  "message": "https://api.telegram.org is unreachable through proxy group auto"
}
```

`status` is `firing` after `probe.failureThreshold` consecutive failures and
`resolved` on the first success afterwards. Each replica reports on its own.

## Good to know

- **Anyone in the cluster can use the gateway** unless you restrict it. Add a
  NetworkPolicy that admits only the namespaces you connected; the chart does
  not ship one yet. With `defaultAction: DIRECT` this also makes the gateway a
  way around egress policies of other namespaces; set `REJECT` if that matters.
- **Only automatic groups.** Selecting a node by hand in the panel affects one
  replica and is lost on restart.
- **IPv4 TCP only** for transparent clients. The sidecar logs
  `Start Redir server error: operation not permitted` at start: that is its
  unused UDP listener, TCP redirection works.
- **Subscription device limits.** Both replicas fetch the subscription; if your
  provider counts devices, give both the same device header.
- The gateway needs direct access to the subscription URL and to the proxy
  nodes themselves.

## Development

```sh
helm lint charts/mihomo-k8s -f charts/mihomo-k8s/ci/test-values.yaml
helm template egress charts/mihomo-k8s -n egress -f charts/mihomo-k8s/ci/test-values.yaml
```

## License

[GPL-3.0](LICENSE), the same as mihomo.
