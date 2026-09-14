# prompt 包评分卡

base: `cf006e1` | 3 文件 / 591 行（测试 719 行）| cover 86.1% | 取证: 全文读 + cover

| 维 | 级 | 依据 |
|----|----|------|
| 复杂度 | S | loader（加载族）/ source（mtime 热重载）/ getter（接口）三文件各司其职；fallback 双函数对称 |
| 耦合 | S | 叶子包；仅依赖标准库 + 上游 log |
| 测试 | S | cover 86.1%；loader_test 445 行含 fallback/skip-missing/组合加载；LoadFiles skip-missing 有 fail-before 回归（2ab482f） |
| 文档一致 | S | wiki/prompt 已更新可选文件语义（2026-09）；LoadBootstrap/DefaultConfig 契约表述与实现一致 |
| 演进风险 | A | `Source.Get` 读错误回退缓存（source.go:60-84）优雅降级完备；风险仅在 fallbackFS 嵌入集与磁盘漂移——已有 Debugf 留痕 |

发现: 无（反证记录：曾疑 `Source.Get` 文件删除时行为——复核确认走 checkModTimes err → 缓存回退路径，优雅降级成立，见 summary「已反证」）
