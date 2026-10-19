# ORCHESTRATION：package-zero-debt

## 结论层

唯一路径：五包簇单并发（触碰集=包簇边界不相交）→Z6 登记同步→F。0 债务=零兼容别名/零死桩/零双重命名/机制名零残留/三类挂法自检过。最大风险：五单并发的 gen_godoc/architecture 共享面（登记同步统一后置 Z6，单内只跑不动）。

## 分组层·编排四问

**次序**：Z1-Z5 并发（各包簇独立）；Z6 后置（docs/登记共享面单点）。**授权**：每单白名单=本包簇 *.go＋本簇 tests；**LSP 多用**（跨包改名/引用迁移走 LSP，禁手搓批量 sed）；共享面（architecture/hygiene/gen_godoc/docs）=Z6 专属，单内遇登记红只报不动。**凭据**：0 债务扫描清单（每单给 grep 命令+零残留证明）＋三类挂法自检表＋golden 12 场景 match＋行为零变化。**释放**：无资源锁。

## 明细层

| 单 | 包簇 | 触碰集 |
|---|---|---|
| Z1 | evolution/＋meditation/＋consolidation（迁入） | evolution/** |
| Z2 | memory/**＋workspace/** | memory//workspace |
| Z3 | agent/**＋state/**＋task/** | 核心域 |
| Z4 | query/**＋rl/**＋telemetry/** | 时态/平行流 |
| Z5 | model/plugin/view/event/config/tool/**＋bench/＋根包 *.go | 词汇/扩展/执行器 |
| Z6 | docs/**＋golden/runtime/**＋scripts/{doc_budget}＋CONTRIBUTING | 共享面（单点） |

## 台账层

- 2026-10-19 计划建立；待派 Z1-Z5 并发。
