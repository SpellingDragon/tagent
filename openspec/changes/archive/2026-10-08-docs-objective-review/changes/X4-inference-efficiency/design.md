# Design: X4 推理效率探索

## 主读路径

`agent/context_manager.go`（BuildInvocation/assembleRequest/面板注入位）、`agent/compress/`（context_compressor/smart_compress/deterministicLevel）、`event/types.go` EstimateTokens 与 compress 侧 CharsPerToken、`config/config.go` 预算线、`tool/recall/` 与 `memory/engine/`（召回路径）、`agent/agent.go`（工具声明装配）。

## 核验假设详单

| # | 假设（来源） | 核验方法 |
|---|---|---|
| H1 | token 估值双常数并存（event /3 vs compress 2.0 chars/token）且消费方可能混用（D2） | 全 grep 估值调用点，列消费方-常数对照表 |
| H2 | 估值器 ~15% 低估 + tools-schema 开销未入预算线（D1） | 读预算判定处（Compress L361-372）确认估值输入集是否含 schema/历史/系统提示 |
| H3 | 面板每回合重渲染且含 wall-clock，注入位置切割其后缓存（D3） | 读面板注入点在消息序列的确切位置与内容构成 |
| H4 | 压缩触发唯一（0.8 预算线无第二触发）（D3） | grep 触发路径复核 |
| H5 | recall 关键词腿按时间新近排序（非相关度）（D1） | 读 engine Retrieve 排序分支 |
| H6 | 前缀稳定承诺的收益边界= system+声明区+整理间冻结段，压缩轮与面板后缀不受保（D2/D3 合成） | 沿装配链画出"稳定段/可变段"分段图 |
| H7 | 冥想/综述 LLM 调用位于推理主路径外（低频叠加）（D4 旁证） | 核对综述调用点的触发频率与阻塞面 |

## 现有验证命令候选

`go test ./evals/ -count=1`（票据召回率等现有行为证据）；`go test ./agent/compress/ -count=1`。

## 风险与回退

效率结论无实测数据支撑时一律标"静态推演"；不得以推演数字冒充测量值。
