# plain-structure：朴素结构重构

## Why
用户裁定：设计不妥协。两轮结构尝试（杂物抽屉误判→泛型注册机制）均不干净——机制层、绕边包、留壳文件是脏源。

## What
回到朴素：config=声明平铺（一处可读全键）、域=PlanOf+执行、根包=纯装配薄层、应用层=自定义+挂点注册。删机制债（section.go/memory-decl/recorder/meditation_plan）、spec 归位平铺、根包职责命名、应用层去壳。

## Capabilities
plain-structure：行为零变化（指纹/golden 字节不变）＋结构归位（file 级清单）。
