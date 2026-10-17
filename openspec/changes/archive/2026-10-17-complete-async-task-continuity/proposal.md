# Complete async task continuity

## Why

异步ACK只结束前台等待，旧实现却在稳定输出、事件队满、混批提交、调用退出及进程重启等边界分别丢失结果、等待或归属。后补看板重通知无法替代从受理到消费的同一事实链。需要把等待条件、producer寿命和逻辑调用明确分开，并使每段失败可按原身份恢复。

## What Changes

- 意图受理、资源准备与绑定提交先于业务激活；任务信号冻结round/owner/seq/time/WaitFor。
- accepted回调、raw outbox与精确ACK共同保留结果，拒收独立退避重试。
- 命令稳定仅作观察，ready/input须显式证据，原资源identity与字节基线防旧信号串轮。
- 本地逻辑调用保存私有投影、等待和结果地址，重启按原invocation附着，不重复已消费goal。
- 输入observe/decision分流并写精确处置；单最近期限timer主动执行统一TTL；Close等待真实退出。
- 同步wiki、历史根因报告和CHANGELOG；不改变存储flush-only定位或声称任意工具副作用exactly-once。

## Impact

影响agent/task、ActionTool、同构调用环、任务journal、continuation、输入处置和恢复装配。远端A2A仍保留传输deadline，不承诺其远程进程checkpoint恢复。完整验证矩阵由集成负责人补入报告，未执行的全包/长跑不得记成通过。
