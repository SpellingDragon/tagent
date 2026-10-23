# design：learning-core-wiring

## D1 替换事件化（否决：直接调用 Replace）
配置变更→事实→lease 消费——与冥想/压缩同模式；链是唯一真相，替换历史可审计。

## D2 lease 借版本（否决：请求内直取 model）
请求开始借 current 版本引用——请求中版本不换（lease 既有不变量），新请求自动新版本。

## D3 Capture 三点接线（否决：仅 Recorder）
Recorder 记版本化记录；Capture 固定请求视图＋封印——两者互补（S09/S10 各承诺一半）。

## D4 导出读链（否决：生产路径内联导出）
ChainExporter 消费链（读投影/事实）组装样本——训练导出不侵入运行路径，崩溃安全。

## D5 并发
W1（loop+capture+lease+事件——热点合单）与 W2（exporter+S10+golden）文件零相交。
