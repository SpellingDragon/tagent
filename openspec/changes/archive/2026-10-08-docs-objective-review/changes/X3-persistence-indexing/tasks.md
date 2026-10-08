# Tasks: X3 持久化与索引探索

- [x] 9.1 深读写入/索引/生命周期主路径，产出机制地图章节（≥15 处 文件:符号 引用，含每 turn 写入账表与查询面清单）—— 验证：`grep -oE '[a-z_/]+\.go' 报告 | sort -u | wc -l ≥ 10`
- [x] 9.2 完成 H1-H8 假设核验表（含写放大静态账与索引补白）—— 验证：`grep -c '^| H' 报告 ≥ 8`
- [x] 9.3 运行验证命令候选（命令账记录全文+退出码）并落盘 `docs/.dev/20261007-code-exploration-X3-persistence-indexing.md` —— 验证：`test -f 该文件 && grep -c '^## ' 该文件 ≥ 7 && grep -c 'go test' 该文件 ≥ 1`
