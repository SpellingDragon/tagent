# orchestration Specification（boundary-closure 一级编排契约）

## ADDED Requirements

### Requirement: 启动门——症状锚先红

一级变更 SHALL 在任何实现派发前，把六条症状锚（快）测试落在基线树上并确认全部红着（锚①-④在 assembly、⑥在 tool/exec、⑦在 memory/replay），并以症状锚⑤（慢）（既有 TestLocalSmoke）的评审两次实测红凭据为计划期凭据（自有红证于 W2 首日、W1 合并增量（01+04+05）已入 main、02/03 未入树时补齐，见一级 tasks 0.1）。任一锚在基线树变绿 SHALL 视为误诊，先回写一级 design D1 再继续。

#### Scenario: 六条症状锚（快）命令

- **WHEN** W0 执行后从仓库根分别运行 `go test ./assembly -run 'TestSymptomAnchor' -count=1 -v`、`go test ./tool/exec -run 'TestSymptomAnchor' -count=1 -v`、`go test ./memory/replay -run 'TestSymptomAnchor' -count=1 -v`
- **THEN** 三包 FAIL 行数分别为 4/1/1 且均 >0（非空转证明），无 PASS/SKIP 行冒充红证

#### Scenario: 症状锚⑤（慢）独立命令

- **WHEN** 执行一级 tasks 0.1 的症状锚⑤（慢）命令 `cd examples/wechat-bot && go test -tags smoke -run '^TestLocalSmoke$' -count=1 -timeout 2h -v ./...`
- **THEN** 计划期红凭据=评审两次实测 175/159 KiB/轮 > 128 KiB/轮；W2 首日自有红证同命令重跑，日志含 rounds=300 与斜率数字（非空转），输出存 run notes

### Requirement: 契约守护

本变更 SHALL 保持 `api/**` 零改动，唯一例外=`api/config` 新增两个可选字段（02 的 provider 窗口与输出余量声明；**预算门开 ⟺ 显式 provider 窗口声明 ∨ AgentSpec.MaxTokens>0（缺省推导）；MaxTokens=0 且未显式声明 = 预算门不启用（记 debug 事实，不裁剪不拒发）**；`api/config/decode.go`、`api/config/fingerprint.go` 的配套行同属该例外清单）；交付记录 schema 演进 SHALL 仅发生在 01 且向后兼容旧记录读取；`tagent.record.v1` 既有字段语义 SHALL 不变；**08 的声明类型常量驻 assembly、09 保留既有 `meditation` 节，二者对 `api/**` 零改动**。收尾 SHALL 以双断言：`git diff <基线>..HEAD -- api/` 输出仅含列明的 api/config 新增可选字段行（含 decode/fingerprint 配套行），且 `git status --porcelain -- api/` 为空。

#### Scenario: 收尾契约断言

- **WHEN** F1 执行 `git diff 48d8eb93..HEAD -- api/` 与 `git status --porcelain -- api/`
- **THEN** 前者输出仅含列明的 api/config 新增可选字段行（含 decode/fingerprint 配套行）、后者输出为空，均 exit 0

### Requirement: 变更级 DoD

一级变更完成 SHALL 同时满足：七条症状锚全绿、全量门禁与 race 门 exit 0、**08 证据位（声明事实/发布记录/失败回执三链测试＋watcher 三例＋重启对账/跨维窗口）与 09 证据位（自主整理、自察四测试、激活因果、自管产出不计入状态积累）各有 PASS 行**、06 的对照 run（含任务成功率/送达率/延迟/token 成本/人工介入次数五指标，其中人工介入次数=审批入口调用数+blocked 上报数）落盘可回读、07 的样本三要素（请求回读、独立验收、验收结论入 rl annex）齐备率 100%（**分母=全部样本，含 incomplete**；reward 向量派生为显式欠账，不计入 DoD）、F1-F4 全部勾选且各附凭据。

#### Scenario: DoD 核验

- **WHEN** F3 在回滚树与修复树各跑一轮七锚命令（全带 `-v`）
- **THEN** 回滚树全红（六条症状锚（快）FAIL 行数 4/1/1、症状锚（慢）红）、修复树全绿（六条症状锚（快）PASS 行数 4/1/1、症状锚（慢）绿）

#### Scenario: 08/09 证据位核验

- **WHEN** F3 收集 08/09 域的 DoD 证据
- **THEN** 08 的声明事实/发布记录/失败回执三链测试与 watcher 三例、09 的自察四测试/激活因果/自管产出不计入状态积累各有 PASS 行，两树各包 PASS/FAIL 行数均 >0 且两份输出均存入 run notes

## RENAMED Requirements

- FROM: `### Requirement: 波次解锁以入 next 为准`
- TO: `### Requirement: 波次解锁以入 main 为准`

## MODIFIED Requirements

### Requirement: 波次解锁以入 main 为准

波次 SHALL 仅作汇报分组；一个二级域的解锁条件 SHALL 是其全部前置域已合入集成树（main）并通过全量门核验，MUST NOT 以"前置域本地绿"替代，亦不等同于同波其他域完成。编排者 SHALL 在每次派发前重读 ORCHESTRATION.md 第二节并核对四问与实况一致。

#### Scenario: 前置先行解锁

- **WHEN** W1 中 01 已入 main 并过全量门，而 04 仍在实施
- **THEN** 02 的前置（W1 全入 main——01 的 loop.go 提交面与 05 的投影面已稳定，02 依赖其稳定）尚未满足，02 不可先派；若 04/05 提前完成则前置提前满足

#### Scenario: 前置仅本地绿不解锁

- **WHEN** 01 域在派发分支上全绿但未合入集成树（main）
- **THEN** 02 与 03 域的实施派发 SHALL 不开始

### Requirement: 波次解锁以依赖入 main 为准

波次 SHALL 仅作汇报分组；子域 SHALL 按 design D2 外部依赖解锁，同文件簇（host_tools/host_document、wechat*.go）跨域改动 MUST 合单或串行；解锁的集成线 SHALL 为 **main**（与「波次解锁以入 main 为准」同义），MUST NOT 以“前置域本地绿”替代，且 MUST NOT 等待整波。

#### Scenario: 同文件簇不合单即冲突

- **WHEN** 两单触碰集相交且未合单
- **THEN** 编排者 MUST 拦截派发（触碰集=改写文件 ∪ 构建闭包 ∪ 验证读取包）

#### Scenario: 前置仅本地绿不解锁

- **WHEN** 01 域在派发分支上全绿但未合入集成树（main）
- **THEN** 02 与 03 域的实施派发 SHALL 不开始

### Requirement: 共享面由冻结方单写

`api/event` 的类型与 meta 键 SHALL 由 01 域单写并在其入 main 后冻结；此后任何
二级域对冻结文件的改动 MUST NOT 入链，除非编排者以新派发单显式解冻。

#### Scenario: 冻结后的越界改动被拒

- **WHEN** 01 域已入 main 且某 W2 派发单白名单不含 `api/event`
- **THEN** 该域对 `api/event` 的任何改动 SHALL 不入链，违规单 SHALL 重开

## REMOVED Requirements

### Requirement: 启动门

**Reason**: 语义并入本变更新增的「启动门——症状锚先红」；旧条款绑定历史变更的批 1 前置，与本变更无关，显式退役避免同主题重复。

### Requirement: 契约守护三层闭环

**Reason**: 语义并入本变更新增的「契约守护」；旧条款的 settle/会话占位红线属历史变更，显式退役避免同主题重复。

### Requirement: 变更级 DoD 以两证与全量门为准

**Reason**: 语义并入本变更新增的「变更级 DoD」；旧条款的“两条症状锚”口径由七症状锚 + F3 双向证据取代，显式退役避免同主题重复。
