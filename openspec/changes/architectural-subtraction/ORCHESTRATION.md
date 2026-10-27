# architectural-subtraction 编排文档

> 基线 HEAD `bd05430e`；启动门已满足（fidelity-and-recovery 归档 `3c2cf89`）。派发前重读本档二节；单完/波后回填四节台账。

## 一、结论（先行）

- **抵达 DoD 的唯一路径**：D1（批1 统一提交/事件编码/历史读取）→ D2（批2 责任：核验→快照单源→Observer 分层）→ D3（批3 删 Phase/删呈现决策/四锚核验）→ F1–F4，每批以「锚复跑+定点破坏+全套门禁」收口后才放行下一批。
- **当前最大风险**：批 1 的 1.3（历史路径统一）是唯一改写正式恢复语义的叶——HistoryFace 语义迁移若与 loop 的读写面错位，会在冷启动与协议锚同时爆红；对策=先写负锚 TestSingleHistoryPath（红）再动产品码，G7 锚全程钉住。

## 二、编排四问

### 2.1 次序 —— 谁先谁后

就绪队列（派发单元=批，D0 闭包实测根包簇合单）：

| 派发单 | 孙任务 | 前置 | 状态 | 触碰集（改写 ∪ 验证读取） |
|---|---|---|---|---|
| D1 | 1.1→1.2→1.3（单代理内串行） | 启动门✅ | ready | facts.go/tools.go/age_fold.go/memory/event_body.go/run.go/request.go + tests/integration/host、memory、runtime/agent 测试闭包 |
| D2 | 2.1（核验）→2.2→2.3 | D1 | blocked | settle_turn.go（读）/run.go/request.go/rl//api/plugin//telemetry/ + 同上测试闭包 |
| D3 | 3.1→3.2→3.3 | D2（3.2 前置=2.2） | blocked | api/plugin/present.go·phases.go·material.go/memory/replay//request.go + evolution 测试闭包 |
| Vn | 波后验证（独立子代理） | Dn 完成 | — | 只读全仓 + 原任务行验收面 |
| Gn | 门禁（后台 shell） | Dn 完成 | — | 只读 |

- 并发准入：批内叶全部触碰根包闭包（D0 表）→ 合单串行；**批与波后验证/门禁并行**（验证子代理在 Dn 冻结树上只读运行；后台门禁同树跑）。
- 关键路径：D1→D2→D3→F（全串行，无旁路）。
- 本轮做不了：F1–F4（须三批全毕）；2.1 并行化改造（显式出案，减法变更不加机制）。

### 2.2 授权 —— 谁准写什么

| 派发单 | 白名单（精确到文件） | 共享面 |
|---|---|---|
| D1 | facts.go、tools.go、age_fold.go、memory/event_body.go、run.go、request.go、tests/integration/host/*_test.go（新建/改）、memory/*_test.go（新建） | 根包五文件为本单独写；design D6 验收面冻结 |
| D2 | run.go、request.go、rl/*（capture 面）、api/plugin/*、telemetry/*、settle_turn.go（仅 2.3 注册面）、host_face.go（RequestSnapshots 读出面）、tests/integration/host/*_test.go、rl/*_test.go、api/plugin/*_test.go | api/plugin 为 2.3 独写；settle_turn.go 在 D2 内只许 2.3 触注册声明 |
| D3 | api/plugin/present.go·phases.go·material.go、memory/replay/*、request.go、examples/contract/api_plugin/*、memory/replay/*_test.go | request.go 为 3.2 独写（3.1 不触根包） |
| 编排者 | openspec/changes/architectural-subtraction/**（账目/findings/ORCHESTRATION）、git 提交、主规格 delta、LEDGER | 唯一提交权 |

- 偏离分级：**预授权自决**——测试放置包位微调（负锚具名与 `-run` 正则保持逐字符一致并回报）、注释措辞、Go 风格化；**必须上报**——验收面任何变动、触碰白名单外文件、发现主规格条款失真（D3.7）、需要改语义断言（D3.4 批准权）。
- 概念预算门（lint S1–S4）：删概念=债减（无需登记）；**新增**导出名/概念须登记 BASELINE+canonical example（先例 da2c8d5），本变更原则上不新增概念。

### 2.3 凭据 —— 什么才算完成

- 五查按派发单核（查1 勾选真实性/查2 产物盘点/查3 数字溯源/查4 遗漏检测/查5 生产消费者实测）。
- 每叶复跑命令=任务行验证面（正锚/负锚/消费者锚三要素，R29 冻结）；退出码 0 才勾，非空转证明（passed>0）。
- 必亲验数字：各锚 passed 数、`grep -c` 结构断言值、F2 逐门原始退出码、每批三问的 diff 行数。
- 批收尾门禁（用户裁定 F2 全套逐批）：lint + selftests + check-openspec + 三模块 short + short 十轮 + long 六片（后台）+ mutations --all + 症状 job —— 长跑项后台 shell 轮询（R32b），不阻塞编排回合。

### 2.4 释放 —— 锁与等待表

| 资源 | 占用者 | 释放条件 | 等待表（释放后消费） |
|---|---|---|---|
| 根包写权 | D1/D2/D3 依次 | 批收尾门禁全绿+验证子代理放行 | 下一批派发；F1 契约断言 |
| 后台门禁槽 | Gn | 逐门退出码落 evidence | 编排者核账→批放行 |
| git 提交权 | 编排者 | 批三问+验证报告齐 | 批提交（用户已授权全链推进） |

- 释放=触发：批放行动作里**当场派发**下一批（R31 连续推进至法定停点：DoD/提交归档/重大取向/blocked）。

## 三、明细层

- 派发单-孙任务矩阵：D1(1.1,1.2,1.3) D2(2.1,2.2,2.3) D3(3.1,3.2,3.3)；每单预算=单代理一个会话窗口（预估 D1 最重：1.3 语义迁移+3 新测试；若代理窗口不足，按叶边界拆续派，白名单不变）。
- 档位：无外部模型依赖（全部替身/本地测试）；真实 tmux/real-smoke 不在本变更面。
- 遗留欠账（R10–R14 核对）：无性能主张（纯减法，无基准任务）；无新配置档位；消费者枚举=各叶消费者锚已点名正式入口；真实路径=冷启动 env 复跑已入 1.2/1.3 验证面。
- 每批三问模板：①`git diff --stat <批前>..HEAD`（删了什么）②批报告（责任归谁：删除后的唯一责任载体）③锚复跑+定点破坏（验收仍成立）。

## 四、波次台账（边打边回填）

| 波次/单 | 五查结论 | 集成验证（命令+退出码） | 跨域裁决 | 账目勾选凭据 | 下轮情报 | 锁释放与队列消费 |
|---|---|---|---|---|---|---|
| （待填） | | | | | | |
