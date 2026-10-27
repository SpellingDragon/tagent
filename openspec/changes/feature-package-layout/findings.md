
---

## 共享面收口记录（2026-10-27，HEAD ab28851 之后）

- **W1 三域单零写入**（迁移主干已把域内注释清干净——三单以扫描证据+锚复跑交差，不为交差造改动）：telemetry 12 PASS+消费锚 12 PASS；rl 7/16 PASS+三消费锚；evolution 76 PASS+变异 11/11。
- **共享面五件由编排者补**：①golden/runtime/architecture.json 五包登记与全部 allow/forbidden 边精确迁移（ARCH_IMPL RESULT ok）；②5 个旧 docs/api 残件删除（gen_godoc 曾报 generator-no-longer-writes）；③机械替换误伤三处还原（OPERATION:199/215 框架包 `trpc-agent-go/telemetry/*` 被误加 features 前缀；wiki/README:38-39 docs 内部相对链接被当包路径；OPERATION:243 `./evolution/governance` 漏网）+agent-architecture:142「独立顶级包」措辞；④内核两处注释旧径（memory/skeleton_assert.go:6、api/event/compress_request.go:13）+docs/api 重生成 GEN=0；⑤CONTRIBUTING features 规约句+cli「对外装配面」短语（D2 成文）。
- **D1 判据张力（W1-E 发现，裁定采纳）**：runtime/agent→features/evolution/governance（WithCallPass）实证「严格可移除」不成立——判据改「能力从属」；端口化另案候选。
- **存量悬空登记（非本变更引入，不扩面）**：behavior-matrix:52 refine.go、agent-architecture:164 asset_drift.go、behavior-matrix:9 gate.go、:22 checkIndex、agent-architecture 第三节 20+ 条 3369af7 前旧树——属既有文档债，留档待文档批。
- **保持不变的「路径状」身份**（可观测性/持久化契约，迁移不改名）：OTel meter 名 `tagent/telemetry`（×2）、事件类型前缀 `tagent/governance/v1/*`。
