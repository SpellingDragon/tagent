# 归档区（只读）

> 位置：`openspec/changes/archive/`。本目录是**历史账本**，不是工作区；这里的一切都是
> 已经落地的裁决与证据，改动它等于改写历史。

## 只读声明

1. **历史记录不改写**：归档件（`proposal.md` / `design.md` / `tasks.md` / `specs/` /
   `evidence/`）一经归档即冻结。勾选状态、任务编号、决策结论、证据数字都按当时实况保留，
   哪怕事后发现写得不准也不回改——改了就没人能复现当时的判断。
2. **勘误走新文件**：发现归档件有错、过时或与现状冲突时，在本目录**另起**勘误文件
   （如 `2026-10-18-runtime-rebuild/errata-<主题>.md`），或在新的活动变更里处置；
   不得就地改原文。勘误文件自己说明「针对哪件归档的哪一句、依据是什么」。
3. **归档件是审计证据**：`tasks.md` 的每个 `- [x]`、`evidence/` 里的 run-id 与指标、
   `ORCHESTRATION.md` 的台账，都是「声称完成」的可复核凭据。凭据不可事后补写、
   不可用追认缩水替换原验收面——这正是归档区存在的理由。
4. **只进不改**：新增归档只能由 `openspec archive <change>` 产生（归档前先跑
   `scripts/check-openspec.sh`，主 spec 必须 `--strict` 通过；**不要**用
   `--skip-specs` / `--no-validate` 绕过）。本文件是归档区唯一的非归档件。

只读性目前是**评审约定 + 下面这条旁证命令**，没有独立机器门；需要机器强制时，形式是
「归档件在归档提交之后零改动」的 diff 断言，而不是给目录加文件锁。

```bash
# 某件归档自归档提交以来是否被就地改过（输出为空 = 未被改写）
id=2026-10-18-runtime-rebuild
git log --oneline "$(git log -1 --format=%H -- "openspec/changes/archive/$id")..HEAD" \
  -- "openspec/changes/archive/$id"
```

## 归档清单

逐目录清单纯机械可生成，本文件不复制它（复制必然漂移）：

```bash
ls -d openspec/changes/archive/*/
```

下表只登记**需要人读直达证据入口**的归档；未登记的按目录名（`<日期>-<变更名>`）检索即可。

| 归档 | 主题 | 证据入口 |
|---|---|---|
| [2026-10-18-runtime-rebuild](2026-10-18-runtime-rebuild/) | 运行时十域重建：把「以 prototype 为准」的表述与落地后的 16 包现架构对齐，概念瘦身（S1–S4）与契约守护随同收口 | [tasks.md](2026-10-18-runtime-rebuild/tasks.md)（叶任务与勾选）· [ORCHESTRATION.md](2026-10-18-runtime-rebuild/ORCHESTRATION.md)（波次台账与放行凭据）· [design.md](2026-10-18-runtime-rebuild/design.md)（D2 子域表/D4 合并规则/D5 三级验收）· [evidence/](2026-10-18-runtime-rebuild/evidence/)（run-id 与指标） |
