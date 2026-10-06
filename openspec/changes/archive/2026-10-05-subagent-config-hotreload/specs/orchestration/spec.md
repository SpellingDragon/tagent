## ADDED Requirements

### Requirement: 二级子变更启动门

域 02（summary-knobs-hotparams）的实现类孙任务 SHALL NOT 启动，直至域 01（hotupdate-matrix-audit）的摘要 knob 归属结论（配置位置、通道归属、字段名）已随其 delta 并入 main specs。域 03（percall-subagent-overrides）无启动门，可与 W0 并行。

#### Scenario: 无结论先行被门禁拦下

- **WHEN** 域 02 的实现孙任务在 01 结论入 main 前被执行
- **THEN** 该产出 SHALL 视为未验收，编排者 SHALL 打回并要求按结论复跑

### Requirement: 三禁区契约守护

本变更族任何二级提交 SHALL NOT 触碰三禁区：① `agent/org/fingerprint.go` 的 `orgSubset` 字段集（fp 白名单）；② `NewTagentAgent` 的恒装源装配（无无源状态）；③ `applyHotAll` 提交点的零写入语义（不得重引入任何 push 式热参写入）。收尾 F1 以 diff 断言核验。

#### Scenario: 顺手改禁区被收尾断言拦截

- **WHEN** 任一二级域的提交修改了三禁区之任一
- **THEN** F1 的 `git diff <基线>..HEAD` 断言 SHALL 失败，该域 SHALL 说明缘由并经用户裁决后方可豁免

### Requirement: 变更级 DoD

一级变更完成 SHALL 同时满足：三个二级域各自 spec scenarios 全绿并归档；F1 三禁区 diff 为空；F2 全量门禁（lint.sh、根包 `-short`、`-race` 根+agent 包、openspec strict、CI 四 job）exit 0；F3 DoD 逐项核验记入档；F4 归档与导览完成。

#### Scenario: DoD 可勾选核验

- **WHEN** 编排者执行 F1-F4
- **THEN** 每项以命令 exit 0 或 diff 为空为判据，主观判断 SHALL NOT 充当勾选依据
