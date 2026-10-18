# resident-release-evidence delta（09-acceptance）

> 本 delta 只承载**新增条款**；既有 Requirement（验证工具对底层失败保真、证据分层与可复现性、双模块与真实消费者验收、常驻准出证据、发布与外部操作权限、编排热更以生产委派变化验收、安全验收不由统计或豁免替代、验收覆盖完整装配链与复杂度收敛）语义不变，**继承**（见 `../proposal.md` §继承）。

## ADDED Requirements

### Requirement: 完整门禁与 golden 真实运行输出

准出 SHALL 以**完整门禁命令集**为据，且逐条命令**独立记录原始退出码**（MUST NOT 用 `grep/head/tail` 的退出码代替原命令）；两 module（根 module 与微信独立 module）SHALL 分别 build/vet/test。零失败 MUST NOT 由过滤输出、命中过滤器或子集读数得出。

正式装配场景 SHALL 输出**真实观测**并与 golden 对照，断言落到**值/消息/身份关系**（MUST NOT 以键名对齐或"人工 PASS 汇总"代替）；缺 case、错值、未知断言、子测试 Skip、包级 Fail 均 SHALL 判失败。随机 ID/时间可规范化，但 SHALL 保留一对一关系、顺序与因果；观测器 MUST NOT 写成期望值复制器。

#### Scenario: 门禁不靠过滤得出

- **WHEN** 汇总准出门禁结果
- **THEN** 每条命令含原始退出码与未跑范围；任一 非零 退出即失败；不以子集或过滤后的读数宣称全绿

#### Scenario: 值断言而非键名

- **WHEN** 场景 runner 与 golden 对照
- **THEN** 报告给出值/消息/身份关系的差异；仅键名一致不构成通过；子测试 Skip 视为失败

### Requirement: 离线迁移与回退保真（读旧写新，不伪 restore 副作用）

离线迁移 SHALL 只读旧数据并导入**新路径**：源数据摘要（字节）MUST 保持不变，MUST NOT 让旧程序读新格式（新格式 MUST NOT 回写旧路径）；缺失外置文件与不可恢复任务 SHALL 具名列出。迁移中断 SHALL 可重跑，回退 SHALL 只读旧数据、走新路径。

回退 MUST NOT 伪造 restore 副作用：不得假装写回了本不存在的记录、不得自动改写旧档、不得以"回退成功"掩盖未清理的新路径残留。

#### Scenario: 源字节不变

- **WHEN** 完成一次离线迁移
- **THEN** 源数据摘要（字节）与迁移前一致；旧程序无法读到新格式（新格式未回写旧路径）

#### Scenario: 中断可重跑、缺口具名

- **WHEN** 迁移中断后重跑
- **THEN** 可收敛到一致结果；缺失外置文件/不可恢复任务被具名列出，不静默跳过

#### Scenario: 不伪 restore 副作用

- **WHEN** 执行回退
- **THEN** 仅只读旧数据、走新路径；无伪造的写回记录、无自动改写旧档；残留如实呈现
