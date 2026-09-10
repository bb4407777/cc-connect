package core

import (
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
)

// ── 与 macOS 系统信任层（Apple SecTrust）脱钩 ─────────────────────────────
//
// 2026-09-10 事故（工单 t_a31f543c）：macOS 的 CT 策略间歇性拒绝腾讯 ilink
// 边缘证书与 *.feishu.cn 证书，SecTrust 报
//
//	x509: “<host>” certificate is not standards compliant
//
// 导致 cc-connect（微信中枢）发不出收不进、lark-cli 全线挂；而 openssl/curl
// 对同一证书校验通过（不是证书坏、不是网络坏）。
//
// 根因：Go 在 darwin 上默认走平台校验器（crypto/x509 的 systemVerify →
// SecTrustEvaluateWithError），Apple 的策略由它说了算。实测（见
// tlsroot_test.go 与 ~/skills/bot/SKILL.md「通道故障排查」）：
//   - SSL_CERT_FILE=/etc/ssl/cert.pem  在 darwin 上被忽略；
//   - GODEBUG=x509usefallbackroots=1    在 darwin 上无 SetFallbackRoots 即无效。
//
// 唯一可靠的解法：显式设置 tls.Config.RootCAs。Go 的 verify 逻辑
// （crypto/x509/verify.go）只在 RootCAs 为 nil 或 systemPool 时才调平台校验器；
// RootCAs 一旦是我们自己从 PEM 装的普通 CertPool，就走纯 Go 链路校验
// （不做 Apple CT/吊销检查），信任集合仍来自系统根文件 /etc/ssl/cert.pem（128 张）。
//
// 代价（明确的取舍，非疏漏）：失去 Apple 的 CT 与吊销策略校验。
// 关闭方式：CC_TLS_CA_FILE=off（回到平台校验器）。
const (
	// TLSRootFileEnv 指定根证书 PEM 路径；off/0/false/disable 关闭加固。
	TLSRootFileEnv = "CC_TLS_CA_FILE"

	defaultTLSRootFile = "/etc/ssl/cert.pem"
)

var (
	tlsRootMu    sync.Mutex
	tlsRootCache = map[string]*x509.CertPool{}
)

// TLSRootFile 返回要加载的根证书 PEM 路径，"" 表示不加固
// （被显式关闭，或非 darwin 且未显式指定）。
func TLSRootFile() string {
	if v := strings.TrimSpace(os.Getenv(TLSRootFileEnv)); v != "" {
		switch strings.ToLower(v) {
		case "off", "0", "false", "disable", "none":
			return ""
		}
		return v
	}
	// 只在 darwin 上默认加固：其他平台 Go 本来就读系统 PEM，行为已等价，
	// 额外指定反而可能收窄信任集合（最小改动原则）。
	if runtime.GOOS != "darwin" {
		return ""
	}
	return defaultTLSRootFile
}

// ExtraRootCAs 返回显式根证书池；nil 表示不加固（调用方保持原行为，绝不因
// 加固失败而更坏）。按路径缓存，读不到/解析不出时记日志并返回 nil（fail-open）。
func ExtraRootCAs() *x509.CertPool {
	path := TLSRootFile()
	if path == "" {
		return nil
	}
	tlsRootMu.Lock()
	defer tlsRootMu.Unlock()
	if pool, ok := tlsRootCache[path]; ok {
		return pool
	}
	pool := loadRootCAs(path)
	tlsRootCache[path] = pool
	return pool
}

func loadRootCAs(path string) *x509.CertPool {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		slog.Warn("tls: extra root CAs unreadable, keeping platform verifier", "file", path, "err", err)
		return nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		slog.Warn("tls: no certificate parsed from root CA file, keeping platform verifier", "file", path)
		return nil
	}
	slog.Info("tls: explicit RootCAs installed (skips macOS platform verifier)", "file", path)
	return pool
}

// HardenTLSConfig 补上显式 RootCAs（已有 RootCAs 则原样尊重，不覆盖）。
func HardenTLSConfig(cfg *tls.Config) *tls.Config {
	pool := ExtraRootCAs()
	if pool == nil {
		return cfg
	}
	if cfg == nil {
		return &tls.Config{RootCAs: pool}
	}
	if cfg.RootCAs == nil {
		cfg.RootCAs = pool
	}
	return cfg
}

// HardenTransport 返回带显式 RootCAs 的 transport。tr 为 nil 时克隆
// http.DefaultTransport（保留系统代理/超时等既有行为）；加固不可用时原样返回。
// 注意：http.Transport 一旦被使用就不能再改字段，调用方须在首次请求前调用。
func HardenTransport(tr *http.Transport) *http.Transport {
	if tr == nil {
		if def, ok := http.DefaultTransport.(*http.Transport); ok {
			tr = def.Clone()
		} else {
			tr = &http.Transport{}
		}
	}
	tr.TLSClientConfig = HardenTLSConfig(tr.TLSClientConfig)
	return tr
}
