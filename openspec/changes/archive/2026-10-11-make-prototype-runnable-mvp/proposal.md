## Why

prototype 已展示事件、投影、运行时状态与工具之间的关系，但持续循环、异步回流和退出尚不能运行，模块接线与 mock 边界也未形成可验证的说明。用户已将目标从可读骨架明确提升为可运行的 mock MVP，并要求逐文件能力说明及带架构图的 README。

## What Changes

- 修通首事件消费、持续事件循环、内部事件发布、工具结果回流、最终答复和协作式退出；保留 BaseTAgent 的函数式骨架。
- 由 state 统一维护生效配置、治理账本、任务归属与冥想水位；config 只描述和装配，任务模块只推进生命周期，memory 只管理事实与投影视图。
- 使批次级配置快照、按原调用回传结果、治理拒绝、摘要票据召回与手动冥想真实运行，复杂策略和物理资源继续 mock。
- **BREAKING（仅 prototype）**：为模型、工具和运行循环接入 context 与显式错误语义，同步更新原型内部调用方；不修改生产模块 API。
- 增加确定性 mock 测试与可运行示例，验证真实原型入口，而不以编译或直接调用测试替身冒充闭环。
- 每个 Go 文件注明职责、未实现能力、其他模块的接线方式；新增 prototype/README.md，含架构图、功能状态矩阵、模块边界、运行命令与限制。

## Capabilities

### New Capabilities

- `prototype-mvp`: 限于 prototype 的可运行事件驱动 MVP、运行时状态权威、mock 边界、文档与验证契约。

### Modified Capabilities

无。既有生产规格只作为架构参考，本变更不宣称实现其持久化、组织级热更或远端资源保证。

## Impact

- 修改 prototype 下现有七个 Go 文件；新增 prototype 下的测试/示例文件及 README.md。
- 新增本变更的 OpenSpec proposal、design、specs、tasks；不改其他主规格或历史归档。
- 只使用 Go 标准库，不修改 go.mod/go.sum，不访问真实模型、微信、tmux、数据库或其他外部服务。
- 原型采用一个 agent 实例绑定一个调用作用域/会话；不扩展为多会话服务器、通用 Runtime 或全局任务调度平台。
- 保留用户当前工作区其他修改和删除，不提交、不推送、不归档。
