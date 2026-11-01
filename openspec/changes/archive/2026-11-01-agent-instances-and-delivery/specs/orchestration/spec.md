# orchestration Specification

## Purpose

本变更的编排契约：防护启动门、波次解锁、契约守护与三级验收的可验收条款。

## ADDED Requirements

### Requirement: 防护启动门

G1 实现单派发前，settle 恰好一次、会话占位、白名单 fail-closed 三条红线的既有测试 SHALL 复跑绿且防护用例立起；W0 未完成，01 域实现单 MUST NOT 派发。改动期间任一红线测试红 SHALL 立即停线。

#### Scenario: 红线先绿后改

- **WHEN** W0 未收口而 01 域实现单就绪
- **THEN** 编排者 MUST 拦截派发，直至防护面凭据入台账

### Requirement: verify 三锚随任务行冻结

每叶 SHALL 携带正锚（测试名与 -run 正则逐字符一致）、负锚（具名错误路径）、消费者锚（正式入口/真实路径）；验收面缩水 SHALL 重开该叶（R29）。

#### Scenario: 空转拦截

- **WHEN** 正锚零匹配或用例数为 0
- **THEN** 该叶勾选无效

### Requirement: 症状锚红转绿双证据

变更完成 SHALL 携带 I1–I4 的红转绿双证据（完整回滚必红、修复后必绿）入 run notes；工程锚全绿而症状锚红时以症状为准。

#### Scenario: 回滚即红

- **WHEN** 收尾 F3 执行
- **THEN** 回滚树上四则症状锚全红且红因命中各自语义缺失

### Requirement: 契约守护三层闭环

主 `openspec/specs/` 与 `api/` 的改动 SHALL 限于 design D2 表声明的 delta；F1 diff 断言、合并规则硬拦、spec 场景映射三层齐备。settle 恰好一次与会话占位语义 MUST NOT 在本变更中放宽。

#### Scenario: 越界契约改动被拦

- **WHEN** 实现引入 D2 表外的主 spec 改动或放宽红线语义
- **THEN** F1 断言失败或红线测试红并定位到文件

### Requirement: 波后偏差评估与独立放行

每波收尾 SHALL 派独立验证子代理对照原任务行验收面五面核查（R30）；修复批次后 SHALL 过独立只读审者拷问至 frontier 空（R35），放行证据取自提交树。

#### Scenario: 追认缩水被拒

- **WHEN** 勾选按缩水后验收面追认
- **THEN** 偏差报告判"补差重开"，返工止于波内
