# 00-contract-evidence 子变更提案

父变更：`event-consumption-integrity`。本域只做契约盘点、行为复现、冻结与既有基线，不修复产品；`00.2` 依赖 `03.1` 的模拟基座，`01`/`02`/`04` 依赖本域冻结件。`02.1`—`02.6` 读取 `00.3` 后即可提供可集成域产物供 `01.5` 接线；`02` 的最终验收在 `01.5` 之后，不以最终 checkbox 反向阻塞 `01.5`。

## Why

后台任务完成后的额外确认现象，暴露了通知身份、事实引用、原调用责任、逻辑消费与外部发送之间缺少逐身份对账。当前静态面已确认两条路径：`host.go:fireNoticeTurn` 创建新 invocation，提交携带正文的 Feedback，再把同一正文交给 `InvokeRequest.Input`；`invoke.go` 另有输入事实提交路径。另一条静态分支是 `settle_turn.go` 的 `firing` 属于整个驱动器，忙时观察直接返回。二者是否构成最终漏处理或重复消费，必须由行为复现确认，不能仅凭静态分支定性。

原微信多余确认的精确根因仍未复现。模型说“重复”、历史再次出现在请求中、单次写链计数都不能单独证明重复消费。若在契约未冻结前直接修产品，容易把“多个合法消费者读取同一事实”误判为重复执行，或把“运输重试”误判为业务重试。未复现原症状时，可基于已确认静态风险形成独立、可重复的红证，但不得声称复现原案例根因；该红证只阻塞对应修复项，不扩大到全机制停工。

## What Changes

- 盘点当前生产支持的八类事件机制 E1—E8：E1 用户 HTTP、E2 工具/子代理、E3 输出渠道、E4 治理审批、E5 冥想回流、E6 压缩恢复、E7 替换、E8 采集观测；逐项记录生产入口、事实身份、消费者、确认/保留点、待验证断言与规格引用。
- 用正式 Boot 装配与本地替身复现候选缺陷：子测试初始固定为 `control_normal_path`（正常对照）、`duplicate_notice_material`、`busy_notice_loss`、`request_and_send_identity_correlation`（Boot 实际请求与发送的逐身份关联）四项。正常对照必须绿；候选缺陷若被行为复现，必须以**业务断言失败**呈现红证。原微信根因未复现时，仍只在这四个固定子测试内以 `basis=static_risk`、`original_root_cause=false` 记录独立、可重复的静态风险红证，不得声称复现原案例根因；不得用 `static_risk_*` 动态通配新增或替代子测试来凑四名。新增子场景必须先同步 `00.2-reproduction.json` 的 `subtests` manifest 与 `tasks.md` 任务行，再实施；不以编译失败、超时或纯静态推断充红证。
- 冻结五轴对账规则：责任身份、事实引用、运输重试、逻辑消费、外部发送分别记录；通知沿用 `TaskID/Round/Kind/Seq`；多个合法消费者读取同一事实不等于业务重复执行。
- 在基线提交的独立工作树/副本建立根、微信、CLI 三模块与 deterministic Golden 的既有基线，干净基线先测，不以跳过过滤排除新测试；逐命令保留原始退出码，红项在隔离基线复验，区分既有红与环境噪声。
- 本域只产出证据与冻结件（`00.2` 额外新增一个复现测试）；产品修复、模拟矩阵、CI 接线分别由 `01`—`05` 域承载。

## Capabilities

### Modified Capabilities

- `event-metadata-contract`：新增通知身份、事实引用、逻辑消费与外部发送动作的分层契约；新增派生视图的有来源与不改写源事实约束。delta 见 `specs/event-metadata-contract/spec.md`。
- `committed-event-attribution`：新增消费归属锚定已提交事实的契约，明确提交结果不确定不得报告为已消费、多消费者各自归属、完成确认必须引用事实键。delta 见 `specs/committed-event-attribution/spec.md`。

纯引用既有要求不复制为新增要求；既有 `persistent-event-loop`、`async-task-execution` 等能力由 `01` 域按实际改变写完整 `MODIFIED`。

## 边界与依赖

### 1. 依赖

- `00.1 → 00.4`：基线在盘点完成后建立，盘点清单决定基线要覆盖的入口与包。
- `03.1 → 00.2`：复现测试使用 `03.1` 产出的同包测试基座（脚本 provider、记录型 sender、模型/工具屏障）。这是**实现依赖**：`03.1` 的 helper 名称与语义未落地前，`00.2` 不得自建第二套基座。
- `00.1 + 00.2 证据 + 00.4 → 00.3`：冻结件必须引用盘点、复现或未复现记录与基线；`00.2` 的诊断红证/未复现记录产物 ready 即可供 `00.3`，不等其最终 checkbox；`00.2` 未复现只把对应 `repair_leaf` 写入 `blocked_leaves`，不阻止 `00.3` 冻结其他机制。
- 外部接口常数：`event.Event` 的 `TaskID/Round/Kind/Seq` 四元、`event.EventID` 事实键、`task.Notice` 字段、`MessageSender` 接口、`Boot` 选项。上述为**接口常数依赖**，可在 `03.1` 未完成时先做静态盘点，但不得据此宣称行为已复现。

### 2. 被依赖方

- `01.1`、`01.3`、`01.4`：读取 `00.3` 冻结的五轴与缺陷—修复叶—负锚映射。
- `02.1`—`02.6`：读取 `00.3` 的消费者归属规则，避免把合法多消费者读取误判为重复；其可集成域产物 ready 即可供 `01.5` 接线，最终验收在 `01.5` 之后。
- `04.1`、`04.3`：读取 `00.3` 的合同对象与直接调用归属清单。
- `05.1`、`05.4`：读取 `00.4` 的既有基线与原始退出码。

### 3. 接口面

- `00.1-contract-inventory.json`：E1—E8 机制数组，每项含 `id`、`mechanism`、`production_entry`、`fact_identity`、`consumers`、`confirmation_or_retention`、`unverified_assertion`、`spec_refs`。
- `00.2-reproduction.json`：`subtests` manifest 初始固定为 `control_normal_path`、`duplicate_notice_material`、`busy_notice_loss`、`request_and_send_identity_correlation` 四项（新增场景先同步 manifest 与任务行）；每项含 `status`、`basis`（`original_case`/`static_risk`/`control`）、`original_root_cause`、`assertion`、`run_id`、`log_ref`；另含 `reproduced`、`blocked`。红证必须是业务断言失败；原始 jsonl 写临时目录/CI `runner.temp`，change 内只留摘要与 `log_ref`。
- `00.3-frozen-contract.json`：五轴定义、`defects[]`（每项含布尔 `confirmed`；`confirmed=true` 必有 `repair_leaf` 与 `negative_anchor`）、`unresolved[]`（每项含非空 `blocked_leaves` 且 `blocked=true`）、`public_api_or_format_change` 裁定门；唯一真源为 `defects[].confirmed`，不得另造顶层 `confirmed[]` 双结构；下游 graph/04 合同按此结构读取。
- `00.4-baseline.json`：每条命令、原始退出码、`log_ref`（指向临时目录/CI `runner.temp` 的原始日志）、隔离复验结论；change 内不写大 jsonl。

### 4. 禁止事项

- 禁止在本域修改任何既有产品源码或既有产品测试，也不得改 CI、依赖、主规格或根账目；`00.2` 只新增复现测试。
- 禁止把静态分支、无调用声明、`Seq=0` 或局部内存字段直接定性为业务缺陷。
- 禁止为“看起来完整”而补造红证；未复现时只可在固定四项子测试内以 `basis=static_risk`、`original_root_cause=false` 记录独立红证，不得声称原案例根因，不得以 `static_risk_*` 动态通配新增/替代子测试凑数；连独立红证都无法形成时记录 `reproduced=false` 并只阻塞对应修复项，不扩大到全机制或 `00.3` 其他冻结。
- 禁止默认新增 public API 或持久化格式；确需新增须由 `00.3` 列明消费者、失败窗口、恢复路径与回退，并标 `public_api_or_format_change=blocked` 上报裁定；另需单独获得实施授权，未裁定/未授权不实施。
- 禁止把 `00.4` 的既有红算作本次变更引入，也禁止把既有绿算作新行为已验证。
- 禁止在 `00.3` 未裁决“原 invocation 下一步 decision”与“新 turn 获取当前代”的关系前，授权 `01.1`/`01.3` 用新建 invocation 绕过。

## Impact

- 本域只新增 `evidence/` 下的盘点、复现、冻结与基线文件（JSON/MD 摘要 + `log_ref`）；原始运行 jsonl 写 `${TMPDIR:-/tmp}/event-consumption-integrity/`（CI 为 `${{ runner.temp }}/event-consumption-integrity/`），不写入规划目录；产品树零改动。
- `00.2` 会新增一个 host 集成测试文件，但它属于未来实施授权范围，本轮不落盘。
- 若 `03.1` 未就绪，`00.2` 保持 blocked；`00.2` 的诊断证据产物 ready 即可供 `00.3`，不等待最终 checkbox；其 checkbox 需在 `01.5` 修复后由同一 `TestEventIntegrityReproduction` 以 `event-short` 十轮独立绿证（`verify_after_01.5`）完整验收，不因早期诊断角色永久排除。若 `00.2` 未复现且无独立静态风险红证，只把对应 `repair_leaf` 写入 `blocked_leaves`，不阻塞 `00.3` 其他机制，也不用假红推进。

## 本阶段不实施

本文件只物化规划。所有产品叶任务保持未勾选；不运行产品测试、不新增产品测试、不改 CI、不安装依赖、不提交、推送、归档或访问真实模型/微信。独立 OpenSpec 校验由主代理统一执行。
