# Tasks

## 1. fail-before

- [x] 1.1（红证据：`loopback_without_auth_admitted` 期望 202 实得 400；另两针首跑即绿=不变量钉） 新增两测并确认红形：① 无 token + `RemoteAddr=127.0.0.1:5555` 带 `trigger_source:"user"` 期望 202 且事件带声明血统（现码 400 红）；② 无 token + 非回环源同请求期望 400（现码绿，作不变量钉） —— 验证：`go test ./rl/ -run 'DeclarationTrust' -count=1` 首跑 ① 红证据入档

## 2. 实施

- [x] 2.1 `rl/http_api.go` 增 `requestFromLoopback(r)`（`net.SplitHostPort`+`net.ParseIP().IsLoopback()`，解析失败 false）；声明校验条件改为 `h.authToken == "" && !requestFromLoopback(r)`；无 token 而经回环获豁免时 `log.Infof` 记一行可归因 —— 验证：`go build ./... && go vet ./rl/` exit 0
- [x] 2.2 `examples/wechat-bot/mail-poller/mail_poller.py`：声明条件 `tok` → `tok or URL host ∈ {127.0.0.1, localhost, ::1}`，注释同步（跨机无凭仍不声明，防 400 与重试互锁） —— 验证：`python3 -m py_compile` exit 0
- [x] 2.3 测例转绿 + 值域不因源放宽一针（回环源 + 非法值 → 仍 400） —— 验证：`go test ./rl/ -count=1` exit 0

## 3. 收口

- [ ] 3.1 全量：根包 `-short`、`-race` 根+rl、`bash scripts/lint.sh`、`openspec validate --strict`；push 并确认 CI 四 job 绿 —— 验证：各 exit 0
- [ ] 3.2 回件远端：根因图、两条部署路径（配 token 或默认回环零配置）、其凭据强度提醒（注释级探针≠值变更消费，附改值探针法）、回执噪声预期归零 —— 验证：sent 目录反查 msg_id
- [x] 3.3 `openspec archive`（delta 已并入主 specs：inbound-intent-declaration 受理需求修订 ~1，`openspec validate --specs --strict` 106/106，lint ok）（delta 并入主 specs） —— 验证：`openspec list --json` 无该 change
