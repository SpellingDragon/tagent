# host-layer：核心 Host 层——Boot 一行取代 56 个装配函数

## Why
wechat.go 3053 行中 56 个函数（~2200 行）是框架装配逻辑（文档解析/agent 构建/模型解析/工具面构造/冥想驱动/HTTP 面），不是微信自定义。21 个 tagent 包 import 进应用层。设计预期：应用层只做自定义＋挂点注册。

## What
核心加 Host 层：Boot(configPath)→全自动装配；wechat.go 只留微信协议适配（~800 行）；main.go 只留 Boot＋bindWechat（~50 行）。

## Capabilities
host-layer：Boot 入口＋工具面自动装配＋冥想归核＋标准 HTTP 面。
