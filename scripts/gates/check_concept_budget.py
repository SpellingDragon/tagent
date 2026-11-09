#!/usr/bin/env python3
"""check_concept_budget.py — 概念预算静态上限（S1–S4），不是产品验证。

判定对象与口径（与 openspec/changes/runtime-rebuild/concept-slim-plan.md §一逐条对应）：

* S1 iface-methods：多态接口 ≤2 方法；消费者侧窄口 ≤3 方法。「多态」由静态实现者
  计数判定——某接口的方法名集合（同包嵌入接口展开后）被 ≥2 个一方接收者类型完整
  覆盖即视为多态端口，否则视为消费者侧窄口。
* S2 struct-fields：导出结构 ≤5 个导出字段。只数结构体顶层的导出字段名：嵌入类型
  不算、未导出字段不算、多行函数类型算一个字段。
* S3 ctx-bare：**调用点**裸 `ctx.Value(` = 0；跨缝关系入签名或类型化 accessor（accessor 实现体的单次 ctx.Value 是合法落点，按文件具名登记）。按文件计数，
  baseline 中点名的类型化 accessor 实现是唯一合法落点（仍然 warn，不静默）。
* S4 contract-surface：每个导出接口都要有 canonical 范例文件
  `examples/contract/<pkg_slug>/<Iface>_example.go`（含 `func Example<Iface>()`）
  与 Godoc 红线清单（`// MUST ...` / `// MUST NOT ...` 合计 ≤6 条，且 doc 块出现
  「红线」标记）。pkg_slug 是包目录把 `/` 换成 `_`（接口重名跨包存在，故按包分目录；
  组合根亦按目录名 assembly，无特殊拼写）。

棘轮（只降不升）：现状大面积超标，故 BASELINE 是文件内显式债务清单，每行
  ("<rule>", "<file>:<Symbol>", <observed>)，observed 为登记时的实测值。
  * 命中豁免 → warn（禁静默放行），计入 counts.baseline_exempted；
  * 实测 > observed → error（棘轮：只降不升）；
  * 当前不再超标 / 符号消失 → warn「stale，删除该行」。
  S4 的豁免是布尔（observed=None）：范例与红线都补齐后该行即 stale。

规则可单独运行（--rule S1|S2|S3|S4/D1，或规则名），下游收敛叶按规则对账。

D1 domain-surface（与 S1–S4 分门的独立结果，不进形状 BASELINE 棘轮、不可被其豁免）：
  只回答「相对基线新增/改名的公共命名面是否经过裁定」：
  * 基线：golden/runtime/architecture.json 的 concept_bindings.baseline（commit=git 提交，
    或 tree=本地树快照供夹具用）。基线/目标缺失或解析失败一律 fail-closed。
  * 新增＝当前登记包里的导出命名类型（struct/interface/别名/定义类型）其名字不在基线
    任何登记包中；同名搬迁不算新增（净额 0）；纯改名用 renamed_from 对账且净额 0。
  * 每个新增名必须在 concept_bindings.bindings 绑定 canonical concept＋kind
    （fact/read/declaration/port，没有 mechanism 捷径）＋非 superseded 主 spec 锚；
    无绑定即具名拒绝——实现机制不能借绑定成为领域词。
  * 规范单主：标 superseded 的主 spec 在 ## Requirements 里仍含 SHALL/MUST 即红；
    注释/历史备注不是迁移。
  * 方法、普通函数、测试支架不逐个登记；未导出类型不管。

局限（如实登记，不冒充 type-check）：
  * 纯正则＋括号匹配的静态解析，不做 Go 类型检查；方法集按名字匹配，跨包同名类型
    可能虚增实现者数——偏差方向只会让接口更易判为「多态」而更严格，不会放宽；
  * 只扫 golden/runtime/architecture.json 登记的一方包（未登记的新包由
    check_runtime_architecture.py 拦），跳过 *_test.go（测试不是契约面）；
  * 不判语义：字段是否真该拆、红线是否切题仍需评审。

productVerified 恒为 null：静态上限通过不等于产品成立。

用法：
  python3 scripts/gates/check_concept_budget.py [--rule S1] [--json]
  python3 scripts/gates/check_concept_budget.py --self-check
退出码：0=通过（含豁免 warn）；1=有 error 或输入不可用。
"""
from __future__ import annotations

import argparse
import io
import json
import os
import re
import subprocess
import sys
import tarfile
import tempfile

import runtime_checklib as lib

# ---- 标尺常数（与 concept-slim-plan.md §一 一致；改这里等于改验收面）----
POLYMORPHIC_METHOD_LIMIT = 2
CONSUMER_METHOD_LIMIT = 3
STRUCT_FIELD_LIMIT = 5
CTX_VALUE_LIMIT = 0
REDLINE_LIMIT = 6
# 实现者数达到此值即视为「多态接口」（S1 用严口径）。
POLYMORPHIC_MIN_IMPLEMENTERS = 2
EXAMPLE_ROOT = "examples/contract"
REDLINE_MARKERS = ("红线", "redline", "REDLINE")

RULE_IFACE = "iface-methods"
RULE_STRUCT = "struct-fields"
RULE_CTX = "ctx-bare"
RULE_CONTRACT = "contract-surface"
# D1 与 S1–S4 分门：形状规则共用 BASELINE 棘轮；domain-surface 是独立结果，
# 不进 apply_baseline（无豁免路径），默认随全量运行。
RULE_DOMAIN = "domain-surface"
SHAPE_RULES = (RULE_IFACE, RULE_STRUCT, RULE_CTX, RULE_CONTRACT)
RULES = SHAPE_RULES + (RULE_DOMAIN,)
RULE_LABELS = {
    RULE_IFACE: "S1",
    RULE_STRUCT: "S2",
    RULE_CTX: "S3",
    RULE_CONTRACT: "S4",
    RULE_DOMAIN: "D1",
}
RULE_ALIASES = {label: rule for rule, label in RULE_LABELS.items()}
NUMERIC_RULES = (RULE_IFACE, RULE_STRUCT, RULE_CTX)

# ---- D1 domain-surface 常量 ----
# 绑定类别只有四种；机制/测试支架没有绑定捷径（Writers/Movement/Continuations 应内化）。
DOMAIN_KINDS = ("fact", "read", "declaration", "port")
CONCEPT_BINDINGS_KEY = "concept_bindings"
# 导出命名类型声明（struct/interface/别名/定义类型一并纳入；函数内声明因缩进不匹配）。
TYPE_DECL_RE = re.compile(r"^type\s+(?P<name>[A-Za-z_]\w*)\s*(?P<rest>\S)", re.M)
# superseded 标记：blockquote 备注或标题行里出现 superseded（与主 spec 现行约定一致）。
SUPERSEDED_MARK_RE = re.compile(r"(?i)^\s*(>|#{1,6}\s).*\bsuperseded\b")
NORMATIVE_RE = re.compile(r"\b(SHALL|MUST)\b")
SPECS_SUBPATH = ("openspec", "specs")

# ---- 债务清单（baseline）：现状超标件，只降不升；每清一户删一行 ----
# 生成口径：本检查器对当前树的实测值。S1 的 observed 是接口方法数，S2 是导出字段数，
# S3 是该文件的裸 ctx.Value 调用数；S4 是布尔债务（observed=None）。
#
# 键一律是**当前树路径**：包迁移（query/→memory/query/、agent/→runtime/agent/、
# state/→runtime/state/、task/→runtime/task/、event/→api/event/、config/→api/config/、
# governance/→evolution/governance/、host_bridge.go→cli/host_face.go、diagnostics.go→host_face.go、
# host_face.go→host_diagnostics.go、host.go 投递段→host_delivery.go、根包整体→assembly/）只做键正名，
# observed 不动——路径对齐不是豁免扩张。已清零的条目按棘轮生命周期删除，不得回填。
BASELINE = (
    ("iface-methods", "features/rl/capture.go:Capture", 3),
    ("iface-methods", "features/rl/capture_wiring.go:CaptureScope", 4),
    ("iface-methods", "runtime/agent/loop.go:Capture", 3),
    # memory/ledger AtomicStore 4m（2026-11-05 03 资源有界；hot-update-and-purity-closure
    # 09 改写后由 kvPort 3m 正名为原子存储引擎 4m）：同一 KV 面上的三种读动作（单键/前缀
    # 枚举、前缀键序、命名空间清单）与一次带屏障提交，拆成两个接口会让前缀枚举或命名
    # 空间清单绕开内存脏合并另立一条读路，与本域“不立第二读路”的裁决冲突。
    ("iface-methods", "memory/ledger/atomic_store.go:AtomicStore", 4),
    # features/evolution/governance GovernanceEventLog 3m（2026-11-05 03 收口）：治理账的
    # 持久面＝写入、前缀回放与同步屏障三种动作，同一本账；拆窄口会让前缀回放绕开
    # Sync 屏障另立一条读路，与本域“一条链一本账”的裁决冲突。
    ("iface-methods", "features/evolution/governance/denial.go:GovernanceEventLog", 3),
    ("struct-fields", "runtime/agent/delegate.go:Delegation", 9),
    ("struct-fields", "runtime/task/spec.go:Spec", 9),
    ("struct-fields", "tool/subagent/builder.go:Builder", 8),
    ("iface-methods", "tool/recall/table.go:Table", 3),
    ("struct-fields", "memory/replay/age_segments.go:Segment", 6),
    ("struct-fields", "memory/replay/age_segments.go:AgeFoldReport", 7),
    ("struct-fields", "runtime/task/registry.go:Obligations", 6),
    ("struct-fields", "tool/subagent/target.go:TargetOptions", 8),
    # S1 iface-methods — 8 项（observed=实测接口方法数）
    ("iface-methods", "runtime/agent/delegate.go:DelegationTasks", 4),
    ("iface-methods", "runtime/state/durable.go:FactLog", 4),
    ("iface-methods", "runtime/state/store.go:Store", 4),
    # hot-update-and-purity-closure 12 引入叶：Stateful 是一份可恢复状态切片在接缝上的
    # 三问（键名/说出状态/放回状态），5 个实现者（投影、送达账、邮箱、任务账、输入轮）。
    # 拆窄口会让恢复路径绕过快照面另立一条写回通道（说出状态者与放回状态者分离），
    # 与「一份切片一个持有者」的接缝裁决冲突；收窄留作后续，ratchet 警示保持可见。
    ("iface-methods", "runtime/state/checkpoint.go:Stateful", 3),
    ("iface-methods", "runtime/task/registry.go:Manager", 4),
    ("iface-methods", "runtime/task/registry.go:Registry", 4),
    ("iface-methods", "tool/exec/stop.go:Executor", 4),
    ("iface-methods", "tool/mcp/adapter.go:Registry", 5),
    # SettleWake 5m（2026-10-27: 邮箱账面本体＝唤醒账户，五格分别是欠账回折、未消费盘面、
    # 认领、退回与消费回执；账面是一个账户的五种动作，拆窄口会把“同一账”切碎；
    # 窄口收敛留作后续，ratchet 警示保持可见）
    ("iface-methods", "assembly/settle_turn.go:SettleWake", 5),
    # S2 struct-fields — 43 项（observed=实测导出字段数）
    ("struct-fields", "tool/subagent/subagent.go:Definition", 10),
    # Declaration 6f（声明面本体）：组织声明=入口/代理/引用/基线/MCP 服务器代际/原始文档，MCP 面随 mcp_servers 声明下沉各成一格，拆开会让声明面出现影子结构。
    ("struct-fields", "api/config/config.go:Declaration", 6),
    # AgentSpec 26f（声明面本体）：单个 agent 的声明格＝核心（身份/模型/引用/参数/回合预算/回放窗口）＋熔断两镜（max_calls_per_response/long_context_tokens，单响应调用量与长上下文阈值，人审挂起式）＋捕获预算（capture_budget_mb，轨迹常驻上限）＋请求预算两字段（provider_context_window/output_reserve，provider 窗口与输出余量的可选声明；导出是 YAML 解码前提，MUST NOT 退回未导出访问）＋ memory/meditation/file/exec 四个域段平铺在同一格；窗口三字段（max_tokens/compress_threshold/keep_recent_tasks）替代 replay_budget；三个熔断/预算键与 max_tool_iterations 同模式，是声明字段不是影子结构（oom-bounded-capture-and-storm-fuse）。
    ("struct-fields", "api/config/config.go:AgentSpec", 26),
    # MeditationSpec 6f：冥想运行声明本体，六个格分别是启用、节奏两格、提示、观察面与回流白名单，字段即声明形状。
    ("struct-fields", "api/config/config.go:MeditationSpec", 6),
    # event.ParsedMeta 9f（00.25 具名登记）：ParseEventMeta 的解析结果本体＝数据面，
    # 九个冻结键各自成一格（另有 PassThrough/Unknown 原样留存）——字段即被解析元数据的
    # 原样形状，瘦身会丢掉“不丢条目”的保真承诺，故不瘦身。typed-meta 收口新增
    # kind/task_id 两个冻结键：通知类型与任务身份进机械字段，呈现层不再拼文本。
    ("struct-fields", "api/event/meta.go:ParsedMeta", 11),
    # runtime/task.Notice 6f：通知事实本体＝数据面，身份四格（TaskID/Round/Kind/Seq）
    # 加事件引用与结果正文；正文是消费方读取的结算材料，FactKey 仍只认身份四格。
    ("struct-fields", "runtime/task/registry.go:Notice", 6),
    # event.Event 16f（S2 类别豁免先例，具名）：事件事实本体＝数据面，16 个字段是事件
    # 身份/内容/归属/账目四块的数据语义（序列化保真面），不拆构造、不拆块；可读性由
    # api/event/types.go 的语义块文档分组承担。
    ("struct-fields", "api/event/types.go:Event", 16),
    ("struct-fields", "features/evolution/posterior.go:Evaluation", 6),
    ("struct-fields", "cli/host_face.go:ApprovalDecision", 8),
    ("struct-fields", "memory/event_body.go:EventBody", 20),  # 2026-10-26: full-fact fidelity (fidelity-and-recovery 2.2) names every field a restore needs; ratchet kept, count follows the declared design
    ("struct-fields", "memory/replay/projection.go:ChainFact", 7),
    ("struct-fields", "memory/replay/projection.go:RebuildReport", 9),
    ("struct-fields", "memory/query_events.go:EventRecord", 7),
    ("struct-fields", "memory/query_events.go:QueryOptions", 9),
    ("struct-fields", "memory/replay.go:DeliveryRecord", 8),
    ("struct-fields", "memory/search/port.go:Query", 9),
    ("struct-fields", "memory/ledger/segment_store.go:SegmentRecord", 6),
    ("struct-fields", "features/evolution/governance/denial.go:DenialRecord", 9),
    ("struct-fields", "features/evolution/governance/goal.go:Goal", 6),
    ("struct-fields", "memory/query/inventory.go:InventoryCounts", 6),
    # assembly.InvokeRequest 6f：投递元数据随输入行（send 线 2026-11-01，InputMeta
    # 与 Input 同生命周期），不拆块；可读性由字段块文档承担。
    ("struct-fields", "assembly/run.go:InvokeRequest", 6),
    ("struct-fields", "memory/query/inventory.go:NoticeEntry", 9),
    ("struct-fields", "memory/query/inventory.go:SessionEntry", 6),
    ("struct-fields", "memory/query/snapshot.go:DetailState", 6),
    ("struct-fields", "memory/query/subscribe.go:Update", 6),
    ("struct-fields", "memory/query/types.go:ObjectRef", 7),
    ("struct-fields", "features/rl/capture.go:CaptureManifest", 9),
    ("struct-fields", "features/rl/capture.go:RequestSnapshot", 11),
    # 数据保真面（记录 schema＝序列化载体，字段为数据语义本体非配置选择面——
    # S2 类别豁免，同 event.Event 16f 先例）。装配期配置/选择面不入本块：配置面
    # 零债硬拦。以下逐项具名引入叶与保真理由。
    # 06.8 加性 ModelVersion：替换记录携带版本归属通路的证据。
    ("struct-fields", "features/rl/replacement.go:ReplacementRecord", 6),
    # 08.4 新产数据面（诊断读出报告＝数据保真面非配置选择面，S2 类别豁免；
    # 引入叶 08.4——七段读出是报告语义本体，同 Explanation 先例）
    ("struct-fields", "assembly/host_diagnostics.go:DiagnosticReport", 7),
    ("struct-fields", "features/rl/record.go:AttemptRecord", 12),
    ("struct-fields", "features/rl/record.go:DecisionRecord", 7),
    ("struct-fields", "features/rl/record.go:EpisodeRecord", 9),
    ("struct-fields", "features/rl/record.go:ModelViewRef", 8),
    # 06.3 采集账目（引入叶）：准入/拒收/写错/在飞四类计数是丢失账本体，
    # Dropped() 由分类计数合成，拆块即审计面需要二次归并。
    ("struct-fields", "features/rl/sink.go:CaptureLedger", 8),
    ("struct-fields", "runtime/state/resources.go:SkillLoad", 6),
    ("struct-fields", "runtime/state/session.go:MailboxEntry", 6),
    ("struct-fields", "runtime/task/wrapper.go:Result", 6),
    # hot-update-and-purity-closure 12 引入叶（S2 类别豁免，同 event.Event 16f 先例）：
    # TaskCheckpoint 是框架 checkpoint 状态的任务账本体＝数据保真面——一格对应「重启后
    # 与链折叠逐格相等」所需的一条数据（声明/状态/链修订/轮次/四个移动戳/改道两格/
    # 幂等变更账/未结义务），收窄字段即让恢复面在链折叠侧二次归并或丢掉一格保真。
    ("struct-fields", "runtime/task/checkpoint.go:TaskCheckpoint", 13),
    # 04.2 执行资源解释（引入叶）：租约/续期/停滞/义务的只读投影，字段即
    # Manager.Explain 的回复本体，拆块即解释面需要二次装配。
    ("struct-fields", "tool/exec/exec.go:Explanation", 10),
    # 01.10b 执行状态事件（引入叶，S2 类别豁免，同 event.Event 16f 先例）：观察本体＝
    # 数据面，资源键/相位/退出码/确认/释放/细节/归属/时刻八格是一次生命周期快照的语义
    # 本体，收窄字段等于拆数据面，消费方须把同一观察二次归并。
    ("struct-fields", "tool/exec/state.go:StateEvent", 8),
    # hot-update-and-purity-closure 11/09 引入叶（S2 类别豁免，同 event.Event 16f 先例）：
    # meditation.Plan 是文档启用的一段反思声明本体（观察 agent/会话/节奏与下限/静态提示/
    # 观察与读取命名空间/回流目标/水位锚），Faces 是一次挂载装配所读写的运行面集合
    # （锚存储、观察与注入面、模型在飞读、实例、会话查询）——两者都是解析后的声明/装配
    # 结果本体，不是可选配置开关，收窄字段等于让 root 重新读冥想要参数（ADR 0004）。
    ("struct-fields", "features/evolution/meditation/mount.go:Plan", 10),
    ("struct-fields", "features/evolution/meditation/mount.go:Faces", 6),
    # rl SFTSample（export_chain 11 引入叶）：一行 SFT 样本＝该回合呈现的输入、助手给出的
    # 标签、所属 episode 与回合号、会话，以及批回合逐条输入归属；六格是导出行本体，
    # 收窄会让归属二次归并成匿名文本块。
    ("struct-fields", "features/rl/export_chain.go:SFTSample", 6),
    # rl PreferenceRow（偏好导出行）：两两对齐的一次尝试对＝提示、工具声明、胜出补全、
    # 被弃补全、偏好来源与推导版本；七格是偏好行本体，收窄即丢掉一次偏好判定的归属依据。
    ("struct-fields", "features/rl/export_chain.go:PreferenceRow", 7),
    # S3 ctx-bare — accessor 落点具名登记（调用点裸读=0；实现体单次 ctx.Value=合法）
    ("ctx-bare", "api/plugin/phases.go:*", 1),   # CallPassFrom：派发→受理 pass 传输（锁定工具端口无第二通道）
    ("ctx-bare", "runtime/task/wrapper.go:*", 1),    # CallFrameFrom：per-call 身份经端口唯一通道
    ("ctx-bare", "assembly/settle_turn.go:*", 1),    # settleBarrierFrom：回合终态屏障经类型化 accessor 落到驱动与投递之间
    # S4 contract-surface 已出清（叶 00.24）：全部导出接口的 canonical 范例与 Godoc
    # 红线均已落地，本表不再登记 S4 债务。以下一条为再次登记：ResultDelegation 是
    # 结算携带结果正文引入的可选拓展面，范例与红线随后续批次补齐；在此之前显式
    # 计入债务，新增接口不因“尚未补范例”而免检。
    ("contract-surface", "tool/subagent/tool.go:ResultDelegation", None),
    # hot-update-and-purity-closure 引入叶的三条 S4 债务（同 ResultDelegation 先例：范例与
    # Godoc 红线随后续批次补齐，在此之前显式计入，新增接口不因“尚未补范例”而免检）：
    # GenerationFaces＝每代 native 面的报告口，AtomicStore＝每层打开的原子存储引擎口，
    # ChainFailureNamer＝durable 读面具名说出“无正文”的原因。
    ("contract-surface", "assembly/run.go:GenerationFaces", None),
    ("contract-surface", "memory/ledger/atomic_store.go:AtomicStore", None),
    ("contract-surface", "tool/recall/get.go:ChainFailureNamer", None),
    # 同批 rl 证据导出的两个读面口（EvidenceLines 只读行记录面、SnapshotReadBack 快照
    # 回读面）：范例与红线随证据导出叶补齐前显式计入债务。
    ("contract-surface", "features/rl/export_chain.go:EvidenceLines", None),
    ("contract-surface", "features/rl/export_chain.go:SnapshotReadBack", None),
    # 同波 domain 12 的第四条 S4 债务（同 ResultDelegation 先例）：Stateful 是状态接缝的
    # 委托面，范例与 Godoc 红线随后续批次补齐；在此之前显式计入，新增接口不因“尚未补
    # 范例”而免检。
    ("contract-surface", "runtime/state/checkpoint.go:Stateful", None),
)
# ---- Go 源码静态扫描（纯 stdlib；不做类型检查，见模块 docstring 的局限）----
IFACE_DECL_RE = re.compile(r"^type\s+(?P<name>[A-Za-z_]\w*)\s+interface\s*\{", re.M)
STRUCT_DECL_RE = re.compile(r"^type\s+(?P<name>[A-Za-z_]\w*)\s+struct\s*\{", re.M)
METHOD_DECL_RE = re.compile(
    r"^func\s*\(\s*(?:[A-Za-z_]\w*\s+)?\*?(?P<recv>[A-Za-z_]\w*)"
    r"(?:\s*\[[^\]]*\])?\s*\)\s*(?P<name>[A-Za-z_]\w*)\s*\(",
    re.M,
)
CTX_VALUE_RE = re.compile(r"\bctx\.Value\(")
IFACE_METHOD_RE = re.compile(r"^(?P<name>[A-Za-z_]\w*)\s*\(")
LOCAL_EMBED_RE = re.compile(r"^(?P<name>[A-Za-z_]\w*)(?:\[[^\]]*\])?$")
FOREIGN_EMBED_RE = re.compile(r"^(?P<pkg>[A-Za-z_]\w*)\.(?P<name>[A-Za-z_]\w*)(?:\[[^\]]*\])?$")
FIELD_RE = re.compile(
    r"^(?P<names>[A-Za-z_]\w*(?:\s*,\s*[A-Za-z_]\w*)*)\s+(?P<rest>\S.*)$", re.S
)
REDLINE_MUST_NOT_RE = re.compile(r"^//\s*MUST\s+NOT\b")
REDLINE_MUST_RE = re.compile(r"^//\s*MUST\b")
EXAMPLE_FUNC_RE = "func Example{name}("


def strip_go(text):
    """返回 (no_comments, code)：注释置空；code 另把字符串/字符面量置空。

    两版都与原文等长（换行保留、字符位置不变），因此行号/括号位置可互相对齐；
    括号匹配只在 code 上做，字段/方法名读取在 no_comments 上做。
    """
    size = len(text)
    no_comments = list(text)
    code = list(text)

    def blank(chars, start, end):
        for pos in range(start, min(end, size)):
            if chars[pos] != "\n":
                chars[pos] = " "

    index = 0
    while index < size:
        char = text[index]
        if char in "\"'`":
            quote = char
            end = index + 1
            while end < size:
                if quote != "`" and text[end] == "\\":
                    end += 2
                    continue
                if text[end] == quote:
                    end += 1
                    break
                end += 1
            blank(code, index, end)
            index = end
            continue
        if char == "/" and index + 1 < size and text[index + 1] == "/":
            end = text.find("\n", index)
            end = size if end < 0 else end
            blank(no_comments, index, end)
            blank(code, index, end)
            index = end
            continue
        if char == "/" and index + 1 < size and text[index + 1] == "*":
            end = text.find("*/", index + 2)
            end = size if end < 0 else end + 2
            blank(no_comments, index, end)
            blank(code, index, end)
            index = end
            continue
        index += 1
    return "".join(no_comments), "".join(code)


def brace_span(code, open_index):
    """返回与 `{` 配对的 `}` 之后的位置；不配对时返回文本末尾。"""
    depth = 0
    for pos in range(open_index, len(code)):
        char = code[pos]
        if char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                return pos + 1
    return len(code)


def split_items(block):
    """按顶层换行/分号切分接口或结构体体；括号、花括号内的换行不切。

    多行方法签名（`Foo(\\n  a int,\\n) error`）因此仍是一个 item。
    """
    items = []
    buffer = []
    paren = 0
    brace = 0
    for char in block:
        if char == "(":
            paren += 1
        elif char == ")":
            paren = max(0, paren - 1)
        elif char == "{":
            brace += 1
        elif char == "}":
            brace = max(0, brace - 1)
        if char in "\n;" and paren == 0 and brace == 0:
            item = "".join(buffer).strip()
            if item:
                items.append(item)
            buffer = []
            continue
        buffer.append(char)
    item = "".join(buffer).strip()
    if item:
        items.append(item)
    return items


def stripped_line_at(code, offset):
    return code.count("\n", 0, offset) + 1


def doc_block(lines, decl_line):
    """紧贴在声明上方（中间不空行）的 `//` 注释块；空行或代码即中断。"""
    collected = []
    cursor = decl_line - 2
    while cursor >= 0:
        text = lines[cursor].strip()
        if not text.startswith("//"):
            break
        collected.append(text)
        cursor -= 1
    return list(reversed(collected))


def parse_interface_items(items):
    """把接口体切成方法名与嵌入项（同包/外包）。"""
    methods = []
    local_embeds = []
    foreign_embeds = []
    for item in items:
        method = IFACE_METHOD_RE.match(item)
        if method:
            methods.append(method.group("name"))
            continue
        local = LOCAL_EMBED_RE.match(item)
        if local:
            local_embeds.append(local.group("name"))
            continue
        foreign = FOREIGN_EMBED_RE.match(item)
        if foreign:
            foreign_embeds.append(item)
    return methods, local_embeds, foreign_embeds


def exported_field_names(item):
    """一个结构体顶层 item 里的导出字段名；嵌入字段与未导出字段不计。"""
    match = FIELD_RE.match(item.strip())
    if not match:
        return []
    names = [part.strip() for part in match.group("names").split(",")]
    return [name for name in names if name[:1].isupper()]


def scan_file(rel_path, text):
    """解析一个 Go 文件里的接口/结构体/方法接收者，返回三元组。"""
    no_comments, code = strip_go(text)
    original_lines = text.splitlines()
    interfaces = []
    structs = []
    for match in IFACE_DECL_RE.finditer(code):
        open_index = code.index("{", match.end() - 1)
        end = brace_span(code, open_index)
        body = no_comments[open_index + 1:end - 1]
        methods, local_embeds, foreign_embeds = parse_interface_items(split_items(body))
        line = stripped_line_at(code, match.start())
        interfaces.append({
            "file": rel_path,
            "pkg": os.path.dirname(rel_path) or ".",
            "line": line,
            "name": match.group("name"),
            "methods": methods,
            "local_embeds": local_embeds,
            "foreign_embeds": foreign_embeds,
            "doc": doc_block(original_lines, line),
            "key": "{}:{}".format(rel_path, match.group("name")),
        })
    for match in STRUCT_DECL_RE.finditer(code):
        open_index = code.index("{", match.end() - 1)
        end = brace_span(code, open_index)
        body = no_comments[open_index + 1:end - 1]
        fields = []
        for item in split_items(body):
            fields.extend(exported_field_names(item))
        structs.append({
            "file": rel_path,
            "pkg": os.path.dirname(rel_path) or ".",
            "line": stripped_line_at(code, match.start()),
            "name": match.group("name"),
            "fields": fields,
            "key": "{}:{}".format(rel_path, match.group("name")),
        })
    receivers = {}
    for match in METHOD_DECL_RE.finditer(code):
        receivers.setdefault(match.group("recv"), set()).add(match.group("name"))
    return interfaces, structs, receivers


def pattern_matches(pattern, pkg):
    """与 check_runtime_architecture 同口径：精确包路径，或 `^...$` 锚定正则。"""
    if pattern == pkg:
        return True
    if pattern.startswith("^") and pattern.endswith("$"):
        return re.match(pattern, pkg) is not None
    return False


def load_policy(arch_path):
    policy = lib.load_json_object(arch_path)
    raw = policy.get("packages")
    if not isinstance(raw, list):
        raise lib.CheckFailure("{}: packages must be a list".format(arch_path))
    patterns = [item.get("pattern") for item in raw
                if isinstance(item, dict) and isinstance(item.get("pattern"), str)]
    if not patterns:
        raise lib.CheckFailure("{}: no package patterns declared".format(arch_path))
    scope = policy.get("scope") if isinstance(policy.get("scope"), dict) else {}
    excludes = scope.get("exclude_dir_names")
    if not isinstance(excludes, list):
        excludes = []
    return patterns, {name for name in excludes if isinstance(name, str)}


def in_scope(rel_path, excludes):
    return not any(segment in excludes for segment in rel_path.split("/"))


def collect_sources(repo_root, arch_path):
    """返回 (sources, metrics)：只含政策登记且非排除目录下的非测试 Go 文件。"""
    if not os.path.isdir(repo_root):
        raise lib.CheckFailure("repo root not found: {}".format(repo_root))
    patterns, excludes = load_policy(arch_path)
    sources = []
    skipped_unregistered = set()
    for current, dirs, files in os.walk(repo_root):
        dirs[:] = sorted(name for name in dirs
                         if not name.startswith(".") and name not in excludes)
        for name in sorted(files):
            if not name.endswith(".go") or name.endswith("_test.go"):
                continue
            absolute = os.path.join(current, name)
            rel_path = os.path.relpath(absolute, repo_root).replace(os.sep, "/")
            if not in_scope(rel_path, excludes):
                continue
            pkg = os.path.dirname(rel_path) or "."
            if not any(pattern_matches(pattern, pkg) for pattern in patterns):
                skipped_unregistered.add(pkg)
                continue
            try:
                with open(absolute, "r", encoding="utf-8") as handle:
                    text = handle.read()
            except OSError as exc:
                raise lib.CheckFailure("cannot read {}: {}".format(rel_path, exc))
            sources.append((rel_path, pkg, text))
    if not sources:
        raise lib.CheckFailure(
            "no registered first-party Go sources under {}: implementation not present".format(
                repo_root))
    metrics = {
        "files": len(sources),
        "packages": len({pkg for _rel, pkg, _text in sources}),
        "skipped_unregistered_packages": sorted(skipped_unregistered),
    }
    return sources, metrics


def build_index(sources):
    index = {"interfaces": [], "structs": [], "receivers": {}, "sources": sources}
    for rel_path, _pkg, text in sources:
        interfaces, structs, receivers = scan_file(rel_path, text)
        index["interfaces"].extend(interfaces)
        index["structs"].extend(structs)
        for recv, names in receivers.items():
            index["receivers"].setdefault(recv, set()).update(names)
    return index


def resolved_methods(iface, by_name, cache, stack=()):
    """展开同包嵌入接口后的方法名集合；环或外包嵌入已在上游报出。"""
    if iface["key"] in cache:
        return cache[iface["key"]]
    if iface["key"] in stack:
        return []
    methods = list(iface["methods"])
    for embed in iface["local_embeds"]:
        for candidate in by_name.get(embed, ()):
            if candidate["pkg"] == iface["pkg"]:
                methods.extend(resolved_methods(candidate, by_name, cache, stack + (iface["key"],)))
    cache[iface["key"]] = methods
    return methods


def implementer_count(methods, receivers):
    if not methods:
        return 0
    need = set(methods)
    return sum(1 for have in receivers.values() if need <= have)


def rule_iface_methods(index):
    """S1：多态接口 ≤2 方法；消费者侧窄口 ≤3 方法。"""
    findings = []
    by_name = {}
    for iface in index["interfaces"]:
        by_name.setdefault(iface["name"], []).append(iface)
    cache = {}
    for iface in index["interfaces"]:
        methods = resolved_methods(iface, by_name, cache)
        count = len(methods)
        implementers = implementer_count(methods, index["receivers"])
        polymorphic = implementers >= POLYMORPHIC_MIN_IMPLEMENTERS
        limit = POLYMORPHIC_METHOD_LIMIT if polymorphic else CONSUMER_METHOD_LIMIT
        if count <= limit:
            continue
        kind = "polymorphic port" if polymorphic else "consumer-side narrow port"
        detail = "{} methods > {} ({}; {} implementer(s))".format(
            count, limit, kind, implementers)
        if iface["foreign_embeds"]:
            detail += "; foreign embeds counted as 1 each: {}".format(
                ", ".join(iface["foreign_embeds"]))
        findings.append({
            "rule": RULE_IFACE,
            "key": iface["key"],
            "file": iface["file"],
            "line": iface["line"],
            "value": count,
            "detail": detail,
        })
    return findings


def rule_struct_fields(index):
    """S2：导出结构 ≤5 个导出字段。"""
    findings = []
    for struct in index["structs"]:
        if not struct["name"][:1].isupper():
            continue
        count = len(struct["fields"])
        if count <= STRUCT_FIELD_LIMIT:
            continue
        findings.append({
            "rule": RULE_STRUCT,
            "key": struct["key"],
            "file": struct["file"],
            "line": struct["line"],
            "value": count,
            "detail": "{} exported fields > {} ({})".format(
                count, STRUCT_FIELD_LIMIT, ", ".join(struct["fields"])),
        })
    return findings


def rule_ctx_bare(index):
    """S3：裸 ctx.Value 调用 = 0（按文件计数）。"""
    findings = []
    for rel_path, _pkg, text in index["sources"]:
        no_comments, _code = strip_go(text)
        count = len(CTX_VALUE_RE.findall(no_comments))
        if count <= CTX_VALUE_LIMIT:
            continue
        findings.append({
            "rule": RULE_CTX,
            "key": "{}:*".format(rel_path),
            "file": rel_path,
            "line": 0,
            "value": count,
            "detail": "{} bare ctx.Value call(s) > {}; use a typed accessor".format(
                count, CTX_VALUE_LIMIT),
        })
    return findings


def package_slug(pkg):
    # The slug is the package directory with `/` folded to `_`; the composition root
    # is a named directory too (assembly/), so no package needs a special spelling.
    return pkg.replace("/", "_")


def example_path(pkg, name):
    return "{}/{}/{}_example.go".format(EXAMPLE_ROOT, package_slug(pkg), name)


def redline_problem(doc):
    must = sum(1 for line in doc if REDLINE_MUST_RE.match(line))
    must_not = sum(1 for line in doc if REDLINE_MUST_NOT_RE.match(line))
    marked = any(marker in line for line in doc for marker in REDLINE_MARKERS)
    total = must + must_not
    if not marked:
        return "no 红线 marker in Godoc"
    if not must:
        return "no '// MUST ...' redline"
    if not must_not:
        return "no '// MUST NOT ...' redline"
    if total > REDLINE_LIMIT:
        return "{} redline lines > {}".format(total, REDLINE_LIMIT)
    return None


def rule_contract_surface(index, repo_root):
    """S4：每导出接口＝canonical 范例文件＋Godoc MUST/MUST NOT 红线清单。"""
    findings = []
    for iface in index["interfaces"]:
        if not iface["name"][:1].isupper():
            continue
        problems = []
        rel_example = example_path(iface["pkg"], iface["name"])
        absolute = os.path.join(repo_root, rel_example)
        if not os.path.isfile(absolute):
            problems.append("missing canonical example {}".format(rel_example))
        else:
            try:
                with open(absolute, "r", encoding="utf-8") as handle:
                    body = strip_go(handle.read())[0]
            except OSError as exc:
                raise lib.CheckFailure("cannot read {}: {}".format(rel_example, exc))
            if EXAMPLE_FUNC_RE.format(name=iface["name"]) not in body:
                problems.append("{} lacks func Example{}()".format(
                    rel_example, iface["name"]))
        redline = redline_problem(iface["doc"])
        if redline:
            problems.append(redline)
        if not problems:
            continue
        findings.append({
            "rule": RULE_CONTRACT,
            "key": iface["key"],
            "file": iface["file"],
            "line": iface["line"],
            "value": None,
            "detail": "; ".join(problems),
        })
    return findings


def collect_findings(repo_root, arch_path, rules):
    sources, metrics = collect_sources(repo_root, arch_path)
    index = build_index(sources)
    findings = []
    if RULE_IFACE in rules:
        findings.extend(rule_iface_methods(index))
    if RULE_STRUCT in rules:
        findings.extend(rule_struct_fields(index))
    if RULE_CTX in rules:
        findings.extend(rule_ctx_bare(index))
    if RULE_CONTRACT in rules:
        findings.extend(rule_contract_surface(index, repo_root))
    findings.sort(key=lambda item: (item["rule"], item["file"], item["line"], item["key"]))
    metrics["interfaces"] = len(index["interfaces"])
    metrics["exported_interfaces"] = sum(
        1 for iface in index["interfaces"] if iface["name"][:1].isupper())
    metrics["structs"] = len(index["structs"])
    return findings, metrics


def apply_baseline(report, findings, baseline):
    """棘轮：豁免必须 warn；超 baseline 即 error；不超标即 stale。"""
    recorded = {}
    for entry in baseline:
        rule, key, observed = entry
        recorded[(rule, key)] = observed
    hit = 0
    for finding in findings:
        slot = (finding["rule"], finding["key"])
        label = RULE_LABELS[finding["rule"]]
        if slot not in recorded:
            report.error("{} {} {}: {} (not baselined)".format(
                label, finding["rule"], finding["key"], finding["detail"]))
            continue
        allowed = recorded[slot]
        if allowed is None:
            hit += 1
            report.warn("{} {} {}: {} (baselined debt, still open)".format(
                label, finding["rule"], finding["key"], finding["detail"]))
            continue
        if finding["value"] > allowed:
            report.error("{} {} {}: {} exceeds baseline {} (ratchet: only-down)".format(
                label, finding["rule"], finding["key"], finding["detail"], allowed))
            continue
        hit += 1
        report.warn("{} {} {}: {} (baselined at {}, tighten or remove the entry)".format(
            label, finding["rule"], finding["key"], finding["detail"], allowed))
    seen = {(finding["rule"], finding["key"]) for finding in findings}
    stale = 0
    for rule, key, _observed in baseline:
        if (rule, key) in seen:
            continue
        stale += 1
        report.warn("baseline entry {} {}:{} matches no current violation (stale: remove it)"
                    .format(RULE_LABELS[rule], rule, key))
    report.count("baseline_entries", len(baseline))
    report.count("baseline_exempted", hit)
    report.count("baseline_stale", stale)
    return findings


# ---- D1 domain-surface：新增/改名公共命名面的绑定门＋规范单主门 ----

def read_text_fail_closed(path, what):
    try:
        with open(path, "r", encoding="utf-8") as handle:
            return handle.read()
    except OSError as exc:
        raise lib.CheckFailure("cannot read {} ({}): {}".format(what, path, exc))


def spec_path(repo_root, capability):
    return os.path.join(repo_root, *SPECS_SUBPATH, capability, "spec.md")


def spec_is_superseded(repo_root, capability):
    """主 spec 是否带 superseded 标记（blockquote 备注或标题行）。"""
    text = read_text_fail_closed(spec_path(repo_root, capability),
                                 "spec anchor of a concept binding")
    return any(SUPERSEDED_MARK_RE.search(line) for line in text.splitlines())


def requirements_block(text):
    """## Requirements 节的正文；节标题被删掉但还有 Requirement 块时按全文算。

    后者防止「删了节标题躲检查」：superseded 文件只要还留 Requirement 块，
    其中的 SHALL/MUST 仍会被读到。
    """
    lines = text.splitlines()
    start = None
    for index, line in enumerate(lines):
        if line.startswith("## Requirements"):
            start = index + 1
            break
    if start is None:
        return text if any(line.startswith("### Requirement:") for line in lines) else ""
    collected = []
    for line in lines[start:]:
        if re.match(r"^##\s", line):
            break
        collected.append(line)
    return "\n".join(collected)


def superseded_spec_findings(repo_root):
    """标 superseded 的主 spec 不得在 Requirements 里继续含现行 SHALL/MUST。"""
    specs_root = os.path.join(repo_root, *SPECS_SUBPATH)
    if not os.path.isdir(specs_root):
        raise lib.CheckFailure(
            "openspec/specs not found under {}: spec authority cannot be audited "
            "(fail-closed)".format(repo_root))
    findings = []
    for capability in sorted(os.listdir(specs_root)):
        path = os.path.join(specs_root, capability, "spec.md")
        if not os.path.isfile(path):
            continue
        text = read_text_fail_closed(path, "main spec")
        if not any(SUPERSEDED_MARK_RE.search(line) for line in text.splitlines()):
            continue
        hits = NORMATIVE_RE.findall(requirements_block(text))
        if not hits:
            continue
        findings.append({
            "rule": RULE_DOMAIN,
            "key": "openspec/specs/{}/spec.md".format(capability),
            "file": "openspec/specs/{}/spec.md".format(capability),
            "line": 0,
            "value": len(hits),
            "detail": "superseded spec keeps {} current SHALL/MUST statement(s) in "
                      "## Requirements: migrate effective clauses to the owning "
                      "capability, then strip the old Requirements; a historical note "
                      "is not a migration".format(len(hits)),
        })
    return findings


def exported_type_inventory(repo_root, arch_path):
    """登记包内全部导出命名类型：name → 排序后的文件列表。"""
    sources, _metrics = collect_sources(repo_root, arch_path)
    inventory = {}
    for rel_path, _pkg, text in sources:
        _no_comments, code = strip_go(text)
        for match in TYPE_DECL_RE.finditer(code):
            name = match.group("name")
            if not name[:1].isupper():
                continue
            inventory.setdefault(name, set()).add(rel_path)
    return {name: sorted(files) for name, files in inventory.items()}


def extract_baseline_commit(repo_root, commit, dest):
    """git archive 基线提交，落盘其中的 .go 成员；失败即 fail-closed。"""
    try:
        proc = subprocess.run(["git", "-C", repo_root, "archive", commit],
                              capture_output=True)
    except OSError as exc:
        raise lib.CheckFailure("cannot run git for baseline commit {}: {} "
                               "(fail-closed)".format(commit, exc))
    if proc.returncode != 0:
        detail = proc.stderr.decode("utf-8", "replace").strip()
        raise lib.CheckFailure("cannot read baseline commit {} in {}: {} (fail-closed)"
                               .format(commit, repo_root, detail))
    os.makedirs(dest, exist_ok=True)
    try:
        with tarfile.open(fileobj=io.BytesIO(proc.stdout), mode="r:") as archive:
            for member in archive.getmembers():
                if not member.isfile() or not member.name.endswith(".go"):
                    continue
                parts = member.name.split("/")
                if member.name.startswith("/") or ".." in parts:
                    raise lib.CheckFailure("baseline archive member escapes the tree: {}"
                                           .format(member.name))
                target = os.path.join(dest, *parts)
                parent = os.path.dirname(target)
                if parent:
                    os.makedirs(parent, exist_ok=True)
                handle = archive.extractfile(member)
                if handle is None:
                    continue
                with open(target, "wb") as out:
                    out.write(handle.read())
    except tarfile.TarError as exc:
        raise lib.CheckFailure("baseline commit {} is not a readable archive: {} "
                               "(fail-closed)".format(commit, exc))
    return dest


def materialize_baseline(repo_root, baseline_cfg, dest):
    """按 concept_bindings.baseline 解析基线树：commit（git）或 tree（本地快照）。"""
    if not isinstance(baseline_cfg, dict):
        raise lib.CheckFailure("concept_bindings.baseline must be an object with "
                               "commit or tree (fail-closed)")
    commit = baseline_cfg.get("commit")
    tree = baseline_cfg.get("tree")
    if isinstance(commit, str) and isinstance(tree, str):
        raise lib.CheckFailure(
            "concept_bindings.baseline: provide exactly one of commit/tree")
    if isinstance(commit, str) and commit.strip():
        return extract_baseline_commit(repo_root, commit.strip(), dest)
    if isinstance(tree, str) and tree.strip():
        path = tree.strip()
        if not os.path.isabs(path):
            path = os.path.join(repo_root, path)
        if not os.path.isdir(path):
            raise lib.CheckFailure(
                "concept_bindings.baseline tree not found: {} (fail-closed)".format(path))
        return path
    raise lib.CheckFailure(
        "concept_bindings.baseline needs a commit or a tree (fail-closed)")


def binding_anchor_problem(repo_root, spec):
    spec_file = spec_path(repo_root, spec)
    if not os.path.isfile(spec_file):
        return "spec anchor openspec/specs/{}/spec.md not found (fail-closed)".format(spec)
    if spec_is_superseded(repo_root, spec):
        return "spec anchor {} is superseded; bind the live owning capability".format(spec)
    return None


def _binding_structural_problems(repo_root, binding):
    if not isinstance(binding, dict):
        return ["binding must be an object"]
    problems = []
    kind = binding.get("kind")
    if kind not in DOMAIN_KINDS:
        problems.append("kind must be one of fact/read/declaration/port, got {!r}"
                        .format(kind))
    concept = binding.get("concept")
    if not isinstance(concept, str) or not concept.strip():
        problems.append("concept must name the canonical concept")
    spec = binding.get("spec")
    if not isinstance(spec, str) or not spec.strip():
        problems.append("spec must name the owning capability")
    else:
        anchor = binding_anchor_problem(repo_root, spec.strip())
        if anchor:
            problems.append(anchor)
    return problems


def domain_surface_report(repo_root, arch_path):
    """D1 主判定：返回 (findings, warnings, counts)；fail-closed 输入抛 CheckFailure。"""
    policy = lib.load_json_object(arch_path)
    config = policy.get(CONCEPT_BINDINGS_KEY)
    if not isinstance(config, dict):
        raise lib.CheckFailure(
            "{}: concept_bindings section missing or not an object; the domain-surface "
            "gate refuses to run without its binding table (fail-closed)".format(arch_path))
    unknown = sorted(set(config) - {"baseline", "bindings"})
    if unknown:
        raise lib.CheckFailure("concept_bindings has unknown key(s) {}: use "
                               "baseline/bindings only".format(", ".join(unknown)))
    bindings = config.get("bindings")
    if not isinstance(bindings, dict):
        raise lib.CheckFailure("concept_bindings.bindings must be an object: symbol -> "
                               "{concept, kind, spec, renamed_from?}")
    current = exported_type_inventory(repo_root, arch_path)
    with tempfile.TemporaryDirectory() as tmp:
        baseline_root = materialize_baseline(repo_root, config.get("baseline"),
                                             os.path.join(tmp, "baseline"))
        base = exported_type_inventory(baseline_root, arch_path)

    new_names = sorted(set(current) - set(base))
    removed_names = sorted(set(base) - set(current))
    relocations = sorted(name for name in set(current) & set(base)
                         if set(current[name]) != set(base[name]))
    findings = []
    warnings = []
    renames = 0
    net_facts = 0

    for name in new_names:
        key = "{}:{}".format(current[name][0], name)
        binding = bindings.get(name)
        if not isinstance(binding, dict):
            findings.append({
                "rule": RULE_DOMAIN, "key": key, "file": current[name][0],
                "line": 0, "value": None,
                "detail": "new exported named type has no concept binding: mechanism is "
                          "not a domain concept — bind concept/kind "
                          "(fact/read/declaration/port)/spec to a live main spec, or "
                          "internalize it",
            })
            continue
        problems = _binding_structural_problems(repo_root, binding)
        renamed_from = binding.get("renamed_from")
        if renamed_from is not None:
            if not isinstance(renamed_from, str) or not renamed_from.strip():
                problems.append("renamed_from must name the previous public symbol")
            elif renamed_from not in base:
                problems.append("renamed_from {!r} is absent from the baseline; a pure "
                                "rename must start from an existing public name"
                                .format(renamed_from))
            elif renamed_from in current:
                problems.append("renamed_from {!r} is still present; a rename must "
                                "retire the old public name".format(renamed_from))
        if problems:
            findings.append({
                "rule": RULE_DOMAIN, "key": key, "file": current[name][0],
                "line": 0, "value": None, "detail": "; ".join(problems),
            })
            continue
        if renamed_from is not None:
            renames += 1
        elif binding.get("kind") == "fact":
            net_facts += 1

    # 表卫生：非新增名的绑定不被需要——结构仍要合法（红），合法则 warn 防烂账。
    for name in sorted(bindings):
        if name in new_names:
            continue
        problems = _binding_structural_problems(repo_root, bindings[name])
        if problems:
            findings.append({
                "rule": RULE_DOMAIN, "key": "concept_bindings:{}".format(name),
                "file": os.path.basename(arch_path), "line": 0, "value": None,
                "detail": "; ".join(problems),
            })
        else:
            warnings.append("concept_bindings[{!r}] is not required: the name is "
                            "unchanged since the baseline (remove the entry or it rots)"
                            .format(name))

    findings.extend(superseded_spec_findings(repo_root))
    findings.sort(key=lambda item: (item["file"], item["key"]))
    counts = {
        "domain_surface.new": len(new_names),
        "domain_surface.renames": renames,
        "domain_surface.relocations": len(relocations),
        "domain_surface.removed": len(removed_names),
        "domain_surface.net_facts": net_facts,
        "domain_surface.bindings": len(bindings),
        "domain_surface.superseded_specs_with_requirements": sum(
            1 for item in findings if item["key"].startswith("openspec/specs/")),
    }
    return findings, warnings, counts
FIXTURE_ARCH_NAME = ".concept-arch.json"
FIXTURE_ARCH = {
    "schema_version": 1,
    "scope": {"exclude_dir_names": ["examples"]},
    "packages": [{"pattern": "."}, {"pattern": "good"}, {"pattern": "bad"}],
}

GOOD_FIXTURE = {
    "good/faces.go": """package good

import "context"

// GoodFace 红线清单（2 条）：
// MUST 在一次调用内保持 id 稳定
// MUST NOT 持有调用方指针
type GoodFace interface {
	Read(ctx context.Context, id string) (string, error)
	Close() error
}

type firstImpl struct{}

func (firstImpl) Read(ctx context.Context, id string) (string, error) { return "", nil }
func (firstImpl) Close() error                                        { return nil }

type secondImpl struct{}

func (*secondImpl) Read(ctx context.Context, id string) (string, error) { return "", nil }
func (*secondImpl) Close() error                                        { return nil }

type GoodConfig struct {
	A int
	B int
	C int
	D int
	E int
}
""",
    "examples/contract/good/GoodFace_example.go": """package good

// ExampleGoodFace 是该窄口的 canonical 范例（一对最小实现）。
func ExampleGoodFace() {}
""",
}

BAD_FIXTURE = {
    "bad/faces.go": """package bad

import "context"

// BadFace 红线清单（2 条）：
// MUST 调用前加锁
// MUST NOT 在回调里重入本接口
type BadFace interface {
	A()
	B()
	C()
}

type implOne struct{}

func (implOne) A() {}
func (implOne) B() {}
func (implOne) C() {}

type implTwo struct{}

func (*implTwo) A() {}
func (*implTwo) B() {}
func (*implTwo) C() {}

type Big struct {
	A1 int
	B1 int
	C1 int
	D1 int
	E1 int
	F1 int
}

func leak(ctx context.Context) interface{} { return ctx.Value("k") }
""",
}

BAD_EXPECTED = {
    RULE_IFACE: "bad/faces.go:BadFace",
    RULE_STRUCT: "bad/faces.go:Big",
    RULE_CTX: "bad/faces.go:*",
    RULE_CONTRACT: "bad/faces.go:BadFace",
}


def write_fixture_tree(root, files):
    for rel_path, text in files.items():
        absolute = os.path.join(root, rel_path)
        parent = os.path.dirname(absolute)
        if parent:
            os.makedirs(parent, exist_ok=True)
        with open(absolute, "w", encoding="utf-8") as handle:
            handle.write(text)


def check_baseline_table(report, repo_root):
    """BASELINE 自身的完整性：形状、规则名、文件存在、observed 必须真超标。"""
    seen = set()
    minimum = {
        RULE_IFACE: POLYMORPHIC_METHOD_LIMIT,
        RULE_STRUCT: STRUCT_FIELD_LIMIT,
        RULE_CTX: CTX_VALUE_LIMIT,
    }
    for index, entry in enumerate(BASELINE):
        where = "BASELINE[{}]".format(index)
        if not isinstance(entry, tuple) or len(entry) != 3:
            report.error("{} must be a (rule, key, observed) tuple".format(where))
            continue
        rule, key, observed = entry
        if rule not in RULES:
            report.error("{} has unknown rule {!r}".format(where, rule))
            continue
        if not isinstance(key, str) or ":" not in key:
            report.error("{} key must be '<file>:<Symbol>', got {!r}".format(where, key))
            continue
        if (rule, key) in seen:
            report.error("{} duplicates baseline entry {}:{}".format(
                where, RULE_LABELS[rule], key))
        seen.add((rule, key))
        file_part, _sep, symbol = key.partition(":")
        if not os.path.isfile(os.path.join(repo_root, file_part)):
            # 文件被拆分/改名后该行已失效：警告而非失败，正常模式也会报 stale。
            report.warn("{} names missing file {} (stale entry: remove or re-point it)".format(
                where, file_part))
        if rule == RULE_CTX:
            if symbol != "*":
                report.error("{} ctx-bare entries are file-wide, symbol must be '*'".format(where))
        elif symbol == "*":
            report.error("{} symbol must name a type, not '*'".format(where))
        if rule == RULE_CONTRACT:
            if observed is not None:
                report.error("{} contract-surface debt is boolean, observed must be None".format(where))
            continue
        if type(observed) is not int or observed <= minimum[rule]:
            report.error("{} recorded value {!r} must be an int above the limit {} "
                         "(a compliant symbol must not sit in the baseline)".format(
                             where, observed, minimum[rule]))
    report.count("baseline_entries", len(BASELINE))


def check_fixture_rules(report, good_root, bad_root):
    """正反样例：合规件零发现、超标件被点名；每条形状规则必须能单独跑。

    只迭代 SHAPE_RULES：D1 的正反样例由 unittest 夹具覆盖（需要基线树与 specs
    目录，不适合塞进 --self-check 的极简夹具）。
    """
    good_arch = os.path.join(good_root, FIXTURE_ARCH_NAME)
    bad_arch = os.path.join(bad_root, FIXTURE_ARCH_NAME)
    for rule in SHAPE_RULES:
        label = RULE_LABELS[rule]
        findings, _metrics = collect_findings(good_root, good_arch, (rule,))
        if findings:
            report.error("self-check {}: compliant fixture was flagged: {}".format(
                label, findings[0]["detail"]))
        findings, _metrics = collect_findings(bad_root, bad_arch, (rule,))
        keys = [finding["key"] for finding in findings]
        if keys != [BAD_EXPECTED[rule]]:
            report.error("self-check {}: expected exactly [{}] on the violation fixture, got {}".format(
                label, BAD_EXPECTED[rule], keys))
    report.count("selfcheck_fixture_cases", len(SHAPE_RULES) * 2)


def check_baseline_ratchet(report, bad_root):
    """豁免/棘轮/清零三条路径都在反样例上跑一遍（不是只测过路径）。"""
    findings, _metrics = collect_findings(bad_root, os.path.join(bad_root, FIXTURE_ARCH_NAME), RULES)
    exempt = [(finding["rule"], finding["key"], finding["value"]) for finding in findings]
    probe = lib.Report("selfcheck-exempt")
    apply_baseline(probe, findings, exempt)
    if probe.errors:
        report.error("self-check: recorded baseline values must exempt, got: {}".format(
            probe.errors[0]))
    if probe.counts.get("baseline_exempted") != len(findings):
        report.error("self-check: exempt path counted {} of {} findings".format(
            probe.counts.get("baseline_exempted"), len(findings)))
    regress = [(finding["rule"], finding["key"],
                None if finding["value"] is None else finding["value"] - 1)
               for finding in findings]
    probe = lib.Report("selfcheck-ratchet")
    apply_baseline(probe, findings, regress)
    numeric = sum(1 for finding in findings if finding["value"] is not None)
    if len(probe.errors) != numeric or not any("ratchet" in message for message in probe.errors):
        report.error("self-check: a value above the recorded baseline must be a ratchet error "
                     "(expected {} errors, got {})".format(numeric, len(probe.errors)))
    probe = lib.Report("selfcheck-stale")
    apply_baseline(probe, [], [(RULE_IFACE, "nowhere.go:Ghost", 4)])
    if not any("stale" in message for message in probe.warnings):
        report.error("self-check: a baseline entry without a current violation must warn stale")
    report.count("selfcheck_ratchet_errors", numeric)


def self_check(report, repo_root, arch_path):
    """--self-check：表完整性＋S1–S4 正反样例＋棘轮三路径；不依赖当前树的通过与否。"""
    del arch_path  # 自校只用内置 fixture 架构；真实树由正常模式判定
    check_baseline_table(report, repo_root)
    with tempfile.TemporaryDirectory() as tmp:
        good_root = os.path.join(tmp, "good_repo")
        bad_root = os.path.join(tmp, "bad_repo")
        for root in (good_root, bad_root):
            write_fixture_tree(root, {FIXTURE_ARCH_NAME: _json_text(FIXTURE_ARCH)})
        write_fixture_tree(good_root, GOOD_FIXTURE)
        write_fixture_tree(bad_root, BAD_FIXTURE)
        check_fixture_rules(report, good_root, bad_root)
        check_baseline_ratchet(report, bad_root)
    # 自校只跑形状规则夹具；D1 的正反锚在 unittest 里（需要基线树与 specs 目录）。
    report.count("selfcheck_rules", len(SHAPE_RULES))
    report.note("self-check runs rule fixtures only; it never claims the real tree passes")


def _json_text(value):
    return json.dumps(value, ensure_ascii=False, indent=2) + "\n"


def parse_args(argv):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", default=".", help="仓库根目录（默认当前目录）")
    parser.add_argument(
        "--architecture",
        default=os.path.join("golden", "runtime", "architecture.json"),
        help="一方包清单政策（相对 --repo-root）",
    )
    parser.add_argument(
        "--rule",
        action="append",
        default=[],
        help="只跑某条规则：S1..S4、D1(domain-surface) 或规则名（可重复；默认全部）",
    )
    parser.add_argument("--self-check", action="store_true",
                        help="规则自校：baseline 表完整性＋S1–S4 正反样例＋棘轮三路径")
    parser.add_argument("--json", action="store_true", help="以 JSON 输出报告")
    return parser.parse_args(argv)


def resolve_rules(requested):
    if not requested:
        return RULES
    chosen = []
    for name in requested:
        key = name.strip()
        rule = RULE_ALIASES.get(key.upper())
        if rule is None and key in RULES:
            rule = key
        if rule is None:
            raise lib.CheckFailure("unknown rule {!r}; use S1..S4 or one of {}".format(
                name, ", ".join(RULES)))
        if rule not in chosen:
            chosen.append(rule)
    return tuple(chosen)


def run(argv):
    args = parse_args(argv)
    report = lib.Report("check_concept_budget")
    repo_root = os.path.abspath(args.repo_root)
    arch_path = args.architecture
    if not os.path.isabs(arch_path):
        arch_path = os.path.join(repo_root, arch_path)
    try:
        if args.self_check:
            self_check(report, repo_root, arch_path)
            return report.emit(args.json)
        rules = resolve_rules(args.rule)
        findings, metrics = collect_findings(repo_root, arch_path, rules)
        domain_findings, domain_warnings, domain_counts = [], [], {}
        if RULE_DOMAIN in rules:
            domain_findings, domain_warnings, domain_counts = domain_surface_report(
                repo_root, arch_path)
        report.count("rules_run", len(rules))
        report.count("go_files", metrics["files"])
        report.count("packages", metrics["packages"])
        report.count("interfaces", metrics["interfaces"])
        report.count("exported_interfaces", metrics["exported_interfaces"])
        report.count("structs", metrics["structs"])
        for rule in RULES:
            pool = domain_findings if rule == RULE_DOMAIN else findings
            report.count("findings." + rule, sum(1 for f in pool if f["rule"] == rule))
        report.count("findings", len(findings))
        if metrics["skipped_unregistered_packages"]:
            report.warn("packages not registered in {} were not scanned (the architecture gate "
                        "owns registration): {}".format(
                            args.architecture, ", ".join(metrics["skipped_unregistered_packages"])))
        scope_baseline = [entry for entry in BASELINE if entry[0] in rules]
        apply_baseline(report, findings, scope_baseline)
        # D1 与形状面分门：独立成节、独立计数，不走 BASELINE 豁免，也不并入 findings。
        for finding in domain_findings:
            report.error("D1 {} {}: {}".format(
                finding["rule"], finding["key"], finding["detail"]))
        for message in domain_warnings:
            report.warn(message)
        for key in sorted(domain_counts):
            report.count(key, domain_counts[key])
        report.extra["findings"] = findings
        report.extra["domain_surface"] = domain_findings
        report.note("static limits only: field/redline semantics still need review; "
                    "productVerified stays null")
        check_ratchet(report, arch_path, metrics, domain_counts)
    except lib.CheckFailure as exc:
        report.error(str(exc))
    return report.emit(args.json)


def check_ratchet(report, arch_path, metrics, domain_counts):
    """The ratchet freezes the convergence this change proved: floors live in the
    golden architecture beside the bindings, so a later change that re-adds a
    removed public face or grows the interface count past the frozen ceiling
    turns the gate red by name instead of passing at a new, larger normal.

    A golden file without the concept_ratchet key is a missing ratchet, not a
    passing one: it warns by name so the unenforced floors stay visible. The
    held note prints only beside a green report; a gate that failed for any
    other reason must not end with the ratchet saying it held."""
    try:
        with open(arch_path, encoding="utf-8") as handle:
            policy = json.load(handle)
    except (OSError, ValueError) as exc:
        report.error("concept ratchet: cannot read {}: {}".format(arch_path, exc))
        return
    ratchet = policy.get("concept_ratchet") if isinstance(policy, dict) else None
    if not isinstance(ratchet, dict):
        report.warn("concept ratchet: {} names no concept_ratchet; the frozen "
                    "floors are not enforced".format(arch_path))
        return
    removed = domain_counts.get("domain_surface.removed", 0)
    new = domain_counts.get("domain_surface.new", 0)
    interfaces = metrics["exported_interfaces"]
    if removed < int(ratchet.get("removed_min", 0)):
        report.error("concept ratchet: domain_surface.removed={} below the frozen floor {}".format(
            removed, ratchet["removed_min"]))
    if new > int(ratchet.get("new_max", 0)):
        report.error("concept ratchet: domain_surface.new={} exceeds the frozen ceiling {}".format(
            new, ratchet["new_max"]))
    if interfaces > int(ratchet.get("exported_interfaces_max", 1000000000)):
        report.error("concept ratchet: exported_interfaces={} exceeds the frozen ceiling {}".format(
            interfaces, ratchet["exported_interfaces_max"]))
    if report.errors:
        return
    report.note("concept ratchet held: removed>={}, new<={}, interfaces<={}".format(
        ratchet.get("removed_min", 0), ratchet.get("new_max", 0),
        ratchet.get("exported_interfaces_max", 0)))


if __name__ == "__main__":
    sys.exit(run(sys.argv[1:]))
