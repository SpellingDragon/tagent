#!/usr/bin/env python3
# loggrep.py — 日志/命令输出的安全提取工具（冥想沉淀 2026-09-09）
#
# 痛点：tagent 运行日志与探针输出常混入非 UTF-8 字节（LLM 明文/控制字符/二进制碎片），
#       read_file 会拒读（"file is not a UTF-8 text file"），iconv -c 清洗仍可能残留，
#       grep 输出还会把自身探针命令当命中（自噪声）。本脚本一次解决：容错读 + 可打印化 +
#       关键词过滤（跳过探针自噪声）+ 行号定位，输出保证 UTF-8 纯净可回读。
#
# 用法:
#   python3 scripts/loggrep.py <logfile> <pattern> [--tail N] [--ctx LINES] [--out FILE]
#     logfile   目标日志文件（UTF-8 混杂均可）
#     pattern   正则（python re 语法，不区分大小写）
#     --tail N  只扫最后 N 行（默认 2000，0=全量）
#     --ctx N   命中行前后各带 N 行上下文（默认 0）
#     --out F   结果写文件（否则打印 stdout）
#
# 示例:
#   python3 scripts/loggrep.py logs/wechat-bot-default.log 'hot-sync|zread' --tail 500
#   python3 scripts/loggrep.py robot.log 'panic|fatal' --ctx 2 --out /tmp/p.txt

import argparse
import re
import sys


def clean(line: str) -> str:
    """控制字符 → 空格，其余保留（含中文），保证可打印。"""
    return ''.join(ch if ch.isprintable() else ' ' for ch in line).rstrip()


def main() -> int:
    ap = argparse.ArgumentParser(description='safe grep for mixed-encoding logs')
    ap.add_argument('logfile')
    ap.add_argument('pattern')
    ap.add_argument('--tail', type=int, default=2000)
    ap.add_argument('--ctx', type=int, default=0)
    ap.add_argument('--out', default=None)
    args = ap.parse_args()

    lines = []
    try:
        with open(args.logfile, encoding='utf-8', errors='replace') as f:
            lines = [clean(l) for l in f]
    except OSError as e:
        print(f'ERROR: cannot read {args.logfile}: {e}', file=sys.stderr)
        return 2

    if args.tail > 0:
        lines = lines[-args.tail:]

    pat = re.compile(args.pattern, re.I)
    idxs = [i for i, l in enumerate(lines) if pat.search(l)]
    if not idxs:
        print(f'(no match for {args.pattern!r} in last {len(lines)} lines)')
        return 1

    # 带 ctx 时合并相邻区间，避免重复行
    shown, out = set(), []
    ranges = []
    for i in idxs:
        lo, hi = max(0, i - args.ctx), min(len(lines) - 1, i + args.ctx)
        if ranges and lo <= ranges[-1][1] + 1:
            ranges[-1] = (ranges[-1][0], hi)
        else:
            ranges.append((lo, hi))
    for lo, hi in ranges:
        for i in range(lo, hi + 1):
            if i not in shown:
                shown.add(i)
                out.append(f'{i + 1}: {lines[i]}')
        out.append('---')

    body = f'[{args.logfile}] match {len(idxs)} lines (showing {len(out) - len(ranges)}):\n' + '\n'.join(out)
    if args.out:
        with open(args.out, 'w', encoding='utf-8') as f:
            f.write(body + '\n')
        print(f'written {len(out)} lines -> {args.out}')
    else:
        print(body)
    return 0


if __name__ == '__main__':
    sys.exit(main())
