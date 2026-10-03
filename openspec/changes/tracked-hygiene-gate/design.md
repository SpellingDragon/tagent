# Design

## D1 判据选性质，不选名单

黑名单式禁令（枚举 `.tagent-writer.lock`、`relations.journal`…）只能防已知名。本门用两条**性质判据**，且都在当前 HEAD 上有零误报实证：

1. 追踪文件必须非空——当前全部空追踪文件恰好就是那 4 个残骸（全库扫描实证）；
2. 追踪路径不得被任何 `.gitignore` 规则命中——当前命中集恰好就是 `hottest-sub1/2`（`git check-ignore --no-index` 实证）。

## D2 `check-ignore` 必须 `--no-index`

实测坑：`git check-ignore` **默认跳过已追踪文件**（对 `hottest-sub1/relations.journal` 返回 rc=1），加 `--no-index` 才做纯模式判定（rc=0，命中 `.gitignore:64:/hottest-*/`）。不加这个旗标，门会对最该抓的目标失明。此坑写死在脚本注释与本节，防止后人"简化"。

## D3 接入 lint.sh 而非新开 validators step

仓库自己的原则：同一脚本本地与 CI 同义，"batch author 与 CI 不可能对 lint passed 含义不同"（ci.yml 注释）。`scripts/lint.sh` 已是聚合面（gofmt/vet/comment_policy/doc-refs/gen_godoc/proc-refs），卫生门作为其中一段，错误文案沿用既有风格。

## D4 fail-before 的顺序

先落门脚本、对未清理的 HEAD 运行——MUST 红（抓到 4 空文件 + 2 个可忽略目录）并留痕；随后清理；同命令转绿。删除是一次性动作，规则才是交付物；门的红先于清理出现，证明它防的正是这次事故的形状。

## D5 清理边界

删：`hottest-sub1/`、`hottest-sub2/`（追踪侧）；`own-sub1/2`、`own-a2okagent`、`own-zzbadagent`、`hottest-sub3`、`hottest-drop-main/sub1/sub2`、`go.work.sum`（工作树侧）。不删：`.pre-isolation-backup/`、`.tagent-workspace/`、`.agents/`、`.qoder-handover.log`——运行与交接资产，其去留是独立判断，不搭本变更的车。

## D6 不扩大 .gitignore

untracked 侧已有 `/own-*/`、`/hottest-*/` 兜底且实证有效（`hottest-sub3` 正常被忽略）。门的职责在 tracked 侧；两边各管一段，不过度设计。
