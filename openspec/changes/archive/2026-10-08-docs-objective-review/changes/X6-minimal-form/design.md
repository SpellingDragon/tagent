# Design: X6 最小形态与收敛探索

## 主读路径

`go list -deps ./...` 依赖图（工具产出）、`config/config.go` 全默认值、组合根 `tagent.go`/`build_agent.go`/`wiring.go`（装配面即能力清单）、`event/wf_facts.go`（退役重力）、`grep` 启用键全仓普查、X1-X5 六份探索报告（前置输入）。

## 核验假设详单

| # | 假设（来源） | 核验方法 |
|---|---|---|
| H1 | 五子系统代码量级（governance 2751 / reliability 2944 / evolution 2299 / org 1044+根包 / resources 610 行，含测试）与唯一非测试消费者 wechat-bot（D5） | 复测：`find + wc -l` 按目录；grep 启用键 |
| H2 | wf.* 七类型无退役条件（D2） | 读 wf_facts.go 与其消费面 |
| H3 | stub/死枚举清单（StoreEventWithEmbedding / rustviking 向量三命令 / TaskDead）（D1/D3） | 逐项 grep 调用方 |
| H4 | 依赖单向无循环（D2） | `go list -deps` 或 import 图工具复核 |
| H5 | X1-X5 报告间结论冲突清单（本轮新增） | 交叉比对六份报告的核验表，登记冲突对（同题异判） |
| H6 | "最小运行形态"的能力需求矩阵可由装配面推导（E12 交付） | 从组合根装配清单反推：默认装配 vs opt-in 装配 vs 死代码，三档分类 |

## 现有验证命令候选

`go build ./...`、`go vet ./...`（编译与静态检查作为依赖健康证据）；`go list -deps ./... | wc -l`。

## 风险与回退

X1-X5 若有未完成域，12.2 只消费已过四查的报告并在报告内明示缺口；冲突裁决只登记候选，不替 W3c 定案。
