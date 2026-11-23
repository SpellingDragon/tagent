# 04 远端同步（tagent.yaml）

## Why

一级 design §四 D4 乙硬要求：预算门开 ⟺ `context_window>0`（唯一来源），`replay_window` 不再兼任预算窗缺省。远端 `tagent.yaml` 现只声明 `max_tokens:128000`（回放窗）、无 `provider_context_window`——乙下若不显式声明 `context_window`，微信助手长会话**失去预算保护**（门闭、整包发送、无拒发）。故 D-04 升级为**必做**。

## What

`examples/wechat-bot/tagent.yaml`、`tagent.rl.yaml`：`max_tokens:128000`→`replay_window:128000` + **必显式** `context_window:128000`；按需声明输出限 `max_tokens`；注释如实改写（「待接线」清单随归位更新）。

## 边界与依赖

- **依赖**：01 键名落地（W1 后，新键才可 strict 解码）。
- **被依赖方**：远端 wechat-bot 部署（唯一部署档）；smoke 入口（2×回放窗解耦意图保留）。
- **接口面**：`host_document.go` hostAgent yaml 标签是远端 yaml 实际解析层；`loadHostDocument` strict 解码拒未知键。
- **禁止事项**：不触外来未跟踪五件；不改 providers 注册表连接属性（provider/api_endpoint/api_key_env，本期不新增 model 级容量）；与 03/05 触碰集互不相交，可并行；不触远端运行中的机器人（仅改仓库内 yaml 声明，部署须用户授权）。