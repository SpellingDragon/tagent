# O6 设计

## 启动门与消费者
首个消费者是离线工具使用SFT数据集，不是policy gradient运行器。现有transformers/datasets在转换时惰性加载；单测使用小型确定tokenizer替身，正式验收必须额外用本地真实tokenizer。I0核实其apply_chat_template、tools及assistant mask能力，记录版本与template hash；不能从在线文档或旧环境猜。缺资产时阻断本域真实验收，不暗中联网下载。

## 授权导出
新增rl.ExportTrainingFacts(ctx, store, options, writer)窄库接口：options包含显式partition允许表、时间范围/cutoff、分页大小（默认200、上限1000）、可选call_id筛选。空授权拒绝；先按授权分区分页QueryEvents，再GetEvent读rawMetadata；父键读取前从其身份核验分区，读取后再次核验。不得把无参查询变全库扫描，不打开第二writer或读生产KV文件绕锁。
输出JSONL保留完整FullEvent副本与parent_key，manifest写cutoff、分区、来源摘要、读取完整性。feedback→parent→Metadata.call_id精确join；相同关联出现冲突标ambiguous，父不存在标expired_or_missing（不猜原因）。task级反馈无精确call关联时单独保留，不按成功/失败自动分摊reward。多条反馈保留数组。快照是只读导出物，不能用作生产重放真源。

## strict转换
在既有脚本新增 `--strict --events <facts.jsonl> --manifest <capture-manifest> --format jsonl|hf --train-ratio 0.9 --split-seed 0`；输出必须不存在或为空，禁止覆盖既有数据集。无strict保留legacy行为并打印格式/质量警示。
strict读v2采集，校验封账摘要、call唯一性、response终态和必需tools；每条拒绝记录source/call_id/reason，不静默skip。按SDK消息角色、tool_call_id/参数原串、tool结果建立工具回环；空正文assistant toolcall是有效目标；作为本次预测目标的末条assistant允许其工具结果尚未发生，不能强求未来结果而丢掉动作样本；只有历史中断裂的工具配对才拒绝。多模态不支持/不完整流/未知归属/非法JSON分别拒绝，不抹部件降格文本。
整体调用本地tokenizer.apply_chat_template，tools使用声明schema。只对目标assistant（正文或toolcall）可靠mask范围计loss，历史assistant和system/user/tool全0；labels在mask0处为-100。优先模板generation mask；定位目标末轮须以完整token序列及可验证边界为依据，禁止分别encode后假设前缀相同；无法证明则template_mask_unsupported拒绝。
保存input_ids、attention_mask、loss_mask、labels；另存sample_id、source_call_ids/event_keys、feedback原值、root_session、split、tokenizer/template hash及隐私策略版本。train/test按 `(capture_namespace,root_session_id)` 哈希分组，子agent跟随根组；无可信根组拒绝strict分割。未发生反馈的样本明确unrated，不把0当缺省reward。

## 隐私与完整性
capture文件无凭据但可能含用户内容，导出必须显式选择 `--content-policy synthetic|retain|redact`；验收只能synthetic。redact加载本地规则，作用于messages、tool参数/结果和feedback；不提供规则则拒绝，不宣称通用PII识别。脱敏前后digest保留，互相关联字段不可盲改；被改坏的tool JSON拒绝。
不自动访问外部文件/URL票据。仅训练模型已见文本无需世界快照；需要外部内容的用例标requires_external_asset并拒绝strict可重演声明。不改memory TTL，也不复制未经授权分区。

## 验收器与格式
新增scripts/verify_runtime_acceptance.py及unittest：检查必需Go测试PASS、非SKIP/零调用、run_id、capture统计、export摘要、sample/mask非空、session组无交叉。此脚本是长期产品验证工具，不硬编码change名/路径。HF格式显式要求datasets可用，否则失败；JSONL格式显式可用，不再用静默格式fallback通过strict验收。
真实验收至少两个root session，各真实调用nonce工具并回复；为一个已存输出绑定反馈，导出→模板→样本→构造一个padding collator batch，核对labels/mask和非空目标token。可用API模型作为teacher、本地tokenizer为显式student，manifest区分二者，不能冒称相同权重。训练更新和收益不在本轮。

## 文件、文档与回滚
新增Go导出文件是现有rl能力的可调用消费入口，不新增顶层包/服务；Python测试放scripts/test_convert_trajectories.py。tests/llm_contract_test.go与testutil真实模式由编排者写。文档进rl-architecture及tests/README，解释legacy/strict、授权、完整性与资产前置。关闭新选项可继续legacy；旧文件不迁移/覆盖。
