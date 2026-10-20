# design：host-protocol

## D1 Boot vs Assemble 分层
Boot=turnkey（配置→运行一条路，面向应用宿主）；Assemble=parts（手动组装，面向框架扩展/测试）。

## D2 协议（proposal.md 签名冻结）
Host/Boot/HostOption/MessageSender/DeliverFunc/FaceAddr——两端唯一契约。

## D3 并发
H1（根包 host.go＋测试）与 H2（examples/wechat-bot）文件零相交——协议冻结后同时派发。

## D4 验收
wechat main≤50 行/go≤1000 行/import≤3；Boot 零手动装配；golden match。
