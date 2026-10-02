# -*- coding: utf-8 -*-
"""De-number agent-domain test and helper identifiers.

Judgement used: a digit is process residue only when the reader has to consult an
archived change to decode it. Generational vocabulary (g1/g2), type-shaped names
(Int64) and product names (hy3) stay.
"""
import io, re, glob

HOLD = {'agent/reliability_matrix_test.go', 'agent/deep_review_regressions_test.go',
        'agent/settle_accounting_barrier_test.go'}

R = {
 # generation-invariant suites
 'TestI1ConcurrentDelegationsEndToEnd': 'TestConcurrentDelegationsEndToEnd',
 'TestI1HostDirectFormEquivalence': 'TestHostDirectFormEquivalence',
 'TestI2_BeforeModelCompleteness_RealPipeline': 'TestBeforeModelCompletenessRealPipeline',
 'TestI3_RenderLegality_NativePairing': 'TestRenderLegalityNativePairing',
 'TestI4_AssemblyIgnoresFrameworkTail': 'TestAssemblyIgnoresFrameworkTail',
 # hot params / snapshot seeding
 'TestM3_OutputCapIsConstructionDerivedBoundary': 'TestOutputCapIsConstructionDerivedBoundary',
 'TestM3_ResidentBudgetHotAppliesToRealConsumer': 'TestResidentBudgetHotAppliesToRealConsumer',
 'TestM3_BackgroundHotApplyConcurrentWithCompress_NoRace': 'TestBackgroundHotApplyConcurrentWithCompressNoRace',
 'TestM34_InitialSnapshotSeededAtConstruction': 'TestHotParamSnapshotSeededAtConstruction',
 'TestM34_FreshSubCallSeededFromSnapshot': 'TestFreshSubCallSeededFromSnapshot',
 'TestM34_InFlightSubCallAppliesAtBoundary': 'TestInFlightSubCallAppliesAtBoundary',
 'm34FinalResp': 'scriptedFinalResp', 'm34ToolCallResp': 'scriptedToolCallResp', 'noopLeafM34': 'noopLeafTool',
 # lifecycle / lease
 'TestLifecycle41_BoundedReturnThenExactlyOneFinalExit': 'TestBoundedReturnThenExactlyOneFinalExit',
 'TestD53_CommitSeparateFromReclaim': 'TestCommitSeparateFromReclaim',
 'TestD53_HeldResourcesCountedNotForceClosedUnderChurn': 'TestHeldResourcesCountedNotForceClosedUnderChurn',
 # per-call projection isolation
 'TestW1_ConcurrentCallsIsolateTheirProjections': 'TestConcurrentCallsIsolateTheirProjections',
 'TestW1_AutoInjectFiresThroughTransparentWrapper': 'TestAutoInjectFiresThroughTransparentWrapper',
 'TestW1_ExplicitKeysTakePriority': 'TestExplicitKeysTakePriority',
 'TestW1_EmptyProjectionIsGraceful': 'TestEmptyProjectionIsGraceful',
 'TestW1_NormalToolsUnaffected': 'TestNormalToolsUnaffected',
 'TestW1_PiercesNestedDecorators': 'TestPiercesNestedDecorators',
 'TestW1_CallIsolation': 'TestCallIsolation',
 'w1Gate': 'enteredGate', 'w1FlowModel': 'recordingFlowModel', 'w1ChildAgent': 'subCallChildAgent',
 'w1InjectedKeys': 'injectedEventKeys', 'w1WaitEntered': 'waitGateEntered', 'w1WaitRuns': 'waitChildRuns',
 'w1Has': 'containsKey', 'w1Projection': 'callProjection', 'w1PublishedProjection': 'publishedCallProjection',
 # restart matrix
 'TestRestart30_DeterministicIndependentRestarts': 'TestDeterministicIndependentRestarts',
 'r30ChildRound': 'restartChildRound', 'r30Contents': 'restartLogContents',
 'r30Plan': 'restartPlan', 'r30Stack': 'restartStack',
 # spawner ownership / attribution
 'TestD3_SubagentSpawnerOwnership': 'TestSubagentSpawnerOwnership',
 'd3ProbeTool': 'spawnerProbeTool',
 'TestS2m_DelegationOriginCarriesInvocationID': 'TestDelegationOriginCarriesInvocationID',
 'TestS2m_ControlKeyNotModelVisible': 'TestControlKeyNotModelVisible',
 'TestS2m_SequentialDelegationsPerCallAttribution': 'TestSequentialDelegationsPerCallAttribution',
 # invocation-ring continuation / cancel
 'TestS3mB_WindowCrossingContinuation': 'TestWindowCrossingContinuation',
 'TestS3mB_RunDoesNotStompSharedSessionContext': 'TestRunDoesNotStompSharedSessionContext',
 'TestS3mB_ContainmentNonAsyncCallClosesAfterFirstAnswer': 'TestContainmentNonAsyncCallClosesAfterFirstAnswer',
 'TestS4_CallerCancelClosesChannelNotCallee': 'TestCallerCancelClosesChannelNotCallee',
 'TestS4_CalleeTimeoutViaCtx': 'TestCalleeTimeoutViaCtx',
 # settle routing / accounting
 'TestS3mC_RoutePublishesToBoundBus': 'TestSettleRoutePublishesToBoundBus',
 'TestS3mC_UnboundFallsBackToBus': 'TestSettleUnboundFallsBackToBus',
 'TestS3mC_AccountingSurvivesConvergence': 'TestSettleAccountingSurvivesConvergence',
 'TestS3mC2_MultiLevelWindowCrossing': 'TestMultiLevelWindowCrossing',
 'TestS3mA_DeliverTaskSettled_Decision': 'TestDeliverTaskSettledDecision',
 'TestS3mA_BackgroundSettle_RoutesToSink': 'TestBackgroundSettleRoutesToBoundBus',
 'TestS3mA_BackgroundSettle_FallsBackToBus': 'TestBackgroundSettleFallsBackToBus',
 # task-record-sink fixtures
 'rb2Partition': 'sinkPartition', 'rb2Key': 'sinkEventKey',
}


def main():
    hits = {k: 0 for k in R}
    files = sorted(set(glob.glob('agent/*.go')) - HOLD)
    for f in files:
        s = io.open(f, encoding='utf-8').read()
        orig = s
        for old, new in sorted(R.items(), key=lambda kv: -len(kv[0])):
            s, n = re.subn(r'\b' + re.escape(old) + r'\b', new, s)
            hits[old] += n
        if s != orig:
            io.open(f, 'w', encoding='utf-8').write(s)
    missed = [k for k, v in hits.items() if v == 0]
    print('未命中的映射项(应为空):', missed)
    with io.open('/tmp/agent_demap.tsv', 'w', encoding='utf-8') as fh:
        for old, new in sorted(R.items()):
            fh.write('%s\t%s\n' % (old, new))
    left = sorted({n for f in files for n in re.findall(r'^func (Test[A-Za-z0-9_]+)\(', io.open(f, encoding='utf-8').read(), re.M)
                   if re.search(r'(?i)(s[0-9][a-z]?m?[b-c]?|d[0-9]{1,2}|m[0-9]{1,2}|w[0-9]|i[1-4][a-z]?_|lifecycle[0-9]|restart[0-9]{2})', n)})
    print('疑似残留:', left)


main()
