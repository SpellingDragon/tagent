# Openspec 误删恢复记录（2026-09-15）

## 事故
`rm -rf openspec/changes` 级误删波及 9 个 change 目录 53 文件；git 通道关闭
（`.gitignore:82` 忽略 `openspec/`，全历史无入库），唯一恢复源=LLM 轨迹挖掘。

## 恢复结果
- **22 个内容文件**：从 `data/trajectories/wechat-session.jsonl`（4189 条请求）
  按"恢复历史中出现过的文件正文"回收，已落回原位（staging: /tmp/openspec_recovery）。
- **8 个 `.openspec.yaml`**：模板确定性重建（schema: spec-driven + created 日期）。
- **15 个内容文件未恢复**（轨迹中从未以完整正文出现过）：
  - 已归档/已合并（13）：archive/2026-09-10-tagent-action-quiet-timeout 全 4 件、
    tagent-compress-event-sourcing/spec（实现=event-sourcing rebuild 已在主干）、
    tagent-restart-wakeup-loop/tasks（被保险链+回收机制覆盖）、
    tagent-unify-model-call-config 其余（实现=cc21241 已落地）、
    fix-tagent-flaky-tmux-longoutput 部分（实现已上线）、
    tagent-ima-wiki-batch-maintenance 部分（知识库已迁 ima 云端单源）
  - 素材指针：plan 7c2d4d8b settle 回执含 unify 四件套完整结构摘要
    （memory key 1201451c35000000，可 recall 深挖）；其余可按需重建。

## 防复发
- 本文件即单一真相源；后续如需正式重建某个 change，凭指针召回素材、
  标注"恢复重建版"另起日期。
