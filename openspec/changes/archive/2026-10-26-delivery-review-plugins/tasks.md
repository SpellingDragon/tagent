# 编排账目（只编排）

> **只编排**：验收条款在域 specs。**启动门**：麻雀（G-B 的 hold→批准补投闭环）先红转绿。
> **波次语义**：分组汇报用；解锁=依赖入 main。

## 跟踪图

```mermaid
graph LR
  S1[01 审核插件投递语义] --> S2[02 发送插件出口（hold/补投）]
  S2 --> S3[03 entry 去 send + 测试迁移]
  S3 --> S4[04 症状 G-A/G-B + 合同]
  classDef crit fill:#f88,stroke:#900,color:#fff
  class S2 crit
```

## 麻雀（W0，必须先完成）

- [x] 1.0 完成投递 hold→批准补投→恰一条 的短闭环（审核插件投递语义 + host 投递出口衔接的最小实现；红证先行：现状无投递审核对象）—— 前置：无

## W1

- [x] 1.1 完成 `delivery-plugin` 能力（投递唯一出口：deliverFresh 与跨代理 send 共用；三态允许/记录/挂起；拒绝留审计）—— 前置：1.0
- [x] 1.2 完成 `governance` 投递语义扩展（内容/血统/目标进裁决输入；默认 allow；规则可 hold）—— 前置：1.0
- [x] 1.3 完成 entry 去 send（tagent.yaml + 台架文档配置；send 保留为跨代理工具不装配）—— 前置：1.1、1.2

## W2

- [x] 2.1 迁移审批/收尾轮测试（Wave2Exec 两例已换对象：send 调用→exec_run/投递 hold；Wave2Errors 的 expired-approval 场景**不迁移**——投递挂起无 TTL/摘要/重试执行，语义不可等价，保留在工具审批通道且当前绿，理由见 findings；编排者裁定 A：部分迁移接受）
- [x] 2.2 新增症状 G-A（entry 无 send + 正文恰一条）与 G-B（hold 挂起零消息/批准补投恰一条/拒绝零投递）
- [x] 2.3 主规格「循环面的模型感知边界」加"投递前审核"场景；v1/v2 合同审批场景核对

## F

- [x] F1 契约零改动断言（v2 合同只动审批相关场景，逐行核对）—— `e6b67aa..HEAD`：v1/v2 两份 golden 合计 diff 0 字节；主规格仅 `persistent-event-loop` +4/-0（投递前审核场景），`openspec validate delivery-review-plugins --strict` exit 0。证据 `evidence/F1-contract.txt`
- [x] F2 全量门禁（lint/三模块/short 十轮/long 六片/mutations/症状 job）—— 9 项全绿：lint 0；根/微信/cli-host 三连 0-0-0（根 38 ok/0 FAIL）；short 十轮 exit 0、10 互异 run_id、每轮 28 test_id/140 子测试（≈28m）；long 六片 6/6 match（7/256 首轮 60s 等待超时红证保留 + 独占受控重跑 match，batch 3+3 并发）；mutations 11/11 killed、control 先绿；bundle ok=true（17 份证据/9 包/180 测试）；症状 job 6 父/10 子全 pass、0 skip；unittest 233 OK + check-openspec 0。证据 `evidence/F2-gates.txt`
- [x] F3 DoD + G-A/G-B 回滚即红双证据（还原 entry send 授权→G-A 红；禁用投递审核→G-B 红）—— a：tagent.yaml+台架文档恢复 send → G-A 红（smoke_test.go:3635 部署文档、3653 工具面）→ 还原（blob/shasum/diff 三重一致）→ 绿；b：host.go `deliverOne` 短路 ruler 查询 → G-B 红（3688/3715，held=[]）→ 还原 → 绿。两红两绿 + 两次还原凭据见 `evidence/F3-redgreen.txt`
- [x] F4 归档 + 导览（留主代理授权，本单不做）—— 授权已到：用户 2026-10-26 明示执行归档。归档：`openspec archive delivery-review-plugins` exit 0 → `openspec/changes/archive/2026-10-26-delivery-review-plugins/`（11 份：proposal/design/tasks/findings + 两份域 delta + evidence/ 四份）。主规格合并：一级 `orchestration` delta 由 CLI 自身并入（+3 requirement：麻雀先行/症状回滚即红/同文件簇串行）；`persistent-event-loop` 的 MODIFIED 与主规格逐字一致（「投递前审核是投递的必要条件」实施期 2.3 已直接写入），合并后该文件 diff 0 行、无重复场景。导览：麻雀 1.0 红证 → `evidence/1.0-red.txt`；契约零改动 → `evidence/F1-contract.txt`；九项门禁 → `evidence/F2-gates.txt`；G-A/G-B 回滚即红 → `evidence/F3-redgreen.txt`（HEAD `0cf8438` + 三文件 sha256 基线）。run-id 逐字符核对：`0cf8438`（证据 HEAD）与 `28aa080`（1.0 红证基线 HEAD，脚手架为工作树未提交件）均以 `git log -1 --format='%s' <sha>` 逐字解析。复验：`openspec validate --specs --strict` → 130 passed/0 failed；`bash scripts/check-openspec.sh` → exit 0；`bash scripts/lint.sh` → exit 0

## 粒度统计

4 组 8 项；麻雀单 1（1.0）、W1 三单（1.1/1.2 相邻文件簇串行、1.3 依赖二者）、W2 三单、F 四笔。
