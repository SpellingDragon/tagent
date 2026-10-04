# Design

## D1 保守合并是唯一被验证的形态

本轮四版脚本中只有第三版（成员内容逐字节保留、只剥 package/import、import 保完整行含别名）达成 vet 绿 + 196 守恒。任何"顺手整理成员内容"（剥锚行/删叙述/加说明行）的版本都引入静默损失——成员的粘连关系（锚行+叙述+Test doc）是源文件 lint 绿的来源，合并必须原样保形。

## D2 剩余 free-standing 的修法与红线

合并文件中部只允许"声明 doc"一种注释位。成员 2~N 的头块剥离规则：块内锚行与文件级叙述删除；粘连 Test doc（`// Test` 开头及其续行）保留并与后续声明保持粘连。**红线**：doc 块边界判定只能动注释行——第三轮修注释时把 [d..i+1] 切片扩到声明行，20 处 func 声明被静默滤掉，靠 `expected declaration` 才暴露。

## D3 验证三连是每笔提交的门槛

`gofmt -l`（语法整形）→ `go vet .`（**退出码用 PIPESTATUS 判**；本轮 `|head` 吃掉非零退出，假 VET-ok 差点放行）→ `-list` 集合对账（func Test/Benchmark 逐名，196/196）。另：`reset --hard` 不清 untracked，新名基底文件（agent_architecture_test.go）残留会让下一轮判定全歪——每组做完必须 `git status` 归零。
