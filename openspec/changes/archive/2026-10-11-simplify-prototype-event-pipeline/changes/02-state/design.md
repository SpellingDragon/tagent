## Decisions
以 ../../design.md D4/D5 为确切签名，只写 `.qoder/staging/prototype-reset/02-state/state.go` 与 config.go。
state以id注册AgentState，owner持队列、context、Memory和TaskManager。Apply同id复用，缺id关闭Tasks回收；govern只检查Config变更，无工具门。JSON快照dto不含函数，任务running恢复unknown，owner绑定保留；Save限定检查点，无崩溃事务。
config解析JSON并验证Registry引用，构造可替换BaseTAgent壳；组装ModelRequest包括model/params/system/skill/tool，然后CallModel。state不替代核心事件分派。
只做简洁闭包/struct，域内仅语法验证，集成后由主代理测真实子进程。若无法满足冻结签名回报，不自行改公共契约。
