# 03 技术要点

## 要点

1. **夹具**：脚本模型（既有 `convModel`/scripted 形态）+ 反思节奏压缩（`rhythmFloor`/`minGap` 测试参数化）+ 水位读数接口（Reflection watermark 只读面）+ 资源计数（goroutine 数、`runtime.NumGoroutine` 前后差、可加 fd 计数）。
2. **轮数**：30–50 轮（D11）；每轮=反思产生产物→投递→消费确认→水位推进；中间插一次进程重启（子进程形态，参照 `notice_crash_test.go`/`settle_wake` 夹具）验证水位续接。
3. **咬证**：`watermark_regresses_is_red`/`duplicate_consumption_is_red` 用变异（人为回退水位/重复投递）证明断言会咬。
4. **墙钟**：脚本模型下 30–50 轮应在分钟级；超预算则轮数取下限 30 并记录（R5 时间断言不作判定）。
5. **升级路径**：02.4 账目落地后，3.3 的事实计数断言升级为账目直读（等价断言，不缩验收面）。

## 文件清单

- 新：`tests/integration/evolution/reflection_golden_test.go`（夹具+主体）、`tests/integration/evolution/reflection_golden_fixture_test.go`。
- 改：`features/evolution/meditation`（仅测试参数化开口，如节奏下限可注入）。

## 风险与回退

- 风险：golden flaky（时间敏感）——全部时间控制走参数化节奏而非真实等待；断言用事件计数不用墙钟。
- 回退：纯测试面，可独立回滚。
