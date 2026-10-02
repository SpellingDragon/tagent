#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Comment-slot sweeps for a Go scope, used by the documentation change.

Modes (comma-separated via --do):
  sweep   delete comments that are not in a documentation slot (free-standing);
  strip   remove process-artifact residue from comment lines;
  prefix  insert the declared identifier at the head of its own doc group.

Design constraints that this tool must never violate:
  * one mutation type per pass, and every item recomputes positions from a fresh scan;
  * deletions only ever remove whole comment lines — a statement carrying a trailing
    comment has just the comment stripped (see tasks.md 56.2 for the accident that
    taught this);
  * each candidate file is syntax-probed before it is written;
  * an optional skip list (one path per line, '#' comments) is honoured so that
    concurrent work can be excluded. The list must be maintained deliberately — a
    live `git status` cannot be used for this, because our own uncommitted edits
    also show as modified (tasks.md 77.2).

Default is dry-run; pass --run to write. Judgment lines removed by `sweep` are
never dropped silently: they go to --gone, and those matching judgment keywords
also to --quarantine for later adjudication against the long-lived docs.
"""
import argparse
import io
import os
import re
import subprocess
import sys

STRIP = [
    (re.compile(r'\s*[（(][^（()）]*(?:§|\b20[0-9]{2}-[0-9]{2}-[0-9]{2}\b|design|spec\b|'
                r'report|hardening|review|cold-eyes)[^（()）]*[)）]'), ''),
    (re.compile(r'§[0-9]+(?:\.[0-9]+)*[①-⑨]?'), ''),
    (re.compile(r'\b20[0-9]{2}-[0-9]{2}-[0-9]{2}\b'), ''),
    (re.compile(r'\(\s*(?:R|T|C|D|S|M|F|W|A|L)[0-9]+(?:\.[0-9]+)*\s*[，,]?\s*\)'), ''),
    (re.compile(r'\s{3,}'), ' '),
]
JUDGMENT = re.compile(u'禁|不得|必须|否则|原因|契约|静默|竞态|防|误|唯一|绝不|只能|须|'
                      u'must|never|only|single source|guard|silent|race|prevent|instead of|without')
DECL = re.compile(r'^(?:func \(\w+ \*?(\w+)\) (\w+)\(|func (\w+)\(|type (\w+)|var (\w+)|const (\w+)'
                  r'|\t+(\w+)(?=[ \t]+\S))')


def sh(args):
    return subprocess.run(args, capture_output=True).stdout.decode('utf-8', 'replace')


def load_skip(path):
    if not path or not os.path.exists(path):
        return set()
    return {l.strip() for l in io.open(path, encoding='utf-8')
            if l.strip() and not l.startswith('#')}


def files_in(scope, skip):
    """All .go files under scope, minus the skip list.

    Walks the directory rather than `git ls-files`: the policy scans directories too,
    so an untracked file is still in scope. Using ls-files silently excluded those
    (found on agent/governance, where every report belonged to untracked tests).
    """
    out = []
    for root, dirs, names in os.walk(scope):
        dirs[:] = [d for d in dirs if d not in ('testdata', '.git')]
        for n in sorted(names):
            if not n.endswith('.go'):
                continue
            p = os.path.join(root, n)
            if p not in skip and os.path.exists(p):
                out.append(p)
    return sorted(out)


def scan(scope, rule):
    # -no-baseline: these tools measure one scope at a time, which the ratchet guard
    # rejects on purpose — a smaller scope would read as a lowered baseline.
    hits = []
    for l in sh(['go', 'run', './scripts/comment_policy', '-v', '-no-baseline', scope]).split('\n'):
        m = re.match(r'(\S+):(\d+): %s: (.*?)(?: \| (\S+))?$' % rule, l.strip())
        if m:
            hits.append((m.group(1), int(m.group(2)), (m.group(4) or '').strip()))
    return hits


def write(p, text):
    io.open('/tmp/sweep_probe.go', 'w', encoding='utf-8').write(text)
    r = subprocess.run(['gofmt', '-e', '/tmp/sweep_probe.go'], capture_output=True)
    if r.returncode != 0:
        raise AssertionError('%s: probe rejected: %s' % (p, r.stderr.decode()[:160]))
    io.open(p, 'w', encoding='utf-8').write(text)


def do_sweep(scope, skip, run, gone, quar, rounds=60):
    removed = 0
    for _ in range(rounds):
        hits = {}
        for p, ln, _ in scan(scope, 'free-standing'):
            if p not in skip:
                hits.setdefault(p, []).append(ln)
        if not hits:
            break
        for p, lns in hits.items():
            L = io.open(p, encoding='utf-8').read().split('\n')
            for i in sorted(set(lns), reverse=True):
                k = i - 1
                if k >= len(L):
                    continue
                st = L[k].strip()
                if st.startswith('//'):
                    line = st
                    del L[k]
                elif '//' in st:
                    line = st
                    code = L[k].split('//')[0]
                    if code.strip():
                        L[k] = code.rstrip()
                    else:
                        del L[k]
                else:
                    continue
                gone.write('%s:%d\t%s\n' % (p, i, line[:200]))
                if JUDGMENT.search(line):
                    quar.write('%s:%d\t%s\n' % (p, i, line[:200]))
                removed += 1
            if run:
                write(p, '\n'.join(L))
        subprocess.run(['gofmt', '-w'] + sorted(hits), capture_output=True)
    return removed


def do_strip(scope, skip, run):
    touched = 0
    for p in files_in(scope, skip):
        L = io.open(p, encoding='utf-8').read().split('\n')
        ch = False
        for i, l in enumerate(L):
            if not l.lstrip().startswith('//'):
                continue
            s = l
            for rx, rep in STRIP:
                s = rx.sub(rep, s)
            s = re.sub(u'钉住\\s*[：:]\\s*', u'钉住 ', s)
            if s != l:
                L[i] = s.rstrip()
                ch = True
                touched += 1
        if ch and run:
            write(p, '\n'.join(L))
    return touched


def do_prefix(scope, skip, run, cap=200):
    seen, done, skipped = set(), 0, []
    movable = set(files_in(scope, skip))
    for _ in range(cap):
        todo = [x for x in scan(scope, 'doc-not-name-prefixed')
                if x[0] in movable and x not in seen]
        if not todo:
            break
        p, ln, name = todo[0]
        seen.add((p, ln, name))
        L = io.open(p, encoding='utf-8').read().split('\n')
        i = ln - 1
        if i >= len(L) or L[i].lstrip().startswith('//'):
            skipped.append((p, ln, name, 'anchor not a declaration'))
            continue
        if name == '_':
            skipped.append((p, ln, name, 'blank identifier group'))
            continue
        m = DECL.match(L[i])
        names = [g for g in m.groups() if g] if m else []
        if not names and re.match(r'^\s*(?:const|var|type)\s*\($', L[i]):
            for k in range(i + 1, min(i + 8, len(L))):
                mm = re.match(r'^\t*(\w+)', L[k])
                if mm and not L[k].lstrip().startswith('//') and mm.group(1) != ')':
                    names = [mm.group(1), L[k].strip()]
                    break
        if not names or name not in names:
            # The policy reports a group's names joined ("A,B,C"); the doc must start
            # with the first member, so accept that form too (tasks.md 82.4).
            first = name.split(',')[0].strip()
            if not names or first not in names:
                skipped.append((p, ln, name, 'decl mismatch %s' % names))
                continue
        j = i - 1
        while j >= 0 and L[j].lstrip().startswith('//'):
            j -= 1
        j += 1
        if j >= i:
            skipped.append((p, ln, name, 'no doc group above'))
            continue
        head = names[0]
        rest = L[j].lstrip()[2:].strip()
        if rest.startswith(head):
            skipped.append((p, ln, name, 'already compliant'))
            continue
        lead = L[j][:len(L[j]) - len(L[j].lstrip())]
        L[j] = lead + u'// ' + head + u' ' + rest
        if run:
            write(p, '\n'.join(L))
        done += 1
    return done, skipped


def do_tdoc(scope, skip, run, gone, quar, cap=600):
    """One test doc = one intent line (starting with the func name) + its index.

    Extra lines are argument/narration and belong in assertion messages or the docs,
    so they are reported away (and archived). The group is replaced in a single
    slice assignment per item and the scan is redone afterwards, so no stale index is
    ever used (the mistake recorded in tasks.md 68/72).
    """
    seen, done, skipped = set(), 0, []
    movable = set(files_in(scope, skip))
    stall = 0
    for _ in range(cap):
        todo = [x for x in scan(scope, 'test-doc-not-one-line')
                if x[0] in movable and (x[0], x[1]) not in seen]
        if not todo:
            break
        before = done
        p, ln, _ = todo[0]
        L = io.open(p, encoding='utf-8').read().split('\n')
        # The policy reports the DECLARATION line for this rule (as for
        # doc-not-name-prefixed, see tasks.md 64.2), so anchor there and walk up.
        d = ln - 1
        if d >= len(L):
            seen.add((p, ln))
            skipped.append((p, ln, 'line out of range'))
            continue
        if L[d].lstrip().startswith('//'):
            d += 1
        if not re.match(r'\s*func (?:Test|Example)\w+\(', L[d] if d < len(L) else ''):
            seen.add((p, ln))
            skipped.append((p, ln, 'declaration is not a test func'))
            continue
        k = d
        j = d - 1
        while j >= 0 and L[j].lstrip().startswith('//'):
            j -= 1
        i = j + 1
        if i >= k:
            seen.add((p, ln))
            skipped.append((p, ln, 'no doc group above'))
            continue
        fn = re.match(r'\s*func ((?:Test|Example)\w+)\(', L[k]).group(1)
        group = L[i:k]
        idx = [x for x in group if re.match(r'\s*//\s*(契约|规格):', x)]
        body = [x for x in group
                if x.strip() != '//' and x not in idx and x.strip()]
        if not body:
            seen.add((p, ln))
            skipped.append((p, ln, 'no content line'))
            continue
        keep = body[0].lstrip()[2:].strip()
        keep = re.sub(r'^' + re.escape(fn) + r'\s*', '', keep).strip(u' —:：')
        # Drop a leading copula/verb left over from "TestX 是/验证/钉住 …" so the
        # result reads as one intent clause instead of "钉住 是 …".
        keep = re.sub(u'^(?:是|为|验证|校验|钉住|覆盖|检查|确保|保证)\\s*[，,：:]?\\s*', '', keep)
        # A doc comment is wrapped prose, and an English period is NOT a reliable
        # sentence end ("…S3m-c.1 convergence contract." proved it: joining stopped at
        # an abbreviation and cut the sentence — tasks.md 95 and 98). So `tdoc` only
        # compresses when a clause already ends with a Chinese full stop; anything
        # else is left for a human to author.
        if not keep.endswith(u'。'):
            skipped.append((p, ln, fn, 'intent does not end with 。 — needs manual authoring'))
            seen.add((p, ln))
            continue
        new = [u'// ' + fn + u' 钉住 ' + (keep or u'本文件的既有契约')]
        if idx:
            new += [u'//'] + idx
        for x in body[1:]:
            st = x.strip()
            gone.write('%s:%d\t%s\n' % (p, ln, st[:200]))
            if JUDGMENT.search(st):
                quar.write('%s:%d\t%s\n' % (p, ln, st[:200]))
        if run:
            L[i:k] = new
            write(p, '\n'.join(L))
        done += 1
    return done, skipped


def do_name_refs(scope, skip, run, names_file):
    """Remove references to enumerable change names from comment lines.

    The list comes from a file (one name per line) so the rule is evidence-based: a
    token is only removed when it is an actual change directory name. Nothing is
    invented — this never rewrites prose, it only deletes citations and the empty
    punctuation they leave behind.
    """
    names = [l.strip() for l in io.open(names_file, encoding='utf-8') if l.strip() and not l.startswith('#')]
    if not names:
        raise SystemExit('empty change-name list')
    pat = re.compile('|'.join(re.escape(n) for n in sorted(names, key=len, reverse=True)))
    touched = 0
    for p in files_in(scope, skip):
        L = io.open(p, encoding='utf-8').read().split('\n')
        ch = False
        for i, l in enumerate(L):
            if not l.lstrip().startswith('//') or not pat.search(l):
                continue
            s = pat.sub('', l)
            # 清掉引用留下的空壳：空括号、尾随的编号序列、悬空标点与多余空格。
            s = re.sub(r'\s*\d+(?:\.\d+)?(?:\s*/\s*\d+(?:\.\d+)?)*\s*(?=[)）]|\s|$)', '', s)
            s = re.sub(r'[（(]\s*[)）]', '', s)
            s = re.sub(r'[,，;；]\s*(?=[)）])', '', s)
            s = re.sub(r'[：:，,、/]\s*$', '', s)
            s = re.sub(r'[：:，,、/]\s*(?=[)）])', '', s)
            s = re.sub(r' {2,}', ' ', s)
            if s.split('//', 1)[1].strip() in ('', '/'):
                L[i] = None            # 该行内容只是一个引用 ⇒ 整行删除
                ch = True
                touched += 1
                continue
            if s != l:
                L[i] = s.rstrip()
                ch = True
                touched += 1
        if ch:
            L = [x for x in L if x is not None]
        if ch and run:
            write(p, '\n'.join(L))
    return touched


def rule_counts(paths):
    """Count findings per rule by asking the gate itself.

    -no-baseline keeps this a measurement: the axis check compares two trees over the
    same scope, and consulting the ratchet here would confuse a snapshot scan with a
    real gate run.
    """
    out = subprocess.run(['go', 'run', './scripts/comment_policy', '-v', '-no-baseline'] + list(paths),
                         capture_output=True).stdout.decode('utf-8', 'replace')
    counts = {}
    for line in out.split('\n'):
        m = re.match(r'\S+?:\d+: ([a-z-]+):', line)
        if m:
            counts[m.group(1)] = counts.get(m.group(1), 0) + 1
    return counts


def do_axis(base, scopes):
    """Assert a comment-only batch did not raise any rule count.

    Hand-copied residue word lists have already missed gate tokens once, so the
    oracle here is the gate itself: compare each rule's finding count between the
    working tree and the pre-batch snapshot. Exit non-zero on any increase.
    """
    # A missing snapshot dir would silently compare against zero findings and
    # report every rule as risen, so refuse before measuring.
    for sc in scopes:
        if not os.path.isdir(sc):
            raise SystemExit('axis: scope %r is not a directory' % sc)
        if not os.path.isdir(os.path.join(base, sc)):
            raise SystemExit('axis: snapshot %s lacks scope %r' % (base, sc))
        # A partial snapshot would compare a subtree against the whole tree and
        # report every rule as risen, so require the file sets to match.
        head_n, base_n = go_count(sc), go_count(os.path.join(base, sc))
        if head_n != base_n:
            raise SystemExit('axis: snapshot of scope %r is partial (%d of %d .go files)'
                             % (sc, base_n, head_n))
    now, before = rule_counts(scopes), rule_counts([os.path.join(base, sc) for sc in scopes])
    if not before:
        raise SystemExit('axis: base scan produced no findings at all; refusing to compare')
    bad = []
    for rule in sorted(set(now) | set(before)):
        n, b = now.get(rule, 0), before.get(rule, 0)
        flag = 'RISEN' if n > b else ('ok' if n < b else 'same')
        if n > b:
            bad.append((rule, b, n))
        print('  %-30s %5d -> %5d  %s' % (rule, b, n, flag))
    if bad:
        print('axis: %d rule(s) increased: %s' % (len(bad), ', '.join(r for r, _, _ in bad)))
        return 1
    print('axis: no rule increased (base=%s)' % base)
    return 0


def gate_pattern(var):
    """Compile a regex straight out of the gate source, so the pre-write check
    can never drift from the words the gate actually matches.
    """
    src = io.open(os.path.join('scripts', 'comment_policy', 'main.go'), encoding='utf-8').read()
    m = re.search(var + r' = regexp\.MustCompile\(`([^`]*)`\)', src)
    if not m:
        raise SystemExit('gate pattern %s not found; the gate source changed shape' % var)
    return re.compile(m.group(1), re.IGNORECASE)


def do_lint_lines(path):
    """Report candidate comment lines that the gate would flag as residue.

    Runs before any write: an offending line is cheaper to fix in the draft than
    to discover in the post-batch axis diff.
    """
    bad = 0
    for i, line in enumerate(io.open(path, encoding='utf-8').read().split('\n')):
        t = line.strip()
        if not t:
            continue
        for var in ('auditMarker', 'rationale', 'mechanismStep'):
            hit = gate_pattern(var).search(t)
            if hit:
                print('  L%d %s hits %r in %s' % (i + 1, var, hit.group(0), t[:60]))
                bad += 1
                break
    print('lint-lines: %d offending line(s)' % bad)
    return 1 if bad else 0


def go_count(root):
    n = 0
    for _, _, fs in os.walk(root):
        n += sum(1 for f in fs if f.endswith('.go'))
    return n


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('scope')
    ap.add_argument('--do', default='sweep,strip,prefix')
    ap.add_argument('--skip-list', default='')
    ap.add_argument('--gone', default='/tmp/sweep_gone.txt')
    ap.add_argument('--quarantine', default='/tmp/sweep_quarantine.txt')
    ap.add_argument('--names', default='', help='file of enumerable change names for the name-refs mode')
    ap.add_argument('--run', action='store_true')
    ap.add_argument('--base', default='', help='pre-batch snapshot dir for the axis mode')
    ap.add_argument('--axis-scopes', default='', help='comma list of scopes compared by the axis mode')
    ap.add_argument('--lines', default='', help='file of candidate comment lines for the lint-lines mode')
    a = ap.parse_args()
    skip = load_skip(a.skip_list)
    modes = [m.strip() for m in a.do.split(',') if m.strip()]
    if 'lint-lines' in modes:
        if not a.lines:
            raise SystemExit('lint-lines needs --lines <file>')
        return do_lint_lines(a.lines)

    if 'axis' in modes:
        if not a.base or not a.axis_scopes:
            raise SystemExit('axis needs --base <snapshot> and --axis-scopes a,b')
        return do_axis(a.base, [x.strip() for x in a.axis_scopes.split(',') if x.strip()])
    print('scope=%s movable=%d modes=%s run=%s' % (a.scope, len(files_in(a.scope, skip)), modes, a.run))
    if not a.run:
        print('dry-run: nothing written')
        return 0
    gone = io.open(a.gone, 'a', encoding='utf-8')
    quar = io.open(a.quarantine, 'a', encoding='utf-8')
    try:
        if 'sweep' in modes:
            print('sweep: 删除游离注释 %d 行' % do_sweep(a.scope, skip, a.run, gone, quar))
        if 'tdoc' in modes:
            n, sk = do_tdoc(a.scope, skip, a.run, gone, quar)
            print('tdoc: 压成一行意图 %d 处；跳过示例 %s' % (n, sk[:3]))
        if 'name-refs' in modes:
            if not a.names:
                raise SystemExit('name-refs needs --names <file of change names>')
            print('name-refs: 剥除变更名引用 %d 行' % do_name_refs(a.scope, skip, a.run, a.names))
        if 'strip' in modes:
            print('strip: 剥残留 %d 行' % do_strip(a.scope, skip, a.run))
        if 'prefix' in modes:
            n, sk = do_prefix(a.scope, skip, a.run)
            print('prefix: 本名前缀 %d 处；跳过示例 %s' % (n, sk[:4]))
    finally:
        gone.close()
        quar.close()
    return 0


if __name__ == '__main__':
    sys.exit(main())
