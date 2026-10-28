## MODIFIED Requirements

### Requirement: 配置热更维度矩阵

每个子 agent 配置维度 SHALL 归属且仅归属一条热通道：**FP 代际面**（视图变更，下回合生效、在途钉定）、**SRC 源拉取面**（参数变更，下一次消费即生效）、**FILE 懒读面**（文件即真源、消费边界按 mtime 懒读，如 prompt 正文），或明示的 **RESTART 拒**（目录/端点类不可迁移资源，拒绝即正确行为）。维度归属与消费点 SHALL 以矩阵形式登记，每行 SHALL 携带代码行号或测试名证据；**携带而消费点未读＝假热更**，SHALL 视为缺陷而非"部分支持"。（三通道划分系域 01 执行期情报 C1 回写：初稿"FP/源"二分与既有 FILE 懒读通道冲突，不得为凑二分法拆毁既有机制）

`mcp_servers` 声明 SHALL 归 **FP 代际面**，MUST NOT 归 FILE 懒读面：其进声明指纹（`config.Fingerprint` 的 `mcp_servers` 键），真实改动必换代，新代发布时注册表重绑到新值；指纹相同的重复发布 SHALL 判为 unchanged 而不开新代。

工具与插件声明 SHALL 归 **RESTART 拒**：工具实例（`agents.<id>.tools` 所声明的 send/recall/knowledge/spec/file 等）只在 Boot 构造一次；插件声明在本机文档方言里没有 `plugins` 键，其落点=`agents.<id>.meditation` 声明的冥想计划（reflection 插件）与 Boot 期的宿主附加，同属 Boot 固定面。热更没有重建通路，故 SHALL 具名拒绝而非换代后静默沿用旧实例。

FILE 懒读面 SHALL 在**请求边界**读取并固化实际版本：文件变化直接生效，MUST NOT 等待 register、后验评估或结构前置质量门；在途请求 MUST NOT 被半途拼接新文件，后续请求 SHALL 读到新版本。FILE 变化 MUST NOT 触发结构 generation 换代。

RESTART 拒 SHALL 以具名错误拒绝并要求重启（`ErrHostReloadRestartRequired`，`Unwrap` 到 `ErrHostReloadStructural`），拒绝 MUST 发生在 natives 复用与文档换入之前：被拒的 reload MUST NOT 改动生效文档、MUST NOT 残留半发布的代、MUST NOT 推进 revision。

#### Scenario: 已登记维度的消费点有契约测

- **WHEN** 矩阵中任一维度标记为 SRC 或 FILE 拉取/懒读通道
- **THEN** 该维度 SHALL 存在断言"消费点读到解析值/重读值"的契约测试，测试名登记于矩阵行

#### Scenario: 新维度接入有唯一路径

- **WHEN** 后续变更新增任一配置维度
- **THEN** 该维度 SHALL 经"FP 子集入列"、"源加字段+消费点读"、"文件懒读"或"明示 RESTART 拒"之一接入，SHALL NOT 新增第五种机制或 push/订阅/广播通道，且矩阵 SHALL 增行登记

#### Scenario: 文件正文变化直接生效

- **WHEN** prompt/skill 文件在两次请求之间被修改
- **THEN** 下一请求边界 SHALL 读取新版本并固化；结构 generation SHALL NOT 变化；在途请求 SHALL 保持其已固化版本

#### Scenario: MCP 服务器声明随新代热生效

- **WHEN** 一次发布改动了 `mcp_servers` 声明（地址/传输/凭据环境变量名）
- **THEN** 该改动 SHALL 进入声明指纹并触发换代，新代发布时 MCP 注册表 SHALL 重绑到新声明
- **AND** 该维度 MUST NOT 被登记为 FILE 懒读面，MUST NOT 出现"改了文件却等某次懒读才生效"的静默形态

#### Scenario: 工具与插件声明越档具名拒绝

- **WHEN** 一次 reload 改动了某 agent 的工具声明或插件声明落点（`tools` / `meditation`）
- **THEN** 该 reload SHALL 返回 `ErrHostReloadRestartRequired` 并在回执中点名被改的 YAML 路径
- **AND** 生效文档与 Boot 期已构造的工具/插件实例 MUST 保持旧态，revision MUST NOT 推进，回执 MUST NOT 声称该变更已生效

### Requirement: 结构变更与文件直接生效分路及错误原因链

结构变更（模型/任务策略/资源引用/memory 与 `mcp_servers` 声明）与文件懒读（prompt/skill）是**两条独立消费路径**：结构变更 SHALL 走 `config.DecodeStrict → config.Fingerprint → Run.Publish → state.Publisher.Publish`（候选装配、基线复检、治理裁决、台账提交后一次切换）；文件变更 SHALL 直接生效。结构性变化 MUST NOT 借文件通道绕开前置检查，文件变化也 MUST NOT 被宣称经过前置质量门。

工具与插件声明 SHALL NOT 按"经前置检查后提交"处理：其消费实例只在 Boot 构造一次，热更没有重建通路，故 reload SHALL 在前置检查处以 `ErrHostReloadRestartRequired` 具名拒绝并要求重启，MUST NOT 以"已提交/已生效"结案——对这类字段宣称可热更即为该 Requirement 要消灭的静默 applied 形态。

`mcp_servers` 声明 SHALL 走结构路径的 FP 代际热面（进指纹、换代时重绑注册表），MUST NOT 被归入文件懒读面；其真实改动 SHALL 换代热生效，指纹相同 SHALL 判 unchanged。

结构路径失败时系统 SHALL 保留**错误原因链**（源文件 → 归一行 → 具名原因），使失败可定位；MUST NOT 以笼统「reload 失败」吞掉原因。

#### Scenario: 两条路径互不绕门

- **WHEN** 同时发生一次 prompt 文件修改与一次工具声明修改
- **THEN** 文件修改直接生效（不进前置门），工具声明修改 SHALL 被具名拒绝并要求重启（`ErrHostReloadRestartRequired`）；两者作用范围 MUST NOT 混淆
- **AND** 工具声明修改 MUST NOT 以"经前置检查后已提交"结案：生效文档 MUST 保持旧态

#### Scenario: MCP 声明走代际面而非文件通道

- **WHEN** 一次 reload 只改动了 `mcp_servers` 声明
- **THEN** 该改动 SHALL 经 `config.Fingerprint → Run.Publish` 换代，并在发布时重绑 MCP 注册表
- **AND** 该改动 MUST NOT 被表述为"文件正文变化直接生效"，MUST NOT 留在待懒读的悬置状态

#### Scenario: 结构失败保留原因链

- **WHEN** 一次结构变更被前置检查拒绝或解析/构造失败
- **THEN** 生效绑定 SHALL 保持旧态，且回执 SHALL 携带可定位的具名原因与归一行，MUST NOT 只报笼统失败

#### Scenario: 不借文件通道绕门

- **WHEN** 某变更试图以「仅改文件正文」的方式引入结构性差异
- **THEN** 该结构性差异 SHALL NOT 通过文件通道生效，须走结构路径经前置检查
