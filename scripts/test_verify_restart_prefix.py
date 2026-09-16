#!/usr/bin/env python3
"""test_verify_restart_prefix.py — v3 判定回归测试（hardening-review-batch2 4.3）

固化深度审查复现的两个反例 + 两个正例：
- 反例A：较早旧记录 10 条、死亡前 100 条、恢复 30 条 → v2 曾配对旧记录判
  EXACT；v3 必须以死亡前最后请求为锚 → CRITICAL + exit 1。
- 反例B：死亡前 100 条、恢复前 90 条 → v2 FOLD 分支 IndexError；v3 →
  CRITICAL（丢失 10）+ exit 1。
- 正例：完整逐字节恢复 → EXACT + exit 0。
- 看板：仅看板行差异 → BOARD_DIFF（不计 EXACT/CRITICAL）+ exit 0。

运行: python3 scripts/test_verify_restart_prefix.py   （全过 → exit 0）
"""
import json
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, 'verify_restart_prefix.py')


def make_record(agent, session, batch_index, n_history, with_board=False):
    """构造一条 trajectory 记录：system 1 条 + 历史 n_history 条 + 可选看板行。"""
    msgs = [{'role': 'system', 'content': 'SYS'}]
    for i in range(n_history):
        msgs.append({'role': 'user' if i % 2 == 0 else 'assistant',
                     'content': f'msg-{i}'})
    if with_board:
        msgs.append({'role': 'user', 'content': '[后台任务看板] snapshot-v2 (动态注入)'})
    return {
        'agent_name': agent,
        'session_id': session,
        'batch_index': batch_index,
        'timestamp': '2026-09-16T00:00:00Z',
        'llm_call': {'request': {'messages': msgs}},
    }


def run(records):
    with tempfile.NamedTemporaryFile('w', suffix='.jsonl', delete=False,
                                     encoding='utf-8') as f:
        for r in records:
            f.write(json.dumps(r, ensure_ascii=False) + '\n')
        path = f.name
    try:
        p = subprocess.run([sys.executable, SCRIPT, path],
                           capture_output=True, text=True, timeout=30)
        return p.returncode, p.stdout + p.stderr
    finally:
        os.unlink(path)


def expect(cond, label):
    if not cond:
        print(f'FAIL: {label}')
        sys.exit(1)
    print(f'PASS: {label}')


def main():
    A, S = 'main-agent', 'sess-1'

    # 反例A（v2 错判 EXACT）：前置旧记录 10 条 → 死亡前 100 → 恢复 30。
    recs = [
        make_record(A, S, 5, 10),    # 较早旧记录（v2 误配对源）
        make_record(A, S, 6, 100),   # 死亡前最后请求（真锚点）
        make_record(A, S, 0, 30),    # 重启后：仅 30 条
    ]
    code, out = run(recs)
    expect(code == 1 and 'CRITICAL: 1' in out and 'TRUNCATED: 30/100' in out,
           'counter-example A: 30/100 after 100 → CRITICAL + exit 1 (v2 wrongly EXACT)')

    # 反例B（v2 IndexError）：死亡前 100 → 恢复前 90。
    recs = [
        make_record(A, S, 6, 100),
        make_record(A, S, 0, 90),
    ]
    code, out = run(recs)
    expect(code == 1 and 'CRITICAL: 1' in out and 'TRUNCATED: 90/100' in out,
           'counter-example B: 90/100 truncated → CRITICAL + exit 1, no crash')

    # 正例：完整逐字节恢复（+ 同看板行）→ EXACT + exit 0。
    recs = [
        make_record(A, S, 6, 100, with_board=True),
        make_record(A, S, 0, 100, with_board=True),
    ]
    code, out = run(recs)
    expect(code == 0 and 'EXACT: 1' in out,
           'byte-identical restore → EXACT + exit 0')

    # 看板演进：历史全等、看板行内容不同 → BOARD_DIFF + exit 0（非 EXACT）。
    full = make_record(A, S, 6, 100, with_board=True)
    changed = make_record(A, S, 0, 100, with_board=True)
    changed['llm_call']['request']['messages'][-1]['content'] = \
        '[后台任务看板] snapshot-v3 (演进后的动态注入)'
    code, out = run([full, changed])
    expect(code == 0 and 'BOARD_DIFF: 1' in out and 'EXACT: 0' in out,
           'board-only drift → BOARD_DIFF (not EXACT, not CRITICAL) + exit 0')

    # 无世系记录：UNPAIRED → exit 1。
    recs = [
        make_record('other-agent', 'sess-x', 6, 100),  # 异身份
        make_record(A, S, 0, 80),
    ]
    code, out = run(recs)
    expect(code == 1 and 'UNPAIRED: 1' in out,
           'no same-identity anchor → UNPAIRED + exit 1')

    print('\nALL PASS')


if __name__ == '__main__':
    main()
