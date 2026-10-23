# design：event-typing

## D1 Type 判定（否决：前缀解析）
折叠再折叠按 ProjectionEntry.Type；SettleDriver 已按 TypeTaskSettled（先例）；fireNoticeTurn 的
随动事件 Meta 带 kind/task_id 机械键。

## D2 呈现层渲染（否决：Meta 存拼接文本）
noticeText/historySummary 渲染留给模型可读面；Meta 键值机械（kind=settled），不带 "tagent: " 前缀拼接。

## D3 并发
A（typing 核心：memory/replay＋runtime/agent＋根包）与 B（冒烟第二波：examples/wechat-bot smoke）
零相交。B 的折叠断言如遇 A 未落（前缀尚在）按现状写＋标注，终态编排者联合对齐。
