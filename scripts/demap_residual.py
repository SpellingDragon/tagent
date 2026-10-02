# -*- coding: utf-8 -*-
"""Residual identifier de-numbering across the swept packages.

Only archive-decodable numbers are renamed; level/version/format vocabulary (L1/L2/L3,
V1/V2, MD5, 401, Int64, e2e, Hy3, sub2/sub3, 30Round, 80Percent, round3) is domain
vocabulary and stays. `r30Stack` is untouched because a held file still references it.
"""
import io, re, glob, json

R = {
    'd52PullModel': 'plainTextPullModel',
    'TestReconcileZombies_Channel2NilProbeOrphan': 'TestReconcileZombies_DeadSessionTrackerRetiresNilProbeOrphan',
    '"channel 2 must retire the aged nil-probe orphan (ok=%v)"':
        '"a wired dead-session tracker must retire the aged nil-probe orphan (ok=%v)"',
    'TestI1_NoSinkNoPanic': 'TestProjectionNoSinkNoPanic',
    'TestI1_PipelineStoreProjectsExactlyOnce': 'TestPipelineStoreProjectsExactlyOnce',
    'TestI1_SkippedEventsNotProjected': 'TestSkippedEventsNotProjected',
    'TestI1_ToolTurnProjectsAllSteps': 'TestToolTurnProjectsAllSteps',
    'wp4Loop': 'envelopeInjectingLoop',
    'wp4Post': 'postEnvelopeBody',
    'TestActionTool33_MonitorsArePerGeneration': 'TestMonitorsArePerGeneration',
}

SCOPE = ['*.go', 'agent/task/*.go', 'plugin/*.go', 'rl/*.go', 'tool/action/*.go']
HOLD = {'reliability_matrix_test.go', 'deep_review_regressions_test.go',
        'settle_accounting_barrier_test.go', 'compaction_safety_test.go'}


def main():
    hits = {k: 0 for k in R}
    # identifier renames must not silently drop the Test/Benchmark prefix
    for old, new in R.items():
        if old.startswith(('Test', 'Benchmark')) and not new.startswith(('Test', 'Benchmark')):
            raise SystemExit('refusing to rename %s to non-test %s' % (old, new))
    files = [f for pat in SCOPE for f in glob.glob(pat) if f.rsplit('/', 1)[-1] not in HOLD]
    for f in files:
        s = io.open(f, encoding='utf-8').read()
        o = s
        for old, new in sorted(R.items(), key=lambda kv: -len(kv[0])):
            if old.startswith('"'):
                if old in s:
                    s = s.replace(old, new)
                    hits[old] += 1
                continue
            s, n = re.subn(r'\b' + re.escape(old) + r'\b', new, s)
            hits[old] += n
        if s != o:
            io.open(f, 'w', encoding='utf-8').write(s)
    print('未命中(应为空):', [k for k, v in hits.items() if v == 0])
    with io.open('/tmp/resid_demap.tsv', 'w', encoding='utf-8') as fh:
        for old, new in sorted(R.items()):
            if not old.startswith('"'):
                fh.write('%s\t%s\n' % (old, new))


main()
