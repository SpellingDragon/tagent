# config-hot-reload delta（01-state-binding）

> 复用登记（非规范，不改本条语义）：FILE 懒读面复用框架 `skill`（`RefreshableRepository.Refresh()`/`NewFSRepository`）整体替换 index 的语义，本域只做「请求边界一致读取与固化」薄胶水，不复制 scanner/YAML parsing。
> 本 delta 覆盖「配置热更维度矩阵」「首代可回滚」，并新增「结构变更与文件直接生效分路及错误原因链」；本能力其余义务由主 spec `openspec/specs/config-hot-reload/spec.md` 继承（**证据映射，不改**）。

## MODIFIED Requirements

### Requirement: 配置热更维度矩阵

每个子 agent 配置维度 SHALL 归属且仅归属一条热通道：**FP 代际面**（视图变更，下回合生效、在途钉定）、**SRC 源拉取面**（参数变更，下一次消费即生效）或 **FILE 懒读面**（文件即真源、消费边界按 mtime 懒读，如 prompt 正文与 mcp_servers）；或明示的 **RESTART 拒**（目录/端点类不可迁移资源，拒绝即正确行为）。维度归属与消费点 SHALL 以矩阵形式登记，每行 SHALL 携带代码行号或测试名证据；**携带而消费点未读＝假热更**，SHALL 视为缺陷而非"部分支持"。（三通道划分系域 01 执行期情报 C1 回写：初稿"FP/源"二分与既有 FILE 懒读通道冲突，不得为凑二分法拆毁既有机制）

FILE 懒读面 SHALL 在**请求边界**读取并固化实际版本：文件变化直接生效，MUST NOT 等待 register、后验评估或结构前置质量门；在途请求 MUST NOT 被半途拼接新文件，后续请求 SHALL 读到新版本。FILE 变化 MUST NOT 触发结构 generation 换代。

#### Scenario: 已登记维度的消费点有契约测

- **WHEN** 矩阵中任一维度标记为 SRC 或 FILE 拉取/懒读通道
- **THEN** 该维度 SHALL 存在断言"消费点读到解析值/重读值"的契约测试，测试名登记于矩阵行

#### Scenario: 新维度接入有唯一路径

- **WHEN** 后续变更新增任一配置维度
- **THEN** 该维度 SHALL 经"FP 子集入列"、"源加字段+消费点读"或"文件懒读"三者之一接入，SHALL NOT 新增第四种机制或 push/订阅/广播通道，且矩阵 SHALL 增行登记

#### Scenario: 文件正文变化直接生效

- **WHEN** prompt/skill 文件在两次请求之间被修改
- **THEN** 下一请求边界 SHALL 读取新版本并固化；结构 generation SHALL NOT 变化；在途请求 SHALL 保持其已固化版本

### Requirement: 首代可回滚

热更 reloader MUST 在安装时保存启动代有效配置快照；第一次结构热更成功后 Rollback MUST 可用（不得因无上一代快照而失败）。

回滚 SHALL 复用同一条发布路径并从上一份**完整有效配置**（结构 + 热参数）重建为**新 generation**；MUST NOT 保存旧执行器作为恢复依据（见主 spec「数值与结构共享完整有效配置回滚记录」）。

#### Scenario: 首次热更后立即回滚

- **WHEN** 进程启动后的第一次结构热更成功，随后调用 Rollback
- **THEN** 回滚 MUST 恢复到启动代配置并再次可用，不得报「无上一代快照」

#### Scenario: 回滚产生新代而非复用旧执行器

- **WHEN** 一次结构回滚执行
- **THEN** SHALL 产生新的 generation 序号并走同一发布路径，MUST NOT 复用旧执行器对象或旧 generation 序号

## ADDED Requirements

### Requirement: 结构变更与文件直接生效分路及错误原因链

结构变更（模型/工具/插件/任务策略/资源引用/memory）与文件懒读（prompt/skill）是**两条独立消费路径**：结构变更 SHALL 走 `config.Load → Compile → Prepare → 前置检查 → commit`；文件变更 SHALL 直接生效。结构性变化 MUST NOT 借文件通道绕开前置检查，文件变化也 MUST NOT 被宣称经过前置质量门。

结构路径失败时系统 SHALL 保留**错误原因链**（源文件 → 归一行 → 具名原因），使失败可定位；MUST NOT 以笼统「reload 失败」吞掉原因。

#### Scenario: 两条路径互不绕门

- **WHEN** 同时发生一次 prompt 文件修改与一次工具声明修改
- **THEN** 文件修改直接生效（不进前置门），工具声明修改 SHALL 经前置检查后才提交；两者作用范围 MUST NOT 混淆

#### Scenario: 结构失败保留原因链

- **WHEN** 一次结构变更被前置检查拒绝或解析/构造失败
- **THEN** 生效绑定 SHALL 保持旧态，且回执 SHALL 携带可定位的具名原因与归一行，MUST NOT 只报笼统失败

#### Scenario: 不借文件通道绕门

- **WHEN** 某变更试图以「仅改文件正文」的方式引入结构性差异
- **THEN** 该结构性差异 SHALL NOT 通过文件通道生效，须走结构路径经前置检查
