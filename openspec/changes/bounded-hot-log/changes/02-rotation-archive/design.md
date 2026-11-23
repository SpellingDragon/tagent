# Design · 02

## D1 段与轮转（02.201）

- 段界＝cut：轮转点即 cut 提交批在日志中的位置；段头记录段 id、起止提交序、对应 cut 序号、字节数。字节阈值仅**请求提前 cut**，不在非安静点封段（顶层 D3）。同输入段界确定（由 cut 序号与提交序决定，不依赖时间精度）。
- 封段器生成段索引（键→偏移+长度+校验），随段文件旁置；索引是派生物，可从段重建（具名通知），不作第二真相。
- 与 checkpoint 的关系：同一 cut 的两个投影——逻辑层取状态切片，物理层取段边界与 manifest 提交；安静点标记与切片事实属其 cut 所在段，该段恒热直至下一 cut 推进搬迁水位（段界 ≤ 倒数第二个 cut 的段可搬）。
- 段界只在 cut 提交批的 barrier 返回后推进；cut 失败（quiet 标记已写而 checkpoint 未落盘）不封段，残帧留在开段，由 `tailFactKeys` 的 `quietPointBoundsTheAccount` 识别。

## D2 搬迁与 manifest（02.202）

- 序：新段 file 写归档卷→fsync→manifest 原子提交（generation+1，含段元数据与 sha256）→删热段→更新热索引收缩水位。
- 崩溃切点：copy 中（归档半文件，manifest 未提）→重开忽略并重搬；fsync 后 manifest 前→同上；manifest 后删除前→重开删热段（幂等）；删除中→逐段幂等。
- manifest 双文件 swap；损坏/缺→`ErrArchiveCorrupt` 拒 boot 读该域，绝不当作“段不存在”。

## D3 热盘 GC 与索引收缩（02.203）

- 热段删除后：槽表条目按段界清除、`order.log` 重写为该热段交集（或分段 order 文件）、original/ref 族按 01.101 结论随段迁移或按引用保留；不得留下指向归档键的热索引条目（定位改走 manifest）。
- 收缩后 `slots` 头部的 `Seq/ValuesSize/OrderSize` 水印按新热文件重述——物理重放水位的坐标系是收缩后的热文件，不是历史日志；重述从 manifest 覆盖的水位之后重建，与 `order.log`/`order.line` 同一事务面。
- 收缩必须与 manifest 同水位：崩溃后以 manifest 为准重算，不反向用热索引修 manifest。

## D4 定位读（02.204）

- `Read(key)`：热槽表→命中直读；未中→manifest 段域判定（键域摘要）→段索引→段读取并校验；三态错误契约落地；`pointReadLog` 窄口语义不变。**boot 只校验热段与 manifest/段索引，归档段读时校验（不预扫归档卷）**——开档校验量有界于热窗。
- 性能：段域判定 O(#segments)；段索引二分；读延时可测（01.102 口径）。

## D5 签发区

逐叶精确写面/锚/崩溃注入点/回滚触发；共享面 `journal.go`/`segment_store.go` 由编排者单写或串行。

## D6 停止条件

- 需改事实身份/账本责任；需在热卷内保留全部键定位指针（则热索引不有界）→回设计门。
