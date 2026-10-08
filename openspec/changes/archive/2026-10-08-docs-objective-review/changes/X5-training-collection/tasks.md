# Tasks: X5 训练采集探索

- [x] 11.1 深读轨迹/反馈/转换器主路径，产出机制地图章节（≥15 处 文件:符号 引用，含采集链断点图）—— 验证：`grep -oE '[a-z_/]+\.(go|py)' 报告 | sort -u | wc -l ≥ 8`
- [x] 11.2 完成 H1-H7 假设核验表（含归因语义补白与最小改造面定位）—— 验证：`grep -c '^| H' 报告 ≥ 7`
- [x] 11.3 运行验证命令候选（命令账记录全文+退出码）并落盘 `docs/.dev/20261007-code-exploration-X5-training-collection.md` —— 验证：`test -f 该文件 && grep -c '^## ' 该文件 ≥ 7 && grep -c 'go test\|python3' 该文件 ≥ 1`
