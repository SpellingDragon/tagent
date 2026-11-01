# 08 技术要点

1. **契约裁决**（D12）：首选对齐主 spec（healthz 接 `authorize`，与 `/task` 同源凭据面）——理由：spec 已钉"鉴权不豁免"，改注释=文档撒谎。**被否**：改 spec 迁就免鉴权现状（会放松运维面）；若部署层已有网关鉴权使端点免鉴权合理，则走"显式修订条款"路径并附部署事实。
2. **五态分类**：探测函数输出类别枚举 {healthy, auth_fail, unavailable, timeout}，脚本分支：auth_fail→告警退出（不 kill）、unavailable/timeout→等待窗、healthy→继续。现有"401/403 专判在重启后"位置前移到所有前置探测。
3. **验证形态**：五态模拟用本地 stub HTTP（200/401/403）+超时/拒连，`bash` 门脚本断言分支动作（kill 计数器为 0）。

文件清单：`assembly/host_face.go`、`examples/wechat-bot/restart-tagent.sh`、`restart-maintenance.sh`、`tests/gates/test_health_probe_states.sh`（新）。
风险：healthz 加鉴权会打红既有健康检查调用方（smoke `rig.healthz()`）——同步带 token，04 域迁移清单登记。

## D12 裁决记录（8.1 首叶定夺，已执行）

**裁定：路径 A —— healthz 对齐主 spec 的"鉴权不豁免"，接 `authorize`（与 `/task`、`/reload`、`/debug/pprof` 同一门）。**

- 实现：`assembly/host_face.go` 新增 `(*hostFace).healthz`，先 `authorize`；拒绝时 `401` + `WWW-Authenticate: Bearer realm="tagent"`，放行时响应体仍为裸 `ok`（200 语义与体不变）。
- 凭据同源：`newHostFace(ctx, host, os.Getenv(hostFaceTokenEnv))`（`host.go:580`），`hostFaceTokenEnv = "TAGENT_RL_AUTH_TOKEN"`——与两个运维脚本探针读的是同一个环境变量。

**路径 B（显式修订主 spec 迁就免鉴权）被否，部署事实核实如下（仓库内可复跑）：**

1. 仓库无网关/反代鉴权层：`grep -rln 'nginx\|caddy\|proxy_pass\|reverse_proxy' --include='*.yml' --include='*.yaml' --include='*.conf' --include='Dockerfile*'` 零命中；`examples/wechat-bot/docker-compose.yml` 仅 `ports: 127.0.0.1:8089:8089`（回环绑定，注释"经反代/网络策略暴露"但反代本身不在本仓）。
2. 部署文档已假定 healthz 受 token 保护：`deploy/README.md:174` 写明"`/healthz` 回 401——设了 `TAGENT_RL_AUTH_TOKEN` 而探针没带 Bearer；`restart-*.sh` 会自动带上，手测需自行加 `-H`"。
3. 运维脚本已按受保护实现：`probe_healthz` 组 `AUTH_ARGS=(-H "Authorization: Bearer $RL_TOKEN")`，401/403 归 `auth_fail`。
4. 结论：**实现是唯一未对齐的一环**；走路径 B 要同时改主 spec + README + 两脚本注释以放松运维面，代价更高且削弱运维面——故否。

**消费者盘点（healthz 的调用方，逐个实测）：**

| 消费者 | 带凭据 | 8.1 后的结果 |
|---|---|---|
| `restart-tagent.sh` / `restart-maintenance.sh` 探针块 | 是（`AUTH_ARGS`） | live face 实测：token 对 → `healthy/200`；token 错或无 → `auth_fail/401`（守卫 rc=3，不 kill） |
| `examples/wechat-bot` smoke `rig.healthz()`（面经 `assembly.Boot` 装配，台架 `TAGENT_RL_AUTH_TOKEN=""`） | 否（token 缺省空） | 走"无凭据→只放行回环对端"分支，200 `ok`，绿（`TestLocalSmokeWave2Errors` / `TestLocalSmokeGuards` / `TestLocalSmoke`） |
| `deploy/README.md:85,132,174`、`wizard.sh:269` 手测文案 | 文档 | README 已写明需自行加 `-H`；`wizard.sh` 文案未提 `-H`，属文档待补（未在 8 域白名单） |
| `docker-compose.yml` healthcheck `curl -fsS .../healthz` | 否 | **token 留空（缺省）时绿；运维设了 `TAGENT_RL_AUTH_TOKEN` 时会被 401 判 unhealthy**。外白名单，登记为待办：建议 healthcheck 改 `CMD-SHELL` 带 `Authorization: Bearer $TAGENT_RL_AUTH_TOKEN` |

**8.3 同源处置**：两脚本注释原写"healthz 受 token 保护…与 `rl.AuthTokenFromEnv` 同源"——`rl.AuthTokenFromEnv` 在仓库中不存在（`grep -rn AuthTokenFromEnv` 零命中），且原注释口径标注为"待 8.1 裁决"。8.1 裁定后注释改为：healthz 经 `assembly/host_face.go` 的 `authorize` 判定、凭据同源 `hostFaceTokenEnv`（env `TAGENT_RL_AUTH_TOKEN`）、未带凭据得 401 落 `auth_fail`——注释与实现一致，且探测逻辑一字未改（8.2 门仍绿）。
