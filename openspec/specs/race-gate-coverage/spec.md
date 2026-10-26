# race-gate-coverage Specification

## Purpose
TBD - created by archiving change harden-inbox-writer-uniqueness. Update Purpose after archive.
## Requirements
### Requirement: race 门覆盖第一方全包

CI 的 race job MUST 覆盖主模块全部第一方包，含根包 `.`；CI 命令与本地等价命令（`./scripts/race_check.sh .` 及各子包集）MUST 同参。扩大覆盖 MUST 以本地同参命令通过为前置；命中 race 时按既有纪律处置——第一方 race 先修，上游签名 race 走登记 waiver——MUST NOT 通过收窄包集合或放水分类器换取绿色。

race job SHALL 同时覆盖微信独立 module 的适用并发测试（`go -C examples/wechat-bot` 同参命令），该 module MUST NOT 因"非根 module"被排除在 race 证据之外。race 清单 SHALL 按新代码实际包**全量登记**（含子包）。本地确定性新增用例重复 10 次；真实付费用例 MUST NOT 被重复命令隐式触发。

`no DATA RACE` 文本本身 MUST NOT 被视为成功；上游 waiver 仅当**全部** DATA RACE 顶层帧命中登记签名/版本/测试**且**无普通（非 race）失败时适用。命令 SHALL 记录原始退出码；MUST NOT 提高空注释 baseline 或删除失败门以换取绿色。

#### Scenario: 根包交错有证据力

- **WHEN** 根包内出现跨 goroutine 交错缺陷
- **THEN** race job 能以失败暴露它，而不是只对 `./agent/...` 等子集有证据力

#### Scenario: 加包不得放水

- **WHEN** 把新包加入 race job 时本地同参命令出现 race
- **THEN** 处置路径为修复或登记 waiver，包集合不得因此回退

#### Scenario: 微信 module 不被排除

- **WHEN** 检查 race 覆盖清单
- **THEN** 微信独立 module 的适用并发测试包含在内，且与本地同参命令一致，无因 module 边界被静默排除

#### Scenario: 无 race 文本不是成功

- **WHEN** go test 因编译错误/非法包/超时/普通断言失败而非零退出，但输出不含 DATA RACE 文本
- **THEN** 门仍判失败；不以"无 race 文本"或分类器识别旧家族为由通过

