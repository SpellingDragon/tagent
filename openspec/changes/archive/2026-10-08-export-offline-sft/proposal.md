# O6 授权离线导出与SFT样本闭环

## Why

采集完成不等于训练可用。现有转换器用role/content平文拼接且忽略空正文toolcall；本域交付一个明确的离线消费者，证明工具调用、反馈关联和loss mask不丢失语义。

## What Changes
- 新增显式分区授权的只读事实/反馈快照导出，不开新HTTP服务。
- v2 strict工具感知SFT转换、manifest/拒绝清单、会话级train/test隔离与脱敏策略。
- 真实模型在本地运行后，用本地tokenizer验证样本；legacy入口保留并明确不等价strict。

## Capabilities
- 新增offline-training-export；既有RL prompt-only不冒充在线RL训练，本阶段不做模型权重训练。

## 边界与依赖
- 父docs-objective-review；接口常数D14-S3/S4/S5；O6.1–O6.5可用v2 fixture独立开发，O6.6真实集成依赖O5.6。
- 事实只从调用者提供的MemoryStore读取；授权空集拒绝；被TTL清理或无call_id时如实缺失，禁止新增保留表或改原文。
- 独占rl/training_export.go及对应测试、scripts/convert_trajectories.py和新unittest；scripts验收工具、tests集成与文档由编排者单写。
- 禁止：强制OTel、通用Metadata索引、自动下载权重、执行remote tokenizer代码、发送生产私密历史、在线RL/AReaL改造。
