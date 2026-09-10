package core

import (
	"net/http"
	"time"
)

// HTTPClient is a shared HTTP client with a reasonable timeout for platform use.
// 2026-09-10 code: transport 走显式 RootCAs（core.HardenTransport），
// macOS 平台校验器（SecTrust/CT 策略）间歇拒证书时不再整片打断出站调用。
var HTTPClient = &http.Client{
	Timeout:   30 * time.Second,
	Transport: HardenTransport(nil),
}
