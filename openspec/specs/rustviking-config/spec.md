# rustviking-config Specification

> **勘误（2026-11-21 契约同步核验，10.R5e）**：本规范原以 `MemoryConfig`（Go 树 0 命中）与 `resolveMemoryStore`（0 命中）表述现行条款；现行载体是存储开档配置的 `RustVikingBinary` 字段与其 file 档开档路径——开档解析该字段、为空则回落 PATH 上的 `rustviking`、解析不到即具名拒绝。两条要求的实质承诺（路径可配且缺省可回落、file 档由真实 rustviking 客户端与真实关系／快照后端兑现提交屏障、不以 mock 结项）逐条仍在位，故本次只正名载体并删去无承载的分句（声明面并无对应的配置文件键，故场景不再断言配置文件书写），不改承诺。

## Purpose

本规范定义 rustviking-config 能力：存储开档配置 SHALL 带一个可选的 rustviking 可执行文件路径字段，为空时回落为可执行名并按 PATH 解析，解析不到即具名拒绝开档；file 档 SHALL 由真实 rustviking 客户端与真实关系／快照后端兑现提交屏障，MUST NOT 由 mock 或占位实现充当。

## Requirements

### Requirement: 存储开档配置支持 RustViking 可执行文件路径

存储开档配置 SHALL 带一个可选的 rustviking 可执行文件路径字段；字段为空时 SHALL 回落为可执行名 `rustviking` 并按 PATH 解析。解析不到可执行文件时开档 SHALL 以具名错误（缺可执行文件）失败，MUST NOT 退化为内存档、静默降级，或返回一个能提交却无真实后端的替身。

#### Scenario: Binary path explicitly configured

- **WHEN** 开档调用显式给出 `/usr/local/bin/rustviking` 作为该字段
- **THEN** 该档位的 CLI 调用 SHALL 走 `/usr/local/bin/rustviking`

#### Scenario: Binary path not configured

- **WHEN** 该字段为空或未给出
- **THEN** 开档 SHALL 回落为 `rustviking` 并在 PATH 上解析

#### Scenario: 可执行文件解析失败

- **WHEN** 该字段为空且 PATH 上没有 `rustviking`
- **THEN** 开档 SHALL 以具名错误失败，MUST NOT 以替身或内存档顶替

### Requirement: file 档由真实客户端与真实后端兑现

`type: file` 档位 SHALL 由真实 rustviking 客户端与真实关系／分片原子快照后端兑现提交屏障，MUST NOT 使用 mock 或占位实现。具体包路径、类型名与构造签名的收敛方式不在此冻结：实现布局 SHALL 按真实消费者最小签名收敛，MUST NOT 以复活旧键值子包、旧类名或镜像 provider/tool 协议为基线。原文与**实际投影**的分界保留：原文读取与原文存储字节一致，模型投影按记录重建，两者不互相冒充。

#### Scenario: 生产接线不用 mock

- **WHEN** 审查 file 档的存储装配
- **THEN** SHALL 为真实 rustviking 客户端与真实关系／快照后端，MUST NOT 使用 mock 或占位实现

#### Scenario: 不复活旧子包与镜像结构

- **WHEN** 审查实现布局
- **THEN** MUST NOT 以复活旧键值子包、旧类名或镜像 provider/tool 协议为基线；包路径、类型名与构造签名按真实消费者收敛

#### Scenario: 真实客户端实际持久化

- **WHEN** file 档在某个存储根上提交一条键值
- **THEN** 该键值 SHALL 经真实 rustviking CLI 落库，另一次开档 SHALL 读回同一字节

#### Scenario: 真实 CLI 用例不以替身结项

- **WHEN** 运行真实 rustviking 用例而本机没有可用 CLI
- **THEN** 该用例 SHALL 具名跳过并记明该证据属于需要真实资源的运行，MUST NOT 用替身凑通过、MUST NOT 与之合并计为同一结论
