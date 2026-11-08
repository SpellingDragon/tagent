# 06-guards-and-tests 提案

## Why

拓展与突破的边界须机械守护；测试按用户裁定后置到实现完成后重写。

## What Changes

F0 新增「链外持久物必须由已登记票据事实按内容寻址引用并校验」规则；R3 登记新引用面；重写测试（正/负/消费者/症状锚）。

## 边界与依赖

依赖：01–05；被依赖方：收尾；接口面：scripts/gates/check_durable_truth_registry.py、golden/runtime/durable_truth.json、各域测试文件；禁止事项：MUST NOT 追认缩水验收面。
