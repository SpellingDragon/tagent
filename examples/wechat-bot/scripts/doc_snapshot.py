#!/usr/bin/env python3
# doc_snapshot.py — 长文破坏性编辑前的快照/回滚工具（AGENTS.md「可逆性优先」条文的落地实现）
#
# 痛点：对长文（yaml/配置/交付文档）做不可逆编辑前需要可还原备份；曾发生删除唯一备份后
#       凭记忆 reconstruct 翻车。本工具提供：快照（带时间戳，保留多版）→ 编辑 → 必要时 restore。
#
# 用法:
#   python3 scripts/doc_snapshot.py snapshot <file>     # 编辑前留快照（存 .snapshots/）
#   python3 scripts/doc_snapshot.py restore  <file>     # 回滚到最近一次快照
#   python3 scripts/doc_snapshot.py list     <file>     # 查看该文件的全部快照
#
# 设计：快照放目标文件同目录 .snapshots/<name>.<ts>.bak；restore 只在快照存在时动作，
#       恢复前把当前（可能已损坏）版本再存一份 .damaged.<ts>.bak，双保险不丢数据。

import shutil
import sys
import time
from pathlib import Path


def snapdir(target: Path) -> Path:
    d = target.parent / '.snapshots'
    d.mkdir(exist_ok=True)
    return d


def snaps(target: Path):
    return sorted(snapdir(target).glob(f'{target.name}.*.bak'))


def main() -> int:
    if len(sys.argv) != 3 or sys.argv[1] not in ('snapshot', 'restore', 'list'):
        print(__doc__)
        return 2
    op, arg = sys.argv[1], Path(sys.argv[2])
    if not op == 'snapshot' and not arg.exists():
        print(f'ERROR: {arg} not found')
        return 2

    if op == 'snapshot':
        if not arg.exists():
            print(f'ERROR: {arg} not found')
            return 2
        ts = time.strftime('%Y%m%d-%H%M%S')
        dst = snapdir(arg) / f'{arg.name}.{ts}.bak'
        shutil.copy2(arg, dst)
        print(f'snapshot ok: {dst}')
        return 0

    existing = snaps(arg)
    if not existing:
        print(f'ERROR: no snapshot for {arg}')
        return 1
    if op == 'list':
        for s in existing:
            print(f'{s}  ({s.stat().st_size}B)')
        return 0

    latest = existing[-1]
    if arg.exists():
        damaged = snapdir(arg) / f'{arg.name}.damaged.{time.strftime("%Y%m%d-%H%M%S")}.bak'
        shutil.copy2(arg, damaged)
        print(f'current (possibly damaged) version kept: {damaged}')
    shutil.copy2(latest, arg)
    print(f'restored {arg} <- {latest}')
    return 0


if __name__ == '__main__':
    sys.exit(main())
