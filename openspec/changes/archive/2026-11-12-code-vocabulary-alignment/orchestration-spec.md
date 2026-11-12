# orchestration Specification

## Purpose

约束 code-vocabulary-alignment 的编排行为：启动门、波次解锁、共享面单写守护、spec 最小化（不新建重复能力）、症状锚优先与独立放行，使三域并发不互踩、完成以复跑凭据为准、词汇门红转绿不被工程目标替换、规范词汇不增殖。

## ADDED Requirements

### Requirement: 启动门与波次解锁

后置轨 SHALL 仅在其前置依赖就绪后派发；波次 SHALL 仅作汇报分组，不得作解锁栅栏。C（门重写纯短语禁＋删基线）SHALL 立案即跑；A/B1/B2（carrier 全退役）SHALL 待 C 完成且编排者捕获红锚（当前树 390 命中）后并发；D（golden/wiki/GLOSSARY 漂移）触碰集与 A/B 不相交 SHALL 可并发；E（godoc 重生成）SHALL 待 A+B 全部 .go 注释定稿；F SHALL 待 E。03 structural-golden 已完成，甲不改。

#### Scenario: A/B 等待门红锚

- WHEN C 门重写未完成或红锚未捕获
- THEN A/B1/B2 不派发（无红锚则无以验转绿）；C 照常跑

#### Scenario: A/B1/B2 并发准入

- WHEN A 簇（生产 .go）与 B1/B2 簇（测试 .go）文件级不相交，且级联符号均包内私有（生产私有标识符仅本生产文件引用，测试自有 carrier 局部/字段独立）
- THEN 三轨同波派发；各轨独立可编译，权威 go test 由编排者于全落后统一跑（避免并发 transient 误判）

### Requirement: 共享面单写守护

跨轨共享面 SHALL 由编排者单写或串行：godoc 重生成 SHALL 由编排者在 A+B 全部 .go 改定后统一跑，MUST NOT 并发重生成；lint.sh SHALL 归 C 单写；golden 概念散文（architecture.json/durable_truth.json）归 D、docs/api 派生面归 E，不互踩。A（生产）与 B（测试）同包 SHALL 各自保持独立可编译（私有标识符定义与全部引用同轨改齐），权威 go test 由编排者于全落后跑。收尾 SHALL 以 F1 diff 断言契约零未授权改动。

#### Scenario: godoc 并发互踩被阻

- WHEN A 与 B 各自跑 gen_godoc.sh
- THEN 编排者收口为单写：全部 .go 改定后统一重生成一次，docs/api diff 仅词汇改名

#### Scenario: 同包生产/测试并发不互踩

- WHEN A 改 runtime/task/lifecycle.go 私有 carrier 字段、B2 改 runtime/task/*_test.go 自有 carrier 局部
- THEN 二者符号不相交（测试不引用生产私有字段），各轨独立可编译；编排者于 A+B 全落后跑权威 go test 复核

### Requirement: spec 最小化不新建重复能力

本变更 SHALL 只产生一处能力 delta（architecture-guardrails MODIFIED，02 持有）；MUST NOT 新建 code-vocabulary 等重复能力——规范词汇已各有其主（session-checkpoint-material／deterministic-compress-level／fold-provenance／restart-recovery-vocabulary／fact-vocabulary）。01/03 SHALL 为纯实现子域，满足既有 requirement，无 spec 文本。甲把 carrier 统一到既有精确实名（commit/DurableCommit/credential/URL/projection/envelope/fact/record），SHALL NOT 造新概念。

#### Scenario: 新建重复能力被拒

- WHEN 某域试图新建 code-vocabulary 能力承载词汇不变量
- THEN 该提案被拒，词汇不变量归 architecture-guardrails 既有命名守护轴，机制词汇归各既有机制 spec

### Requirement: 症状锚优先与独立放行

验收矩阵第一行 SHALL 为词汇门症状锚（当前树 390 红、清理后 0 绿）；工程目标 SHALL 只能追加；负例自测 SHALL 证明门真拒植入违规（非空转）；放行 SHALL 经独立只读审者末轮显式 frontier 空，且证据取自提交树。

#### Scenario: 工程绿而锚红

- WHEN 某轨 Go 测试全绿但词汇门对当前树仍红（carrier 全义/folding generation 未清）
- THEN 以锚为准判该轨未完成，不得归档

#### Scenario: 红转绿因果与负例真拒

- WHEN 门对清理前树跑、A/B/D/E 清理后跑、仅回退清理后跑，或负例自测植入违规
- THEN 清理前红（390，症状现）、清理后绿（0）、回退清理复红（因果）；负例自测植入违规必被拒（exit 1），只打印不拒判门空转；门是本变更新建，完整回滚会移除门本身，故因果证据取「回退清理」而非「回滚全变更」，诸证据入 F3 run notes

#### Scenario: 独立审者放行

- WHEN 修复完成请求放行
- THEN 独立只读审者分轮拷问至末轮显式 frontier 空＋扫描范围＋逐项证据，修复自证不得放行