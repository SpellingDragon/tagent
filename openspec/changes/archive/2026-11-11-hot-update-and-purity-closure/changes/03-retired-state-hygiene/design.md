# Design: 03-retired-state-hygiene

## 技术要点

- publicationRecords.records：无读取方，直接删（反事实：删掉无消费者坏）。
- configFact：顶层字段与嵌套 Publication 二选一保留单一 wire；保留嵌套、删顶层重复，一致性守卫改为结构保证而非运行时比对。
- budgetMB：撤死参 openHostCapture(root,0) 与死分支，签名收敛。
- SetRetrievalTuning：唯一生产调用传常量 → 撤独立写权，改读常量/派生；store_searcher 注释失真同步修。
- 快照 Cursor：只写不读 → 删或补读（二选一，反事实裁决）；pinnedFacts boot trim 前从链重播种。

## 文件清单

runtime/state/commit.go、assembly/host.go、assembly/host_capture.go、assembly/run.go、memory/search/store_searcher.go、runtime/state/recovery.go、对应测试。

## 风险与回退

- 风险：configFact wire 收敛改变事实编码 → 需兼容读旧事实或一次性迁移；以"读旧写新"过渡并负锚钉旧事实可读。
- 回退：单域回滚；删影子/死参无行为变化，回滚零风险。