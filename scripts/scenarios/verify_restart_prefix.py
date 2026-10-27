#!/usr/bin/env python3
"""verify_restart_prefix.py — 重启边界前缀一致性验证 v3（hardening-review-batch2 4.1-4.3）

用法: python3 verify_restart_prefix.py <trajectory.jsonl>

v3 判定规则（spec: trajectory-verification）:
- 身份配对: 重启点（batch_index==0）与 (agent, session) 身份绑定；Before 锚点
  = 同身份死亡前**最后一条**请求——禁止向前搜索更早/更易匹配的记录。
- 分段比较: 历史段与系统注入段（看板行）分开报告；看板差异单列 BOARD_DIFF，
  不计入 EXACT 也不计入 CRITICAL。
- 判定等级:
    EXACT          历史段逐字节前缀全等且无看板差异 —— 通过
    BOARD_DIFF     历史段全等、仅看板段不同 —— 通过（动态段单列）
    CRITICAL       恢复历史是死亡前历史的真前缀（截尾）—— 丢失 len(A)-len(B) 条
    DIVERGED       在前缀范围内出现内容分歧 —— 人工排查
    FRESH          小历史(<30 条) —— 子 agent 首调/新会话，方法不适用
    UNPAIRED       同身份找不到死亡前请求 —— 无法验证，计失败
- 退出码: CRITICAL>0 或 UNPAIRED>0 → 1（验收失败可见）；其余 0。

v2→v3 修复: 向前任意配对可把「死亡前 100 条→恢复 30 条」误判 EXACT；
FOLD 分支先于截尾判断且 bh[len(bh)] 越界；CRITICAL 不影响退出码。
"""
import json
import sys

BOARD = '[后台任务看板]'
FRESH_MIN_HISTORY = 30


def load_recs(path):
    with open(path, encoding='utf-8', errors='replace') as f:
        return [json.loads(line) for line in f if line.strip()]


def msgs(r):
    return r['llm_call']['request']['messages']


def identity(r):
    """(agent, session) 身份键；缺失字段用 '?' 占位（同键才配对）。"""
    agent = r.get('agent_name') or r.get('llm_call', {}).get('agent_name') or '?'
    session = r.get('session_id') or r.get('llm_call', {}).get('session_id') or '?'
    return str(agent), str(session)


def split_segments(ms):
    """(历史段, 看板段)：看板行从历史段剥离、单独保留（分段比较）。"""
    history, board = [], []
    for m in ms:
        if str(m.get('content', '')).startswith(BOARD):
            board.append(m)
        else:
            history.append(m)
    return history, board


def strip_sys(ms):
    i = 0
    while i < len(ms) and ms[i].get('role') == 'system':
        i += 1
    return ms[i:]


def prefix_len(a, b):
    n = min(len(a), len(b))
    for i in range(n):
        if a[i] != b[i]:
            return i, False
    return n, True


def main():
    if len(sys.argv) < 2:
        print(__doc__)
        sys.exit(2)
    path = sys.argv[1]
    recs = load_recs(path)

    def hist(r):
        h, _ = split_segments(strip_sys(msgs(r)))
        return h

    def boardseg(r):
        _, b = split_segments(strip_sys(msgs(r)))
        return b

    zeros = [i for i in range(len(recs)) if recs[i].get('batch_index') == 0]
    switches = sum(1 for i in range(1, len(recs))
                   if recs[i]['batch_index'] < recs[i - 1]['batch_index']
                   and recs[i]['batch_index'] != 0)
    print(f"file={path} records={len(recs)} 真重启点(batch==0)={len(zeros)} "
          f"非零回退(agent切换,排除)={switches}")

    stats = {'EXACT': 0, 'BOARD_DIFF': 0, 'CRITICAL': 0,
             'DIVERGED': 0, 'FRESH': 0, 'UNPAIRED': 0}
    rows, failures = [], []

    for bi in zeros:
        B = recs[bi]
        bh = hist(B)
        if not bh or len(bh) < FRESH_MIN_HISTORY:
            stats['FRESH'] += 1
            rows.append((bi, 'FRESH', -1, len(bh), 0, 'small history — method N/A'))
            continue

        # 身份配对：同 (agent, session) 的死亡前最后一条请求（严格锚定，
        # 禁止向前搜索更早/更易匹配的记录）。
        anchor = -1
        for j in range(bi - 1, -1, -1):
            A = recs[j]
            if A.get('batch_index', 1) == 0:
                continue  # 跳过其它重启点（新进程首调，非死亡前状态）
            if identity(A) == identity(B):
                anchor = j
                break

        if anchor < 0:
            stats['UNPAIRED'] += 1
            failures.append(bi)
            rows.append((bi, 'UNPAIRED', -1, len(bh), 0,
                         'no same-identity pre-restart record — NOT verified'))
            continue

        ah = hist(recs[anchor])
        m, prefix_ok = prefix_len(ah, bh)

        if prefix_ok and len(bh) == len(ah):
            if boardseg(recs[anchor]) == boardseg(B):
                stats['EXACT'] += 1
                rows.append((bi, 'EXACT', anchor, m, len(ah), ''))
            else:
                stats['BOARD_DIFF'] += 1
                rows.append((bi, 'BOARD_DIFF', anchor, m, len(ah),
                             'history equal; board segment differs (dynamic, single-listed)'))
        elif m == len(bh) and len(bh) < len(ah):
            # 截尾优先于一切宽松判定（v2 反例：先 FOLD 后越界）。
            lost = len(ah) - len(bh)
            stats['CRITICAL'] += 1
            failures.append(bi)
            rows.append((bi, 'CRITICAL', anchor, m, len(ah),
                         f'history TRUNCATED: {len(bh)}/{len(ah)} — lost {lost} msgs'))
        else:
            x = ah[m] if m < len(ah) else {'content': '<eof>'}
            y = bh[m] if m < len(bh) else {'content': '<eof>'}
            stats['DIVERGED'] += 1
            rows.append((bi, 'DIVERGED', anchor, m, len(ah),
                         f'div@{m} A={str(x.get("content"))[:60]!r} B={str(y.get("content"))[:60]!r}'))

    print('\n=== 逐重启点 ===')
    for bi, v, j, m, lah, note in rows:
        t = str(recs[bi].get('timestamp', ''))[:19]
        print(f'[{bi}] {t} batch0 {v:10} pair@{j} prefix={m}/{lah} {note}')
    print('\n=== 汇总 ===')
    for k in ('EXACT', 'BOARD_DIFF', 'CRITICAL', 'DIVERGED', 'FRESH', 'UNPAIRED'):
        print(f'{k}: {stats[k]}')

    # 4.2：验收失败必须可见——CRITICAL/UNPAIRED 非零 → exit 1。
    if stats['CRITICAL'] > 0 or stats['UNPAIRED'] > 0:
        print('\nVERDICT: FAIL (history loss or unverifiable restart points present)')
        sys.exit(1)
    print('\nVERDICT: PASS')


if __name__ == '__main__':
    main()
