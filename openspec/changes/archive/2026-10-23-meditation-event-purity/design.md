# design：meditation-event-purity

## D1 冥想驱动归核（否决：保留 host.go ticker）
ticker 留在 host.go＝冥想依赖宿主文档；Assemble 嵌入方无冥想。归核后 host 只解析声明＋挂载驱动。

## D2 事件驱动计时（否决：cron 轮询）
PostCommitObserver 推进计时器：事实提交→检查成熟（新素材＋interval＋min_gap）→fire。
无事件＝无新素材＝不反思。cron 在新颖性语义下多余且是"宿主级调度器"旁路。

## D3 跨会话写＝模型决策（否决：机制投递 DeliverFunc）
deliver_summary 工具：模型显式调用；目标=deliver_to 白名单活跃会话；事件落目标链。
机制投递删除——静默污染是 bug（用户裁定）。

## D4 并发（协议冻结后两单）
核心单（M1+M2 合并——host.go 热点文件）＋wechat 单（M3）文件零相交。

## D5 验收三点锚
负锚：无 deliver_summary 调用→零跨会话事件。正锚：调用→落链→下轮装配可见。防循环锚：summary 戳 no-novelty。
