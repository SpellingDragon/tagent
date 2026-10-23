# ORCHESTRATION：plain-structure

## 结论层
唯一路径：W1（A 合单＋B 并发）→W2（C→D 并发）→F。最大风险=A6 消费面广（四域＋根＋宿主）；指纹字节不变是硬门。

## 分组层
次序：A1-A6 同文件簇合单（config＋四域＋消费点）；B1 独立并发；C1 串行 A 后（同碰 meditation）；D1-D4 并发（W2）。授权：每单白名单=tasks 行文件集；golden/scripts/doc_budget=编排者。凭据：三点锚逐叶 exit；指纹 golden 字节对照；波后独立验证（R30）。释放：无锁。

## 明细层
A 单触碰：api/config、memory（含 decl）、evolution/meditation、tool/{exec,file}、根包消费点、examples/wechat-bot。B：telemetry＋contract。

## 台账层
- 计划建立；W1 派发（A＋B）。
