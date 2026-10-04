## ADDED Requirements

### Requirement: race 门覆盖第一方全包

CI 的 race job MUST 覆盖主模块全部第一方包，含根包 `.`；CI 命令与本地等价命令（`./scripts/race_check.sh .` 及各子包集）MUST 同参。扩大覆盖 MUST 以本地同参命令通过为前置；命中 race 时按既有纪律处置——第一方 race 先修，上游签名 race 走登记 waiver——MUST NOT 通过收窄包集合或放水分类器换取绿色。

#### Scenario: 根包交错有证据力

- **WHEN** 根包内出现跨 goroutine 交错缺陷
- **THEN** race job 能以失败暴露它，而不是只对 `./agent/...` 等子集有证据力

#### Scenario: 加包不得放水

- **WHEN** 把新包加入 race job 时本地同参命令出现 race
- **THEN** 处置路径为修复或登记 waiver，包集合不得因此回退
