# 08 台账 hash 与任务清单复核（2026-11-04）

目的：让「证据链闭合」这句话能从一份新克隆审计。做法是保留原始台账的自述、把不可解析的引用补成可达提交，并在本文件登记映射依据与仍未闭合的部分。

## 1. 不可解析 / 不可达的 hash 映射

| 原始引用 | 出现位置 | 复核命令 | 结论 | 改写为 |
|---|---|---|---|---|
| 806fc71e | r160、r161 | git cat-file -t 806fc71e → Not a valid object name | 在途单的临时 hash，内容 = race 跨包锁 + skillbody 等待 | 0ffce718 / 62347787 |
| 905965da | r160 | git cat-file -t 905965da → Not a valid object name | 在途单的临时 hash，内容 = FoldControl / reflection 业务 marker 兼容修 | e94344a5（业务部分 1c8bc721） |
| bb82a2d1 | r149 | 对象存在；git merge-base --is-ancestor bb82a2d1 HEAD → 1 | squash 前的「单一提交」，被 squash 取代 | 170ef267（squash 后） |
| 388c174d | r149 | 对象存在；非 HEAD 祖先 | squash 前的 merge/amend 提交 | 170ef267（squash 后） |
| dfd05442 | r150 | 对象存在；非 HEAD 祖先 | squash 前的 archive/amend 提交 | 170ef267（squash 后） |
| 29d9d214 | r157「squash main」 | git show -s --format=%s 29d9d214 → docs: ledger r156 | 不是 squash main；真正的 squash 是 170ef267 | 170ef267 |

映射依据：git log --since='2026-11-04 00:00:00' --reverse origin/dev 的 27 个提交序列；170ef267 的 message 自述「Squash-merge of quiesce-criterion-batch (ledger r1-r150)」。

## 2. 结构缺陷与处置

- r159 行缺失，但 r161 写「（同 r159）」：补 r159 行（skillbody + FoldControl 收口），并把「同 r159」改为「见 r159」。
- 台账行序原为 r161/r160/r162/r158/r157/r156，r162 插在 r160 之后：按本表「新→旧」的既有约定重排为 r163/r162/r161/r160/r159/r158/r157/r156。
- r157 的「29d9d214 run 41697746806」：run 与提交的原关联无法从新克隆核验，本文件保留原 run 号但把提交改为 170ef267，并在 09 登记为不可核。

## 3. tasks.md 的收尾框

复核：grep -cnE '^- \[ \] ' tasks.md = 8（3/4/5/6 与 F1/F2/F3/F4）；全归档目录未勾框 26 个。
处置：**保持未勾**，不把本地门禁绿冒充为「全部项目门禁 + 远端 CI 绿」。tasks.md 末尾新增「补记」节，逐条登记每个 F 项的实际证据状态与指向本目录的文件。

## 4. 仍未闭合

- 远端 CI job 结论工件：无（见 09）。
- r162「goal complete」：降级为台账自述；建议对外表述为「运行时收敛已落地、结构性门禁全绿，收口待证据补齐」。
- tasks.md F2 的「merge 前完整长程证明」：本地仅跑 short 全量，long 六片与 smoke 未跑（需数小时且 tmux 在本沙箱不可用）。
