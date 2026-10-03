# Tasks

## 1. P1 追踪卫生

- [x] 1.1 （实测：10 个残骸目录全为 0 字节、仅含 lock/journal、无 .md/.json 数据后删除；`.pre-isolation-backup/`、`.tagent-workspace/` 按约保留）`git rm` hottest-sub1/ hottest-sub2/（4 个零字节文件）；删孤儿 `go.work.sum`；清工作树遗物（own-*、hottest-sub3、hottest-drop-*；`.pre-isolation-backup/`、`.tagent-workspace/` 属运行/备份数据，默认保留）
- [x] 1.2 （实现为 `codetools tracked-hygiene` 子命令，纯函数 + 3 条单测；接入 `scripts/lint.sh` 与本地同命令；负路径双红留痕 `hygiene-probe.log`）新增 `scripts/check_tracked_hygiene`（空文件/运行期后缀/顶层白名单三查）并接入 validators job；负路径探针（空文件与 `x.lock` 各造一枚）双红留痕后撤除
- [x] 1.3 （`go test ./... -short` 31 包全绿零 FAIL、`GOMAXPROCS=1` 根包 59.6s ok、lint ok 含新门）全量 short + lint 绿；提交

## 2. P2 退役陈旧 AReaL 训练桥 + dotted-refs 门（依 D6/D7 与用户裁决）

- [x] 2.1 （桥三件 git rm；convert_trajectories.py git mv 至 scripts/；README 知识并入 wiki rl-architecture 新增 #offline-converter 与退役记录；白名单重生成 21 目录不含 train）删除 `train/rl/tagent_adapter.py`、`examples/wechat-bot/train_tagent.py`、`examples/wechat-bot/train_rl_config.yaml`；`git mv train/rl/convert_trajectories.py scripts/convert_trajectories.py`；`train/rl/README.md` 中仅转换器相关内容并入 wiki rl 页，其余随桥退役；顶层白名单去 `train`
- [x] 2.2 （run.sh 摘除 areal 命令族/变量/状态块共 59 处，bash -n 过；切除留下的孤儿 shift;; 与无标题横幅两处手术伤已修；`rl` 运行时模式与 AREAL_API_KEY 认证行保留；root/bot README 同步）`run.sh` 移除 areal 命令族与 AREAL_* 变量（`rl` 运行时模式保留）；root/bot README 与 wiki `rl-architecture` 同步：退役事实、保留面（`rl/` Go 包、转换器、RL 运行时配置）、重接条件
- [x] 2.3 （codetools dotted-refs：位置精确提取 + 纯函数判定 + 2 组单测；接入 lint.sh；探针红/复原绿留痕 dotted-refs-probe.log；期间 doc-refs 连锁抓到退役记录的悬空引用并已改写）新增 `codetools dotted-refs`（位置精确：`python -m` 后续 token 与 yaml `workflow:` 值；仓库内解析或显式外部允许表）+ 单测 + 接入 lint.sh；负路径探针（造一条指向已删布局的 workflow 引用必红）留痕
- [x] 2.4 （`go test ./... -short` 31 包零 FAIL；bot 模块三连绿 final rc=0——首跑曾见一次无法复现的 exit status 1，如实记档交 CI 裁决；lint ok 含两道新门）全量 short + lint（含两新门）绿；提交

## 3. P3 结构一步到位（D10/D11）

### C1 配置模型外迁 config 包

- [~] 3.2 config.go → config/ 包 —— **代码已完成，工作树未提交**（详见 design D12 实录：三处散布方法已随迁、别名层已建、config+根包测试绿；剩 1 条 orgSubset doc 残行、README 布局行、分层断言预跑）
  - [x] config/ 五文件成形（config/modelref/clone/lifecycle/config_test）
  - [x] config_alias.go 别名层（20 类型 + 2 常量 + 2 函数）+ IsRemoteRef 导出化
  - [x] 4 个注册表系测试并回 registry_test.go（colocation 判定与设计一致）
  - [x] build/vet/config 测试/根包全量 short 57.9s/gen_godoc/hygiene 白名单 全过
  - [ ] 清 org_hotreload.go:444 orgSubset doc 残行（Clone doc 第二次没跟走）
  - [ ] README 布局表补 config/ 行；TestArch_LayeredDependencyDirection 预跑确认不误红
  - [ ] TestLiveSession 全量并发红（32.19s，单跑绿）定性后处置
- [ ] 3.3 C1 全绿提交（build/vet/根包 short/别名面抽样测试）

### C2 纯查询随迁 + 分层立法修订

- [ ] 3.4 partition_collision 四查询（agentMemoryFingerprint/changedMemoryAgents/reachableAgents/remoteDeclarationOnly）迁 config 包并导出；根包调用点改引；partition_collision.go 收缩为注册表文件
- [ ] 3.5 `TestArch_LayeredDependencyDirection` 扩集（config 层 + agent 主体不 import config 断言）；spec delta MODIFIED 两条法条落措辞；wiki agent-architecture #package-layout 表更新
- [ ] 3.6 C2 全绿提交

### C3 世代机制外迁 agent/org

- [ ] 3.7 owner_retirement/org_candidate_txn/org_candidate_overlay 三件 git mv 入 agent/org；`StoreOwnerRegistry` 注入接口 + `ShellBuilder` 闭包；导出面最小化（Ledger/CandidateTxn/buildOverlay 仅包间必需）；org_hotreload 整文件留根（发布权物理位置，wiki 标注）
- [ ] 3.8 根包接线与调用点改造；docs/api 重生成；C3 全绿提交（含 -race 与 GOMAXPROCS=1）

## 4. P4 巨型测试按域拆分

- [ ] 4.1 org_candidate_test(2007)/cross_generation_test(1969)/org_hotreload_test(1715) 按域拆 ≤600 行文件，留根包；纯逻辑单测（如有）随 agent/org
- [ ] 4.2 `check_test_merge.sh` 对每个源文件证无损；同位门逐文件核对锚点与镜像
- [ ] 4.3 全量 short + race + P=1 + lint 绿；提交

## 5. 收口

- [ ] 5.1 README 布局说明与 docs/wiki 索引同步（新包、新目录、train 新址）
- [ ] 5.2 push 并确认 CI 四 job 绿；归档
