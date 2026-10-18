## Why

上一轮 `next` 重构（rebuild-formal-runtime）失败于三种模式：计划边做边改（20 域 114 任务执行期膨胀）、完成声明先于证据（局部绿灯冒充整体成立）、结构碎片化（镜像接口/薄文件/复活旧机制）。清理后的工作树没有任何可运行基线。本变更以**可判定的预期**重建：每个目标先落 KR 与验收判据（[expectations.md](expectations.md)），再以两阶段交付把架构骨架与实施分离，防再跑偏。

## What Changes

**交付形态**：10 个二级领域、89 个叶任务＝阶段一 24 个 P0 骨架（数据/接口定义＋CI 合规注释文档，零逻辑）＋阶段二 65 个 `NN.M` 实施。阶段一过**骨架评审门 `00.P0.3`**（CodeReview＋主编排者复核＋用户确认三道）后才解锁阶段二——01–09 全部 `NN.M` 以该门为硬前置（task-graph 硬边，检查器拒收缺边）。旧任务编号与勾选不继承；120 个主规格逐能力复核归属（[coverage.md](coverage.md)，裁决存档 [adjudications-00.2.md](adjudications-00.2.md)），不作为源码恢复清单。

**五个 Objective（判定口径的完整表述在 expectations.md，此处只列变更面）**：

- **O1 可持续演进与 tRPC 真复用**——复用锁定版原生 model/tool/ToolSet/MCP/skill/file/embedding/OTel；成功标准是真实生产请求路径走到原生能力，不是 import 别名；不为举例造新包；共享签名可改但须证据＋影响闭合。
- **O2 职责明确且可靠的执行/历史**——state 只持生效绑定与领域现场（OrgPlan/StatePlan/Publication 三对象＋借用句柄），task 持转移/通知义务、memory 持原文/投影写权威，三者不互相代管；受理 Receipt≠义务结清；unknown≠dead；文件热读与结构前置门两通道分离。
- **O3 统一授权运行总图**——Query 只读投影（统计从明细派生、授权过滤先行、有界不可变快照、订阅具名 gap/reset）；观测旁路不拥有状态；关闭 OTel 仍可查。
- **O4 自然 RL 数据＋真实 SFT 消费**——请求边界快照/脱敏/logprobs 三态/有界封账/授权导出/episode≠session 分组防泄漏/真实 tokenizer 消费闭环；用途资格三态（qualified/ineligible/unknown），不伪造资格、不承诺训练提升；不接 PPO/GRPO。
- **O5 完整宿主交付与可信验收**——两 module 公共入口＋微信独立 module（真实微信往返属必验，未授权只是不能执行）；场景 runner 从正式 API 派生观测（断言值/消息/身份关系，不照抄 expected）；迁移读旧写新源字节不变；完整门禁逐条原始退出码；未裁项如实保持未完成。**最终交付物**：可运行公共入口与微信宿主、架构及场景正式档、真实运行 golden 及检查器、完整验收报告、SFT 消费/反馈闭环证据、明确的未完成或待裁清单（外部计划 §10）。

**明确不做**（外部计划 §2「不做」）：不新建通用 DI 容器、插件 DAG、第二套 ReAct、第二份可写会话历史、镜像 provider/tool 协议、永久去重总库；不为保留旧接口发明消费者；不因文件短就合并、不因文件长就机械拆包。

**口径纪律（先于一切计数）**：三类证据互不替代（规划格式 ≠ 结构覆盖 ≠ 运行行为）；重大方向（Runner 承载/存储 DB-WAL 更换/跨进程动态插件/采样训练引擎）未获用户裁定前 blocked；外部批准计划只读；P0 是各 KR 的阶段一载体，不新增 KR；中间闭环不称正式版完成；原数据保留期限按现存配置逐项核实，无配置依据不自定为无限或自动删除。

## Capabilities

### New Capabilities

顶层不新增产品能力。新增或修改的产品规格下沉到各二级领域，按既有能力名称登记；新增能力须说明独立需求及与既有规格的区别。

### Modified Capabilities

- `orchestration`：两阶段结构、骨架评审门、叶任务级读写冲突准入、凭据核销和授权范围。顶层仅承载编排规格。

二级领域及能力责任见 `design.md`；原规格到新任务的映射见 `coverage.md`（缺口与裁决见 `adjudications-00.2.md`）。不将未核验旧机制标为自动继承。

## Impact

- **当前授权边界**：阶段一骨架波产品代码已获授权并落地入库（commit `6f8d4d7`：16 包＋module 根，零逻辑，全套验证绿）；阶段二 65 项实施被 `00.P0.3` 门锁住——门的第一、二道（CodeReview 有条件通过＋主编排者复核全处置）已完成于 [skeleton-review.md](skeleton-review.md)，第三道用户确认待决。提交/推送经用户明示后执行；归档、真实付费资源、微信发送各自独立授权。
- 后续实现范围包含根 module 公共入口、config/state/agent/task/memory/workspace/plugin/model/telemetry/rl/evolution/query 及必要工具适配，以及微信独立 module。
- **BREAKING**：不沿用已清除生产树的内部包/API 形态；仍需保留的配置和数据通过明确映射迁移，不原地改写旧数据。
- 外部批准计划 `运行时_重设_实施计划_304e54c3.md` 保持只读；prototype、历史 wiki、主 `openspec/specs/`、用户未跟踪文件不改动。
- 检查器通过、骨架编译断言绿、评审门通过均不推出产品行为通过——运行行为由阶段二点名用例与 09 验收证据判定（expectations「三类证据互不替代」）。
