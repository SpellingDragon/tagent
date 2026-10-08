# O6 叶任务（均未执行）

- [x] 6.1 建立v2/legacy/缺manifest/无mask模板fixture，核实本地tokenizer能力并登记版本 —— 验证：`python3 -m unittest discover -s scripts -p test_convert_trajectories.py`；真实tokenizer前置失败保持验收未完成。
- [x] 6.2 实现授权分页事实导出、父键二次授权及missing/ambiguous结果，不改store —— 验证：`go test ./rl -run '^TestTrainingExport_PartitionAuthorization$' -count=1`。
- [x] 6.3 实现精确feedback join及导出manifest，保留多反馈/未评分/缺亲缘，不自动reward聚合 —— 验证：`go test ./rl -run '^TestTrainingExport_FeedbackAssociation$' -count=1`。
- [x] 6.4 实现strict工具回环、整体chat template与目标assistant mask，空正文toolcall不能丢 —— 验证：`python3 -m unittest discover -s scripts -p test_convert_trajectories.py`；覆盖TestToolRoundtrip/TestAssistantLossMask。
- [x] 6.5 实现session组分割、legacy警示、拒绝清单、内容策略与显式输出格式 —— 验证：`python3 -m unittest discover -s scripts -p test_convert_trajectories.py`；覆盖TestSessionSplit/TestExportPrivacy/TestLegacyMode。
- [x] 6.6 联调O5采集→成功事实→反馈→只读导出→SFT，关闭/缺失不伪完整；编排者集成验收器及negative tests —— 前置：O5.6；验证：`go test ./tests -short -run '^TestTrainingCapture_OfflineDataset$' -count=1`；`python3 -m unittest discover -s scripts -p test_verify_runtime_acceptance.py`。
- [ ] 6.7 本地跑真实模型两session工具往返后用真实tokenizer生成并校验collator batch —— 验证：`TAGENT_REQUIRE_REAL_MODEL=1 go test ./tests -run '^TestRealModel_OfflineSFT$' -count=1 -json`；`HF_HUB_OFFLINE=1 python3 scripts/verify_runtime_acceptance.py --run-dir "$TAGENT_ACCEPTANCE_DIR"`；非SKIP且样本目标mask非空。
- [x] 6.8 交编排者同步使用方式、边界、隐私/授权、真实资产前置与旧转换兼容文档 —— 验证：`bash scripts/lint.sh && bash scripts/check-openspec.sh`。

> 编排者核销（W1）：`python3 -m unittest discover -s scripts -p test_convert_trajectories.py` PY=0（46 tests，22 拒绝码全覆盖）。真实 tokenizer/HF/collator 属 6.6/6.7 与 F11 阻塞项（本机 transformers/datasets 缺失，已登记）。CLI 参数表进 D 文档接线由 W3 处理。

> 编排者核销：6.2/6.3=C4（ExportTrainingFacts 表驱动全绿+变异自证；call_id 已引 event 常数）；6.6=Go 半（tests/training_export_e2e_test.go）+Python 双流（C5 主源换 capture、78 例全绿）；--expect-tests 全链核账待 6.7 真模型后执行。

> 编排者核销：6.8=D 战报③行6/9/10（rl-architecture 训练导出/双流/条件 trace 节 + README 验收前置与桥退役脚注 + tests/README 矩阵行回填；gen_godoc --check 绿）。6.6/6.7 保持未勾：strict 数据集三件依赖真实 tokenizer 资产（用户裁定后补），核账器对 acceptance5 的 14 项绿 + 3 项 dataset 具名红即该裁定的留痕。
