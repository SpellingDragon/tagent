# orchestration Specification

## Purpose
TBD - created by archiving change simplify-prototype-event-pipeline. Update Purpose after archive.
## Requirements
### Requirement: 隔离并发与新语义优先
本变更 SHALL 以用户最新六点与 agent id 孤儿回收规则为依据。子域按一级 design 冻结签名，在独立 staging 写树并发交付；旧批次栅栏、普通工具治理和输入折叠不作为验收前提。

#### Scenario: 子域合并
- **WHEN** 三域 staging 产物就绪
- **THEN** 主代理核对接缝并集成到 prototype，未集成的语法检查不报告为全包通过

### Requirement: 只有集成路径作为行为证据
验收 SHALL 删除旧单元测试，只通过配置、agent循环、插件、任务、模型和state恢复的集成路径验证八个目标场景；真实跨进程通过子进程加载快照取证。

#### Scenario: 交付检查
- **WHEN** prototype 交付
- **THEN** 集成测试十次及 race 十次通过，README与文件注释说明实际边界，不宣称物理执行资源已恢复

