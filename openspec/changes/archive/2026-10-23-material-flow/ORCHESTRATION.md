# ORCHESTRATION：material-flow

## 结论层
唯一路径：波1（P1-P4 并发）→波2（P5-P7 并发）→波3。最大风险=P1 管线接口重构深度；行为零变化硬门（材料流等价）。

## 分组层
次序：P1独占api/plugin；P2独占meditation；P3独占subagent；P4独占根包invoke/tools——文件零相交全并发。波2消费波1接口：P5合单loop+facts（热点文件）；P6合单request+prepare；P7插件域扫尾。授权：每单白名单=tasks行文件集；golden/doc_budget=编排者。凭据：三点锚；行为零变化（golden match）；波后独立验证。释放：无锁。

## 明细层
| 单 | 文件簇 |
|---|---|
| P1 | api/plugin/** |
| P2 | evolution/meditation/** |
| P3 | tool/subagent/** |
| P4 | invoke.go, tools.go |
| P5 | runtime/agent/loop.go, facts.go |
| P6 | request.go, prepare.go |
| P7 | memory/**, evolution/(根), governance适配 |
| P8 | golden, scripts, docs |

## 台账层
- 计划建立；波1 派发（P1-P4）。
