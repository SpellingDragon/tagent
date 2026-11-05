# exec 回执完整性（exec-integrity）

父变更：boundary-closure（W1；依赖 W0 锚⑥红着基线）。

## Why

远端宿主 2026-11-04 正式故障上报（一级 design D1 E8）：`exec_run` 连续三次回放陈旧 scrollback 且回报 exit=0（末尾 `Pane is dead (status 0)`），touch 探针两轮未落盘——命令实际未执行而回执"成功"。静默失败被误报为成功，依赖 exec 回执的一切"已完成"结论不可信。远端规避纪律（每命令全新 tmux 会话）证明一次性会话模式可用，但框架侧未修。

## What Changes

1. 死 pane/陈旧回执检测：执行器在捕获输出与读取退出码前 MUST 校验 pane 状态与会话世代一致性；检测到死 pane 复用/世代漂移时 **MUST 重建会话并执行新命令**（一次性模式）；显式失败仅当重建不可行，MUST NOT 把旧 scrollback 与旧退出码当作本次结果，MUST NOT 以“探活后直接报错”替代重建。
2. 回执-事实一致性：`exec_run` 回执在声明成功前 MUST 能证明本次命令真实执行（会话世代绑定：新会话名/世代令牌进 prompt 或环境，回读校验），替代远端"探针写入读回"人工纪律。
3. 集成锚双跑：真实 tmux 集成测试本地+CI 双跑（本机 tmux 已实测可用，E11 更正）；fake-tmux 单测锚保留作快速回归。

## 边界与依赖

- **依赖**：无实现依赖（W0 锚⑥由编排者先落）。
- **被依赖方**：wechat-bot 宿主的 exec 纪律文档更新（远端待裁决项的框架侧闭环）。
- **接口面**：不改 api/**；`exec_run`/`exec_release` 工具声明与参数不变；ResourceKey 语义不变。
- **禁止事项**：不动 assembly/、memory/；不引入 tmux 以外的新执行后端；不为测试改生产分支语义。
