## ADDED Requirements

### Requirement: 预算归位 rename 与注册表同原子单

配置面预算字段 rename（`max_tokens`→`replay_window`、`provider_context_window`→`context_window`、收回 `max_tokens` 作真实输出限）SHALL 与 `golden/runtime/window_bounds.json` 重分类在**同一原子单**落地。`check_window_bounds.py` 强制「恰一个 window 类」，漏改注册表或门类即红。W1 MUST 主线程串行原子，每子步 `go build ./...` 保持可编译，MUST NOT 派并发 agent 分摊 rename（触碰集自相交）。

#### Scenario: 漏改注册表即红

- **WHEN** rename 落地而 `window_bounds.json` 未同步重分类
- **THEN** `check_window_bounds.py` SHALL exit 非零（`windows!=1` 或新键未注册），W1 不得勾选

#### Scenario: 同簇并发被拒

- **WHEN** 两单写集同时含 `api/config/decode.go` 或 `golden/runtime/window_bounds.json`
- **THEN** 两单 SHALL NOT 并发；编排者合单或串行，台账记录理由

### Requirement: 契约面恰为设计所列（F1 非零 diff）

本变更 SHALL **有意**修改契约（`request-budget-accounting` spec、`window_bounds.json`、`docs/GLOSSARY.md` §一）。F1 MUST NOT 断言契约零 diff，而 SHALL 断言契约改动**恰为 design §四/§六所列**，无计划外漂移。冻结归档（`openspec/changes/archive/**`）与 §五冻结拼写（reincarnation 族）SHALL 零 diff（历史审计只附勘误不改写）。

#### Scenario: 计划外契约漂移被拒

- **WHEN** F1 核对 `git diff <基线>..HEAD -- openspec/specs golden docs/GLOSSARY.md`
- **THEN** 每处改动 SHALL 对照 design 列明；出现 design 未列的契约改动 SHALL 判 F1 红

#### Scenario: 冻结面零 diff

- **WHEN** F1 核对 `archive/**` 与 §五冻结拼写
- **THEN** SHALL 零 diff；历史编排条款引用的旧门语义（甲）只附勘误注，不改写历史 F1 断言本身

### Requirement: 预算门唯一来源（乙）的编排守护

预算门开 SHALL ⟺ `context_window>0`（唯一来源）；`replay_window` 只驱动折叠/投影，MUST NOT 兼任预算窗缺省。本变更 SHALL 删除 derived-window 耦合（`budgetWindow` 去 `|| MaxTokens`）。远端 `tagent.yaml` MUST 显式声明 `context_window`，否则长会话失去预算保护（D-04 升级为必做）。

#### Scenario: 只声明 replay_window 门闭

- **WHEN** agent 只声明 `replay_window` 无 `context_window`
- **THEN** 预算门 SHALL 闭、整包发送（S4 锚）；MUST NOT 因 `replay_window>0` 派生门开

#### Scenario: 远端漏写 context_window 被拒

- **WHEN** 远端 yaml 未显式声明 `context_window`
- **THEN** 部署验收 SHALL 判预算保护缺失，D-04 不得勾选

### Requirement: c75 症状直译锚启动门

W0 SHALL 以 S1（c75 隔离复现：`replay_window=128000, context_window=128000`、无输出限、输入 96407 → 放行）与 S4（只 `replay_window` → 门闭整包）的**行为红锚**作为启动门。红因 MUST 命中凭空 `reserve=32000` 拒发（S1）与 derived 门开拒发（S4），MUST NOT 是编译失败。S1 红转绿双证据（完整回滚必红、修复必绿）SHALL 入 F3 run notes。S2（输出限真发）依赖 1.2 新增字段，其红→绿在 1.2 同单内完成，W0 先以现状锚钉 `served.MaxTokens==nil`。

#### Scenario: 未出红证即改行为被拒

- **WHEN** S1/S4 尚未在基线树产出行为红证
- **THEN** W1 行为修改 SHALL NOT 进入实施；交付物缺红证视为启动门未过

#### Scenario: 回滚即红

- **WHEN** F3 完整回滚本变更复跑 S1
- **THEN** S1 SHALL 红（reserve=32000 → budget=96000 → 96407 拒发）；修复后 SHALL 绿（reserve=0 → budget=128000 → 放行），两次证据入 run notes