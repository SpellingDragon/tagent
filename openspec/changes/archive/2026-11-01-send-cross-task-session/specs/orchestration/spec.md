# orchestration Specification

## Purpose

本变更的编排契约：启动门、波次解锁、契约守护与三级验收的可验收条款。静态决策见 design.md，驱动方式与当场账见 ORCHESTRATION.md。

## ADDED Requirements

### Requirement: 最小可证伪路径先行

W0 SHALL 在全量展开前完成症状锚 S2 的红→绿短测（从用户症状直通缺陷机制）；S2 未绿，W1 之外的实现单 MUST NOT 派发（R34 规模警戒：叶数>20）。

#### Scenario: S2 先行

- **WHEN** W0 结束
- **THEN** `TestSendSymptomReentersAgentTask` 由红转绿的双证据入台账，且其红因命中"重入不可达"而非编译失败

### Requirement: 波次解锁以依赖入 main 为准

波次仅为汇报分组；子域 SHALL 按 design D2 外部依赖解锁，同文件簇（host_tools/host_document、wechat*.go）跨域改动 MUST 合单或串行。

#### Scenario: 同文件簇不合单即冲突

- **WHEN** 两单触碰集相交且未合单
- **THEN** 编排者 MUST 拦截派发（触碰集=改写文件 ∪ 构建闭包 ∪ 验证读取包）

### Requirement: verify 三锚随任务行冻结

每叶 SHALL 携带正锚（测试名与 -run 正则逐字符一致）、负锚（具名错误路径）、消费者锚（正式入口/真实路径）；验收面缩水 SHALL 重开该叶（R29）。

#### Scenario: 空转拦截

- **WHEN** 正锚测试名与 -run 正则不一致或零匹配
- **THEN** 该叶勾选无效（非空转证明：用例数>0）

### Requirement: 症状锚红转绿双证据

变更完成 SHALL 携带 S1–S3+N1–N6 的红转绿双证据（完整回滚本变更必红、修复后必绿）入 run notes；工程锚全绿而症状锚红时以症状为准（R33）。

#### Scenario: 回滚即红

- **WHEN** 收尾 F3 执行
- **THEN** 回滚树上九则症状锚全红且红因命中各自语义缺失，修复树全绿

### Requirement: 契约守护三层闭环

主 `openspec/specs/` 与 `api/` 的改动 SHALL 限于 design D2 表声明的 MODIFIED/ADDED delta；F1 `git diff` 断言、合并规则硬拦、spec 场景映射三层齐备。

#### Scenario: 越界契约改动被拦

- **WHEN** 实现引入 D2 表外的主 spec 改动
- **THEN** F1 断言失败并定位到文件

### Requirement: 波后偏差评估与独立放行

每波收尾 SHALL 派独立验证子代理对照原任务行验收面五面核查（R30）；每个修复批次后 SHALL 过独立只读审者拷问至 frontier 空（R35），放行证据取自提交树。

#### Scenario: 追认缩水被拒

- **WHEN** 勾选按缩水后验收面追认
- **THEN** 偏差报告判"补差重开"，返工止于波内
