# rl — RL 训练桥(tagent 自有 HTTP 协议)

本包是 tagent 与**任意外部 RL/训练框架**之间的桥。协议是 tagent 自有的 HTTP 契约
(AReaL 是第一个消费者,经其 Python adapter 适配)——**换训练框架(veRL/自研)时
tagent 侧零改动,只需为新框架写一个 adapter**(对照下表实现客户端即可)。

## 端点契约

| 端点 | 方向 | 语义 |
|---|---|---|
| `POST /task` | 框架→tagent | 提交消息批次(messages);body 含 `llm_base_url` 时热切模型(SwappableModel) |
| `GET /healthz` | 框架→tagent | 存活探测 |
| `POST /feedback` | 框架→tagent | 绑定外部 verdict 到产出事件(hex event_key + verdict;parent 缺失 404/部分成功 201+warning) |
| `GET /feedback/wait?timeout=N` | 框架→tagent | long-poll 新 feedback 通知(≤30s;内存队列,重启清空=接受丢失) |
| `GET /diagnostics` | 框架→tagent | 诊断快照 JSON(向量健康/存储规模/wal_quarantined) |

## 轨迹采集(框架拉取侧)

- `TrajectoryRecorder`: LLM 调用轨迹(JSONL;含 trace 关联字段)——文件由训练侧读取;
- `SwappableModel`: 模型热切换(配合 POST /task 的 llm_base_url);
- `agent_loop.go`: 框架内嵌 loop(不走 HTTP 时的进程内替代)。

## 替换性裁定(2026-09-08 设计审计)

AReaL 耦合为零代码级(仅注释提及)——协议面即缝。新框架接入 = 适配器实现上表
+ 轨迹文件消费,不触本包。
