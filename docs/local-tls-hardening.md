# 本地补丁：TLS 与 macOS 系统信任层（Apple SecTrust）脱钩

本文件描述**本机专有补丁**（非上游），工单 `t_a31f543c`，2026-09-10 由 code（编程同事）加入。
上游同步（merge upstream）时若本节涉及的代码被改写，须按下面的「补丁内容」重新落一遍。

## 1. 事故与根因

2026-09-10 17:00 起，macOS 的 CT（Certificate Transparency）策略开始拒绝腾讯 ilink 边缘证书
与 `*.feishu.cn` 证书，cc-connect（微信中枢）18:07 起**发不出、收不进**，日志：

```
weixin: sendMessage: Post "https://ilinkai.weixin.qq.com/ilink/bot/sendmessage":
  tls: failed to verify certificate: x509: “weixin.qq.com” certificate is not standards compliant
```

`openssl` / `curl` 对同一证书校验通过（`Verify return code: 0 (ok)`），`curl` 直连 API 返回 200
——证书与接口本身没坏。

根因：Go 在 darwin 上默认走**平台校验器**（`crypto/x509` 的 `systemVerify` → `SecTrustEvaluateWithError`），
Apple 的 CT/吊销策略由它说了算。实测确认两条常见绕法在 darwin 上**都无效**：

| 手段 | 结果 |
| --- | --- |
| `SSL_CERT_FILE=/etc/ssl/cert.pem` | 无效（darwin 走平台校验器，不读该变量） |
| `GODEBUG=x509usefallbackroots=1` | 无效（未调用 `SetFallbackRoots` 即无效果） |

## 2. 补丁内容（显式 RootCAs = 纯 Go 校验）

`crypto/x509/verify.go` 的选择逻辑：`opts.Roots` 为 nil 或 `systemPool` 时才调平台校验器；
**只要我们从 PEM 装一个普通 `CertPool` 塞进 `RootCAs`，Go 就改用纯 Go 链路校验**（不做 CT/吊销检查）。

- 新增 `core/tlsroot.go`
  - `TLSRootFile()` — 读 `CC_TLS_CA_FILE`；未设时在 darwin 上默认 `/etc/ssl/cert.pem`；
    值为 `off/0/false/disable/none` → 关闭加固（回到平台校验器）。
  - `ExtraRootCAs()` — 按路径缓存地加载根证书池；读不到/解析不出 → 记 WARN 并返回 nil（**fail-open**，绝不因加固失败而更坏）。
  - `HardenTLSConfig` / `HardenTransport` — 补 `RootCAs`（已有则不动）；`nil` transport 会克隆 `http.DefaultTransport`。
- `core/httpclient.go` — 共享 `HTTPClient` 走加固 transport。
- `platform/weixin/` 三条出站 TLS 链路全部加固：
  1. `client.go: newAPIClient`（发/收消息的普通请求）
  2. `client.go: longPollClient`（入站长轮询，clone `http.DefaultTransport` 后加固）
  3. `weixin.go: cdnUTLSConfig`（微信 CDN 上传/下载，uTLS Chrome 指纹 + 显式 RootCAs）

代价（明确取舍）：失去 Apple 的 CT 与吊销策略校验；信任集合仍是系统根（`/etc/ssl/cert.pem`，128 张）。

## 3. 构建

与上一次本机构建保持同一组参数（`no_web`、CGO 开、版本串带本地后缀）：

```bash
cd ~/code/cc-connect
go build -tags 'no_web' \
  -ldflags "-s -w -X main.version=v1.5.1-beta.1-utls-h2+tlsroot \
            -X main.commit=$(git rev-parse --short HEAD) \
            -X main.buildTime=$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
  -o dist/cc-connect.tlsroot-20260910 ./cmd/cc-connect
```

产物：`~/code/cc-connect/dist/cc-connect.tlsroot-20260910`（**已构建、未部署**）。

## 4. 部署（需高律师择时；重启微信中枢属其定夺）

```bash
# ① 留档旧二进制
cp ~/skills/cc-connect/bin/cc-connect ~/skills/cc-connect/bin/cc-connect.bak-<日期>-pre-tlsroot
# ② 换二进制
cp ~/code/cc-connect/dist/cc-connect.tlsroot-20260910 ~/skills/cc-connect/bin/cc-connect
# ③ 重启（launchd 托管，KeepAlive；微信桥瞬断约 10-20 秒）
launchctl kickstart -k gui/$(id -u)/com.cc-connect.service
# ④ 验收：启动日志应出现加固生效行 + 通道恢复行
grep -E "explicit RootCAs installed|ilink ready-for-poll" ~/.cc-connect/logs/cc-connect.log | tail -3
```

## 5. 回滚（任一步失败都回到原状）

```bash
cp ~/skills/cc-connect/bin/cc-connect.bak-<日期>-pre-tlsroot ~/skills/cc-connect/bin/cc-connect
launchctl kickstart -k gui/$(id -u)/com.cc-connect.service
```

紧急软化（不换二进制）：写 `CC_TLS_CA_FILE=off` 到 launchd plist 的 `EnvironmentVariables`
（`~/Library/LaunchAgents/com.cc-connect.service.plist`）并 kickstart，即回到平台校验器行为。
注意：回滚后仍需 keychain 叶证书例外兜着（例外按指纹绑定，上游轮换即失效）。

## 6. 验证（本轮实测结果）

- `go test -tags no_web ./core/ ./platform/weixin/` 全绿（含 7 例新增回归）。
- `core`：`TestPlatformVerifierRejectsCertWithoutSCT`（darwin 对照组，证明前提成立）、
  `TestHardenTransportBypassesPlatformVerifier`（显式 RootCAs 验过同一张被平台拒的证书）、
  `TestHardenTransportKeepsExistingRootCAs`、`TestExtraRootCAsKillSwitch`、`TestExtraRootCAsMissingFileFailsOpen`。
- `TLS_LIVE_TEST=1 go test ./core/ -run TestHardenedTransportLiveUpstreams`：
  真机打 `ilinkai.weixin.qq.com:443`（HTTP 404）与 `open.feishu.cn:443`（HTTP 200）**均通过校验**。
- `platform/weixin`：`TestWeixinOutboundTLSHardening` 断言三条链路都带显式 RootCAs。

## 7. 配套看护

本机 `~/skills/bot/scripts/tls-trust-watch.py`（cron `tlstrust1`，每 30 分钟）负责
「证书轮换 / 信任层再拒」的检出与提醒；`~/skills/bot/bin/tls-trust-probe` 是它用的 Go 双模探针。
运维口径见 `~/skills/bot/SKILL.md`「通道故障排查」。
