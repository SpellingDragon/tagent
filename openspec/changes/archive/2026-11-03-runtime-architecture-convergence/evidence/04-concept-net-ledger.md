# 概念净账（4.5 发布物，r138）

冻结判据：公共概念面相对基线 dfdfa72e 净减少。本账为一次性证明 + 常驻棘轮的双证。

## 终值（概念门实测，r137 快照）

| 指标 | 基线 | 终值 | 判定 |
|---|---|---|---|
| domain_surface.removed | 0 | **10** | 净减成立 |
| domain_surface.new | 0 | 1（ConfigChange，绑定 fact→config-hot-reload；基线推进后为 0） | 收敛期唯一新增，已绑定 |
| domain_surface.renames | 0 | **0** | 无改名冒充收敛 |
| exported_interfaces | 78 | **77** | 公共接口面下降 |

## 移除的十个公共面

| 面 | 域 | 轮次 | 替代 |
|---|---|---|---|
| Writers | runtime/agent | S4（r71） | Loop 内建 writer 台账，close 经 Begin/Stopped 读 |
| PresentedFace | runtime/agent | S4（r71） | HistoryFace 可选能力断言（SessionPresented） |
| Movement | runtime/task | S4（r71） | registry/store 私有 atomic 序号 + runMovement 复合围栏 |
| Continuations | tool/subagent | S4（r71）/m-2（r87） | 包内 continuationTable + NewTool opaque 共享 |
| Channel / ChannelRow / Change / Dimension / Router | runtime/state | W2b-4（r121） | 通道唯一=config_change 事实链（r110 裁定） |
| ModelReplacement（事实族） | api/event | W2b-3（r123） | config_change dimension=model（MetaKeyModel* 词汇保留） |

## 常驻棘轮（r137）

golden/runtime/architecture.json::concept_ratchet 冻结 \`removed≥10 / new≤1 / interfaces≤77\`（基线推进后收紧 new≤0）。
概念门每次运行核 Floor，越线具名红（变异证刃：floor+1 → FAILED）。

## 行为门对照

三关（不丢消息/不谎报收尾/不双驱）证据矩阵见 ORCHESTRATION r90/F3 表；LOC 诚实拆账（生产净增、构成=新语义实现）见 r76。
