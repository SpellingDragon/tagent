# Tasks：host-protocol（协议已冻结——双端并发）

> 协议见 proposal.md（签名级）。两单文件零相交：H1 核心 host.go／H2 wechat 瘦身。

- [x] H1 核心侧：`host.go` 实现 Boot/Host/HostOption/MessageSender/DeliverFunc＋自动装配（config→Declaration→agents→models→tools→storage→meditation→注入面）＋测试 —— 验证：`go test . -count=1`＋`go test ./tests/integration/host -run "TestBoot" -count=1`＋负锚（缺配置拒/缺 sender 拒）
- [x] H2 应用侧：wechat.go 瘦身（删全部装配逻辑，改用 `tagent.Boot`＋`WithSender/WithApprover`）＋main.go ≤50 行＋tagent import ≤3 —— 验证：`cd examples/wechat-bot && go test ./... -count=1`＋`wc -l main.go wechat.go`＋`grep -c "SpellingDragon/tagent" wechat.go` ≤3＋golden match
- [x] F1 契约零改动（golden match）＋F2 全量门禁（lint ok/38包/13 wechat 测试）＋F3 DoD
- [ ] F4 archive（需明示）
