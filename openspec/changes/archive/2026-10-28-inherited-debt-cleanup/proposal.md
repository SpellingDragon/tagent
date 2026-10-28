# 遗留清理：smoke 静默红、U5 竞态升级、结构门红、文档悬空

## 症状（验收矩阵第一行）

| 原话 | 直译测试 |
|---|---|
| 「本机tmux应该是可用的，其他遗留事项一并清理」（用户，2026-10-28） | smoke 全套（tmux 面真实执行）全绿；race 门无 U5 豁免仍绿；check_runtime_structure 绿；wiki 悬空引用清零 |

## Why

前案收口时登记的继承债：①Wave2Data/Recall 基线前已破（受理回执最小化、recall_recent 语义演进后断言未随迁——静默失效家族）；②trpc-mcp-go v0.0.10 U5 竞态已有 v0.0.18 可升；③tmux_executor.go 547>500 行结构门红；④wiki 存量悬空引用（3369af7 前旧树）；⑤data-less 未跟踪残留文件。

## What Changes

- Wave2Data：receipt 断言改受理回执形状（{callID taskID accepted kind}，Accepted=true、非 denied）。
- Wave2Recall：recall_recent 断言改下界语义（count>=2 且夹具可见——运行时自产事实入链为真实语义，恒等 2 是旧世界假设）。
- mcp v0.0.10→v0.0.18，删 U5 双签名，race 全表复跑。
- tmux_executor.go 拆分（session 辅助/命令构造各归其文件，<500 行）。
- wiki 悬空引用就地修（behavior-matrix:9/:22、agent-architecture 结构表、refine/asset_drift 行）。
- data-less 残留文件移出仓库（备份至 /tmp）。

## Impact

examples/wechat-bot 测试面、tool/exec 文件拆分、go.mod 升级、scripts/gates/race_check.sh、docs/wiki。风险：mcp 升级 API 面（tool/mcp 适配层核对）。

## 必答一问

「清理后还有静默红或继承红吗？」——smoke 全套绿+race 无豁免绿+结构门绿三锚为证；FoldControl 复现率复测（环境恢复后）入档。
