## 1. P1 门禁上线（规则按语义两单元 + 基线落定）

- [ ] 1.1 `missing-file-responsibility` 规则：非测试 `.go`（`scripts/` 豁免、嵌套模块按 POLICY_DIRS）无任何 `契约:/规格:` 索引行 → 一条 finding，消息含「先补文档再落索引」。验证：单测覆盖——无索引生产文件计 finding、有索引零 finding、`_test.go` 与 `scripts/` 不计、多索引行取存在性判定的口径
- [ ] 1.2 `doc-not-brief` 形态规则：声明 doc 职责句两行之后出现非 bullet、非索引散文行 → 一条 finding，消息含「迁文优先」。验证：单测覆盖——2 行职责句合规、3 行段落违规、bullet/索引行不计厚、非导出声明同权、package doc 同形态
- [ ] 1.3 `gen_godoc` 共存定案：实测文件级索引行进生产 doc 后 `docs/api` 生成物的表现，裁决过滤或链接化并落地（改 `gen_godoc.sh` 或渲染器），`--check` 绿。验证：一个样本文件加索引行后跑 `gen_godoc.sh --check` 通过且产物符合裁决
- [ ] 1.4 脚手架基线落定：全仓实测两规则计数（预演 112 / 上界 200，以实测落定），`lint.sh --update-baseline` 写双槽并提交；CI 即刻挡增量。验证：故意造一无索引新文件与一 3 行段落 doc → `lint.sh` 双 REGRESSION 红 → 移除恢复绿，留痕 `scaffold-probe.log`
- [ ] 1.5 提交 P1（`scripts/comment_policy/` + `docs/api` 若再生），独立 revert 面

## 2. P2 权威测量与双对账锚冻结

- [ ] 2.1 `pointer-map.txt`：112 文件逐一裁决目标小节（存在/需新建/需扩写三态标注），存入本 change 目录冻结为对账锚。验证：每行 `文件<TAB>目标小节<TAB>三态`，与 finding 清单一一对应
- [ ] 2.2 `beyond-brief.txt`：形态规则权威输出逐声明冻结。验证：计数与 1.4 基线一致，偏差先回查规则再开工
- [ ] 2.3 战役批规程固化进本 tasks（迁文优先→削薄→补指针→`go test <pkg>`+`-race`+`gen_godoc --check`→降幅恰等于 N→pathspec 提交），并核对 `agent/` 热区文件当前占用

## 3. P3 分域战役（每批独立提交；域序：小域练流程，热区压后）

- [ ] 3.1 evolution 批（3 文件）：迁文→削薄→补指针；N=域内两规则 finding 消除数之和
- [ ] 3.2 tool 小域批（tool/spec 等约 6 文件）
- [ ] 3.3 memory 批（11 文件，含 `LocalFileKV` 26 行削薄的迁文样本）
- [ ] 3.4 根包批一（约 7 文件，含 `config.go:Config` 41 行——最大迁文对象，单独裁决落点）
- [ ] 3.5 根包批二（约 7 文件，含 `tagent.go:New` 22 行）
- [ ] 3.6 agent/governance 批（9 文件）
- [ ] 3.7 agent/compress 批（8 文件）
- [ ] 3.8 agent 批一（约 10 文件，批前工作树检查）
- [ ] 3.9 agent 批二（约 10 文件）
- [ ] 3.10 其余小域归拢批（engine/reliability/task/tool/action 等残余）
- [ ] 3.11 examples/wechat-bot 批（5 文件，第二模块独立验证三连）

## 4. P4 归零切硬与 counts 归空（CI 固化终态）

- [ ] 4.1 双槽归零核对：两规则全仓 0 finding，`--update-baseline` 后双槽自然移除
- [ ] 4.2 `missing-test-responsibility:9` 清理：9 个测试文件逐个补索引（目标小节不存在则先补文档）；该槽归零移除
- [ ] 4.3 counts 归空断言：`baseline.json` counts 为空对象；负路径抽验——临时造一无索引生产文件与一 3 行段落 doc → `lint.sh` 双红 → 恢复绿，留痕 `hard-gate.log`
- [ ] 4.4 归档：`openspec archive`，delta 并入 `code-documentation`，`openspec validate --specs --strict` 全绿
- [ ] 4.5 终验与推送：`lint.sh`+`go build ./...`+`go test ./... -short -count=1`+bot 三连+`race_check.sh` 全绿 → push → CI 四 job 绿（test/race/validators/openspec）
