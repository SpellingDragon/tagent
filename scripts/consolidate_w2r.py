# -*- coding: utf-8 -*-
"""W2 remaining domains: memory, tool/action, event, plugin, tool/recall, rl, evolution.

Targets follow each package's production spine; benchmark files stay separate (a
perf witness is its own responsibility). Projected line counts are reported so a
target that would exceed ~1,500 lines gets split by sub-responsibility instead.
"""
import io, os, re, sys, glob

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import consolidate_lib as LIB

MODULE_PREFIX = 'github.com/SpellingDragon/tagent'

HOLD = {
    'memory/compaction_safety_test.go',  # untracked: the user is editing this now
}

# Files never rewritten by a merge: their whole body sits inside one block comment
# (a file disabled on purpose). Re-emitting such a file would reorder it, so it is
# excluded from the mapping and handled by the comment sweep instead.
SKIP = {'rl/http_api_test.go'}

DOMAINS = {
 'memory': {
   'segment_store_test.go': ['segment_store_test.go', 'storage_contract_test.go',
     'cold_partition_test.go', 'snowflake_floor_test.go'],
   'segment_query_test.go': ['segment_query_recency_test.go', 'segment_store_hotpath_test.go'],
   'segment_store_recovery_test.go': ['segment_store_commit_oracle_test.go',
     'segment_store_concurrency_test.go', 'segment_store_orphan_test.go'],
   'segment_store_barrier_test.go': ['segment_store_barrier_test.go'],  # package memory_test
   'segment_store_bench_test.go': ['segment_store_bench_test.go'],
   'mem_spill_test.go': ['mem_spill_test.go', 'mem_spill_capability_test.go',
     'mem_spill_retention_test.go', 'spill_safe_removal_test.go', 'retention_barrier_test.go',
     'retention_lease_test.go'],
   'mem_spill_notify_test.go': ['mem_spill_notify_test.go'],  # package memory_test
   'error_tracking_test.go': ['error_tracking_test.go', 'fault_double_test.go',
     'counterexample_test.go'],
   # External test package (`package memory_test`) cannot share a file with internal ones.
   'error_tracking_engine_test.go': ['error_tracking_engine_test.go'],
   'key_schema_test.go': ['key_schema_test.go', 'key_fuzz_test.go'],
   'lifecycle_test.go': ['lifecycle_test.go', 'wiring_test.go'],
   'query_keyword_test.go': ['query_keyword_test.go', 'query_min_event_key_test.go'],
   'compaction_test.go': ['compaction_test.go'],
   'consolidation_test.go': ['consolidation_test.go'],
   'feedback_test.go': ['feedback_test.go'],
   'relation_store_test.go': ['relation_store_test.go'],
   'testbase_test.go': ['testbase_test.go'],
 },
 'tool/action': {
   'action_test.go': ['action_test.go', 'ack_semantics_test.go', 'clean_output_test.go',
     'ttl_arg_test.go', 'probe_test.go'],
   'declarative_test.go': ['declarative_ttl_test.go', 'spawner_ttl_source_test.go'],
   'poll_schedule_test.go': ['poll_schedule_test.go', 'quiet_timeout_test.go', 'resume_baseline_test.go'],
   'resident_recovery_test.go': ['resident_gate_test.go', 'resident_meta_test.go', 'resident_recovery_test.go'],
   'settle_test.go': ['settle_test.go', 'settle_rearm_test.go', 'settle_reclaim_test.go'],
   'tmux_executor_test.go': ['tmux_executor_security_test.go', 'tmux_available_test.go',
     'tmux_complex_test.go', 'named_session_test.go', 'session_ops_test.go'],
   'tmux_monitor_test.go': ['tmux_monitor_test.go', 'monitor_schedule_test.go',
     'monitor_persession_test.go'],
   'session_lifecycle_test.go': ['lifecycle_test.go', 'rotate_test.go', 'watch_test.go'],
   # `//go:build integration` gates these: a merge would either spread the gate over a
   # whole group or silently downgrade it to an inert comment.
   'tmux_monitor_scenario_test.go': ['tmux_monitor_scenario_test.go'],
   'generation_tracker_test.go': ['generation_tracker_test.go'],
   'tui_integration_test.go': ['tui_integration_test.go'],
   'tui_timeout_test.go': ['tui_timeout_test.go'],
 },
 'event': {
   'registry_test.go': ['registry_test.go', 'registry_feedback_test.go',
     'registry_nonprojection_test.go', 'wf_facts_test.go'],
   'types_test.go': ['types_test.go', 'metadata_test.go', 'key_fuzz_test.go'],
 },
 'plugin': {
   'memory_plugin_test.go': ['memory_plugin_test.go', 'memory_plugin_lastkeys_test.go',
     'memory_dedup_test.go', 'causal_eviction_test.go'],
   'attribution_test.go': ['attribution_test.go'],
   'projection_sink_test.go': ['projection_sink_test.go'],
 },
 'tool/recall': {
   'recall_test.go': ['recall_test.go', 'hybrid_recall_test.go', 'items_cap_test.go',
     'truncation_hint_test.go', 'declaration_stable_test.go'],
   'memory_recall_test.go': ['memory_recall_test.go', 'memory_turn_test.go'],
 },
 'rl': {
   'http_api_closeout_test.go': ['http_api_closeout_test.go', 'http_api_wp4_test.go',
     'feedback_api_test.go', 'endpoint_redirect_test.go'],
   'auth_test.go': ['auth_test.go'],
   'swappable_model_test.go': ['swappable_model_iter_test.go', 'swappable_model_stream_test.go',
     'swappable_recycle_test.go'],
   'trajectory_recorder_test.go': ['trajectory_recorder_test.go', 'trajectory_recorder_iter_test.go',
     'trajectory_trace_test.go'],
   'mock_model_test.go': ['mock_model_test.go'],
 },
 'evolution': {
   'eval_test.go': ['eval_test.go', 'eval_w4_test.go', 'eval_bundle_join_test.go'],
   'judge_test.go': ['judge_test.go'],
   'gitrefine_test.go': ['gitrefine_test.go'],
   'switch_combo_test.go': ['switch_combo_test.go'],
 },
}


def has_build_constraint(text):
    head = text.lstrip()
    return head.startswith('//go:build') or head.startswith('// +build')


def _lines(rel):
    try:
        return len(LIB.read_source(rel).split('\n'))
    except SystemExit:
        return 0


def audit(domain):
    m = DOMAINS[domain]
    existing = {os.path.basename(f) for f in glob.glob(domain + '/*_test.go')}
    planned = {x for srcs in m.values() for x in srcs}
    hold = {os.path.basename(h) for h in HOLD if h.startswith(domain + '/')}
    problems = []
    targets = {os.path.basename(t) for t in m}
    miss = sorted(existing - planned - hold - targets)
    ghost = sorted(x for x in planned if x not in existing and x not in {os.path.basename(t) for t in m})
    if miss:
        problems.append('未归位: %s' % miss)
    if ghost:
        problems.append('幻影源: %s' % ghost)
    # a source claimed by two targets would be duplicated on merge
    seen = {}
    for tgt, srcs in m.items():
        for s in srcs:
            if s in seen and seen[s] != tgt:
                problems.append('源 %s 同时属于 %s 与 %s' % (s, seen[s], tgt))
            seen[s] = tgt
    report = []
    for tgt, srcs in m.items():
        n = sum(_lines(domain + '/' + s) for s in srcs)
        flag = '  ⚠ 超 1500 行需再分' if n > 1500 else ''
        report.append('    %-34s %2d 源 ≈%5d 行%s' % (tgt, len(srcs), n, flag))
    return problems, ' %d → %d ' % (len(existing), len(m) + len(hold)), report


def mixed_packages(domain):
    """Report groups whose sources do not share one package clause.

    Go gives `package foo` and `package foo_test` in the same directory separate
    scopes, so they can never be merged into one file. Detecting this during the
    audit avoids aborting halfway through a run with some targets already written.
    """
    out = []
    for tgt, srcs in DOMAINS[domain].items():
        pkgs = set()
        for s_ in srcs:
            try:
                text = LIB.read_source(domain + '/' + s_)
            except SystemExit:
                continue
            pkgs.add(LIB.split_imports(text)[0])
        if len(pkgs) > 1:
            out.append(('mixed-package', tgt, sorted(pkgs)))
    return out


def collisions(domain):
    out = []
    for tgt, srcs in DOMAINS[domain].items():
        seen = {}
        for s in srcs:
            try:
                text = LIB.read_source(domain + '/' + s)
            except SystemExit:
                out.append(('missing', tgt, s))
                continue
            if has_build_constraint(text) and len(srcs) > 1:
                out.append(('build-constraint', tgt, s))
            for mm in re.finditer(r'^func \(([^)]*)\) ([A-Za-z0-9_]+)\(', text, re.M):
                recv = re.sub(r'[*\[\]]', '', mm.group(1)).split()[-1] if mm.group(1).strip() else '?'
                key = recv + '.' + mm.group(2)
                if key in seen:
                    out.append(('dup-method', tgt, key, seen[key], s))
                seen[key] = s
            # Two sources in one group declaring the same Test/Benchmark name is a real
            # collision (Go rejects it), so test names must be checked too.
            for n in re.findall(r'^func (Test[A-Za-z0-9_]+|Benchmark[A-Za-z0-9_]+)\(', text, re.M):
                if n in seen:
                    out.append(('dup-test', tgt, n, seen[n], s))
                seen[n] = s
            for pat in (r'^func ([A-Za-z0-9_]+)\(', r'^type ([A-Za-z0-9_]+) ',
                        r'^var ([A-Za-z0-9_]+) ', r'^const ([A-Za-z0-9_]+) '):
                for n in re.findall(pat, text, re.M):
                    if n in ('_', 'init', 'TestMain'):
                        continue
                    if n in seen:
                        out.append(('dup-decl', tgt, n, seen[n], s))
                    seen[n] = s
    return out


def leftovers(domain):
    """Report group sources still on disk after a merge (a missed deletion would
    leave duplicate declarations that only the compiler notices)."""
    out = []
    for tgt, srcs in DOMAINS[domain].items():
        for s_ in srcs:
            if s_ == tgt:
                continue
            rel = os.path.join(domain, s_)
            if os.path.exists(rel):
                out.append(rel)
    return out


def run(domain, targets=None):
    m = DOMAINS[domain]
    hold = {os.path.basename(h) for h in HOLD if h.startswith(domain + '/')}
    deletes = []
    for tgt in (targets or m):
        if os.path.basename(tgt) in hold:
            raise SystemExit('%s is on the hold list' % tgt)
        full = os.path.join(domain, tgt)
        srcs = [os.path.join(domain, s) for s in m[tgt]]
        parsed, pkgs = [], set()
        for path in srcs:
            text = LIB.read_source(path)
            if has_build_constraint(text) and len(srcs) > 1:
                raise SystemExit('%s: %s has a build constraint' % (tgt, os.path.basename(path)))
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
        print('merged %-36s <- %2d files' % (tgt, len(srcs)))
    return deletes


if __name__ == '__main__':
    doms = list(DOMAINS)
    if sys.argv[1] == 'check':
        bad = 0
        for d in doms:
            probs, shape, rep = audit(d)
            col = collisions(d) + mixed_packages(d)
            print('%s %s' % (d, shape), 'OK' if not probs and not col else '')
            for p in probs:
                print('   ', p)
                bad = 1
            for c in col[:6]:
                print('    冲突', c)
                bad = 1
            for r in rep:
                print(r)
        sys.exit(bad)
    if sys.argv[1] == 'verify':
        bad = 0
        for d in DOMAINS:
            lo = leftovers(d)
            print('%-14s %s' % (d, 'sources cleared' if not lo else 'STILL PRESENT: %s' % lo))
            bad |= bool(lo)
        sys.exit(1 if bad else 0)
    dom = sys.argv[1]
    dels = run(dom, sys.argv[2:] or None)
    with io.open('/tmp/w2r_delete.txt', 'a', encoding='utf-8') as fh:
        fh.write('\n'.join(dels) + '\n')
    print('待删: %d' % len(dels))
