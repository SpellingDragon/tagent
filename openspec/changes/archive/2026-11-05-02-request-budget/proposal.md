# 完整请求预算（request-budget）

父变更：boundary-closure（W2；依赖 W1 全入 main）。

## Why

预算目前只约束历史投影（一级 design D1 E4/E12）：固定提示词、技能正文、动态环境、工具定义不入最终请求总量。评审探针实证：32 token 历史窗口下约 10KB 固定提示仍完整进入请求。**能力定位（5.9 裁决；归因经 grill 更正）**：`request-budget-accounting` 能力已存在（2026-10-08 归档），实现被 **6625dff8（2026-10-26）** 删除、归档测试全消失——本域是**恢复既有能力**（MODIFIED delta 于 `request-budget-accounting`，目录 specs/request-budget-accounting/）。长期运行时新增技能/工具 schema/在途材料会使请求超出模型窗口而无任何降级或拒绝。

## What Changes

0. 缺口审计：对照 `request-budget-accounting` 归档 requirements ①-⑤（声明快照/完整输入诚实精度/单一压缩/摘要时限/usage 尾包）逐条核对 HEAD 现状，缺口清单入 design；本域必恢复 E4 核心面（完整输入统一预算+降级+拒发），其余面按审计结果裁决（小则并入，大则显式欠账）。
1. 在最终请求装配边界（requestBoundary/projectionAssembler）建立**统一预算计量**：固定文件+已载技能正文+动态环境+投影段+工具定义，与模型窗口同单位。
2. 超限处置（声明式优先级）：动态材料→技能正文（保声明区）→投影进一步折叠→仍超限则**显式拒绝**并落可观测事实（事件/日志），绝不静默截断系统必需面。
3. 输出余量：预算 MUST 预留 max_tokens 输出配额，输入侧预算=窗口−输出余量；**未声明输出余量时=provider 窗口的固定比例常量**（值冻结于代码常量，TestBudgetDefaultOutputReserve 锁定）。
4. 配置面：provider 窗口与输出余量为新增可选字段，FoldWindow/AgentSpec 允许声明总预算与各份额；**两字段 MUST 为导出字段（YAML 解码必需；AgentSpec 导出字段 24→26），concept budget 的 struct-fields AgentSpec 基线同批登记 24→26**；缺省规则=**预算门开 ⟺ 显式 provider 窗口声明 ∨ AgentSpec.MaxTokens>0（缺省推导）；MaxTokens=0 且未显式声明 = 预算门不启用（记 debug 事实，不裁剪不拒发）**；默认即预算生效（行为从“只约束回放”变为“约束整包”）记入 CHANGELOG；**拒发以 failed 终态落链并按既有 failure-notice 告知（不新增通道）**；预算裁决无实验开关（r22 纯化：W2 以无开关形态实施）。

## 边界与依赖

- **依赖**：W1 全入 main（01/04/05 的提交面与投影面稳定后再动请求面；无接口常数依赖）。
- **被依赖方**：06 live-benchmark 以本域预算语义为准（对照实验固定预算）；03 冒烟的 token 面受影响（集成序 02 先入）。
- **接口面**：不改 `api/**` 的唯一例外=`api/config` 新增两个可选字段（provider 窗口、输出余量；**`api/config/decode.go` 与 `api/config/fingerprint.go` 的配套行同例外清单**）；事件事实新增"预算决策"类型需与既有事实族命名一致。
- **禁止事项**：不动 assembly/host_delivery*；不实现新的 token 计数器（复用 FoldWindow 计量同源实现）；不改工具声明顺序稳定性承诺。
