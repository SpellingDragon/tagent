# -*- coding: utf-8 -*-
"""Per-domain test-file consolidation mappings for W2.

One responsibility = one test file; different scenarios become subtests. Targets
follow the production spine of each package. Caps: a target stays under ~1,500
lines, otherwise it splits by sub-responsibility (never by scenario).
"""
import io, os, re, sys, glob

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import consolidate_lib as LIB

MODULE_PREFIX = 'github.com/SpellingDragon/tagent'

DOMAINS = {
 'agent/task': {
   'task_manager_test.go': ['task_manager_test.go', 'spawn_gate_test.go', 'bind_detector_test.go',
     'finalize_consistency_test.go', 'finalize_retired_test.go', 'm3_ttl_consumer_test.go',
     'task_alive_detached_test.go', 'task_batch_concurrency_test.go', 'task_batch_retire_ttl_test.go',
     'task_liveness_reconcile_test.go', 'task_orphan_retire_test.go', 'task_prune_nil_detector_test.go',
     'task_resume_nil_test.go', 'task_resume_test.go', 'task_ttl_counterexample_test.go',
     'task_ttl_orthogonal_test.go', 'task_ttl_test.go', 'task_zombie_reconcile_test.go',
     'watch_notify_test.go'],
   'task_board_test.go': ['task_board_test.go'],
 },
 'agent/compress': {
   'context_compressor_test.go': ['context_compressor_test.go', 'card_fuzz_test.go', 'card_guard_test.go',
     'compress_usermsg_test.go', 'config_formula_test.go', 'hot_params_test.go', 'hot_source_test.go',
     'level_exponential_test.go', 'multimodal_render_test.go', 'rolling_narrative_test.go',
     'settle_fold_test.go', 'skeleton_archive_test.go', 'skeleton_compress_test.go',
     'smart_compress_test.go', 'tool_chain_test.go'],
   'session_projection_test.go': ['session_projection_test.go', 'projection_idempotent_test.go',
     'task_segmenter_test.go'],
 },
 'agent/governance': {
   'gate_test.go': ['gate_w3_test.go', 'enforce_test.go', 'classifier_test.go',
     'switch_combo_test.go', 'tool_test.go'],
   'approval_test.go': ['approval_channel_test.go', 'approval_w2_test.go'],
   'goal_test.go': ['goal_persist_test.go'],
   'ledger_test.go': ['ledger_n2_test.go'],
 },
 'agent/reliability': {
   'inbox_test.go': ['inbox_test.go', 'inbox_atomic_write_test.go', 'inbox_cleanup_account_test.go',
     'inbox_coordination_test.go', 'inbox_prepare_version_test.go', 'inbox_receivecrash_matrix_test.go',
     'inbox_sweep_account_test.go', 'inbox_transitional_reset_test.go', 'inbox_validation_test.go'],
   'degradation_test.go': ['degradation_test.go', 'fault_injection_test.go'],
   'anchor_test.go': ['anchor_test.go'],
 },
}


def has_build_constraint(text):
    head = text.lstrip()
    return head.startswith('//go:build') or head.startswith('// +build')


def coverage(domain):
    m = DOMAINS[domain]
    existing = {os.path.basename(f) for f in glob.glob(domain + '/*_test.go')}
    planned = {x for srcs in m.values() for x in srcs}
    return sorted(existing - planned), sorted(planned - existing)


def collisions(domain):
    out = []
    for tgt, srcs in DOMAINS[domain].items():
        seen = {}
        for s in srcs:
            text = LIB.read_source(domain + '/' + s)
            if has_build_constraint(text) and len(srcs) > 1:
                out.append(('build-constraint', tgt, s))
            # Methods are keyed by receiver type: same method name on two types is
            # not a collision (only package-level names are).
            for m in re.finditer(r'^func \(([^)]*)\) ([A-Za-z0-9_]+)\(', text, re.M):
                recv = re.sub(r'[*\[\]]', '', m.group(1)).split()[-1] if m.group(1).strip() else '?'
                key = recv + '.' + m.group(2)
                if key.split('.')[-1].startswith(('Test', 'Benchmark')):
                    continue
                if key in seen:
                    out.append(('dup-method', tgt, key, seen[key], s))
                seen[key] = s
            for pat in (r'^func ([A-Za-z0-9_]+)\(', r'^type ([A-Za-z0-9_]+) ',
                        r'^var ([A-Za-z0-9_]+) ', r'^const ([A-Za-z0-9_]+) '):
                for n in re.findall(pat, text, re.M):
                    if n in ('_', 'init', 'TestMain') or n.startswith(('Test', 'Benchmark')):
                        continue
                    if n in seen:
                        out.append(('dup-decl', tgt, n, seen[n], s))
                    seen[n] = s
    return out


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
                raise SystemExit('%s: %s has a build constraint and must stay standalone' % (tgt, os.path.basename(path)))
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
        for s in srcs:
            if s != full:
                deletes.append(s)
        print('merged %-34s <- %2d files' % (tgt, len(srcs)))
    return deletes


if __name__ == '__main__':
    if sys.argv[1] == 'check':
        bad = 0
        for d in DOMAINS:
            miss, extra = coverage(d)
            col = collisions(d)
            total = sum(1 for _ in glob.glob(d + '/*_test.go'))
            print('%-20s 测文件 %3d → 目标 %2d | 未归位 %s | 幻影 %s | 冲突 %s' %
                  (d, total, len(DOMAINS[d]), miss or '无', extra or '无', col or '无'))
            bad += bool(miss or extra or col)
        sys.exit(1 if bad else 0)
    dom = sys.argv[1]
    dels = run(dom, sys.argv[2:] or None)
    io.open('/tmp/w2_delete.txt', 'a').write('\n'.join(os.path.join(dom, d) for d in dels) + '\n')
    print('待删: %d' % len(dels))
