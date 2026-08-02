#!/usr/bin/env python3
"""replace_anchors.py — 多锚点精确文本替换（幂等、强校验、防误替换）

用于对文档做批量精确修订：每个 old 字符串必须恰好出现 1 次（count==1），
否则整体中止、绝不部分写回。支持幂等（new 已存在则跳过）、自动 .bak 备份。

用法：
  单文件 + JSON 内联：
    python replace_anchors.py --file PATH --json '[{"old":"A","new":"B"},...]'
  单文件 + --map（old::new，可用多次）：
    python replace_anchors.py --file PATH --map "A::B" --map "C::D"
  多文件（JSON 文件）：
    python replace_anchors.py --json-file map.json
    # map.json: {"files":[{"file":"PATH","replacements":[{"old":"A","new":"B"}]}]}

退出码：0=全部成功；1=有锚点不匹配（未写回）；2=参数错误。
"""
import argparse, json, os, sys

def apply_replacements(text, replacements):
    report = []; applied = 0; skipped = 0
    for i, r in enumerate(replacements):
        old = r['old']; new = r['new']
        cnt = text.count(old)
        if cnt == 0:
            if text.count(new) >= 1:
                report.append(f"  #{i}: SKIP (已应用/幂等)"); skipped += 1; continue
            raise ValueError(f"#{i} old 未匹配且 new 也不存在：{old[:50]!r}")
        if cnt > 1:
            raise ValueError(f"#{i} old 匹配 {cnt} 次（需恰好1次）：{old[:50]!r}")
        text = text.replace(old, new, 1)
        report.append(f"  #{i}: REPLACED"); applied += 1
    return text, report, applied, skipped

def process_file(f, replacements):
    with open(f, encoding='utf-8') as fh:
        text = fh.read()
    new_text, report, applied, skipped = apply_replacements(text, replacements)
    if applied == 0 and skipped == 0:
        print(f"[skip] {f}: 无变更"); return 0
    bak = f + '.bak'
    if os.path.exists(bak):
        os.remove(bak)
    os.rename(f, bak)
    with open(f, 'w', encoding='utf-8') as fh:
        fh.write(new_text)
    print(f"[ok] {f}: 应用 {applied}，跳过 {skipped}")
    for line in report:
        print(line)
    return applied

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--file'); ap.add_argument('--json')
    ap.add_argument('--json-file'); ap.add_argument('--map', action='append', default=[])
    args = ap.parse_args()
    jobs = []
    if args.json_file:
        data = json.load(open(args.json_file, encoding='utf-8'))
        jobs = data.get('files', [])
    elif args.file:
        reps = []
        if args.json:
            reps += json.loads(args.json)
        for m in args.map:
            if '::' not in m:
                print(f"[err] --map 需 old::new 格式：{m}"); sys.exit(2)
            o, n = m.split('::', 1); reps.append({'old': o, 'new': n})
        jobs = [{'file': args.file, 'replacements': reps}]
    else:
        print("[err] 需 --file+--json/--map 或 --json-file"); sys.exit(2)
    total = 0
    for job in jobs:
        try:
            total += process_file(job['file'], job['replacements'])
        except ValueError as e:
            print(f"[ABORT] {job['file']}: {e}"); sys.exit(1)
    print(f"\n完成：共应用 {total} 处替换")
    sys.exit(0)

if __name__ == '__main__':
    main()
