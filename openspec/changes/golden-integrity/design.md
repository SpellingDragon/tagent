# design：golden-integrity

## D1 窗口驱动（否决：独立 replay_budget）
agent 只声明 max_tokens（模型窗口）；呈现余量运行时推导。压缩阈值=窗口×compress_threshold。

## D2 AgeFoldPlugin 插件入流（否决：recordMapped 内联埋点/装配时触发/独立 ticker）
生效方式必须易理解可维护：压缩以 PostCommitObserver 插件身份入流（与冥想 Driver 同端口同模式）——装配处可见、独立文件、改压缩零触碰提交路径。
ObserveCommit 内同步判断——折叠是纯内存＋机械聚合（微秒级），零持久状态（幂等 key 自愈：Rebuild 重放→再触发→同 key）。

## D3 replica 删除（否决：保留并接线）
lean-sweep design 既定裁定硬删；生产消费者零；S07 场景测试是唯一消费者＝测试冒充生产。执行删除＋场景改写。

## D4 并发
G1（api/config+根包装配+replay 接线）与 G2（memory/replica+golden+S07）文件零相交——协议冻结后双单并发。
