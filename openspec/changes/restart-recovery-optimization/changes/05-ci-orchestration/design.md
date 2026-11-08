# 05-ci-orchestration 设计

## 技术要点

03.10 校准为唯一输入（evals-harness spec）；今日校准推荐 4180s/424min；runner 超时前必须落 test-log/evidence（当前 RunnerError 直接抛导致 if-no-files-found: error）；具名 liveness 替代闹钟。

## 风险与回退

见上；每单以复跑命令为准，失败即回退本单并回写计划。
