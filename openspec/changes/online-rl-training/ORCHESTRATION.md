# online-rl-training 编排文档（简版·立档轮）

## 〇 结论

**本变更被 `post-training-data-loop` M2 启动门阻塞，当前只立档、不实施、无就绪派发单。** 唯一路径是：M0-M2 完成并过 SFT/DPO 验证 → 核验 0.1 启动门 → W0 契约冻结 → W1 受控服务联调 → W2 adapter → W3 小规模单角色 RL → F 收尾。当前最大风险不是实现难度，而是**受控推理服务是否回传真实 token IDs 与逐 token logprob**（design D5 首行"token 账本不可得"）——它决定 W1 是否成立，须在 W1 首叶（tasks 2.1 能力探针）先验，不得等到 W3 才发现。

## 一 启动门（四条件，缺一不派发）

| # | 条件 | 判定凭据 | 当前状态 |
|---|---|---|---|
| G1 | `post-training-data-loop` M0-M2 完成 | 该变更 `openspec show --json` 的 artifact 与任务状态 | 未满足（立档时点：仅 proposal、0 任务） |
| G2 | SFT 输入/掩码/held out 成功率实测 | SFT 验收 run-id + 指标 | 未满足 |
| G3 | DPO 数据管线启用且产出合格偏好对 | DPO 导出 run-id + 拒收计数 | 未满足 |
| G4 | 受控推理服务与预算获用户授权 | 用户明示记录（服务、预算、数据边界） | 未满足 |

G1–G3 的顺序依据 design D1（SFT → DPO → RL 不可跳）；G4 是 W1 的硬门，未授权时 W1 起全部叶子保持 `design-blocked`，**不得**以本地替身冒充在线证据。

## 二 待裁项（须用户裁定，编排者不自决）

1. **受控推理服务选型与预算**：候选需具备 prompt/response token IDs、逐 token logprob、策略版本与 tokenizer/template 摘要回传能力；预算上限与数据边界一并裁定。裁定前 W1 不启动。
2. **RL 算法选型**：算法决定 `required_logprobs`、`max_policy_staleness` 与 `loss_normalization` 的合法取值；在算法未定时，W0 只能冻结**字段与拒收语义**，不得冻结算法取值。
3. **（对账点，非待裁）`offline-training-export` reader 面**：M0-M2 的 M1 导出面定稿后对账——若在线 RL 记录需经该 reader 导出，则补 `MODIFIED` delta；若全部落在 `trainer-adapter` 导出面内，则不改。对账结论回写 proposal 的 Capabilities 与 design D6。

## 三 编排四问（当前态）

- **次序**：就绪面为空（受 G1-G4 阻塞）。解锁顺序：0.1 → W0（可与授权并行，纯契约）→ W1（需 G4）→ W2（依赖 W0）→ W3（依赖 W1+W2）→ F。W1 与 W2 可并行，触碰集不相交（`rl/` 账本面 vs `rl/trainer/`）。
- **授权**：本轮写入白名单仅 `openspec/changes/online-rl-training/**`；产品/测试代码零写入；`openspec/specs/` 与 `post-training-data-loop/**` 零写入。实施轮白名单待 M2 后按叶细化（预计 `rl/online_rl.go`、`rl/trainer/**` 与对应测试文件）。
- **凭据**：完成度五查按派发单核；每条判据的复跑命令已写在 `tasks.md` 各叶"验证："后；必须亲自复验的数字：token 数 vs `usage`、拒收码数量、每 seed 成功/硬约束/成本三列、隐藏集与训练集交集。
- **释放**：当前无设备、无端口、无配额占用；无资源锁，无等待表。实施轮若占用受控服务或 GPU，先登记释放表再派发。

## 四 明细层（本轮不做）

- 二级域拆分、孙任务粒度、触碰集与合单判据：**全部待 M2 完成后细化**（用户裁定：实施拆分留后续细化轮）。
- 本文件在细化轮补：派发单-孙任务矩阵、预估墙钟（>10min 标长跑）、并发准入（文件簇零相交）、预算上限、真实路径欠账。

## 五 台账层

| 轮次 | 内容 | 凭据摘要 | 计划回写 |
|---|---|---|---|
| R0 | 立档：proposal/design/specs/tasks/ORCHESTRATION | `openspec validate online-rl-training --strict --no-interactive` 通过 | 无 |

后续每轮派发后在此追加：单号、触碰集、完成/拒收、复跑凭据、计划回写项；**无凭据摘要不得写"完成"**。

## 六 边界声明（与 `post-training-data-loop`）

- 该变更负责 M0-M2 数据契约、验收、奖励、SFT/DPO 导出与试采；本变更负责 M3 在线 RL 协议与 adapter，只消费前者产物。
- 该变更 M1 的 capability 声明字段是**契约位**；本变更实现其协议语义，不重复定义、不改其字段。
- 两变更各自只勾自己的 tasks；跨变更就绪以对方产物入集成树并通过核验为准，不以对方 proposal 的措辞为准。
