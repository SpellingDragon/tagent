# Proposal: host-send-tool

## Why

投递状态机的根源性错配（2026-10-09 宿主案例裁决入档）：**"谁叫醒回合"决定"输出能不能送达"，而模型想对用户说话的表达意图与回合触发源无关**。housekeeping 回合（task-batch-retire 等）里的用户向内容被谱系门正确扣留——门没错，缺的是一条合法的主动通道。meditator 的 `deliver` 工具已在冥想线验证了"显式发送意图 + 白名单 + 拒绝回模型"的完整形态；本变更把它泛化到微信宿主：任何配了 `send` 工具的 agent，在任何回合都能显式把内容送达用户，不再受谱系被动通道约束、也不引入任何时间邻近猜测。

## What Changes

1. **`send` 工具**（examples/wechat-bot 域）：参数 `{content}`（单主人场景免 target；目标解析复用宿主既有三级规则 meta_chat_id>lastActive>拒）；content ≤8KiB；身份由装配闭包固定（发送通道即宿主 bot 实例，LLM 不可指定通道/凭据）；一切失败以 `[send_denied] <具名原因>` 结果文本回模型不断回合；发送路径复用 main.go 现有逻辑（typing 清理、长文 context-token 拆分/截断、附件提取、send-failed 回执）——**不新开第二条到微信的通路**。
2. **授予即授权**：工具经 yaml ToolRef 显式声明挂载（不配则模型不可见）；挂载面默认建议 entry 与 meditator。
3. **心智对齐（提示词层）**：TOOLS/AGENTS/meditator 提示词补一句——非用户回合产出的用户向内容不会自动送达：配了 send 就显式投，没配就等下一用户回合补报（扣留回执在事实链可 recall）。
4. spec：新 capability `wechat-host-send-tool`（授予条件/身份固定/目标三级/上界/拒绝回模型/复用既有发送通路/回执一致）。

## 边界与依赖
- **不动**：谱系白名单与被动通道（user/task/告警通知语义零变化）、投递缝 deliver、扣留回执机制。
- wechat-bot 为独立 go module，工具实现于其内（可 import 根包/框架）；装配走既有 ToolRef 扩展点（factory/option 先例由实施期 grep 定）。
- 被否方案：按时间邻近"延续保留 user 血统"（拒绝猜测，前案已裁）；给框架层加通用 send（通道是宿主概念，微信发送细节不属于 framework）。
