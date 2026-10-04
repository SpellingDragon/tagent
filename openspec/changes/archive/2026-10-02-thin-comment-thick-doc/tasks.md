## 1. P1 门禁上线（规则按语义两单元 + 基线落定）

- [x] 1.1 `missing-file-responsibility` 规则：非测试 `.go`（`scripts/` 豁免、嵌套模块按 POLICY_DIRS）无任何 `契约:/规格:` 索引行 → 一条 finding，消息含「先补文档再落索引」；放置位=任一文档槽位（OQ2 结案，镜像测试侧）。验证：单测覆盖——无索引生产文件计 finding、有索引零 finding、`_test.go` 与 `scripts/` 不计（谓词表+豁免实测）
- [x] 1.2 `doc-not-brief` 形态规则：声明 doc 含三个及以上散文段落（叙述段落计数，物理 wrap 不计；bullet/索引行不计）→ 一条 finding，消息含「迁文优先」。验证：单测覆盖——单句跨行 wrap 合规、两段合规、三段违规、bullet/索引不计厚、非导出声明同权、package doc 同形态（fixture 头部两段为合规活证）
- [x] 1.3 `gen_godoc` 共存定案：实测确认 `go doc -all` 把多文件 file.Doc 索引拼成概览 run-on blob → 裁决生成侧过滤（删 marker 行与裸 docs/ 碎片行），重生成后 residue 0、`--check` 绿、docs/api 纯减 194 行。已落地
- [x] 1.4 脚手架基线落定：权威实测 =113 / `doc-not-brief` =99（段落语义），`lint.sh --update-baseline` 写双槽；CI 即刻挡增量。验证：故意造一无索引新文件与一三段 doc → `lint.sh` 双 REGRESSION 红 → 移除恢复绿，留痕 `scaffold-probe.log`。自纠注：首次 update-baseline 曾为裸引 docs/wiki 路径的门禁自注释静默开通 `unindexed-path-ref:1` 新槽（棘轮旁路同型复发），已改措辞撤槽
- [x] 1.5 提交 P1（`scripts/comment_policy/` + `docs/api` 若再生），独立 revert 面

## 2. P2 权威测量与双对账锚冻结

- [x] 2.1 `pointer-map.txt`：112 文件逐一裁决目标小节（存在/需新建/需扩写三态标注），存入本 change 目录冻结为对账锚。验证：每行 `文件<TAB>目标小节<TAB>三态`，与 finding 清单一一对应
- [x] 2.2 `beyond-brief.txt`：形态规则权威输出逐声明冻结。验证：计数与 1.4 基线一致，偏差先回查规则再开工
- [x] 2.3 战役批规程固化进本 tasks（迁文优先→削薄→补指针→`go test <pkg>`+`-race`+`gen_godoc --check`→降幅恰等于 N→pathspec 提交），并核对 `agent/` 热区文件当前占用。**pathspec 陷阱增补**：`git commit -- <paths>` 静默跳过 untracked 新文件（colocation 3.17 实证——新名合并落点的文件曾漏入库）；凡批次创建新文件，必须先 `git add <新文件>` 再 pathspec 提交。**提交完整性对账**：批次提交后 `git status --short` 必须为空（3.4 实证漏网——pathspec 清单漏列已改文件，CI 树因此 +6 mfr +3 bnb 而红）

## 3. P3 分域战役（每批独立提交；域序：小域练流程，热区压后）

- [x] 3.1 evolution 批:三指针落位(#git-safety 裁决替兄弟锚 #test-facts,#verdict-states,#refine),0 削薄,基线 113→110
- [x] 3.2 tool 域指针半:22 文件落位(3 新锚 resident-continuity/recall-agent/tool-accessor=文档加厚;govx 裁决替兄弟锚为 #govx-entry-only;mcp.go 配 #mcp-live-registry),基线 110→88;削薄半(13)拆出为 3.2b
- [x] 3.3 memory 批:16 指针(三新锚 causal-chain/inmemory-store/file-segment-store+engine.go 改配 #engine-contract)+8 削薄(LocalFileKV 29→5;迁文两节 local-file-kv 落盘模型/error-tracking canonical replay,顺带校正陈旧句 kv.json→分桶),基线 mfr 88→72/bnb 86→78
- [x] 3.4 根包批一:13 指针+三新节(#composition-root/#config-surface 含 Config 的 YAML 示例迁文/#testing-helpers)+Config 44→4 等 14 例削薄,基线 mfr 59→56/bnb 78→65;自纠注:doc 块重写吞掉同组 契约: 行(builtin/testing/tool 三文件)已回补,后续批次改为"先改 doc 再插指针"次序
- [x] 3.5 根包批二:并入 3.4 单批完成(Properties 字段例迁 #config-surface),域内双锚清零
- [x] 3.6 agent/governance 批:9 指针(五域回退行改配 governance-enforcement 专节锚,tool.go 归 #governance-gate)+包注 9→7 要点化,基线 56→47/65→64;第三次 bullet 漏 // 前缀自纠
- [x] 3.7 agent/compress 批:8 指针(裸路径行改配 compression-and-telemetry 专节,64 行文档免锚要求)+6 例续行折要点,基线 47→39/64→58;两次自纠:bullet 漏 // 前缀、折行令 先…再… 首次命中 mechanism-narrative(跨行时逃过正则)——改述不开槽
- [x] 3.8 agent 批一:10 指针(execution-generations/event-flow/compression 专节锚,map 里不存在的 inject-runtime 与 session-model 已按真实锚图改配)+4 例削薄(agent.go 头注 20→7、NewTagentAgent、Run 20、ObligationReport 21),基线 39→29/58→54;两次脚本自伤(撇号未闭合、相邻字符串漏 
)均在 build 前拦截
- [x] 3.9 agent 批二:9 指针(task_record_sink 的 #task-record-contract 不存在,改配实有节 #restore-rebuild)+tool_agent 六例削薄(90→33 行),新建 wiki 两节 #delegation-retry 与 #tool-agent-factory 承接取消归属/重试形状/工厂只装配配置,基线 29→20/54→48
- [x] 3.9a 规程增补:**每次 --update-baseline 之后必须比对 counts 的键集合**,新出现的键一律视为本批自伤(本轮第三次踩同型旁路:tool_agent 自引 docs 路径成 unindexed-path-ref、"先释放…再报错"成 mechanism-narrative、map 陈旧锚成 index-anchor-unknown),必须改内容而非留槽（约 10 文件）
- [x] 3.10a agent 根域归拢：10 例机制叙述入档（execution-generations 补 #generation-wiring-window 与锁外退役/同对象不新造一代/登记先于交付，agent-architecture#framework-boundary 补单向数据流，event-flow 补事实写入同点原子与 processTurn 单原语，memory#feedback-bind 补反馈非凭据），基线 48→38；新节序号重复已纠（九→十）
- [x] 3.10b agent 根域收尾：5 例入档（durable-delivery 补提交门四态分类表，agent-architecture#test-support 补真机探针取证判据；meditation 双闸门文档已有，属无损收缩），基线 38→33
- [x] 3.10c agent/task：4 指针 + 5 例入档（task-lifecycle 新增 #resume-states 合法来源态表与面板不持久化段），基线 33→28 / 20→16
- [x] 3.10d agent/reliability + event：6 指针 + 3 例入档（durable-delivery 新增 #lineage-visibility 白名单同源、#transitional-reset 受管重置边界表、#quarantine-disposition 三结果义务表，#envelope-states 补回执三门），基线 28→25 / 16→10；第 N 次 bullet 漏 `//` 前缀由 build 拦截
- [x] 3.10e agent 余量 + 根包 + rl/workspace/tests：11 例入档（agent-architecture#package-layout 补 50 文件五组职责表、#test-support 补测试存储必须挪出工作树的分派顺序根因，platform-subsystems#composition-root 补单向依赖与 New 接线步骤，execution-generations 补交付账本屏障，rl-architecture#http-api 补 IterModel 保真四不变量），基线 25→14 / mtr 9→8；根包 plain 首跑 FAIL 经复跑与 race（213s）定性为并发负载下既有 flake
- [x] 3.10f 门禁工具与小域：新建 docs/comment-gate-tooling.md（命令面/等值见证语义/表纪律/棘轮作用域四节）承接 codetools 与 comment_policy 的 7 段机制叙述；新建 docs/wiki/agent/prototype-skeleton.md（六件套/生产映射/三条继承不变量）并登记进 wiki 目录；evaluation-suites 补 #offline-bench；testutil/modelutil/prototype/offline_bench 落 4 索引，基线 14→4 / 10→6（5 文件，第二模块独立验证三连）

## 4. P4 归零切硬与 counts 归空（CI 固化终态）

- [x] 4.1 双槽归零核对：两规则全仓 0 finding，`--update-baseline` 后双槽自然移除
- [x] 4.2 `missing-test-responsibility:9` 清理：9 个测试文件逐个补索引（目标小节不存在则先补文档）；该槽归零移除
- [x] 4.3 counts 归空断言：`baseline.json` counts 为空对象；负路径抽验——临时造一无索引生产文件与一 3 行段落 doc → `lint.sh` 双红 → 恢复绿，留痕 `hard-gate.log`
- [x] 4.4 归档：`openspec archive`，delta 并入 `code-documentation`，`openspec validate --specs --strict` 全绿
- [x] 4.5 终验与推送：`lint.sh` ok（含 doc-refs/gen_godoc --check/proc-refs）、`go build ./...` ok、`race_check.sh` OK（agent 83.6s 等 7 包）、bot 模块三连 ok、`openspec validate --specs --strict` 102/102、`baseline.json` counts 为空对象（全部规则 CI 硬零）
  - 全量 `go test ./... -short` 中根包 `TestLiveSessionStaysWatchedAcrossToolGeneration` 在并发负载下失败一次；单跑 3/3 通过，且 `check_comment_only.sh 473bb84 .` 证明 149 个 .go 变更里**唯一改了可执行代码的是门禁自身**（`scripts/comment_policy/main.go` 与其测试），全部产品文件纯注释——该失败不可能由本变更引入，定性为既有超时型抖动（与 hotreload 两枚同源）：`lint.sh`+`go build ./...`+`go test ./... -short -count=1`+bot 三连+`race_check.sh` 全绿 → push → CI 四 job 绿（test/race/validators/openspec）
- [x] 3.2b tool 域削薄半:13 例(迁文优先=role=tool 记录与 sudo 包装两 bullet 入 wiki;余者 wiki 已覆盖直删),基线 doc-not-brief 99→86;附带拦截并行会话三新文件(a2a/wiring/workspace)的 mfr raise——建 #model-wiring 与 #workspace-scratch 两节后补索引,mfr 回稳 88;自纠 recall_subtools 措辞触 audit-marker(“no longer”变更残留)
