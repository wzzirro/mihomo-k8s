package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

const testSecret = "s3cret"

// Shapes follow mihomo v1.19 responses, trimmed to the fields that matter.
const versionJSON = `{"meta":true,"version":"v1.19.34"}`

const providersJSON = `{"providers":{
 "default":{"name":"default","type":"Proxy","vehicleType":"Compatible","updatedAt":"0001-01-01T00:00:00Z","testUrl":"","proxies":[
  {"name":"DIRECT","type":"Direct","alive":true,"history":[],"extra":{}},
  {"name":"auto","type":"URLTest","alive":true,"all":["Node A","Node B","static-1","DIRECT"],"now":"Node A","history":[],"extra":{}},
  {"name":"static-1","type":"Socks5","alive":true,"history":[],"extra":{
    "https://api.telegram.org/":{"alive":false,"history":[{"time":"2026-11-19T13:49:10Z","delay":0}]}}}
 ]},
 "sub":{"name":"sub","type":"Proxy","vehicleType":"HTTP","updatedAt":"2026-11-19T13:39:10.5Z","testUrl":"https://www.gstatic.com/generate_204",
  "subscriptionInfo":{"Upload":10,"Download":348314819679,"Total":0,"Expire":1824019500},
  "proxies":[
  {"name":"Node A","type":"Vless","alive":true,"history":[],"extra":{
    "https://www.gstatic.com/generate_204":{"alive":true,"history":[{"time":"2026-11-19T13:44:10Z","delay":191},{"time":"2026-11-19T13:49:10Z","delay":148}]},
    "https://api.telegram.org/":{"alive":true,"history":[{"time":"2026-11-19T13:49:10Z","delay":250}]}}},
  {"name":"Node B","type":"Vless","alive":false,"history":[],"extra":{
    "https://www.gstatic.com/generate_204":{"alive":false,"history":[{"time":"2026-11-19T13:49:10Z","delay":0}]}}},
  {"name":"Node \"C\" LTE","type":"Trojan","alive":true,"history":[],"extra":null}
 ]}
}}`

const proxiesJSON = `{"proxies":{
 "COMPATIBLE":{"name":"COMPATIBLE","type":"Compatible","alive":true,"history":[],"extra":{}},
 "DIRECT":{"name":"DIRECT","type":"Direct","alive":true,"history":[],"extra":{}},
 "GLOBAL":{"name":"GLOBAL","type":"Selector","all":["DIRECT","auto"],"now":"DIRECT","testUrl":"","history":[],"extra":{}},
 "auto":{"name":"auto","type":"URLTest","all":["Node A","Node B","static-1","DIRECT"],"now":"Node A","testUrl":"https://api.telegram.org/","history":[],"extra":{}}
}}`

func fakeAPI(t *testing.T) *httptest.Server {
	t.Helper()
	routes := map[string]string{
		"/version":           versionJSON,
		"/providers/proxies": providersJSON,
		"/proxies":           proxiesJSON,
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testSecret {
			http.Error(w, `{"message":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestCollector(t *testing.T) {
	srv := fakeAPI(t)
	defer srv.Close()

	c := NewCollector(NewClient(srv.URL, testSecret, 2*time.Second), 2*time.Second, quietLogger())

	want := `
# HELP mihomo_build_info Version of the mihomo core.
# TYPE mihomo_build_info gauge
mihomo_build_info{version="v1.19.34"} 1
# HELP mihomo_group_proxies Nodes of a provider that are members of a proxy group.
# TYPE mihomo_group_proxies gauge
mihomo_group_proxies{group="auto",provider="default"} 1
mihomo_group_proxies{group="auto",provider="sub"} 2
# HELP mihomo_group_proxies_alive Nodes of a provider in a proxy group that passed their last check against the group's test URL.
# TYPE mihomo_group_proxies_alive gauge
mihomo_group_proxies_alive{group="auto",provider="default"} 0
mihomo_group_proxies_alive{group="auto",provider="sub"} 1
# HELP mihomo_group_selected The member a proxy group currently uses.
# TYPE mihomo_group_selected gauge
mihomo_group_selected{group="auto",proxy="Node A"} 1
# HELP mihomo_provider_proxies Number of nodes in a proxy provider (subscription).
# TYPE mihomo_provider_proxies gauge
mihomo_provider_proxies{provider="sub"} 3
# HELP mihomo_provider_updated_timestamp_seconds Time of the last successful load of a proxy provider, including downloads that brought no changes.
# TYPE mihomo_provider_updated_timestamp_seconds gauge
mihomo_provider_updated_timestamp_seconds{provider="sub"} 1.7950955505e+09
# HELP mihomo_proxy_alive Whether the node passed its last health check against url.
# TYPE mihomo_proxy_alive gauge
mihomo_proxy_alive{provider="default",proxy="static-1",url="https://api.telegram.org/"} 0
mihomo_proxy_alive{provider="sub",proxy="Node A",url="https://api.telegram.org/"} 1
mihomo_proxy_alive{provider="sub",proxy="Node A",url="https://www.gstatic.com/generate_204"} 1
mihomo_proxy_alive{provider="sub",proxy="Node B",url="https://www.gstatic.com/generate_204"} 0
# HELP mihomo_proxy_delay_seconds Delay of the node's last health check against url; only reported while the node is alive.
# TYPE mihomo_proxy_delay_seconds gauge
mihomo_proxy_delay_seconds{provider="sub",proxy="Node A",url="https://api.telegram.org/"} 0.25
mihomo_proxy_delay_seconds{provider="sub",proxy="Node A",url="https://www.gstatic.com/generate_204"} 0.148
# HELP mihomo_subscription_download_bytes Downloaded traffic reported in the subscription-userinfo header.
# TYPE mihomo_subscription_download_bytes gauge
mihomo_subscription_download_bytes{provider="sub"} 3.48314819679e+11
# HELP mihomo_subscription_expire_timestamp_seconds Subscription expiry from the subscription-userinfo header; 0 means the provider set none.
# TYPE mihomo_subscription_expire_timestamp_seconds gauge
mihomo_subscription_expire_timestamp_seconds{provider="sub"} 1.8240195e+09
# HELP mihomo_subscription_total_bytes Traffic quota from the subscription-userinfo header; 0 means unlimited.
# TYPE mihomo_subscription_total_bytes gauge
mihomo_subscription_total_bytes{provider="sub"} 0
# HELP mihomo_subscription_upload_bytes Uploaded traffic reported in the subscription-userinfo header.
# TYPE mihomo_subscription_upload_bytes gauge
mihomo_subscription_upload_bytes{provider="sub"} 10
# HELP mihomo_up Whether the last read of the mihomo API succeeded.
# TYPE mihomo_up gauge
mihomo_up 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(want)); err != nil {
		t.Fatal(err)
	}
	// Every series must be unique and every label set consistent.
	if problems, err := testutil.CollectAndLint(c); err != nil {
		t.Fatal(err)
	} else {
		for _, p := range problems {
			t.Errorf("lint %s: %s", p.Metric, p.Text)
		}
	}
}

func TestCollectorAPIDown(t *testing.T) {
	srv := fakeAPI(t)
	defer srv.Close()

	// Wrong secret: every call is rejected, only mihomo_up is reported.
	c := NewCollector(NewClient(srv.URL, "wrong", 2*time.Second), 2*time.Second, quietLogger())
	want := `
# HELP mihomo_up Whether the last read of the mihomo API succeeded.
# TYPE mihomo_up gauge
mihomo_up 0
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(want)); err != nil {
		t.Fatal(err)
	}
}
