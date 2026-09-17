#!/usr/bin/env python3
"""trajectory_analyze.py — tagent trajectory JSONL 拆分分析器（RL 域工具）。

数据源：rl.TrajectoryRecorder 产出的 JSONL（每行一次 LLM 调用：request.messages +
response.usage + batch_index + trace 字段）。支持明文与 gzip（.gz）。

用途：分析远端上下文压缩回收情况——压缩触发点、回收率、大消息分布、
chars/token 估算偏差、task_settled 结算风暴识别。流式解析，单文件可处理
GB 级 trajectory。

用法：
    python3 rl/trajectory_analyze.py trajectory.jsonl[.gz] [--top 25] [--json]
    python3 rl/trajectory_analyze.py traj.gz --top 50

输出：
    [turns]   逐调用摘要：msgs/chars/prompt_tokens/chars-per-token/压缩标记
    [compactions] 压缩触发点与回收率（prompt_tokens 骤降 >10% 判定）
    [top]     最大消息 TOP-N（role/工具/前缀）——大块头定位
    [roles]   role 与工具聚合体积占比
    [verdict] 机器可读结论（--json 时输出全部数据）

分析约定（与 openspec/specs/task-skeleton-compression 对齐）：
    - 压缩触发线 = threshold(0.8) × max_tokens；压缩后占比 >70% 记为
      「回收不及预期」候选（远端 2026-09-17 首点 72.4% 即此形态）。
    - 大消息 TOP 榜若被 [task settled] 合并消息占据 → 结算风暴固化进投影
      （external_input 类不在工具对折叠范围），即「回收不及预期」的直接根因。
"""
import argparse
import gzip
import json
import sys


def iter_records(path):
    opener = gzip.open if path.endswith(".gz") else open
    with opener(path, "rt", encoding="utf-8", errors="replace") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                yield json.loads(line)
            except json.JSONDecodeError as e:
                print(f"WARN: skip malformed line: {e}", file=sys.stderr)


def msg_len(m):
    """消息字符量：content（str 或多模态 parts）+ tool_calls 序列化。"""
    c = m.get("content")
    if isinstance(c, str):
        n = len(c)
    elif isinstance(c, list):
        n = 0
        for part in c:
            if not isinstance(part, dict):
                continue
            t = part.get("text") or ""
            n += len(t)
            if part.get("type") not in (None, "text"):
                n += len(json.dumps(part, ensure_ascii=False))
    else:
        n = 0
    for tc in m.get("tool_calls") or []:
        n += len(json.dumps(tc, ensure_ascii=False))
    return n


def tool_of(m):
    tcs = m.get("tool_calls")
    if tcs:
        fn = tcs[0].get("function") or {}
        return fn.get("name") or ""
    return m.get("name") or ""


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("trajectory", help="trajectory JSONL（或 .gz）")
    ap.add_argument("--top", type=int, default=25, help="大消息 TOP-N（默认 25）")
    ap.add_argument("--compaction-drop", type=float, default=0.10,
                    help="压缩判定阈值：prompt_tokens 降幅比例（默认 0.10）")
    ap.add_argument("--json", action="store_true", help="输出机器可读 JSON")
    args = ap.parse_args()

    turns = []
    for r in iter_records(args.trajectory):
        req = r.get("llm_call") or {}
        msgs = (req.get("request") or {}).get("messages") or []
        usage = ((req.get("response") or {}).get("usage") or {})
        turns.append({
            "batch_index": r.get("batch_index"),
            "timestamp": r.get("timestamp"),
            "n_msgs": len(msgs),
            "chars": sum(msg_len(m) for m in msgs),
            "prompt_tokens": usage.get("prompt_tokens") or 0,
            "completion_tokens": usage.get("completion_tokens") or 0,
            "duration_ms": ((r.get("metadata") or {}).get("duration_ms")) or 0,
        })
    if not turns:
        print("no records", file=sys.stderr)
        return 1
    turns.sort(key=lambda t: (t["timestamp"] or "", t["batch_index"] or 0))

    # [compactions]：prompt_tokens 骤降 = 压缩/回收点
    compactions = []
    for prev, cur in zip(turns, turns[1:]):
        pp, cp = prev["prompt_tokens"], cur["prompt_tokens"]
        if pp and cp < pp * (1 - args.compaction_drop):
            compactions.append({
                "before_batch": prev["batch_index"], "after_batch": cur["batch_index"],
                "before_tokens": pp, "after_tokens": cp,
                "reclaimed_tokens": pp - cp,
                "reclaim_ratio": round((pp - cp) / pp, 4),
                "after_ratio_of_800k_assumption": None,
            })

    # 大消息：取最后一条（最全上下文）
    last = max(turns, key=lambda t: t["n_msgs"])
    last_rec = None
    for r in iter_records(args.trajectory):
        if r.get("batch_index") == last["batch_index"]:
            last_rec = r
    msgs = ((last_rec.get("llm_call") or {}).get("request") or {}).get("messages") or []
    items = []
    for i, m in enumerate(msgs):
        items.append({
            "idx": i, "role": m.get("role"), "tool": tool_of(m),
            "chars": msg_len(m),
            "preview": (m.get("content") if isinstance(m.get("content"), str) else "")[:80].replace("\n", " "),
        })
    total_chars = sum(x["chars"] for x in items)
    items.sort(key=lambda x: -x["chars"])
    top = items[: args.top]

    roles = {}
    for x in items:
        k = x["role"] if not x["tool"] else f"{x['role']}({x['tool']})"
        a = roles.setdefault(k, {"count": 0, "chars": 0})
        a["count"] += 1
        a["chars"] += x["chars"]

    settled_like = [x for x in items if "[task settled]" in x["preview"]]
    settled_chars = sum(x["chars"] for x in items if "[task settled]" in (msgs[x["idx"]].get("content") or ""))

    worst = min((t["prompt_tokens"] for t in turns if t["prompt_tokens"]), default=0)
    peak = max((t["prompt_tokens"] for t in turns if t["prompt_tokens"]), default=0)

    if not args.json:
        print(f"records={len(turns)}  batch {turns[0]['batch_index']}..{turns[-1]['batch_index']}")
        print(f"prompt_tokens: peak={peak} floor={worst} (floor/peak={worst / peak:.1%})" if peak else "no usage")
        print(f"\n[turns] {len(turns)} calls (batch, msgs, chars, prompt_tok, chars/tok, dur_s):")
        for t in turns:
            r = t["chars"] / t["prompt_tokens"] if t["prompt_tokens"] else 0
            print(f"  {t['batch_index']:>6} {t['n_msgs']:>5} {t['chars'] / 1e6:>7.2f}M "
                  f"{t['prompt_tokens']:>9} {r:>8.1f} {t['duration_ms'] / 1000:>6.0f}")
        print(f"\n[compactions] prompt drop > {args.compaction_drop:.0%}:")
        if not compactions:
            print("  (none)")
        for c in compactions:
            print(f"  {c['before_batch']}→{c['after_batch']}: "
                  f"{c['before_tokens']}→{c['after_tokens']} (reclaimed {c['reclaimed_tokens']}, "
                  f"{c['reclaim_ratio']:.1%})")
        print(f"\n[top {len(top)} of {len(items)} messages] total={total_chars / 1e6:.2f}M chars, "
              f"top{len(top)}={sum(x['chars'] for x in top) / total_chars:.1%}:")
        for x in top:
            print(f"  idx={x['idx']:>4} {x['role']:>9} tool={x['tool'] or '-':<16} "
                  f"chars={x['chars']:>8} ({x['chars'] / total_chars * 100:.1f}%)  {x['preview'][:60]}")
        print("\n[roles]:")
        for k, a in sorted(roles.items(), key=lambda kv: -kv[1]["chars"]):
            print(f"  {k:<32} count={a['count']:>4} chars={a['chars'] / 1e6:.2f}M ({a['chars'] / total_chars * 100:.1f}%)")
        if settled_like:
            print(f"\n[verdict] task_settled 结算消息 {len(settled_like)} 条占 "
                  f"{settled_chars / total_chars:.1%} —— 结算风暴固化进投影（external_input "
                  f"不在工具对折叠范围）即回收不及预期的直接根因；建议：批量退役汇总为单事件 + "
                  f"结算类 external_input 纳入压缩票据化折叠。")
    else:
        print(json.dumps({
            "turns": turns, "compactions": compactions,
            "top_messages": top, "roles": roles,
            "settled_like": {"count": len(settled_like), "chars": settled_chars},
            "peak_prompt_tokens": peak, "floor_prompt_tokens": worst,
        }, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
