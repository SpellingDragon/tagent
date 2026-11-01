# Proposal: 01-attention-primitive

> 父变更：attention-lifecycle（无前置域；本域单写 `api/event` 后冻结）。

## Why

三条"向人要答案"的路径没有共同原语，是每类新"需要人"都要再造存储与路由的根
因（一级 proposal 的反事实）。本域先立原语：事实族、meta 键、落链/读回、清除
规则——纯原语，不带任何消费者（消费者在 02/03）。

## What Changes

- 冻结三枚事件类型：`TypeAttentionQuestion` / `TypeAttentionAnswer` /
  `TypeAttentionClearance`，与五枚 meta 键（`attention.kind/target/lineage/
  expires_ms/clearance`），登记进 skeleton_assert 与 FACT-VOCABULARY。
- `runtime/task` 新增 attention 面：提交（question/answer/clearance 落链）与
  读回（在场集：question 尚无 answer、未过期、未清除）两个方法。
- 清除规则引擎：answer 落链即清；读面按 `expires_ms` 判过期；clearance 事实
  （answer|expiry|work-end）为唯一"移除在场"的依据；全部从事实链推导，重启
  后读回一致。

## 边界与依赖

- **依赖**：`api/event` 的事件类型与 meta 登记（接口常数，本域单写后冻结）；
  事实链提交走现有 `Run` 提交面（实现依赖，签名以启动门时 next 为准）。
- **被依赖方**：02 域（审批读写 attention）、03 域（投影/清除/人侧）都只依赖
  本域的提交与读回 API。
- **接口面**：`attention` 包对外两个方法（提交面 + 在场读回面）；不导出内部
  规则引擎结构。
- **禁止事项**：不接任何 governance/fuse/wechat/meditation 代码（消费者归
  02/03）；不改 notice 四元组与 settle 协议；不引入第二存储（无新目录/表）。

## Capabilities

见 `specs/attention-primitive/spec.md`。
