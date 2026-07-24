## ADDED Requirements

### Requirement: 框架默认 prompt 随二进制嵌入

框架 SHALL 将其默认 prompt 集合（`resources/prompts`）嵌入二进制，作为位置无关的默认来源。消费方 SHALL NOT 依赖框架 prompt 的磁盘相对路径即可获得这些默认。

#### Scenario: 无磁盘 prompt 目录也能取到默认

- **WHEN** 运行环境中不存在框架 prompt 的磁盘副本
- **THEN** loader SHALL 仍能从嵌入的默认集合解析出这些 prompt

### Requirement: 缺失 prompt 回退到嵌入默认

当为 loader 配置了 fallback 时，若某个 prompt 文件在磁盘 `BaseDir` 下不存在，loader SHALL 从嵌入的框架默认解析该文件。磁盘上存在的文件 SHALL 始终优先（override 胜出）。绝对路径 SHALL NOT 触发回退。

#### Scenario: 磁盘缺失则回退嵌入

- **WHEN** 引用的 prompt 文件不在 `BaseDir` 下
- **THEN** loader SHALL 返回嵌入默认中同名文件的内容

#### Scenario: 磁盘存在则覆盖嵌入

- **WHEN** `BaseDir` 下存在同名 prompt 文件
- **THEN** loader SHALL 使用磁盘文件内容，SHALL NOT 使用嵌入默认

### Requirement: 目录级回退（整目录）

当引用的是目录扫描（`.md` 合并）且该目录在磁盘 `BaseDir` 下不存在时，loader SHALL 改用嵌入默认中的同名目录。若磁盘目录存在，则以磁盘目录为准，SHALL NOT 与嵌入内容做 per-file 合并。

#### Scenario: 磁盘目录缺失则用嵌入目录

- **WHEN** 目录扫描的目标目录不在磁盘 `BaseDir` 下
- **THEN** loader SHALL 扫描并合并嵌入默认中的同名目录

### Requirement: 未配置 fallback 时行为不变

fallback SHALL 为可选。当未给 loader 配置 fallback 时，其解析行为 SHALL 与引入本能力前完全一致（仅磁盘 `BaseDir`，缺失即报错）。

#### Scenario: 未配置 fallback 缺失即报错

- **WHEN** loader 未配置 fallback 且引用文件在磁盘缺失
- **THEN** loader SHALL 返回文件不存在错误，行为与既有一致
