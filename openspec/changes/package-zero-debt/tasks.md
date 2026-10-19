# Tasks：package-zero-debt（全项目 0 债务重构）

> 用户指令（2026-10-19）：整个项目重来、大刀阔斧、0 债务；子代理多用 LSP；充分分块并发。判据：机制包三类文件（事件词汇/消费接线/扩展策略）、领域包业务归位、命名唯一正名、零兼容别名、零死桩、行为零变化（golden 12 场景）。

## 包簇五单（并发，触碰集不相交）

- [x] Z1 evolution 域全量（三通道对称：meditation 补 reflection/digest/observation/watermark；refine/ 子包；consolidation/ 自 memory 迁入；根留评估业务） —— 验证：`go build ./...`＋`go test ./evolution/... ./memory/... -count=1`＋golden match＋architecture 双绿
- [x] Z2 memory+workspace（0 债务扫描：别名/桩/机制名残留 curation 5 处；票据语义归位；三类挂法） —— 验证：同上＋`go test ./memory/... ./workspace/... -count=1`
- [ ] Z3 agent+state+task（三类挂法核对；死码/桩/遗留名清零） —— 验证：同上
- [x] Z4 query+rl+telemetry（同上；rl 拆 export/sft 子包按三类挂法裁决） —— 验证：同上
- [x] Z5 model+plugin+view+event+config+tool/*+bench+根包（同上；model aliases=框架边界保留但 doc 自证） —— 验证：同上

## 收尾（编排者直管）

- [x] Z6 docs/登记同步（FACT-VOCABULARY/EXTENSIONS/CONTRIBUTING/architecture/hygiene/gen_godoc/doc_budget 全对齐）＋F1 契约零改动＋F2 全量门禁＋F3 DoD
- [x] F4 archive（需明示）
