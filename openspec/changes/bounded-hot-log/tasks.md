# Tasks：bounded-hot-log（一级只编排）

> 本文件只编排。派发唯一依据是 [ORCHESTRATION.md](ORCHESTRATION.md) 与各子变更 `tasks.md`。
> 启动门：runtime-coherence 的 CI 修复收口（PR 可合并）后，01.101 方可派发。
> 波次只是汇报分组；解锁以依赖叶有同候选证据为准。

```mermaid
flowchart TD
    A0[01.101 增长构成实测] --> A1[01.102 有界判据形式化]
    A0 --> A2[01.103 归档契约与配置面]
    A1 --> A3[01.104 设计签发＋独立审]
    A2 --> A3
    A3 --> B1[02.201 段轮转与封段]
    B1 --> B2[02.202 搬迁与 manifest]
    B1 --> B3[02.203 热盘 GC 与索引收缩]
    B2 --> B4[02.204 定位读路径]
    B3 --> B4
    B4 --> C1[03.301 recall/查询/导出跨档]
    C1 --> C2[03.302 有界锚与损坏负锚]
    D1[04.401 缓存接线] --> D2[04.402 race 分片]
    C2 --> F[F1–F4]
    D2 --> F
    D2 --> D3[04.403 真 CI 读数]
    D3 --> F
```

## 子变更

- [ ] [changes/01-bounds-contract/](changes/01-bounds-contract/)：增长实测、判据、归档契约、签发（01.101–01.104）
- [ ] [changes/02-rotation-archive/](changes/02-rotation-archive/)：轮转/封段/搬迁/GC/定位读（02.201–02.204）
- [ ] [changes/03-read-paths-release/](changes/03-read-paths-release/)：消费一致性与验收（03.301–03.302）
- [ ] [changes/04-ci-throughput/](changes/04-ci-throughput/)：CI 缓存与 race 分片、真 CI 读数（04.401–04.403）

## 收尾（一级直管）

- [ ] F1 承接与契约修订零遗漏：`session-checkpoint-material`/`bounded-recovery` 措辞、GLOSSARY 术语、升级说明
- [ ] F2 全量门禁：冻结树完整矩阵＋三 module＋race＋lint＋openspec
- [ ] F3 DoD：S1 稳态平台锚红转绿（回滚即红）、S2 损坏具名拒、P1–P8 逐条证据（含承诺面审计：强度主张≡条件句＋成对见证锚；强度词扫描 warn-only 报告入档，词表可绕过不作门）
- [ ] F4 独立审 frontier 空后归档；真实 CI 绿才判放行
