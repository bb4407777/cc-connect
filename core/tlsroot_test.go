package core

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// httptest 的临时证书没有 SCT，正是 macOS 平台校验器会拒的那类证书。
func newSelfSignedTLSServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	caFile := filepath.Join(t.TempDir(), "roots.pem")
	if err := os.WriteFile(caFile, pemBytes, 0o644); err != nil {
		t.Fatalf("write ca: %v", err)
	}
	return srv, caFile
}

func mustPoolFromPEM(t *testing.T, path string) *x509.CertPool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pem: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(b) {
		t.Fatal("AppendCertsFromPEM failed")
	}
	return pool
}

// 控制组：证明「平台校验器会拒这类证书」在 darwin 上确实成立，
// 即下面的加固用例不是空过。非 darwin 跳过（那里是纯 Go 校验，无此 quirk）。
func TestPlatformVerifierRejectsCertWithoutSCT(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("platform verifier quirk is darwin-only")
	}
	srv, _ := newSelfSignedTLSServer(t)

	// 显式 nil TLSClientConfig = 走平台校验器（本机 SecTrust）。
	client := &http.Client{Transport: &http.Transport{}, Timeout: 10 * time.Second}
	resp, err := client.Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Skip("platform verifier accepted this cert; nothing to prove here")
	}
	t.Logf("platform verifier rejected (expected): %v", err)
}

// 主用例：显式 RootCAs（CC_TLS_CA_FILE → 纯 Go 校验）必须能验过同一张证书。
// 未打补丁时，本用例在 darwin 上会以 not standards compliant 失败。
func TestHardenTransportBypassesPlatformVerifier(t *testing.T) {
	srv, caFile := newSelfSignedTLSServer(t)
	t.Setenv(TLSRootFileEnv, caFile)

	client := &http.Client{Transport: HardenTransport(nil), Timeout: 10 * time.Second}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("hardened client should verify against explicit roots, got: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if pool := ExtraRootCAs(); pool == nil {
		t.Fatal("ExtraRootCAs() = nil, expected a pool loaded from " + caFile)
	}
}

func TestHardenTransportKeepsExistingRootCAs(t *testing.T) {
	_, caFile := newSelfSignedTLSServer(t)
	custom := mustPoolFromPEM(t, caFile)
	tr := HardenTransport(&http.Transport{TLSClientConfig: &tls.Config{RootCAs: custom}})
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.RootCAs != custom {
		t.Fatal("existing RootCAs must be respected, not overwritten")
	}
}

func TestExtraRootCAsKillSwitch(t *testing.T) {
	t.Setenv(TLSRootFileEnv, "off")
	if pool := ExtraRootCAs(); pool != nil {
		t.Fatal("CC_TLS_CA_FILE=off must disable hardening")
	}
}

func TestExtraRootCAsMissingFileFailsOpen(t *testing.T) {
	t.Setenv(TLSRootFileEnv, filepath.Join(t.TempDir(), "does-not-exist.pem"))
	if pool := ExtraRootCAs(); pool != nil {
		t.Fatal("unreadable CA file must fail open (nil pool)")
	}
}

// 真机链路验证（默认跳过）：用生产同一套 core.HardenTransport 打真实上游，
// 证明加固后的 transport 能验过线上证书。TLS_LIVE_TEST=1 时运行。
func TestHardenedTransportLiveUpstreams(t *testing.T) {
	if os.Getenv("TLS_LIVE_TEST") != "1" {
		t.Skip("set TLS_LIVE_TEST=1 to hit live upstreams")
	}
	for _, host := range []string{"ilinkai.weixin.qq.com:443", "open.feishu.cn:443"} {
		client := &http.Client{Transport: HardenTransport(nil), Timeout: 15 * time.Second}
		resp, err := client.Get("https://" + host + "/ping-probe")
		if err != nil {
			t.Errorf("%s: hardened transport failed to verify: %v", host, err)
			continue
		}
		resp.Body.Close()
		t.Logf("%s: TLS verified under explicit RootCAs (HTTP %d)", host, resp.StatusCode)
	}
}
