package main

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	descUp = prometheus.NewDesc("mihomo_up",
		"Whether the last read of the mihomo API succeeded.", nil, nil)
	descBuildInfo = prometheus.NewDesc("mihomo_build_info",
		"Version of the mihomo core.", []string{"version"}, nil)

	descProviderProxies = prometheus.NewDesc("mihomo_provider_proxies",
		"Number of nodes in a proxy provider (subscription).", []string{"provider"}, nil)
	descProviderUpdated = prometheus.NewDesc("mihomo_provider_updated_timestamp_seconds",
		"Time of the last successful load of a proxy provider, including downloads that brought no changes.",
		[]string{"provider"}, nil)

	descSubExpire = prometheus.NewDesc("mihomo_subscription_expire_timestamp_seconds",
		"Subscription expiry from the subscription-userinfo header; 0 means the provider set none.",
		[]string{"provider"}, nil)
	descSubUpload = prometheus.NewDesc("mihomo_subscription_upload_bytes",
		"Uploaded traffic reported in the subscription-userinfo header.", []string{"provider"}, nil)
	descSubDownload = prometheus.NewDesc("mihomo_subscription_download_bytes",
		"Downloaded traffic reported in the subscription-userinfo header.", []string{"provider"}, nil)
	descSubTotal = prometheus.NewDesc("mihomo_subscription_total_bytes",
		"Traffic quota from the subscription-userinfo header; 0 means unlimited.", []string{"provider"}, nil)

	descProxyAlive = prometheus.NewDesc("mihomo_proxy_alive",
		"Whether the node passed its last health check against url.",
		[]string{"provider", "proxy", "url"}, nil)
	descProxyDelay = prometheus.NewDesc("mihomo_proxy_delay_seconds",
		"Delay of the node's last health check against url; only reported while the node is alive.",
		[]string{"provider", "proxy", "url"}, nil)

	descGroupProxies = prometheus.NewDesc("mihomo_group_proxies",
		"Nodes of a provider that are members of a proxy group.", []string{"group", "provider"}, nil)
	descGroupProxiesAlive = prometheus.NewDesc("mihomo_group_proxies_alive",
		"Nodes of a provider in a proxy group that passed their last check against the group's test URL.",
		[]string{"group", "provider"}, nil)
	descGroupSelected = prometheus.NewDesc("mihomo_group_selected",
		"The member a proxy group currently uses.", []string{"group", "proxy"}, nil)
)

// Collector reads the mihomo API on every scrape. It keeps no state, so the
// metrics are exactly what the core reports at scrape time.
type Collector struct {
	client  *Client
	timeout time.Duration
	log     *slog.Logger
}

func NewCollector(client *Client, timeout time.Duration, log *slog.Logger) *Collector {
	return &Collector{client: client, timeout: timeout, log: log}
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{
		descUp, descBuildInfo,
		descProviderProxies, descProviderUpdated,
		descSubExpire, descSubUpload, descSubDownload, descSubTotal,
		descProxyAlive, descProxyDelay,
		descGroupProxies, descGroupProxiesAlive, descGroupSelected,
	} {
		ch <- d
	}
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	up := 1.0
	if version, err := c.client.Version(ctx); err != nil {
		c.log.Warn("read mihomo version", "err", err)
		up = 0
	} else {
		ch <- prometheus.MustNewConstMetric(descBuildInfo, prometheus.GaugeValue, 1, version)
	}

	providers, err := c.client.Providers(ctx)
	if err != nil {
		c.log.Warn("read mihomo providers", "err", err)
		up = 0
	} else {
		collectProviders(ch, providers)
	}

	if proxies, err := c.client.Proxies(ctx); err != nil {
		c.log.Warn("read mihomo proxies", "err", err)
		up = 0
	} else if providers != nil {
		collectGroups(ch, proxies, providers)
	}

	ch <- prometheus.MustNewConstMetric(descUp, prometheus.GaugeValue, up)
}

func sortedNames(providers map[string]provider) []string {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func collectProviders(ch chan<- prometheus.Metric, providers map[string]provider) {
	for _, name := range sortedNames(providers) {
		p := providers[name]
		// "default" (Compatible) holds static proxies and built-ins; it has no
		// subscription, so only its nodes are reported.
		if p.VehicleType != "Compatible" {
			nodes := 0
			for _, px := range p.Proxies {
				if px.isNode() {
					nodes++
				}
			}
			ch <- prometheus.MustNewConstMetric(descProviderProxies, prometheus.GaugeValue, float64(nodes), name)
			if !p.UpdatedAt.IsZero() {
				ch <- prometheus.MustNewConstMetric(descProviderUpdated, prometheus.GaugeValue,
					float64(p.UpdatedAt.UnixNano())/1e9, name)
			}
			if s := p.SubscriptionInfo; s != nil {
				ch <- prometheus.MustNewConstMetric(descSubExpire, prometheus.GaugeValue, float64(s.Expire), name)
				ch <- prometheus.MustNewConstMetric(descSubUpload, prometheus.GaugeValue, float64(s.Upload), name)
				ch <- prometheus.MustNewConstMetric(descSubDownload, prometheus.GaugeValue, float64(s.Download), name)
				ch <- prometheus.MustNewConstMetric(descSubTotal, prometheus.GaugeValue, float64(s.Total), name)
			}
		}

		for _, px := range p.Proxies {
			if !px.isNode() {
				continue
			}
			urls := make([]string, 0, len(px.Extra))
			for url := range px.Extra {
				urls = append(urls, url)
			}
			sort.Strings(urls)
			for _, url := range urls {
				state := px.Extra[url]
				ch <- prometheus.MustNewConstMetric(descProxyAlive, prometheus.GaugeValue,
					boolValue(state.Alive), name, px.Name, url)
				if n := len(state.History); state.Alive && n > 0 && state.History[n-1].Delay > 0 {
					ch <- prometheus.MustNewConstMetric(descProxyDelay, prometheus.GaugeValue,
						float64(state.History[n-1].Delay)/1000, name, px.Name, url)
				}
			}
		}
	}
}

type groupCount struct{ total, alive int }

func collectGroups(ch chan<- prometheus.Metric, proxies map[string]proxy, providers map[string]provider) {
	// Group members are referred to by name; provider nodes are only listed
	// under /providers/proxies.
	type node struct {
		provider string
		proxy    proxy
	}
	nodes := map[string]node{}
	for _, name := range sortedNames(providers) {
		for _, px := range providers[name].Proxies {
			if _, seen := nodes[px.Name]; !seen && px.isNode() {
				nodes[px.Name] = node{provider: name, proxy: px}
			}
		}
	}

	groupNames := make([]string, 0, len(proxies))
	for name, g := range proxies {
		// GLOBAL is the built-in selector of the "global" mode, not a real group.
		if g.All != nil && name != "GLOBAL" {
			groupNames = append(groupNames, name)
		}
	}
	sort.Strings(groupNames)

	for _, name := range groupNames {
		g := proxies[name]
		counts := map[string]*groupCount{}
		for _, member := range g.All {
			n, ok := nodes[member]
			if !ok {
				continue // a built-in or a nested group
			}
			gc := counts[n.provider]
			if gc == nil {
				gc = &groupCount{}
				counts[n.provider] = gc
			}
			gc.total++
			if n.proxy.aliveFor(g.TestURL) {
				gc.alive++
			}
		}
		for provider, gc := range counts {
			ch <- prometheus.MustNewConstMetric(descGroupProxies, prometheus.GaugeValue, float64(gc.total), name, provider)
			ch <- prometheus.MustNewConstMetric(descGroupProxiesAlive, prometheus.GaugeValue, float64(gc.alive), name, provider)
		}
		if g.Now != "" {
			ch <- prometheus.MustNewConstMetric(descGroupSelected, prometheus.GaugeValue, 1, name, g.Now)
		}
	}
}
