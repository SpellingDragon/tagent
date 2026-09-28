# -*- coding: utf-8 -*-
"""W2 tail: the last packages that still carry one-file-per-scenario test layouts.

Packages whose test files are already 1:1 with a responsibility (memory/embedder,
memory/kv, tool/knowledge, tool/memoryx) are deliberately absent: merging them would
be churn without a single-responsibility gain.
"""
import io, os, re, sys, glob

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import consolidate_lib as LIB

MODULE_PREFIX = 'github.com/SpellingDragon/tagent'

DOMAINS = {
 'tests': {
   'async_result_test.go': ['async_result_delivery_e2e_test.go', 'async_result_routing_test.go'],
   'plan_agent_test.go': ['plan_agent_bug_test.go', 'plan_agent_create_behavior_test.go'],
   'llm_contract_test.go': ['contracts_llm_test.go', 'event_keys_llm_test.go', 'mcp_llm_test.go',
     'plan_reentry_llm_test.go', 'hy3_thinking_test.go'],
   'integration_test.go': ['integration_test.go', 'tagent_integration_test.go',
     'edge_case_integration_test.go'],
   'resident_e2e_test.go': ['resident_e2e_test.go', 'resident_durable_e2e_test.go'],
   'async_task_e2e_test.go': ['async_task_e2e_test.go'],
   'causal_chain_test.go': ['causal_chain_test.go'],
   'compression_test.go': ['compression_test.go'],
   'inject_bus_inputs_test.go': ['inject_bus_inputs_test.go'],
   'invariants_test.go': ['invariants_test.go'],
   'multi_user_dispatch_test.go': ['multi_user_dispatch_test.go'],
   # `//go:build soak` gates this file: merging would spread the gate or downgrade it.
   'soak_test.go': ['soak_test.go'],
   'upgrade_rollback_drill_test.go': ['upgrade_rollback_drill_test.go'],
 },
 'memory/engine': {
   'engine_bridge_test.go': ['engine_bridge_test.go', 'engine_bridge_idempotency_test.go',
     'engine_review_fixes_test.go'],
   'diagnostics_test.go': ['diagnostics_test.go'],
   'engine_contract_test.go': ['engine_contract_test.go'],
   'engine_inmemory_test.go': ['engine_inmemory_test.go'],
   'engine_persist_test.go': ['engine_persist_test.go'],
 },
 'prompt': {
   'loader_test.go': ['loader_test.go', 'loader_fallback_test.go'],
   'source_test.go': ['source_test.go'],
 },
 'tool/mcp': {
   'call_test.go': ['call_test.go', 'circuit_break_test.go'],
   'registry_test.go': ['registry_test.go'],
 },
 'examples/wechat-bot': {
   'main_test.go': ['main_delivery_target_test.go', 'main_gate_test.go'],
   'dedup_test.go': ['dedup_test.go'],
   'file_delivery_test.go': ['file_delivery_test.go'],
   'file_intake_test.go': ['file_intake_test.go'],
   'reincarnation_notice_test.go': ['reincarnation_notice_test.go'],
 },
}


def has_build_constraint(text):
    head = text.lstrip()
    return head.startswith('//go:build') or head.startswith('// +build')


def audit(domain):
    m = DOMAINS[domain]
    existing = {os.path.basename(f) for f in glob.glob(domain + '/*_test.go')}
    planned = {x for srcs in m.values() for x in srcs}
    targets = {os.path.basename(t) for t in m}
    problems = []
    miss = sorted(existing - planned - targets)
    ghost = sorted(x for x in planned - existing if x not in targets)
    if miss:
        problems.append('未归位: %s' % miss)
    if ghost:
        problems.append('幻影源: %s' % ghost)
    seen = {}
    for tgt, srcs in m.items():
        for s_ in srcs:
            if s_ in seen and seen[s_] != tgt:
                problems.append('源 %s 同属 %s 与 %s' % (s_, seen[s_], tgt))
            seen[s_] = tgt
    lines = []
    for tgt, srcs in m.items():
        n = sum(len(LIB.read_source(domain + '/' + s).split('\n')) for s in srcs)
        flag = '  ⚠ 超 1500 行' if n > 1500 else ''
        lines.append('    %-34s %2d 源 ≈%5d 行%s' % (tgt, len(srcs), n, flag))
    return problems, len(existing), len(m), lines


def collisions(domain):
    out = []
    for tgt, srcs in DOMAINS[domain].items():
        seen, pkgs = {}, set()
        for s_ in srcs:
            try:
                text = LIB.read_source(domain + '/' + s_)
            except SystemExit:
                out.append(('missing', tgt, s_))
                continue
            if has_build_constraint(text) and len(srcs) > 1:
                out.append(('build-constraint', tgt, s_))
            pkg, _imps, _b = LIB.split_imports(text)
            pkgs.add(pkg)
            for mm in re.finditer(r'^func \(([^)]*)\) ([A-Za-z0-9_]+)\(', text, re.M):
                recv = re.sub(r'[*\[\]]', '', mm.group(1)).split()[-1] if mm.group(1).strip() else '?'
                key = recv + '.' + mm.group(2)
                if key in seen:
                    out.append(('dup-method', tgt, key, seen[key], s_))
                seen[key] = s_
            for n in re.findall(r'^func (Test[A-Za-z0-9_]+|Benchmark[A-Za-z0-9_]+)\(', text, re.M):
                if n in seen:
                    out.append(('dup-test', tgt, n, seen[n], s_))
                seen[n] = s_
            for pat in (r'^func ([A-Za-z0-9_]+)\(', r'^type ([A-Za-z0-9_]+) ',
                        r'^var ([A-Za-z0-9_]+) ', r'^const ([A-Za-z0-9_]+) '):
                for n in re.findall(pat, text, re.M):
                    # Test names are checked by the dedicated rule above; counting them
                    # again here would collide with themselves.
                    if n in ('_', 'init', 'TestMain') or n.startswith(('Test', 'Benchmark')):
                        continue
                    if n in seen:
                        out.append(('dup-decl', tgt, n, seen[n], s_))
                    seen[n] = s_
        if len(pkgs) > 1:
            out.append(('mixed-package', tgt, sorted(pkgs)))
    return out


def leftovers(domain):
    return [os.path.join(domain, s_) for tgt, srcs in DOMAINS[domain].items() for s_ in srcs
            if s_ != tgt and os.path.exists(os.path.join(domain, s_))]


def run(domain, targets=None):
    m = DOMAINS[domain]
    deletes = []
    for tgt in (targets or m):
        full = os.path.join(domain, tgt)
        srcs = [os.path.join(domain, s) for s in m[tgt]]
        parsed, pkgs = [], set()
        for path in srcs:
            text = LIB.read_source(path)
            if has_build_constraint(text) and len(srcs) > 1:
                raise SystemExit('%s: %s carries a build constraint' % (tgt, os.path.basename(path)))
            pkg, imports, body = LIB.split_imports(text)
            pkgs.add(pkg)
            parsed.append((imports, body))
        assert len(pkgs) == 1, (tgt, pkgs)
        pkg = pkgs.pop()
        chosen = LIB.canonical_aliases([im for im, _ in parsed], MODULE_PREFIX)
        bodies = [LIB.unify(pkg, im, body, chosen, MODULE_PREFIX) for im, body in parsed]
        union = {}
        for im, _ in parsed:
            union.update(im)
        io.open(full, 'w', encoding='utf-8').write(LIB.render(pkg, chosen, union, bodies))
        for s_ in srcs:
            if s_ != tgt:
                deletes.append(os.path.join(domain, s_))
        print('merged %-34s <- %2d files' % (tgt, len(srcs)))
    return deletes


if __name__ == '__main__':
    cmd = sys.argv[1]
    if cmd == 'check':
        bad = 0
        for d in DOMAINS:
            probs, before, after, lines = audit(d)
            col = collisions(d)
            print('%-20s %2d → %2d  %s' % (d, before, after, 'OK' if not probs and not col else ''))
            for x in probs + col:
                print('     ', x)
                bad = 1
            for l in lines:
                print(l)
        sys.exit(bad)
    if cmd == 'verify':
        bad = 0
        for d in DOMAINS:
            lo = leftovers(d)
            print('%-20s %s' % (d, 'sources cleared' if not lo else 'STILL PRESENT %s' % lo))
            bad |= bool(lo)
        sys.exit(1 if bad else 0)
    dom = cmd
    dels = run(dom, sys.argv[2:] or None)
    with io.open('/tmp/w2t_delete.txt', 'a', encoding='utf-8') as fh:
        fh.write('\n'.join(dels) + '\n')
    print('待删: %d' % len(dels))
