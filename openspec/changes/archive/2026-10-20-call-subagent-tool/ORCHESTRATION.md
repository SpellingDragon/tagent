# ORCHESTRATION：call-subagent-tool

## 结论层

抵达 DoD 的唯一路径：装配器（1.1-1.3）→ 工具与 task 化执行（2.1-2.3）→ 扩展面保真（3.1-3.4）→ F 收尾。最大风险：trpc agent 构建器在 fork 版本上的可用签名（先核再装配，1.3 首验）；次风险：frontmatter 解析的严格性边界（复用 config 严格解码先例）。

## 分组层·编排四问

**次序**：1.1→1.2 可并行（不同文件）；1.3 依赖 1.1+1.2；2.1 依赖 1.3；2.2/2.3 依赖 2.1；3.x 与 2.x 不相交可并行（3.2 文档、3.3/3.4 golden 依赖 2.2/2.3 产物）。
**授权**：叶级白名单＝`tool/subagent/`、`tests/integration/{execution,config}/`、`golden/runtime/scenarios.json`、`docs/`（扩展点文档）；产品他域禁改；`agent.Delegate`/`config` 结构性变更必须上报。
**凭据**：每叶 verify 三锚逐字符；golden 跑 `--scope deterministic` 断言 S11/S12 attested；全门 `bash scripts/lint.sh`。
**释放**：本批无资源锁；golden runner 单实例（不并发双跑）。

## 明细层

| 派发单 | 叶 | 触碰集 | 就绪条件 |
|---|---|---|---|
| D1 | 1.1+1.2 | tool/subagent（新增） | 立即 |
| D2 | 1.3+2.1+2.2 | tool/subagent＋tests/execution | D1 落盘 |
| D3 | 2.3+3.1 | config 装配＋tests/config,execution | D2 落盘 |
| D4 | 3.2+3.3+3.4 | docs/golden/scenario tests | D3 落盘 |
| F | F1-F3 | scripts 全 | D4 落盘 |

## 台账层

- 2026-10-19 计划建立（proposal/design/tasks/spec/ORCHESTRATION 五件套）——待派发 D1。
