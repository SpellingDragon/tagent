# O5 可选决策采集保真

## Why

现有轨迹记录可保存messages和末响应，但没有独立工具声明、稳定逐调用归属或可确认的丢失统计。训练需要说明“这条样本记录了什么、缺什么”，不要求生产agent依赖遥测或同步等待训练写盘。

## What Changes
- 新增默认关闭的v2采集，复用O3请求快照并记录SDK边界，不宣称wire保真。
- 逐调用身份与响应/事件精确关联，缺失/冲突显式unbound。
- 非阻塞字节有界录制、响应保真、统计与封账manifest；旧录制仍可用。

## Capabilities
- 新增decision-capture；不改变trajectory-recording旧模式的队列满可丢/不阻塞合同。

## 边界与依赖
- 父docs-objective-review；D14-S1/S2/S3为确定接口。
- O5.2依赖O3.2快照；O5.3/O5.4可用S1 fixture并行；O5.5依赖O1.6/O2.6/O3.6及I0真实Runner关联钉测。
- 被依赖方O6.6/真实模型离线闭环。共享插件、ctx、config和root接线仅编排者写。
- 禁止：强制OTel、全HTTP body捕获、改任务调度/TTL、最近call猜测、通过同步写盘拖慢生产模型、默认发送敏感数据。
