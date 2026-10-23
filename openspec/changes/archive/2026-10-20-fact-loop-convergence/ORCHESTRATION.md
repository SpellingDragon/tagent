# ORCHESTRATION：fact-loop-convergence

## 结论层

抵达 DoD 的唯一路径：三域各自收敛（01 事实契约/02 消费拓扑/03 视图归位）→ F 收尾；行为零变化（golden 12 场景全 match 为最终判据）。最大风险：03 的类型迁移触碰 plugin/run/tests/examples 引用面（compile 门兜底，逐处改引用）；次风险：01 五包加方法触发 architecture 边（实现层已允许内部依赖，登记由 03.3 一并做）。

## 分组层·编排四问

**次序**：三域触碰集不相交可并发（01=event/task/rl/config/workspace+docs/FACT-VOCABULARY；02=evolution/agent+tests(protocol)+docs/EXTENSIONS；03=view(新)/plugin/run 族/tests/examples 引用面）。docs 各写各文件；tests 不同文件。F 依赖三域全落。
**授权**：域白名单如上；`fact-loop-convergence/specs/` 外禁改契约面；行为变化=违规（纯归属）。
**凭据**：每叶 verify 逐字符；F2 加"golden 12 场景全 match"（行为零变化的机器判据）。
**释放**：无资源锁；golden runner 单实例。

## 明细层

| 派发单 | 叶 | 触碰集 |
|---|---|---|
| D1 | 01.1-01.3 | event/task/rl/config/workspace＋docs/FACT-VOCABULARY.md |
| D2 | 02.1-02.2 | evolution/agent＋tests/integration/{evolution,execution}＋docs/EXTENSIONS.md |
| D3 | 03.1-03.3 | view(新)/plugin/run*.go/tests/examples 引用面＋golden architecture |
| F | F1-F3 | scripts/全门 |

## 台账层

- 2026-10-19 计划建立（五件套）；待派 D1/D2/D3 并发。
