check.go implements the two comparison modes the repository gates need:
comment-check proves a file changed only in comments; merge-check proves
a consolidation batch is lossless (body identical plus assertion count not
lowered).

- Paths are module-relative and must exist under both roots (comment-check) or
in the head root (merge-check); the shell wrappers materialize the baseline

本文件实现事件归属门（event-ownership）：按 golden/runtime/architecture.json 的 event_ownership
冻结表核对 owner 的可解析直接调用点。通知身份只由 runtime/task 生产，邮箱时点只由 runtime/state 实现、由 facts
随动消费者在一处记账，宿主只做路由 登记与投递受理，不复制第二份消费状态。

判定只认 go/types 解析出的对象身份（声明包 + 接收者类型 + 方法名）；同一方法名的
其它接收者按签名区分，不做名字模糊匹配。方法值回调、按名反射与解析不到的类型信息 登记为 uncovered

Command codetools emits the mechanical facts the repository comment and
test-file policies are checked against: strip prints a file with comments
removed in canonical go/printer form, decls prints one JSON object per top-level
declaration.

- Both subcommands exit non-zero on unreadable or unparsable input so a silent

