# learning-core-wiring：训练数据核心能力接线＋模型替换事件化

## 裁定（用户）
训练数据友好是核心能力——充分接线。运行中换模型应当随事件进行，配置变更只是换模型事件的一种来源。

## 现状
- Recorder：已接生产（loop.go 三处：episode/decision/attempt）
- Capture：完整实现零接线（请求视图固定/尝试准入/封印——S09 承诺）
- ModelLease：完整实现零接线（版本租约：请求中不换/重建→发布→安装——S10 承诺）
- SFT 组装：已删——需按链架构重设计（链是唯一真相，样本是链的导出）
- S10 合同：三成真（episode split）七成死（SFT 描述旧世界）

## 设计

### 模型替换＝事件（与三挂点一致）
- 新事实类型 `model.replacement`：agent/旧版本/新版本/来源戳（config 等）/结果
- 来源（生产者）：配置指纹变化（config-hot-reload 面）产生 replacement 事实；其他来源未来扩展
- 消费（ModelLease 事件化）：lease 以 PostCommitObserver 入流——ObserveCommit 见 replacement 事实→Replace（重建→发布→安装；失败不装＝lease 既有语义）
- loop 借版本：请求开始从 lease 借 current（请求中不换）

### Capture 接线（请求观测）
- loop 请求开始：Capture.Snapshot（固定模型/参数/可见面）
- 尝试产出：Capture.Record（有界准入）
- 回合封印：Seal（manifest 落盘 attempts.jsonl）

### SFT 导出重设计（链→样本）
- 从事实链组装：episode 分组→轮次序列→样本行（输入=投影呈现、标签=assistant 产出）
- 导出器消费链（读事实），不侵入生产路径
- S10 重写：episode split＋capture fidelity＋replacement 生命周期（全真实能力）

## 协议（冻结）
- 事件：event.ModelReplacementType="model.replacement"；Meta[trigger_source]=来源（"config"/…）
- lease 消费：ModelLease 挂 PostCommitObserver（与冥想/AgeFold 同端口模式）
- Capture 接线：loop 三点（Snapshot/Record/Seal）
- 导出：rl.ChainExporter（FromChain(ctx, chain, opts) → SFT 行流）

## 验收
- 正锚：replacement 事实（配置变来源）→ lease 装新版本 → 下一请求借到新版本
- 正锚：capture 三点在生产路径产生 attempts.jsonl＋manifest
- 正锚：ChainExporter 从链组装出非空样本（episode 分组正确）
- 负锚：replacement 失败（新模型构造失败）→ 不装 → 请求继续借旧版本
- S10 重写后锚全存在＋golden match＋lint ok
