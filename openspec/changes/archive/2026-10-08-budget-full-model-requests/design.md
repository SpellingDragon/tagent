# O3 设计

## 请求快照与计量
在modelutil实现D14-S1的纯快照函数；本次请求声明排序/深拷贝一次，预算、最终模型请求和O5采集复用。只读声明视图只用于model.Request.Tools；实际执行工具注册表、CallableTool、Close和Iter能力不改。真实框架兼容测试先钉住这一分离，不用通用工具装饰器掩盖能力。

新增RequestBudget分项：system、历史/本轮messages、tool声明、toolcall参数/ID、reasoning、ContentParts、动态通知/任务板、协议估计；输出limit单列，不重新定义现有max_tokens为provider总窗口。文本与结构JSON使用现行CharsPerToken口径加固定开销，完整参数按实际字节/字符统计；图片/音频/视频只在已有可用元数据能估计时计算，否则列unknown，不填零或假报精确。未知媒体保留既有可用性并暴露预算覆盖不完整，不新增默认拒绝所有多模态。provider总窗口未知时不虚构“输出预留后的安全余量”。

## 唯一装配与压缩
1. 抽出同一request assembly职责，先收集system、冻结声明、板和待发恢复提示；这些固定/动态开销计入本轮预算。仍由ContextCompressor选择/折叠历史。
2. 一次读取五热参数快照，外层触发和内层目标使用同一完整组。输入上限保持max_tokens、触发阈值保持compress_threshold；传入可压缩内容预算时显式区分零与“缺省”，避免负/零预算回退成旧大值。
3. 固定可估部分已超输入上限时返回具名budget_exceeded，不能删system/工具声明硬过门。最终executionGate只校验、保留凭据检查和iterator惰性，不二次压缩。
4. 恢复提示先预留/读副本，只有真正发起模型调用才消费；拒发/未迭代不能吞提示。模型入口发生新增字段变化须重新计量并明确拒绝不一致，不悄悄发超预算请求。

## 同步摘要限时
新增compress.summary_timeout_seconds：0采用5秒、负数拒绝、正值≤120秒；作为摘要结构配置走FP，不新建第四热通道。一轮真折叠的卡片浓缩+叙事共用一个子context时限。modelutil摘要收流监听ctx.Done，nil stream为可判定失败；provider创建阶段仍须遵守ctx取消合同，不用无限goroutine绕过。
超时/摘要错误按既有票据层与旧叙事降级；父ctx取消则停止当轮，不继续主模型调用。不会在后台晚到时覆盖当前投影或持久摘要；票据校验、先写新compaction后supersede顺序保持。

## 架构调整与回滚
允许局部抽出agent/request_assembly.go及modelutil/request_snapshot.go（生产需要时才建），所有调用改到唯一入口，不保留平行路径。预算改进默认作用于已知文本/工具项，旧配置可加载；摘要timeout新字段有文档。无磁盘变化。回滚不会删除事实或采集文件。

## 验证
新增测试保护完整预算输入、schema热变、长args、未知媒体、固定开销超限、内外同热参、父取消、摘要超时/空流、票据保持、iterator未消费。真实模型使用合成大工具schema/长参数和压缩历史，记录实际usage与估计偏差，不把0.846历史统计当本轮值。基准比较构造/计量/摘要总延迟与allocs，不承诺缓存命中一定免费。
文档由编排者写入compression-and-telemetry、memory-architecture压缩节、配置参考及tests/README。
