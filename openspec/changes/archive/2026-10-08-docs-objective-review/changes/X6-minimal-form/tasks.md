# Tasks: X6 最小形态与收敛探索

- [x] 12.1 深读依赖图/默认值/装配面，产出机制地图章节（≥15 处 文件:符号 引用，含三档能力分类表：默认装配/opt-in/死代码候选）—— 验证：`grep -oE '[a-z_/]+\.go' 报告 | sort -u | wc -l ≥ 10`
- [x] 12.2 完成 H1-H6 假设核验表，并交叉消费 X1-X5 报告（冲突对登记 + 引用各报告 ≥5 处）【前置：7-11 全部报告落盘且四查过】—— 验证：`grep -c '^| H' 报告 ≥ 6 && grep -c 'code-exploration-X[1-5]' 报告 ≥ 5`
- [x] 12.3 运行 go build/go vet/go list 只读验证（命令账记录全文+退出码）并落盘 `docs/.dev/20261007-code-exploration-X6-minimal-form.md` —— 验证：`test -f 该文件 && grep -c '^## ' 该文件 ≥ 7 && grep -c 'go build\|go vet\|go list' 该文件 ≥ 1`
