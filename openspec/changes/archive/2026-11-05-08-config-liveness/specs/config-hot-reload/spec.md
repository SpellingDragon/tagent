# config-hot-reload Specification（delta，MODIFIED 于既有能力）

> 头逐字匹配主 spec `openspec/specs/config-hot-reload/spec.md` 的「### Requirement: 配置热更维度矩阵」。全文保留既有条款，仅把"配置文件是静态真源"的三层语义改读为链驱动：文件是编辑界面、声明事实是链上账、唯一发布入口是生效路径；新增回执如实、跨维窗口可审计与重启对账句。本 delta 与 `config-liveness` 的五条不变量同源。

## MODIFIED Requirements

### Requirement: 配置热更维度矩阵

控制面的三层语义 SHALL 如此分工：**配置文件是编辑界面**（单向流入：声明哪些维度存在、承载编辑动作，但不是账）；**链上声明事实**（`config.declaration`）是配置真相（身份=规范指纹、Content=规范声明序列化，可回读；大正文按既有内容寻址文件事实模式引用）；**唯一发布入口**是生效路径（发布换代与请求边界从链上声明事实投影实际值；治理裁决、基线复检、发布记录与 Hold 换代 MUST 原样保留）。结构性维度（model/prompt/binding/org.*）的生效经发布换代、事实为其变更流水；FILE 维度的内容以事实为正源（声明只引用路径，正文住在链上）。MUST NOT 存在这三层之外的第四条生效路径——裸文件读、内存直写与旁路 setter 均不构成生效。

每次装载成功后 MUST 提交带载荷的声明事实；每次发布尝试 MUST 落带 result 的链上事实（失败 result 至少区分装载失败、治理拒绝、安装失败）；`/reload` 回执 MUST 读账如实——发布或安装失败 MUST 报失败，MUST NOT 回成功，MUST NOT 以内存状态自证。声明事实与发布记录 MUST 携带代号与前后关系；绑定代与模型版本两个切换点的窗口内决策组合 MUST 账面可查。重启对账 MUST 以文件指纹与链上最新声明指纹判定：一致即静默（零发布、绑定代不前进）；文件较新 MUST 自动走唯一发布入口；链较新时 MUST 以链为准并记录文件滞后，MUST NOT 用陈旧文件覆盖链（不漂移）。

每个子 agent 配置维度 SHALL 归属且仅归属一条热通道：**FP 代际面**（视图变更，下回合生效、在途钉定；入口移动与代理集合增删属此面——随发布换代生效、在途回合保持旧视图、移除代理的地址对新输入具名拒绝）、**SRC 源拉取面**（参数变更，下一次消费即生效）或 **FILE 事实面**（文件是声明来源，其变更落 `config_change` 事实（dimension=file），消费边界从事实投影，如 prompt 正文与 mcp_servers）；或明示的 **RESTART 拒**（目录/端点类不可迁移资源，拒绝即正确行为）。维度归属与消费点 SHALL 以矩阵形式登记，每行 SHALL 携带代码行号或测试名证据；**携带而消费点未读＝假热更**，SHALL 视为缺陷而非"部分支持"。（三通道划分系域 01 执行期情报 C1 回写：初稿"FP/源"二分与既有 FILE 懒读通道冲突，不得为凑二分法拆毁既有机制）

FILE 变化 SHALL 落 `config_change` 事实（dimension=file）并在**请求边界**从事实投影生效并固化实际版本：MUST NOT 裸读文件绕过事实，MUST NOT 等待 register、后验评估或结构前置质量门；在途请求 MUST NOT 被半途拼接新内容，后续请求 SHALL 读到新版本。FILE 变化 MUST NOT 触发结构 generation 换代。

#### Scenario: 已登记维度的消费点有契约测

- **WHEN** 矩阵中任一维度标记为 SRC 或 FILE 拉取/懒读通道
- **THEN** 该维度 SHALL 存在断言"消费点读到解析值/重读值"的契约测试，测试名登记于矩阵行

#### Scenario: 新维度接入有唯一路径

- **WHEN** 后续变更新增任一配置维度
- **THEN** 该维度 SHALL 经"FP 子集入列"、"源加字段+消费点读"或"文件懒读"三者之一接入，SHALL NOT 新增第四种机制或 push/订阅/广播通道，且矩阵 SHALL 增行登记

#### Scenario: 文件正文变化直接生效

- **WHEN** prompt/skill 文件在两次请求之间被修改
- **THEN** 该修改 SHALL 落 `config_change` 事实（dimension=file），下一请求边界 SHALL 从事实读取新版本并固化；结构 generation SHALL NOT 变化；在途请求 SHALL 保持其已固化版本

#### Scenario: 安装失败回执读账

- **WHEN** 新代已发布而模型重建或安装失败
- **THEN** 链上落 `install-failed` 发布事实，/reload 回执返回失败与具名原因，MUST NOT 返回成功

#### Scenario: 跨维窗口可查

- **WHEN** 一次发布同时切换绑定代与模型版本
- **THEN** 按代号读取发布记录即可还原该窗口的"哪一代绑定 × 哪一版模型 × 各维生效结果"

#### Scenario: 重启一致静默

- **WHEN** 进程重启时文件指纹与链上最新声明指纹一致
- **THEN** 零发布、绑定代不前进，不产生新的发布记录
