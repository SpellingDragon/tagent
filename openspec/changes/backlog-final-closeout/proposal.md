## Why

tagent 经 2026-09-05~07 三天交付(feedback 闭环/审批/goal/降级/git-native 自进化)+独立评审修复后,处于干净基线;但**全部在案遗留项**散处 LEDGER 与答案文档 §四——维护者裁决(2026-09-07):「重编所有项为一份完整变更,这一次要落地干净」,即一次性清空 backlog,不留零散尾巴。

遗留项五大类(全部有案可查):
1. **R1-R5 工程缺口**(独立全面评审裁定,五轴轴1「写入无消费」型):R1 feedback 默认消费面(冥想不回顾负反馈)/R2 诊断快照零消费方/R3 溢出票据全文不可取回/R4 hint counts 重启丢失未声明/R5 example 未装配审批直投通道;
2. **evals 体系**(行业评析点破的生产生死线,答案文档 §四 P2a):无组件级行为 Eval/无 Bad Case 资产化;
3. **D5 M1-M4**(协作与质量门,需按 git-native 后语义重写——原 bundle 载体设计已退役):handoff 契约/ReviewGate critic/RL 反馈通道;
4. **发布工程**:首 version tag(前置全就绪,§7.8 遗留);
5. **文档零漂移收尾**:全仓 grep 复核+wiki 交叉引用。

## What Changes

### 一、R1-R5 工程缺口(§1)

- **R1**:meditation.md §1 回顾清单增「含负反馈事件(task 失败/用户不满)」——零代码,feedback 落库即进反思素材(闭环:负反馈→冥想→改进);
- **R2**:rl HTTP 增 `GET /diagnostics`(DiagnosticsSnapshot JSON 输出)——诊断快照获得消费面;
- **R3**:溢出落盘时同步登记轻量事件(路径+首行摘要,EventSummary 带路径)使 recall 可达,agent 可凭票据找到溢出文件;
- **R4**:platform wiki reliability 节注明「hintTracker counts/recent=会话态,重启重积累(接受丢失)」;
- **R5**:examples/wechat-bot 装配 ApprovalChannel 直投(实现 Deliver→SendTextToUser),审批请求不依赖 agent 转述;README 装配指南一段。

### 二、evals 体系(§2,D4 精简版)

- **`evals/` 一等公民目录**:suites 声明(YAML)+runner(go test 挂载);
- **组件 Eval 两项**(先立骨架后扩):①票据可召回率(压缩后卡片 key 经 recall 取回原文的成功率——记忆命脉的量化)②工具选择正确率(给定场景断言期望工具调用——用既有 mock model 注入);
- **Bad Case 资产化**:tests/README 记录的真实教训(hex 断裂/静默存活)转为 evals 回归用例。

### 三、D5 git-native 语义重写(§3)

- **M1 handoff 契约 ratchet**:子 Agent 交接的结构化 schema(任务描述/上下文摘要/交付物格式/验收标准)——纯 prompt 层契约+回归 eval 守护(防契约漂移),不做运行时校验器(最小机制);
- **M2 ReviewGate critic**:冥想产物的自检步骤(meditation.md §4 已有雏形,强化为「产物须过 critic 自检清单才登记」);
- **M3-M4 RL 反馈通道**:POST /feedback 已有;补 long-poll 端点(GET /feedback/wait,AReaL 侧拉取待评分事件)+TurnTracker(窗口内 turn 计数入 evidence)。

### 四、发布工程(§4)

- 首 tag `v0.1.0`:CHANGELOG 核对(git-native 返工入列)→ tag+push;
- README「开发」节补 evals 运行命令。

### 五、收尾(§5)

- 全仓 grep 复核(bundle/发布道/propose 残留=仅退役声明);LEDGER 记账;门禁全绿。

## Capabilities

### New Capabilities

- `evals-harness`:组件级行为评估——suites 声明/票据可召回率 eval/工具选择 eval/Bad Case 回归资产化。

### Modified Capabilities

- `git-native-refine`(增):溢出取回面(R3 使 recall 可达);审批直投装配(R5)。
- `self-improvement-meditation`(增):负反馈回顾(R1)+critic 自检清单(M2)。
- `rl-feedback`(新名,原 D5 M4 语义):long-poll 反馈通道+TurnTracker。

## Impact

- 代码:rl/http_api(+diagnostics+long-poll)/agent/output_overflow(登记事件)/examples(直投)/evals/(新目录);
- 文档:meditation.md/platform wiki/README/tests README;
- 发布:首 tag;
- 无破坏性变更(全部增量);evals 骨架先行,深度评估集留扩展点。
