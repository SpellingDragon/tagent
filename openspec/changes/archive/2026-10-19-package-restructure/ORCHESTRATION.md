# ORCHESTRATION：package-restructure

## 结论层

唯一路径：01+02 合单派发（同触 memory/ 簇）→ 03（docs，与产品面不相交可并发）→ F。最大风险：ledger/replay 搬迁的引用面（memory 内互相引用+run 族）——compile 门兜底逐处改；次风险：meditation 正名牵动 FACT-VOCABULARY/golden S08 键名（1:1 同步）。

## 分组层·编排四问

**次序**：D1=01.1→02.1→02.2→01.2（同簇串行，一次落盘）→01.3/02.3 收尾；D2=03.1/03.2 并发。**授权**：D1=memory//evolution//golden（S08 键名）/architecture/hygiene/docs/api 生成物；D2=CONTRIBUTING//docs/wiki（边界句）/doc_budget。**凭据**：每叶 verify 冻结；行为零变化=golden 12 场景全 match（11 确定性+S10 待资源）＋`go test ./... -count=1` 全绿。**释放**：无资源锁。

## 明细层

| 派发单 | 叶 | 触碰集 | 就绪 |
|---|---|---|---|
| D1 | 01.1-01.3＋02.1-02.3 | memory//evolution//golden/scripts | 立即 |
| D2 | 03.1-03.2 | CONTRIBUTING//docs/wiki/doc_budget | 立即（不相交） |
| F | F1-F3 | 全门 | D1+D2 |

## 台账层

- 2026-10-19 计划建立（收缩版：不复活登记/两件小改造/判据固化）；待派 D1∥D2。
