# Design

## D1 排空告警轮 = 把两条调度形态坍缩为一条

`processTurn` 串行、`Pull` 只在 turn 完成后取下一批，故「等告警轮完成（MAIN 计数 +2：委派一次 + 收尾一次）」之后注入的 `"in-flight"` **必然**单独成批。armGate 之前加这一步，P=1 的合批形态与 P≥2 的抢先形态都不再可能出现——用例从此只测它声称的场景。

## D2 锚定基线，消灭存在性谓词

`base := countServed(SUB-B)` 取排空后的值；`"parked"` 改为 `> base`，断言②改为 `> base+1`。历史 record 从此不能再替任何谓词达标。

## D3 断言①锚定「完成」而非「进入」

`delegModel` 在 park **之前**就 record 了 SUB-B 调用，故计数只证「进入」。断言①改为等待**第二条** `System=="MAIN"` 且 `ToolResults` 含 `"served:SUB-B"` 的 record——B 的答复真抵达 MAIN 才出现；若回滚将来真的拆掉在途调用，此事件永不发生，fail-before 敏感度不降反升。

## D4 fail-before 与验证口径

- fail-before：`GOMAXPROCS=1 -count=8` 当前 5 FAIL；修复后须 8/8 PASS 且 `GOMAXPROCS=2` 保持 PASS。附带「空转通过」的证伪：修复前在 disarm 前打印 gate 命中时序即可见 gate 从未拦截（诊断已给日志证据，落档为文字结论）。
- 被否方案：调大 20s——被等待事件在失败形态中不存在（供给源在断言之后），无有限上界；真上界为毫秒级 channel 交接 + mock 模型。
