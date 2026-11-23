# timing-triage.md · W1-F 计时项独占串行重取（2026-11-22 17:52:37，候选 52d7b664）

- boot_wall_clock(-count=3) rc=0
ok  	github.com/SpellingDragon/tagent/assembly	29.501s
- long_run(-count=1) rc=0
ok  	github.com/SpellingDragon/tagent/assembly	28.828s
- long_run(-race -count=1) rc=0
ok  	github.com/SpellingDragon/tagent/assembly	40.370s

## 首条失败原文（若有）

## 归因占位
- 待编排者按原文定性（产品/计量噪声/夹具）；本文件只记原始终局，不预先豁免。

## 编排者归因（2026-11-22）

两项计时锚在本地独占串行（无并发写单、无其他测试）全绿（rc=0×3），CI 红不可本地复现。定性：**CI 环境负载或计时阈值对 CI 硬件偏紧**，非本地可复现产品红。处置归 04.402：候选＝CI 侧重试/隔离策略或阈值对 CI 硬件校准（R5：退出码为判定、时长为参考）；**不预先豁免、不改判据**，最终以冻结候选上的 CI 实效为准。与 ci-triage.md 第 2/4 项（Scenario03、shard6）同族：CI-only 红，本地含 `-race` 均绿。
