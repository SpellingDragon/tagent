# Proposal: 根包测试按 wiki 页合并（24→8 文件）

## Why

用户裁决：根包测试文件数过多，按 wiki 页合并（明知回滚 P4 节锚切分）。上一档 `root-slim-second-pass` 的 C 组已判定测试不可外移（0/24），合并是文件数问题的最终解。

**本轮执行已达 90% 并强制回滚**（工作树必须干净收场）：第三版「保守合并」方案已验证 vet 绿 + 测试数 196 完美守恒（func Test 集合逐名对账零丢失），仅剩 14 处 `free-standing`（成员 2~4 的文件头叙述块在合并文件中部非法）未修；修注释时引入声明行误删，按纪律回滚。

## What Changes

- 四组合并：`org_hotreload_test.go`←12（org 页全部）、`cross_generation_test.go`←2、`agent_architecture_test.go`←4（新名）、`resources_test.go`←3；`build_cycle`/`prompts`/`registry` 单文件不动。24→8。
- 合并方式（已验证部分）：**保守合并**——成员内容逐字节保留，只剥 package 与 import 块；import 并集**保完整行含别名**（`agent/task` 与 `tool/task` 撞名教训）；成员间以空行相接。
- 剩余 10%：成员 2~N 的文件头块（锚行+文件级叙述，可能粘连首个 Test doc）在合并文件中部触发 `free-standing`。剥离规则：块内**锚行与文件级叙述删除，粘连的 Test doc（`// Test` 开头行及其 bullets/契约续行）保留**。**教训（三坏根因）**：doc 块边界判定绝不能动非注释行；修完必须 `gofmt -l`+`go vet`（退出码用 PIPESTATUS 判，`|head` 会吃掉）+测试数对账三连。

## Impact

- Affected specs: 无（同位门对"每页单文件"更稳；测试集合守恒 196 由对账脚本证明）
- Affected code: 根包 24→8 测试文件；wiki/README 若有"测试位于某文件"表述同步
