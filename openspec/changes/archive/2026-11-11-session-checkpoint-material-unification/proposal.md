# 会话级 checkpoint 与 Material 契约统一

## Why

链已是唯一真相，可恢复状态也已收编为框架 checkpoint 事实族（run 级协调者、安静点无锁切面、五个 `Stateful` 切片）。剩下的割裂在三处：协调者按 run 触发，切面写的是整个 run 的状态，而压缩本就按会话上下文发生；`Stateful` 只收 snapshot/restore 三个方法，消费者读态与 observation 面各自演化，接一个新消费者就要再想一遍恢复语义；插件六端口把「时机」与「失败语义」写死在端口名与派发表里，同一语义在多个端口重复表达。三者叠加的代价是每加一种派生状态都要新增挂点或端口，而挂点本身就是需要恢复的第二真相。

## What Changes

- **协调者下沉到会话级**：checkpoint 由每会话的协调者按其上下文触发（压缩与切面同一粒度），写该会话各 g(x) 消费者此刻的派生态；恢复＝读最近 checkpoint 加上只对该会话尾部重跑 g(x)，回放期关闭 observation 副作用。newest checkpoint 仍经 journal 逆扫 `NewestOfType` 取全局最新，不拷贝不排序。
- **`Stateful` 收编消费者读态**：reducer 即 g(x) 的纯派生面，emitter 即 observation 的驱动者；`seedFact` 复用 g(x) 派生而不另起一条路径。不新增挂点、不新增端口。
- **六端口收敛为统一 `Material` 契约**：时机（副作用前／提交后／读取时）与失败语义（拦截／保义务／上报欠账）由声明降为策略字段，端口名不再承载语义。
- **派生态有界**：开放项加水位表达，已结留在链上；持久与原子性只在存储提交接缝保证一次。
- **B3/B4 有界投影前置**：压缩消费者 answered 集先纳入 `Stateful` 切片，再谈按 boundary 跳读与载体有界；不得以删除已提交事实或跳过判决集换有界（12.12/12.13 已回退立卡）。

## Capabilities

### New Capabilities

- `session-checkpoint-material`: 会话级 checkpoint 协调、`Stateful` 对消费者读态的收编（reducer/emitter）、统一 `Material` 声明（时机×失败语义为策略），以及派生态有界与恢复一致性契约。

### Modified Capabilities

- 无。本变更不改既有主 spec 的 requirement 文本；`checkpoint-state` 的 run 级契约保留在 hot-update-and-purity-closure domain 12，本变更在其上叠加会话粒度，不改写其断言。

## Impact

- 代码：`assembly/checkpoint.go`、`assembly/run.go`、`runtime/state/checkpoint.go`、`api/plugin/material.go`、`api/event/checkpoint.go`，以及各 `Stateful` 持有者（projection/delivery/mailbox/tasks/inputs 与压缩 answered 集）。
- API：`Stateful` 接缝扩展；插件面按 `Material` 声明收敛，六端口名作为兼容读法保留到迁移完成。
- 兼容：新建 checkpoint 事实仍按既有 `checkpoint:<partitionSeq>` 键族与内容寻址校验落链，已是真相的旧载体不删不改写。
- 文档：`docs/wiki/platform/platform-subsystems.md` 的目标态段随实现推进转正；`docs/GLOSSARY.md` 同步。
