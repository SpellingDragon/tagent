#!/usr/bin/env python3
"""verify_restart_prefix.py — 重启边界前缀一致性验证（基于 trajectory JSONL）

用法: python3 verify_restart_prefix.py <trajectory.jsonl>

原理:
- TrajectoryRecorder 记录每次 LLM 调用的完整 request.messages（前缀快照）
- 真重启标记 = batch_index 归零（新进程计数器从 0 开始）；非零回退是进程内
  多 agent 计数器交错，不是重启
- 判定: 对每个 batch==0 记录 B，向前回溯找同会话最近记录 A，要求 B 的历史段
  （去 system 头）以 A 的历史段为逐字节前缀（崩溃前历史全量保序重现），
  之后允许追加间隙新事件（间隙事件属重启窗口，追加合法）
判定等级:
- EXACT    前缀逐字节全等 —— 验证通过
- FOLD     覆盖率>=90% 但非全等 —— 疑似折叠边界（摘要替换原文，合法，人工确认）
- FRESH    小历史(<30条) —— 子 agent 首调/新会话，本方法不适用（设计如此）
- CRITICAL B 历史是某 A 历史的真前缀 —— 疑似历史丢失，必须排查
- REVIEW   其余无法配对 —— 人工核对
"""
import json, sys

def main():
    f = sys.argv[1]
    recs = [json.loads(l) for l in open(f) if l.strip()]
    def msgs(r): return r["llm_call"]["request"]["messages"]
    def strip_sys(ms):
        i = 0
        while i < len(ms) and ms[i].get('role') == 'system': i += 1
        return ms[i:]
    def pml(a, b):
        n = min(len(a), len(b))
        for i in range(n):
            if a[i] != b[i]: return i
        return n

    zeros = [i for i in range(1, len(recs)) if recs[i]['batch_index'] == 0]
    switches = sum(1 for i in range(1, len(recs))
                   if recs[i]['batch_index'] < recs[i-1]['batch_index'] and recs[i]['batch_index'] != 0)
    print(f"file={f} records={len(recs)} 真重启点(batch==0)={len(zeros)} 非零回退(agent切换,排除)={switches}")

    stats = {'EXACT':0,'FOLD':0,'FRESH':0,'CRITICAL':0,'REVIEW':0}
    rows = []
    for bi in zeros:
        B = recs[bi]; bh = strip_sys(msgs(B))
        if not bh or len(bh) < 30:
            stats['FRESH'] += 1
            continue
        best = (0, -1, 0)  # m, j, len(ah)
        for j in range(bi-1, max(0, bi-300)-1, -1):
            ah = strip_sys(msgs(recs[j]))
            if not ah or len(ah) < 10: continue
            m = pml(ah, bh)
            if m == len(ah):
                best = (m, j, len(ah)); break
            if m > best[0]: best = (m, j, len(ah))
        m, j, lah = best
        if j >= 0 and m == lah:
            stats['EXACT'] += 1
            rows.append((bi, 'EXACT', j, m, lah, ''))
        elif j >= 0 and lah and m/lah >= 0.9:
            stats['FOLD'] += 1
            x = strip_sys(msgs(recs[j]))[m]; y = bh[m]
            rows.append((bi, 'FOLD', j, m, lah,
                         f"div@{m} A={str(x.get('content'))[:50]!r} B={str(y.get('content'))[:50]!r}"))
        elif j >= 0 and m == len(bh) and len(bh) < lah:
            stats['CRITICAL'] += 1
            rows.append((bi, 'CRITICAL', j, m, lah, f"B历史({len(bh)})是A@{j}历史({lah})真前缀→疑似丢失"))
        else:
            stats['REVIEW'] += 1
            rows.append((bi, 'REVIEW', j, m, lah, f"cov={m/lah if lah else 0:.0%}"))

    print("\n=== 逐重启点 ===")
    for bi, v, j, m, lah, note in rows:
        t = recs[bi]['timestamp'][:19]
        print(f"[{bi}] {t} batch0 {v:8} pair@{j} prefix={m}/{lah} {note}")
    print("\n=== 汇总 ===")
    for k, v in stats.items(): print(f"{k}: {v}")

if __name__ == '__main__':
    main()
