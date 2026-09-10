package weixin

import (
	"net/http"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

// 回归护栏（工单 t_a31f543c）：微信三条出站 TLS 链路都必须带显式 RootCAs，
// 否则在 macOS 上会走 Apple SecTrust，被 CT 策略拒时整条通道静默挂掉。
func TestWeixinOutboundTLSHardening(t *testing.T) {
	if core.TLSRootFile() == "" {
		t.Skip("hardening disabled via " + core.TLSRootFileEnv)
	}

	// 1) ilink API 客户端（发消息/收消息的普通请求）
	api := newAPIClient(defaultBaseURL, "token", "", nil)
	assertExplicitRootCAs(t, "ilink api", api.httpClient)

	// 2) ilink 长轮询（入站长连接）
	lp := api.longPollClient(0)
	assertExplicitRootCAs(t, "ilink long-poll", lp)
	if lp.Timeout <= defaultLongPollTimeout {
		t.Fatalf("long-poll timeout = %v, expected > %v", lp.Timeout, defaultLongPollTimeout)
	}

	// 3) CDN 直连（uTLS Chrome 指纹 + 显式 RootCAs）
	cfg := cdnUTLSConfig("novac2c.cdn.weixin.qq.com")
	if cfg.RootCAs == nil {
		t.Fatal("cdn uTLS config lacks explicit RootCAs")
	}
	if cfg.InsecureSkipVerify {
		t.Fatal("cdn uTLS config must not disable verification")
	}
	if cfg.ServerName != "novac2c.cdn.weixin.qq.com" {
		t.Fatalf("cdn uTLS ServerName = %q", cfg.ServerName)
	}
}

func assertExplicitRootCAs(t *testing.T, name string, c *http.Client) {
	t.Helper()
	if c == nil {
		t.Fatalf("%s: nil client", name)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr == nil {
		t.Fatalf("%s: transport = %#v, want *http.Transport", name, c.Transport)
	}
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.RootCAs == nil {
		t.Fatalf("%s: TLSClientConfig.RootCAs is nil → falls back to macOS platform verifier", name)
	}
}

// 关闭开关必须真的关掉（CC_TLS_CA_FILE=off），否则运维没有回退手段。
func TestWeixinHardeningKillSwitch(t *testing.T) {
	t.Setenv(core.TLSRootFileEnv, "off")
	api := newAPIClient(defaultBaseURL, "token", "", nil)
	tr, _ := api.httpClient.Transport.(*http.Transport)
	if tr != nil && tr.TLSClientConfig != nil && tr.TLSClientConfig.RootCAs != nil {
		t.Fatal("CC_TLS_CA_FILE=off must leave RootCAs untouched")
	}
	if cfg := cdnUTLSConfig("cdn.example.com"); cfg.RootCAs != nil {
		t.Fatal("CC_TLS_CA_FILE=off must leave uTLS RootCAs nil")
	}
}
