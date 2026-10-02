# -*- coding: utf-8 -*-
# Domain consolidation driver: one mapping partitions the whole test-file set
# (coverage is asserted, so nothing can be silently skipped), and groups are
# executed end-to-end (target written, sources reported for deletion) so the tree
# never carries duplicate declarations between groups.
import io, os, re, json, sys, glob
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

# Files the user is editing in parallel: never moved, renamed or deleted by a wave.
HOLD = {
    'agent/reliability_matrix_test.go', 'agent/deep_review_regressions_test.go',
    'agent/settle_accounting_barrier_test.go',
}

# target -> sources (sources include the target itself when it already exists)
M = {
 'agent/session_test.go': ['session_subagent_toolstop_test.go','external_context_isolation_test.go','d2_run_input_contract_test.go','d3_spawner_ownership_test.go','d4_correlation_test.go','d5_origin_provenance_test.go','d8_window_tail_test.go','d9_cancel_test.go','d10_concurrency_isolation_test.go','d11_e2e_concurrent_test.go','d12_host_equivalence_test.go','d13_pipeline_convergence_test.go','d14_multilevel_window_test.go','detach_test.go','d42_reentry_test.go','echo_grounding_e2e_test.go','model_entry_grounding_e2e_test.go','multimodal_survival_test.go','task_chain_e2e_test.go','task_settled_test.go'],
 'agent/settle_routing_test.go': ['d6_settle_routing_test.go','d7_settle_accounting_test.go','settle_feedback_test.go','spawn_lineage_write_test.go','reconcile_outstanding_test.go','receipt_credential_test.go','submit_durable_batch_test.go','submit_conflict_quarantine_test.go','persist_prepared_gate_test.go','inbox_receipt_test.go','nonprojection_unified_test.go','event_keys_contract_test.go','event_keys_hex_test.go'],
 'agent/exec_lease_test.go': ['exec_lease_retire_test.go','executor_publish_test.go','executor_publish_seam_test.go','executor_publish_bench_test.go','executor_isolation_test.go','executor_perf_boundedness_test.go','turn_binding_test.go','d6_lease_test.go','face_test.go','relaunch_effective_generation_test.go','org_cross_scenario_test.go'],
 'agent/agent_test.go': ['tagent_agent_test.go','tagent_agent_loop_test.go','agent_loop_test.go','agent_loop_edge_test.go','invariants_test.go','invariants_i2_test.go','counterexample_test.go','context_simulation_test.go','upstream_stop_capability_test.go'],
 # poc_test.go keeps its own file: `//go:build poc` gates it out of normal builds.
 'agent/poc_test.go': ['poc_test.go'],
 'agent/event_bus_test.go': ['event_bus_test.go','event_bus_spill_test.go','persist_bus_event_test.go','build_bus_fact_test.go','metadata_propagation_test.go','attribution_test.go','bundle_id_stamp_test.go','on_event_integration_test.go'],
 'agent/execution_gate_test.go': ['execution_gate_test.go','execution_gate_loop_test.go','completion_gate_test.go','completion_test.go','completion_protocol_test.go','arm_barrier_test.go','crash_finish_matrix_test.go','crash_input_commit_test.go'],
 'agent/tool_agent_test.go': ['tool_agent_test.go','tool_agent_extra_params_test.go','subagent_async_test.go','subagent_resume_test.go','subagent_ttl_test.go'],
 'agent/meditation_test.go': ['meditation_test.go','meditation_selffeed_test.go','meditation_durable_zombie_test.go','meditation_anchor_test.go','retention_owner_test.go','retention_e2e_test.go'],
 'agent/projection_rebuild_test.go': ['projection_rebuild_test.go','projection_fallback_test.go','projection_canonical_ref_test.go','w1_concurrent_projection_test.go','w1_projection_wiring_test.go'],
 'agent/context_manager_test.go': ['context_manager_recycle_test.go','org_hot_params_test.go','m3_hot_consumption_test.go','m34_subcall_hotthread_test.go','m3_race_regression_test.go'],
 'agent/event_loop_test.go': ['lifecycle_test.go','lifecycle_gate_test.go','recovery_notice_boundary_test.go'],
 'agent/trace_test.go': ['trace_test.go','trace_link_test.go','task_trace_test.go'],
 'agent/task_record_sink_test.go': ['task_record_sink_test.go','task_record_emit_test.go','task_board_wiring_test.go'],
 'agent/output_limit_tool_test.go': ['output_limit_tool_test.go','output_overflow_test.go'],
 'agent/test_helpers_test.go': ['test_helpers_test.go','mock_model_test.go'],
 'agent/restart_matrix_test.go': ['restart30_matrix_test.go','reliability_boundary_test.go'],
 'agent/meditation_digest_test.go': ['meditation_digest_test.go'],
 'agent/turn_result_test.go': ['turn_result_test.go'],
}

import consolidate_lib as LIB

MODULE_PREFIX = 'github.com/SpellingDragon/tagent'


ALIAS = {}


def coverage_check(domain):
    existing = {os.path.relpath(f, domain) for f in glob.glob(domain + '/*_test.go')}
    planned = {x for srcs in M.values() for x in srcs}
    hold = {os.path.basename(h) for h in HOLD if h.startswith(domain + '/')}
    missing = sorted(existing - planned - hold)
    extra = sorted(x for x in planned - existing if x not in {os.path.relpath(t, domain) for t in M})
    return missing, extra


def has_build_constraint(text):
    head = text.lstrip()
    return head.startswith('//go:build') or head.startswith('// +build')


def run_groups(domain, targets):
    deletes = []
    for tgt in targets:
        full = tgt if tgt.startswith(domain + '/') else os.path.join(domain, tgt)
        srcs = [os.path.join(domain, s) for s in M[tgt]]
        parsed, pkgs = [], set()
        for path in srcs:
            text = LIB.read_source(path)
            if has_build_constraint(text) and len(srcs) > 1:
                raise SystemExit('%s: %s carries a build constraint and must stay a '
                                 'standalone file (merging would apply its constraint to '
                                 'the whole group, or downgrade it to an inert comment)'
                                 % (tgt, os.path.basename(path)))
            pkg, imports, body = LIB.split_imports(text)
            pkgs.add(pkg)
            parsed.append((imports, body))
        assert len(pkgs) == 1, (tgt, pkgs)
        pkg = pkgs.pop()
        chosen = LIB.canonical_aliases([im for im, _ in parsed], MODULE_PREFIX)
        for im, _ in parsed:
            for path, a in im.items():
                old_a = a or LIB.base_name(path)
                new_a = chosen.get(path) or LIB.base_name(path)
                if old_a != new_a:
                    ALIAS[(tgt, old_a)] = new_a
        bodies = [LIB.unify(pkg, im, body, chosen, MODULE_PREFIX) for im, body in parsed]
        union = {}
        for im, _ in parsed:
            union.update(im)
        io.open(full, 'w', encoding='utf-8').write(LIB.render(pkg, chosen, union, bodies))
        for s_ in srcs:
            if s_ != full:
                deletes.append(s_)
        print('merged %-40s <- %2d files' % (tgt, len(srcs)))
    return deletes


if __name__ == '__main__':
    if len(sys.argv) > 1 and sys.argv[1] == 'check':
        missing, extra = coverage_check('agent')
        print('未归位的 agent 测文件:', missing)
        print('映射里不存在的文件:', extra)
        sys.exit(0 if not missing and not extra else 1)
    domain = 'agent'
    by_base = {k.split('/')[-1]: k for k in M}
    targets = [by_base[t.split('/')[-1]] for t in sys.argv[1:]]
    deletes = run_groups(domain, targets)
    io.open('/tmp/to_delete.txt', 'w').write('\n'.join(os.path.relpath(d, '.') for d in deletes) + '\n')
    print('待删源文件: %d' % len(deletes))
    for d in deletes:
        print(' ', os.path.relpath(d, '.'))
