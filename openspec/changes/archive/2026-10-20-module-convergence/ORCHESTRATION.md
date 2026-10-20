# ORCHESTRATION：module-convergence

## 结论层

唯一路径：01 压缩事件化（行为重排，五叶）→ 02 声明第二辑 → 03 判据收口 → F。最大风险：Fold 预折叠的"待压缩"状态位语义（读路径如实呈现需新键/新字段——加字段须过 S2 概念预算）；次风险：curation 不变量迁移（73 白盒测试中 memory 部分须原语义过）。

## 分组层·编排四问

**次序**：01.1→01.2→01.3→01.4 串行（同 memory/workspace 触碰集）；01.5 依赖全部；02/03 与 01 不相交可并行（tests/docs 触碰）。**授权**：01=memory/workspace/run_request/run_fact；02=tests/docs；03=docs/doc_budget。**凭据**：每叶 verify 冻结；01.5=行为等价双判据（golden+同输入产物不变）。**释放**：无资源锁。

## 明细层

| 派发单 | 叶 | 触碰集 | 就绪 |
|---|---|---|---|
| D1 | 01.1+01.2 | memory/workspace | 立即 |
| D2 | 01.3+01.4 | memory/run_request/run_fact | D1 |
| D3 | 01.5+02.1+02.2+03.1 | tests/docs | D2（01.5 需 D2；02/03 可早） |
| F | F1-F3 | 全门 | D3 |

## 台账层

- 2026-10-19 计划建立（五件套，化归审计 16 面）；待派 D1。
