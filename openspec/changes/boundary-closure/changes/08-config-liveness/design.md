# 配置活线：链驱动技术要点

## 关键决策

### D1 声明事实的载荷形态

- **类型与归属（单源）**：类型字符串以 `api/config` 既有 `Declaration.FactType()` 为**唯一来源**（assembly 只读引用，不自立常量——杜绝双源；零 `api/**` 改动）。
- **身份**：族 4 既有规范指纹（`Declaration.FactKey()`）；同内容重载同一身份，事实键幂等——同声明重载不双发。
- **载荷**：Content＝规范声明序列化（规范 JSON）；大正文不复制进事件，按既有内容寻址文件事实模式引用（声明中的文件引用仍指路径/内容键，正文由既有事实族承载）。
- **生产点**：Boot 与每次 Reload 的装载成功后、发布候选构造前提交；提交失败＝本次变更失败（不发布、回执失败）。

### D2 发布入口消费化

- **唯一入口**：一切变更（手工 /reload、watcher、重启对账）走同一个 `Run.Publish`（`state.Publisher`）；该入口的候选构造、基线复检、治理裁决、发布记录提交、Hold 换代**原样保留**。
- **输入换源**：Reload 不再以"内存 previous 声明 + 新文件"为候选来源，而是：装载文件→提交声明事实→**从链上最新声明事实**重建 `config.Declaration`→按既有路径构造候选→既有 `Publish`。
- **发现链上声明**：按族 4 身份（指纹）与声明类型前缀读取最新一条；读取失败＝具名失败（不猜测、不回退内存代）。
- **零绕过**：不得裸读文件直接生效、不得写内存 setter 代替发布；文件只提供"待提交的声明输入"。

### D3 失败事实的 result 枚举

发布尝试统一落一条带 result 的链上事实（成功与失败同族，读者只读这一条）：

| result | 触发点 | 语义 |
|---|---|---|
| `published` | 发布入口换代成功且安装完成 | 新代生效 |
| `unchanged` | 候选指纹与生效代相同 | 零发布（重启一致即此态） |
| `load-failed` | 严格解码/声明构造失败 | 运行中绑定不变，失败可回读 |
| `governance-rejected` | 基线复检/治理裁决拒绝 | 绑定不变，具名原因 |
| `install-failed` | 新代已发布但模型重建/安装失败 | **不得回成功**；记录已发布代号与失败阶段 |

- 回执 `/reload` 的成败**由链上最新一条发布事实决定**，不以内存状态自证；`install-failed` 必须返回失败与具名原因。
- 失败事实只记"事实与原因"，不写面向用户的失败话术（话术归既有 failure-notice/模型表达）。

### D4 watcher 触发链

- **形态**：hash 轮询（500ms–1s；go.mod 零新增，不引入 fsnotify）；只监听 `TAGENT_CONFIG` 指向的配置源文件。
- **触发判据**：内容 hash 变更才触发；mtime-only 触碰不触发；防抖窗口 1s。
- **链路**：文件 hash 变更→严格装载→提交声明事实→唯一发布入口→落发布事实；**一个入口，无第二条生效路径**。
- **install-failed 同步落账**：发布流程内安装步骤同步判定（发布记录先落 pending-install→同步安装→结果事实 published|install-failed 落链→回执返回）——不依赖租约观察者的异步路径；r17 缺陷（回执先于安装真相）的修复点即在此步。
- **坏配置**：装载失败时运行中绑定不变，落 `load-failed` 事实并记日志；watcher 继续运行（不退出、不清空基线）。
- **无开关**：本域按新设计恒开，不设实验开关（P6 统一开关机制不含 08）。

### D5 重启对账规则

- Boot 读文件并算规范指纹；查链上最新声明事实指纹。
- **一致**→静默：零发布、不前进绑定代、不落新的发布事实（对账不产生幽灵发布）。
- **不一致且文件较新**（文件 mtime 晚于最新声明事实时刻）→自动走唯一发布入口（与手工 /reload 同一条链）。
- **不一致且链较新**（回滚后等）→以链为准，保留运行绑定为链上声明，具名记录"源文件滞后"，**不得用陈旧文件覆盖链**。
- 重启后发布记录必须可回读（durable 账），不得只存内存。

### D6 跨维窗口的审计形态

- 发布事实携带：代号（generation）、前后关系（上一代/回滚来源）、各维生效结果（至少：绑定代、模型版本、配置维度是否 applied/held/skipped）。
- 绑定代与模型版本两切换点在同一个发布窗口内的组合**可查询**：按代号读发布事实即可还原该窗口"哪一代绑定 × 哪一版模型 × 哪些维度已生效"。
- 最小实现＝一条窗口事实（发布记录本身）；不新建第二本账、不引入独立审计文件。

### D7 三钉（核心机制不靠自觉）

1. **golden 固化**：`golden/runtime/architecture.json` 登记声明/发布事实类型与唯一入口的 packages+edges（含 assembly→runtime/state 既有边），`python3 scripts/gates/check_runtime_architecture.py --implementation` exit 0。
2. **lint 钉死唯一入口**：静态扫描配置装载与发布调用点——除 Boot、Reload、watcher 触发链外出现 `loadHostDocument` 或第二发布路径即红；特性专属装配路径（`resolve<Feature>` 形态的新增）即红；门带正反样例自测。
3. **CI 执行**：两道门接入 `scripts/gates/preflight.sh`，逐提交跑。

## 文件清单

- `assembly/host_reload.go`：Reload 改为"装载→声明事实→链上最新声明→既有发布入口"；失败 result 与回执读账。
- `assembly/host_document.go`：Boot 对账（文件指纹 vs 链上声明指纹）；声明序列化与严格解码复用。
- `assembly/facts.go`：`config.declaration` 常量与提交/读取辅助；发布事实的链上读回。
- `assembly/commit.go`：`memoryPublications` 替换为链记录器（`PublicationRecorder` 的落链实现）。
- `runtime/state/publish.go`：发布记录落链的接口处——记录载荷扩展为带各维生效结果的窗口事实（新增字段未导出+方法访问，必要时登记 concept budget 基线）；治理/基线/Hold 零改动。
- `examples/wechat-bot/`（main/wechat.go）：watcher 接线（hash 轮询＋防抖＋只监听 `TAGENT_CONFIG`）。
- `examples/wechat-bot/deploy/README.md`：重启要求改热更说明；prompt 行为如实。
- `docs/GLOSSARY.md`：配置变更词条链真词化（文件是编辑界面、链是账、唯一发布入口、回执如实）；`docs/wiki/` 相关页同步。
- `openspec/changes/boundary-closure/changes/08-config-liveness/specs/config-liveness/spec.md`（ADDED）与 `specs/config-hot-reload/spec.md`（MODIFIED「配置热更维度矩阵」）。

## 风险与回退

- **watcher 误触发**：内容 hash 对比＋防抖；mtime-only 负锚锁死。
- **声明事实链增重**：一条声明事实＋一条发布事实/次；大正文走引用，不复制。
- **对账漂移**：文件/链双向比较＋链新不倒退；重启一致零发布负锚。
- **安装失败仍回成功复发**：`install-failed` result 与回执读账是硬锚（当前红），不是"尽力而为"。
- **回退**：本域无实验开关；回退＝整体 revert 本域（发布入口/世代语义未被改写，回退不损坏既有发布链）。
