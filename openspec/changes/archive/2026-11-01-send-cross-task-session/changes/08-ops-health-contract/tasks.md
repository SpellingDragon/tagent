# 08 ops-health-contract 孙任务

- [x] 8.1 契约定夺：healthz 对齐 `ops-deployment-integrity`"鉴权不豁免"条款（实现 authorize）或显式修订条款——二选一附被否记录入 design —— 验证：`go test ./assembly -run '^TestHealthzContract$' -count=1`；负锚 `unauthenticated_probe_classified`（未带凭据的探针响应类别可辨）
- [x] 8.2 脚本五态探测（N5 锚）：`restart-tagent.sh`/`restart-maintenance.sh` 前置探测区分 200/401/403/超时/拒连；401/403→AUTH_FAIL 告警不杀进程、拒连/超时→等待窗口、200→健康 —— 验证：`bash tests/gates/test_health_probe_states.sh`（五态模拟：200/401/403/timeout/refused 各一断言）；负锚 `auth_fail_never_kills`（401/403 路径零 kill 动作）
- [x] 8.3 注释与实现一致：脚本注释"healthz 受 token 保护"与实际契约对齐（改注释或改实现随 8.1 裁决） —— 验证：`grep -n "token" examples/wechat-bot/restart-*.sh` 与 8.1 裁决一致；`bash -n examples/wechat-bot/restart-*.sh` exit 0
