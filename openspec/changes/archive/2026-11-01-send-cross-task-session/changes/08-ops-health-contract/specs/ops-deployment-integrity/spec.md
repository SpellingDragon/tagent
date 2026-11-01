# ops-deployment-integrity Specification（delta：补五态探测场景）

> 本文件为 MODIFIED delta。"healthz 鉴权不豁免"与"探针区分 401/拒绝/200"条款**方向不变**；补 403/超时两类与"鉴权失败不杀进程"显式场景（R6，design D12）。

## Purpose

运维探针的响应分类契约完备化：五态可辨、鉴权失败绝不触发杀进程。

## MODIFIED Requirements

### Requirement: 探针五态分类

restart / maintenance 健康探针 SHALL 区分五态：200→健康；401/403→AUTH_FAIL（告警、**不触发杀进程**）；超时→等待窗口内正常状态；连接拒绝→等待窗口内正常状态。鉴权失败 MUST NOT 被视作进程失效。

#### Scenario: 鉴权失败不杀进程

- **WHEN** 前置探测收到 401 或 403
- **THEN** 记 AUTH_FAIL 告警并退出判定，不进入重建/停止旧进程路径

#### Scenario: 五态各自可辨

- **WHEN** 分别模拟 200/401/403/超时/拒连
- **THEN** 各态进入正确分支（健康/告警/等待/等待/等待）

### Requirement: 契约与说明一致

healthz 的鉴权契约 SHALL 与运维脚本注释一致（实现鉴权不豁免，或显式修订条款并同步全部说明）；MUST NOT 存在"注释称受保护而实现免鉴权"的脱节。

#### Scenario: 注释与实现一致

- **WHEN** 阅读脚本注释并实测 healthz
- **THEN** 二者对鉴权的陈述一致
