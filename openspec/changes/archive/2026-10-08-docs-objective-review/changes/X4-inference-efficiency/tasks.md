# Tasks: X4 推理效率探索

- [x] 10.1 深读装配链/预算/压缩/召回主路径，产出机制地图章节（≥15 处 文件:符号 引用，含稳定段-可变段分段图）—— 验证：`grep -oE '[a-z_/]+\.go' 报告 | sort -u | wc -l ≥ 10`
- [x] 10.2 完成 H1-H7 假设核验表（含估值常数消费方对照表）—— 验证：`grep -c '^| H' 报告 ≥ 7`
- [x] 10.3 运行验证命令候选（命令账记录全文+退出码）并落盘 `docs/.dev/20261007-code-exploration-X4-inference-efficiency.md` —— 验证：`test -f 该文件 && grep -c '^## ' 该文件 ≥ 7 && grep -c 'go test' 该文件 ≥ 1`
