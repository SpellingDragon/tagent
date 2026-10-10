---
name: gopls-code-nav
description: >
  Go 代码精确导航：跳转定义、找全部引用、接口实现、符号搜索。基于类型系统给出
  精确答案，替代 grep 的猜测式搜索。适用于：改函数签名前评估影响面、找调用点、
  区分同名符号、查接口实现、大仓库符号检索。
---

# Go 代码导航技能 (gopls Code Nav)

## 🎯 何时用

- **找引用**：改函数/类型签名前，列出所有调用点（影响面分析）
- **跳定义**：精确到符号定义（区分同名）
- **实现查询**：接口的全部实现
- **符号搜索**：跨仓库按符号名找位置

## 📋 前置

- 二进制：`/home/lighthouse/data/go/bin/gopls`（v0.23.0）
- **必须在含 go.mod 的 module 目录内执行**
- 首次对仓库调用会触发索引（大仓库 30-60s），之后快
- 本机主战场：`/home/lighthouse/QQchannelRobot`

## 🔧 命令模板

```bash
export PATH=$PATH:/home/lighthouse/data/go/bin
cd /home/lighthouse/QQchannelRobot

# 符号定义位置
gopls definition service/gate/whitelist.go:69:6

# 所有引用（影响面分析——最常用）
gopls references service/gate/whitelist.go:69:6

# 接口全部实现
gopls implements robot.go:233:10

# 文件内符号表
gopls symbols service/gate/gating.go

# 全仓库符号搜索
gopls workspace_symbol EnableGuild
```

## 📐 行列号获取

gopls 需要 `file:line:col`（1-based）：

```bash
grep -n 'EnableGuild' robot.go   # L52 → 用 robot.go:52:1（col 取符号起始列，粗略给 1 即可）
```

## ⚖️ 与 grep 分工

| 工具 | 适合 | 代价 |
|------|------|------|
| grep | 存在性确认、文本搜索 | 便宜、即时 |
| gopls | 类型级问题（调用点/实现/定义） | 首次索引贵，之后快 |

## 🕳 本机坑

- `gopls@latest` 需 go≥1.26，本机 go1.24 装不上——现用 v0.23.0 二进制（已验证可跑）
- **装完必读回 version 输出**：此前三次 `go install` 静默失败（报错被管道 tail 吞），`~/go/bin` 为空却误报装好
- 输出为空 + RC=0 = 零引用（这本身是有用结论，如 EnableGuild 零调用点 = 未接线）
