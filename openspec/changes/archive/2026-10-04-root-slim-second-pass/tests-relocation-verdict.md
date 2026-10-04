# 测试归位判定表（C 组交付物）

判定协议：复制候选到 tests/（package tagent_test + import tagent），探针文件名必须**不以下划线开头**（`_probe_test.go` 会被 Go 工具链忽略，首轮 24/24 全 PASS 即此陷阱的假阳性），`go vet ./tests/` 通过=PASS；另测「机械加 tagent. 前缀」一档。

| 文件 | 零改写 | 首个未解析符号（真墙） |
|---|---|---|
| `build_cycle_test.go` | GREY | Config |
| `cross_generation_support_test.go` | GREY | delegServed |
| `cross_generation_test.go` | GREY | delegYAML |
| `delegation_test.go` | GREY | LoadConfig |
| `guardrails_test.go` | GREY | drillModel |
| `modelref_test.go` | GREY | # github.com/SpellingDragon/tagent/tests_test |
| `org_candidate_close_drain_test.go` | GREY | sdCloseYAML |
| `org_candidate_rollback_test.go` | GREY | writeGateConfig |
| `org_candidate_support_test.go` | GREY | AgentConfig |
| `org_candidate_test.go` | GREY | populatedAgentConfig |
| `org_diagnostics_test.go` | GREY | OrgAgentApply |
| `org_hotreload_closed_owner_test.go` | GREY | ownerWriter |
| `org_hotreload_lockfree_read_test.go` | GREY | scHotAddNumericYAML |
| `org_hotreload_support_test.go` | GREY | Config |
| `org_hotreload_test.go` | GREY | cfgFor |
| `org_hotreload_timing_test.go` | GREY | writeBumped |
| `owner_retirement_test.go` | GREY | testStore |
| `partition_collision_test.go` | GREY | stubModel |
| `prompts_test.go` | GREY | defaultPromptsFS |
| `registry_test.go` | GREY | runtimeConfig |
| `resources_stubs_test.go` | GREY | # github.com/SpellingDragon/tagent/tests_test |
| `resources_test.go` | GREY | Config |
| `tagent_test.go` | GREY | Config |
| `test_stores_test.go` | GREY | # github.com/SpellingDragon/tagent/tests_test |

**结论：零改写 0 个可搬；机械前缀（tagent.Config 等 20 个别名全加）后仍 0 个可搬。**真墙是包内测试 helper 与未导出结构：`cfgFor`/`stubModel`/`testStore`/`delegServed`/`writeGateConfig`/`populatedAgentConfig`/`runtimeConfig`/`defaultPromptsFS`/`ownerWriter`/`delegYAML` 系。这些 helper 本身就是"经装配根灰盒验证世代/退役/重入"的载体（D3 裁决的物理形态）。要搬走它们只有两条路：把 helper 提升为 tests/ 共享文件（测试自洽，可行但属符号级改写，本档边界外），或导出生产符号（违背不扩公共 API 边界）。根包 24 个测试文件留根是结构必然。
