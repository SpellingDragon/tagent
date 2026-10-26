# tool-schema-clarity Specification

## Purpose

exec 资源参数的可枚举声明：`exec_run` 与 `exec_release` 的 kind/resource 在声明里携带机读枚举与模式，与运行期校验同源同词表，使模型仅凭声明就能写出合法调用，不必猜测资源家族或复合键形态。

## Requirements

### Requirement: exec 资源 kind 的合法族写进声明

`exec_run` 与 `exec_release` 的 `kind` 参数 SHALL 在声明中携带机读枚举，其唯一元素为系统实际服务的家族 `tmux`（`exec.KindTmux`）；描述 SHALL 点名该唯一值并说明留空即选默认 `tmux`。描述 MUST NOT 使用"资源家族""标识所在的族"一类抽象名词短语作为唯一说明——模型 MUST 能仅凭声明确定合法取值。

枚举元素 SHALL 取自 `KindTmux` 常量本身，MUST NOT 在声明里另写一份家族名字面量。

#### Scenario: kind 只列出一个合法族

- **WHEN** 读取 `exec_run` 或 `exec_release` 声明中 `kind` 属性的 schema
- **THEN** 其枚举 SHALL 恰好含一个元素且等于 `KindTmux`
- **AND** 其描述 SHALL 含该枚举值本身，并说明省略 `kind` 即选默认 `tmux`

#### Scenario: 旧抽象短语不再作为说明

- **WHEN** 读取两条声明的 `kind`/`resource` 描述
- **THEN** 描述 MUST NOT 含 `the resource family` 或 `canonical key of the execution resource`
- **AND** 判定对象 SHALL 是声明返回的字符串，MUST NOT 以源文件中其它位置的注释代替

### Requirement: resource 形态与会话名校验同源

`exec_run` 与 `exec_release` 的 `resource` 参数 SHALL 在声明中携带机读模式，且该模式与 `validSessionName` 的判定等价：非空、长度不超过 `tmuxSessionMaxName`、字符取自 `[A-Za-z0-9_-]`。描述 SHALL 给出同一上限、同一字符集与一个能通过该校验的合法示例。上限与字符集 MUST 由校验规则所用常量生成，MUST NOT 在描述或模式里另写字面量。

描述 MUST NOT 引导带前缀的复合键形态（如 `tmux:<name>`、`kind:<name>`）或文件路径形态；`resource` 是会话名，不是路径，也不是 `<kind>:<key>` 复合键。

#### Scenario: 模式与校验器判定一致

- **WHEN** 用声明中 `resource` 的模式匹配一组名字，并与 `validSessionName` 的判定逐项比较
- **THEN** 两者 SHALL 在每一项上给出相同结论
- **AND** 该组名字 SHALL 至少覆盖：合法示例、单字符下界、长度等于上限、长度等于上限加一、空串、含冒号的复合键、含空格/点/斜杠的名字

#### Scenario: 描述给出可直接照抄的合法形态

- **WHEN** 模型读取 `resource` 的描述
- **THEN** 描述 SHALL 含上限数值（由 `tmuxSessionMaxName` 生成）、字符集 `[A-Za-z0-9_-]` 与一个合法示例
- **AND** 该示例 SHALL 通过 `validSessionName`

#### Scenario: 描述不引导带前缀的键

- **WHEN** 检查 `resource` 描述是否含 `<前缀>:<键>` 形式的示例
- **THEN** 描述 MUST NOT 匹配 `(?:tmux|kind|resource)\s*:\s*[A-Za-z0-9_-]`
- **AND** 该拒绝 SHALL 覆盖真机出现过的错误形态 `tmux:local`

### Requirement: 两条声明共享同一词表

`exec_run` 与 `exec_release` 对 `kind`/`resource` 的描述与机读约束 SHALL 来自同一份词表：同一个 `KindTmux` 枚举、同一个模式串、同一组上限与字符集常量。MUST NOT 为两条声明各写一份字面量，MUST NOT 只改其中一条。

#### Scenario: 两条声明的词表逐字相等

- **WHEN** 比较 `exec_run` 与 `exec_release` 的 `resource` 模式串与 `kind` 枚举
- **THEN** 两者的模式串 SHALL 逐字相等，两者的枚举 SHALL 相等
- **AND** 任一不等即判定为实现缺陷，MUST NOT 视为"两条工具各自的合理差异"

#### Scenario: 生产装配同时携带两条新声明

- **WHEN** 从生产入口 `exec.NewPreset` 取回工具集合并读取其声明
- **THEN** 两个工具的 `kind`/`resource` 声明 SHALL 都带有上述枚举、模式与描述
- **AND** 证据 MUST 取自该生产入口的返回值，MUST NOT 由测试手工构造的声明代替
