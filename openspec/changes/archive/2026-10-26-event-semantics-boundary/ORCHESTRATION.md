# 编排文档（派发唯一依据·金字塔）

## 一、结论层

**抵达 DoD 的唯一路径**：00 红证（三症状测试全红）→ 01.1 麻雀（受理→`ok` 使 00.2 转绿）→ 01 余量 → 02 失败告知 → 03 schema → 04 合同与 CI → F3 症状红转绿双证据（回滚即红）→ 归档。

**当前最大风险**：**fake 模型下症状测试可能天然绿**——fake provider 是脚本化的，不会像真模型那样复述回执；**判别力必须来自请求侧断言**（`provider.recorded()` 读模型收到的 tool result/输入材料），不能只靠 outbound 计数：
- G1 红证=旧代码下模型请求的 tool result 含 `accepted task-`（受理回执）；转绿后为 `ok`。
- G2 红证=旧代码下结算通知回合的请求含 progress/受理材料；转绿后不含。outbound==1 为叠加断言（用户视角）。
- G3 红证=旧代码下 provider 500 后 outbound=0；转绿后 outbound 含原始错误。
不得以“fake 模型天然安静”充当红证；请求侧断言在旧代码下必红。

**CI 红窗规避**：症状测试**随各自修复首次提交**（D2 交 G1、D3 交 G2、D4 交 G3），使每个提交 CI 全绿；D1 只在工作区写测试并跑红证入账（不提交）。

**症状测试的会话绑定（第三轮 grill 增量）**：三条症状测试一律用 `rig.drive(chat, …)`（生产入站路径，绑定 fakeChannel）而非独立注入会话——`injected:` 会话无送达绑定，框架直投（G3 的失败告知）与 outbound 断言（G2 的恰一条）在独立会话上必然 no-target，测试将永久无法转绿。独立注入会话留给“不打扰微信”的真机诊断，不进症状测试。

**D3 实施核对项**：摘 `progressNotes` 读路径时须核 `NoticesFrom` 的全部消费者——终态通知若共用同一读面，只摘 progress 分类不得误伤终态驱动。

## 二、分组层·编排四问

### 次序（就绪队列到孙任务）

| 单号 | 叶 | 前置 | 状态 | 触碰集 |
|---|---|---|---|---|
| D1 | 00.1/00.2/00.3（合单：同文件 smoke_test.go） | 无 | ready | `examples/wechat-bot/smoke_test.go`（**写测试+跑红证入账，不提交**——红窗规避） |
| D2 | 01.1（受理→ok，麻雀转绿并**首次提交 G1 测试**） | D1 红证 | blocked-by-D1 | `runtime/agent/loop.go`、`examples/wechat-bot/smoke_test.go` |
| D3 | 01.2/01.3（合单：同 loop.go/host.go 簇） | D2 | blocked | `host.go`、`runtime/agent/loop.go` |
| D4 | 02.1/02.2/02.3（合单：同 host.go/loop.go 簇，与 D3 串行） | D3（host.go/loop.go 相交） | blocked-by-D3 | `host.go`、`runtime/agent/loop.go`、`api/model/{terminal,observe}.go`、`rl/produce.go`、`examples/wechat-bot/{wechat.go,smoke_errors_test.go,smoke_harness_test.go}` |
| D5 | 03.1/03.2（合单：同 tool.go） | 无（文件簇与 D1–D4 零相交） | **ready，可与 D1 并发** | `tool/exec/tool.go`（+同包测试） |
| D6 | 04.1/04.2/04.3（合单：契约一次改） | D3+D4 行为落地 | blocked | `openspec/specs/persistent-event-loop/spec.md`（经域 delta）、`golden/runtime/scenarios.json`、`.github/workflows/ci.yml`、`scripts/check_runtime_evidence.py`（如需） |
| F1–F4 | 收尾 | D6 | blocked | 编排者直管（F3 用**定点破坏**：admissionText 还原原文→G1 红；send 成功回执还原→G2 红；失败告知分支禁用→G3 红；逐一复绿，三证据入 notes） |

**关键路径**：D1→D2→D3→D4→D6→F3（最长链）。**本轮做不了的**：D3/D4 在 D2 转绿前不派；D6 在 D4 前不派。

### 授权（每单白名单精确到文件）

- D1：仅 `examples/wechat-bot/smoke_test.go`（三函数）+ 允许读 smoke_harness/provider。
- D2：`runtime/agent/loop.go`（admissionText 单点）、`examples/wechat-bot/smoke_test.go`（仅 G1/G2 断言若需同步）。
- D3：`host.go`（send 回执文本）、`runtime/agent/loop.go`（admitNotice+progressNotes 读路径）。
- D4：`host.go`（deliverFresh 失败分支+Turn.Failure 贯通）、`runtime/agent/loop.go`（Cause 贯通）、`api/model/{terminal,observe}.go`、`rl/produce.go`（AttemptError 委托）、`examples/wechat-bot/{wechat.go,smoke_errors_test.go,smoke_harness_test.go}`。
- D5：`tool/exec/tool.go` + 同包测试。
- D6：主规格 delta（经域 specs 合并）、`golden/runtime/scenarios.json`（v1 S15 改写）、`.github/workflows/ci.yml`（独立 symptom job）、`scripts/check_runtime_evidence.py`（如需 workflow 自测同步）。
- **预授权**：白名单内自由。**必须上报**：白名单外任何文件、契约语义变更、0.5 天以上超预算、发现新缺陷族。

### 凭据（五查落点按单核）

- D1：三测试先红（exit≠0 或子断言红）的原始输出入账；若 fake 下不红，伪协议对照组的红证。
- D2：G1（受理后 outbound=0）转绿；admissionText 单测正/负锚。
- D3：`[send_ok]`→`ok` 单测；progress 不进 intake 的负锚（既有 progress 子测试反向改写）。
- D4：G3 转绿（原始错误入 outbound）；`host_error_not_a_model_failure` 负锚；成功回合零失败话术负锚；日志 terminal=failed 带 error= 的断言。
- D5：新描述含枚举/格式/示例的断言；旧抽象短语消失断言。
- D6：主规格 strict；v1 S15 改写后自测绿；symptom CI job 存在性 + workflow 自测。
- **必亲验数字**：三症状测试的 outbound 计数（0/1/含错误文本）、红转绿两次运行、合同 29 父/143 子（P-06 口径）。

### 释放（锁与等待表）

- 长跑门：仅短测（<2min/单），无长跑锁；simoke smoke 台架可并行（各自 tempdir）。
- D2 转绿即释放 D3/D4；D4 完成即释放 D6。无跨日资源。

## 三、明细层

- 合单判据：同文件簇（D1 三叶同 smoke_test.go；D3 两叶同 loop.go；D4 三叶同 host.go/loop.go；D6 三叶同契约面）→ 按 R21 热点合单，不拆。
- 预算上限：D1–D6 各 ≤1 天；总 ≤5 天。F 直管 ≤1 天。
- 档位欠账：无（不涉档位）；真实路径欠账：症状 3 以 smoke 确定性失败注入为真实路径替身（真机 provider 抖动不可控），F3 另附真机复验记录（用户真机或 20:15:55 既有红证）。
- 不建 task-graph.json（P-05 裁定，14 叶规模不值当）。

## 四、台账层

（每单完成按 ①–⑥ 步记账：勾选真实性/产物盘点/数字溯源/遗漏检测/生产消费者实测/回滚即红。）

### F 收口（单F）—— 2026-10-26（F1/F2/F3 完；F4 未动，待主代理授权）

- **① 勾选真实性**：`tasks.md` F1/F2/F3 已勾并附凭据摘要；F4 保持未勾（归档留授权）。
- **② 产物盘点**：`evidence/F1-contract-zero-diff.txt`（70 行）、`evidence/F2-gates.txt`（168 行）、`evidence/F3-symptom-redgreen.txt`（166 行）；原始日志在 `$TMPDIR/esb-final/`（lint / root-triple / wechat-triple / clihost-triple / short×10 / long×6 / mutations / bundle-check / unittest / openspec-gates / symptom-job / f3-a*、b*、c*、final）。
- **③ 数字溯源**：v2 合同 diff 0 字节；v1 numstat 3/3 且仅 S15；主规格 28/0；short 十轮 10 个互异 run_id、每轮 28 test_id、全 exit 0（28m43s）；long 六片（seed 1/7/42 × window 256/32768）全 verdict=match，inputs 7200 / restarts 720；mutations gate.ok 11/11 killed（control 树先绿）；bundle 17 份 evidence（short 10 + long 6 + mutation 1）ok=true、event_log_packages 9 / event_log_tests 180；症状 job 9 pass/0 skip；unittest 44 tests OK；`check-openspec.sh` 0 + `validate --strict` 0。
- **④ 遗漏检测**：R29 备案（`failed_turn_with_body` 结构不可达）与 recall 既有红均不在 F2 门禁面（三模块 `-short`）；`scripts/doc_budget.json` 为工作树未提交改动（P-08），故 lint 绿是「工作树绿」非「HEAD 绿」；端口 8089 遗留进程 pid 49723 非本轮启动（冒烟台架 `TAGENT_HTTP_PORT=0` 走临时端口，未冲突）。
- **⑤ 生产消费者实测**：症状 job 走 `rig.drive` 生产入站路径（非测试内手工接线），三症状 9 pass；bundle 的 `productVerified` 仍为 null（bundle 只核证据自洽，不冒充产品验证）。
- **⑥ 回滚即红**：G1 exit1（smoke_test.go:3254/3257）；G2 exit1（**非受理面**红锚：结算正文投递两次 → 3337/3357；B3 受理面 3327/3337 仅作耦合旁证）；G3 exit1（3394，告知 0 条）。原 b 破坏面（send 回执还原）与 B2（去 progress 过滤）实测 G2 **exit0 无判别力**，已如实记录并改用非受理面 B4，未伪造红。每次还原均 blob==HEAD + sha256 + `git diff` 0 字节，复绿 exit0。
- **待裁**：F4 归档（域→证据导览、run-id 逐字符核对）留主代理授权；`ORCHESTRATION` 明细层「F3 另附真机复验记录」不在本次 DoD 四项（三症状绿 / 主规格 strict / CI job / lint）内，未做，如实登记。
