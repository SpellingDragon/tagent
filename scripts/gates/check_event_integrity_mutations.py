#!/usr/bin/env python3
"""check_event_integrity_mutations.py — 04.4 故障变异门（fault-mutation gate）。

判据（04 design D5）：控制树先全绿；对每个**已登记**变异，在仓库外的隔离副本
树上应用一处精确文本替换，跑对应正式测试；只有进程非零**且**失败输出命中该变异
登记的具名业务断言 `expect_assertion` 才记 `killed`。编译失败、超时、零测试命中、
无该断言的 panic、测试照绿一律判“验证失败”，不得计为杀死。

变异清单随本工具冻结（`MUTATIONS`），不依赖 change 目录存活；`basis` 分三类：

* `confirmed_defect`：00.3 冻结件 `defects[].confirmed=true` 的缺陷（每个必须有且
  恰有一个启用的变异指向它；缺映射即门失败）。
* `reproduction`：00.3 之后由正式测试复现并修复的形态（G1/02.1），证据引用随清单
  冻结。
* `named_assertion`：非缺陷主张的“测试敏感度”故障注入；只有在既有正式测试的具名
  断言实际杀死它时才启用（本次运行验证），不得为凑数开启未证实形态。

一个变异是一组**补丁**（`replacement` 加可选的 `companion`）：一个缺陷落在几处编辑
或几个文件上时，整组一起应用、一起复原，锚文本逐处必须恰好命中一次。

门锚变异（`gate_path` 非空）不驱动 `go test`：在副本树内跑具名 Python 门
（`--root` 指向副本树），进程非零**且**输出命中登记的 `expect_assertion` 才记
`killed`，归因到门路径（记录的 `gate` 字段）而非 Go 测试名。控制是该门在未变异
副本上的绿判决；门照绿、崩溃、超时或失败文案不命中一律判“验证失败”。门锚变异
不得同时声明 Go 测试。Go 变异的判据与编译面检查不变。

登记项有两种状态：

* `enabled`：进入执行集，未杀死即门失败。
* `pending`：反例已登记但它的**锚尚未落地**（对应叶未实施，失败文案未冻结）。
  pending 项不进入执行集、不出现在 `mutations[]`（`mutations[]` 只装已跑且判杀死的
  记录），只在 `pending[]` 里如实列出；一旦它的锚在测试文件里出现，检查器判门失败
  并要求启用——不允许拿“还在 pending”掩盖已可执行的验收面。

未证实候选（`not_evidence_backed`）不计数为杀死；本工具在 `not_evidence_backed`
非空时判门失败，输出结构与 `check_runtime_evidence.py --bundle` 消费的变异证据
一致：`control_tree_green`、`mutations[].{id,status,expect_assertion,assertion_hit}`、
`not_evidence_backed`、`pending[]`。

报告同时携带 `contract_digest` 与 `module_fingerprints`：前者是 runner 消费的
schema v2 事件合同（默认 `golden/runtime/event_integrity_scenarios.json`）的
sha256，后者复用 runner 的 `module_fingerprint` 给受测（根）模块取 content_hash
与 revision。`--bundle` 据此把变异判决与同一次 runner 证据绑定到同一基线；来自
另一棵树的报告即使自身全绿也不得计数。

写树纪律（R26）：只在 `TMPDIR` 下的副本树内替换产品文件；仓库工作树零写入、
零 index 变更；原始日志只写 `--out` 指定的仓库外目录。

`--replay <dir>` 是离线回放：不驱动 `go test`，改读 `<dir>/control/<测试名>.jsonl`
与 `<dir>/mutations/<id>/run.jsonl` 的录制流（布局与 `--out` 产物一致），判决仍走
同一套判据函数。录制流不含进程状态，故回放读的退出码是流自己声明的包终局
（无 `Test` 的 `fail`／`build-fail` 即非零）。回放不是活的运行证据：它只写
`evidence.replay.json`，而 bundle 收集器只认 `evidence.json`，故回放报告在机制上
进不了 `--bundle`。它用于把判据本身钉在录制证据上，不用于证明产品通过。
"""
from __future__ import annotations

import argparse
import ast
import hashlib
import json
import re
import os
import platform
import shutil
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, Iterable, List, Optional, Sequence, Tuple

SCRIPTS = Path(__file__).resolve().parent
# The gate lives in scripts/gates/, so the repository root is two levels up.
DEFAULT_ROOT = SCRIPTS.parents[1]
PROFILE = "event-integrity"
SCHEMA_VERSION = 2
DEFAULT_TIMEOUT_SECONDS = 900
DEFAULT_FROZEN_CONTRACT = (
    "openspec/changes/archive/2026-10-25-event-consumption-integrity/changes/"
    "00-contract-evidence/evidence/00.3-frozen-contract.json"
)
# The schema v2 event contract is the file the runner digests into every
# evidence envelope and the bundle gate re-digests; the mutation report names
# the same digest so the two can be tied to one baseline.
DEFAULT_EVENT_CONTRACT = "golden/runtime/event_integrity_scenarios.json"
# examples/ holds two nested Go modules (1.3G); the root-module tests never
# build them, so a Go-only copy keeps only the module under test. A run carrying
# gate mutations includes them: a Python gate reads them as part of its corpus.
COPY_EXCLUDED_PREFIXES = ("examples/",)
COMPILE_MARKERS = (
    "build failed",
    "[build failed]",
    "cannot find package",
    "undeclared name",
    "undefined:",
    "syntax error",
)
KILLED = "killed"
NOT_KILLED = "not_killed"
# A patch that never entered the selector's compiled face leaves the run
# unmutated; its verdict is neither a kill nor a surviving mutation.
NOT_APPLIED = "not_applied"
ENABLED = "enabled"
PENDING = "pending"


# ---------------------------------------------------------------------------
# 冻结变异清单
# ---------------------------------------------------------------------------


@dataclass(frozen=True)
class Replacement:
    """One product-code replacement: exactly one occurrence of `old`."""

    file: str
    old: str
    new: str


@dataclass(frozen=True)
class Mutation:
    """One registered fault mutation beside the assertion that must kill it."""

    id: str
    basis: str
    evidence_kind: str  # confirmed_defect | reproduction | named_assertion
    evidence_ref: str
    description: str
    test_package: str = ""
    test_file: str = ""
    test_name: str = ""
    test_selector: str = ""
    # The literal text of the named business assertion that must appear on the
    # failing test: a pending entry may leave it empty until its anchor lands.
    expect_assertion: str = ""
    # The source site of that assertion, `<test file>#<func>`: the function whose
    # body states the literal inside a failing construct. The function is named
    # rather than a line, so moving that statement inside the body never stales the
    # registration. A kill is the failure record carrying the literal at a location
    # the body reports (or at a `t.Helper()` caller bubbling it, see
    # `assertion_report_sites`); a same-case mention from another assertion prints
    # its own location and is no hit.
    expect_assertion_site: str = ""
    # A fault spanning several edits declares its first edit here and the rest
    # in `companion`: the set is applied and restored as one patch, so a defect
    # that lives in two files is one mutation, never two half-kills.
    replacement: Optional[Replacement] = None
    companion: Tuple[Replacement, ...] = ()
    status: str = ENABLED
    # The subtest the registered anchor names when the parent test carries it; a
    # pending entry is released as soon as this subtest appears in its file.
    anchor_subtest: str = ""
    pending_reason: str = ""
    # A gate-anchor mutation is judged by a named Python gate instead of a Go
    # test: the gate runs on the patched copy tree, and a kill is a non-zero
    # exit whose output carries `expect_assertion`. The selftest is the gate's
    # own rejection proof, named so the registration states both faces of the
    # anchor. The Go test fields stay empty for such a mutation.
    gate_path: str = ""
    gate_selftest: str = ""

    @property
    def enabled(self) -> bool:
        return self.status == ENABLED

    def patch_set(self) -> Tuple[Replacement, ...]:
        if self.replacement is None:
            return ()
        return (self.replacement,) + self.companion


MUTATIONS: Tuple[Mutation, ...] = (
    Mutation(
        id="MUT-EVID-02-dual-material-input",
        basis="EVID-02-dual-material-input",
        evidence_kind="confirmed_defect",
        evidence_ref=(
            "00.3-frozen-contract.json defects[EVID-02-dual-material-input]; "
            "negative anchor TestEventIntegrityTaskContinuation/no_duplicate_input_for_same_notice"
        ),
        description=(
            "Restore the notice body as InvokeRequest.Input beside the notice fact, "
            "so one notice identity enters the follow-up range as two new input facts."
        ),
        replacement=Replacement(
            file="assembly/host.go",
            # Anchor migrated per the clearance ruling (G5/G6b follow-up turn
            # checking turn.Terminal): the call assigns the returned turn now.
            # Re-anchored again after the quiesce restructure moved the wake block
            # of `fireNoticeTurn` out of its enclosing branch (04.2: intake gate →
            # quiesce → stop consumers), so the statements stand one level in
            # instead of two. The injected defect is unchanged — the notice
            # material re-enters the follow-up range as a second input fact beside
            # its notice fact.
            old=(
                "\tkey, err := h.commitNoticeMaterial(ctx, addr, fact)\n"
                "\tif err != nil {\n"
                "\t\treturn err\n"
                "\t}\n"
                "\tconsumer := consumers.Ensure(addr)\n"
            ),
            new=(
                "\tkey, err := h.commitNoticeMaterial(ctx, addr, fact)\n"
                "\tif err != nil {\n"
                "\t\treturn err\n"
                "\t}\n"
                "\tif extra := strings.TrimSpace(fact.Message.Content); extra != \"\" {\n"
                "\t\t_, _ = h.run.commitOnly(ctx, addr, event.Event{\n"
                "\t\t\tType:         event.TypeExternalInput,\n"
                "\t\t\tTimestamp:    time.Now(),\n"
                "\t\t\tMessage:      model.Message{Role: model.RoleUser, Content: extra},\n"
                "\t\t\tOwner:        addr,\n"
                "\t\t\tSource:       addr,\n"
                "\t\t\tTarget:       addr,\n"
                "\t\t\tInvocationID: noticeInvocation(task.Notice{TaskID: noticeTaskIDOf(fact), Kind: noticeKindOf(fact), Event: fact.Key}),\n"
                "\t\t})\n"
                "\t}\n"
                "\tconsumer := consumers.Ensure(addr)\n"
            ),
        ),
        test_package="./tests/integration/host",
        test_file="tests/integration/host/event_integrity_test.go",
        test_name="TestEventIntegrityTaskContinuation",
        test_selector="^TestEventIntegrityTaskContinuation$",
        # The literal is the static part of the copy check of continuationSingleCopy,
        # the helper that states it beside the request it judges. The helper reports
        # the line of its caller, so the sites this run may print are its call sites:
        # the continuation subtest that first carries a duplicate speaks first.
        # runDuplicateNoticeMaterial states the literal too, but it belongs to
        # TestEventIntegrityReproduction, which this selector never runs.
        expect_assertion="user copies of",
        expect_assertion_site="tests/integration/host/event_integrity_test.go#continuationSingleCopy",
    ),
    Mutation(
        id="MUT-EVID-03-busy-notice-drop",
        basis="EVID-03-busy-notice-loss",
        evidence_kind="confirmed_defect",
        evidence_ref=(
            "00.3-frozen-contract.json defects[EVID-03-busy-notice-loss]; "
            "negative anchor TestEventIntegrityTaskContinuation/busy_notice_not_dropped"
        ),
        description=(
            "Restore the settle driver dropping a notice that arrives while a "
            "follow-up turn is in flight instead of retaining it. Under the G6b "
            "mailbox account the retention is the account plus the wake and the "
            "drain re-read, so the mutant swallows the observation's wake while "
            "the pump is driving and ends the drain after its one round: "
            "the busy notice is dropped instead of kept."
        ),
        replacement=Replacement(
            file="assembly/settle_turn.go",
            # Patch 1 — the observation of a notice arriving while the pump
            # drives (busy) wakes nothing: the busy notice is dropped instead of
            # kept on the account for a later round.
            # Anchor migrated 2026-11-22 (F2 closing gate, leaf 13.103). The old
            # anchor named the gate of `ObserveCommit` as the raw table read
            # `terminalSettleKinds[noticeKindOf(fact)]`; the settle face now asks
            # the one predicate `terminalDelivery(fact)`, which folds the same
            # table beside the family and the task test. The wake the patch
            # guards is the same statement at the same site, so the fault —
            # a busy observation wakes nothing — is unchanged. Patches 2–4 (the
            # driving flag and the one-round drain) still anchor byte for byte.
            old=(
                "\tif !terminalDelivery(fact) {\n"
                "\t\treturn nil\n"
                "\t}\n"
                "\td.Wake()\n"
                "\treturn nil\n"
                "}\n"
            ),
            new=(
                "\tif !terminalDelivery(fact) {\n"
                "\t\treturn nil\n"
                "\t}\n"
                "\td.mu.Lock()\n"
                "\tbusy := d.busy\n"
                "\td.mu.Unlock()\n"
                "\tif !busy {\n"
                "\t\td.Wake()\n"
                "\t}\n"
                "\treturn nil\n"
                "}\n"
            ),
        ),
        companion=(
            Replacement(
                file="assembly/settle_turn.go",
                # Patch 2 — the driving flag the busy swallow reads.
                old="\tmu      sync.Mutex\n\tstopped bool\n",
                new="\tmu      sync.Mutex\n\tstopped bool\n\tbusy    bool\n",
            ),
            Replacement(
                file="assembly/settle_turn.go",
                # Patch 3 — the pump is busy while it drives its round.
                old=(
                    "func (d *SettleDriver) drain() {\n"
                    "\tctx := d.stopCtx\n"
                    "\ttried := map[seatKey]bool{}\n"
                ),
                new=(
                    "func (d *SettleDriver) drain() {\n"
                    "\tctx := d.stopCtx\n"
                    "\td.mu.Lock()\n"
                    "\td.busy = true\n"
                    "\td.mu.Unlock()\n"
                    "\tdefer func() {\n"
                    "\t\td.mu.Lock()\n"
                    "\t\td.busy = false\n"
                    "\t\td.mu.Unlock()\n"
                    "\t}()\n"
                    "\ttried := map[seatKey]bool{}\n"
                ),
            ),
            Replacement(
                file="assembly/settle_turn.go",
                # Patch 4 — the drain ends after its one round instead of
                # re-reading the account, so nothing picks the swallowed notice
                # up later. (The first form of this patch reversed the exit
                # condition — `if worked { return }` — and spun forever on a
                # round that drove nothing; the unconditional return keeps the
                # one-round-then-out semantics of the drop without the spin.)
                old="\t\tif !worked {\n\t\t\treturn\n\t\t}\n",
                new="\t\t_ = worked\n\t\treturn\n",
            ),
        ),
        test_package="./tests/integration/host",
        test_file="tests/integration/host/event_integrity_test.go",
        test_name="TestEventIntegrityTaskContinuation",
        test_selector="^TestEventIntegrityTaskContinuation$",
        expect_assertion="no request carried the notice material",
        expect_assertion_site="tests/integration/host/event_integrity_test.go#continuationWaitForRequest",
    ),
    Mutation(
        id="MUT-G1-meditation-busy-drop",
        basis="G1-meditation-busy-drop",
        evidence_kind="reproduction",
        evidence_ref=(
            "02.1 reproduction confirmed and repaired in ORCHESTRATION.md ledger (W1首批); "
            "negative anchor TestEventIntegrityMeditationSchedule/busy_observation_holds_watermark"
        ),
        description=(
            "Restore the meditation driver returning early while a pass is in "
            "flight, dropping the observation instead of retaining it."
        ),
        replacement=Replacement(
            file="features/evolution/meditation/driver.go",
            old=(
                "\tif d.firing {\n"
                "\t\td.retained = true\n"
                "\t\td.mu.Unlock()\n"
                "\t\treturn nil\n"
                "\t}\n"
            ),
            new=(
                "\tif d.firing {\n"
                "\t\td.mu.Unlock()\n"
                "\t\treturn nil\n"
                "\t}\n"
            ),
        ),
        test_package="./tests/integration/evolution",
        test_file="tests/integration/evolution/event_integrity_test.go",
        test_name="TestEventIntegrityMeditationSchedule",
        test_selector="^TestEventIntegrityMeditationSchedule$",
        expect_assertion="the busy observation was dropped",
        expect_assertion_site="tests/integration/evolution/event_integrity_test.go#TestEventIntegrityMeditationSchedule",
    ),
    Mutation(
        id="MUT-CAND-01.2-early-confirm",
        basis="CAND-01.2-early-confirm",
        evidence_kind="named_assertion",
        evidence_ref=(
            "01.2 negative anchor TestEventIntegrityConsumptionRecovery/"
            "claim_does_not_confirm_early (existing named business assertion)"
        ),
        description=(
            "Turn a mailbox claim into a completion, so a claim confirms "
            "consumption before the consumer finished the input."
        ),
        replacement=Replacement(
            file="runtime/state/mailbox.go",
            old=(
                "\tif entry.Claim != ClaimPending {\n"
                "\t\treturn ErrClaimConflict\n"
                "\t}\n"
                "\tentry.Claim = ClaimClaimed\n"
            ),
            new=(
                "\tif entry.Claim != ClaimPending {\n"
                "\t\treturn ErrClaimConflict\n"
                "\t}\n"
                "\tentry.Claim = ClaimConsumed\n"
            ),
        ),
        test_package="./tests/integration/history",
        test_file="tests/integration/history/event_integrity_test.go",
        test_name="TestEventIntegrityConsumptionRecovery",
        test_selector="^TestEventIntegrityConsumptionRecovery$",
        expect_assertion="want exactly one claimed entry",
        expect_assertion_site="tests/integration/history/event_integrity_test.go#consumptionClaimedEntry",
    ),
    Mutation(
        id="MUT-CAND-01.1-conflicting-body",
        basis="CAND-01.1-conflicting-body",
        evidence_kind="named_assertion",
        evidence_ref=(
            "01.1 negative anchor TestEventIntegrityNoticeIdentity/"
            "conflicting_body_same_identity (existing named business assertion)"
        ),
        description=(
            "Accept a second body under an already recorded notice identity, "
            "silently keeping the first body instead of naming the conflict."
        ),
        replacement=Replacement(
            file="runtime/state/mailbox.go",
            old=(
                "\tif prior.notice != notice {\n"
                "\t\treturn 0, false, ErrNoticeConflict\n"
                "\t}\n"
            ),
            new=(
                "\tif prior.notice != notice {\n"
                "\t\treturn prior.seq, true, nil\n"
                "\t}\n"
            ),
        ),
        test_package="./tests/integration/execution",
        test_file="tests/integration/execution/event_integrity_test.go",
        test_name="TestEventIntegrityNoticeIdentity",
        test_selector="^TestEventIntegrityNoticeIdentity$",
        expect_assertion="same identity beside a different body",
        expect_assertion_site="tests/integration/execution/event_integrity_test.go#runConflictingNoticeBody",
    ),
    Mutation(
        id="MUT-CAND-02.2-wrong-target",
        basis="CAND-02.2-wrong-target",
        evidence_kind="named_assertion",
        evidence_ref=(
            "02.2 send 形态的负锚 TestSendCrossSessionGates/denies_out_of_whitelist "
            "(existing named business assertion: the deliver_to white list names no "
            "target outside it); 2026-11-01 D 方案重锚：旧锚 features/evolution/"
            "meditation/deliver.go 已删，等价变异落到 assembly/host_tools.go 的白名单门"
        ),
        description=(
            "Let a send call name a target outside the deliver_to white list, so the "
            "declared white list no longer refuses the argument at its own gate."
        ),
        replacement=Replacement(
            file="assembly/host_tools.go",
            old=(
                "\tif !hostSendNames(p.plan.Targets, string(addr.AgentID)) {\n"
                "\t\treturn deliveryRefused, fmt.Sprintf(\"the deliver_to white list of %q names no %q, and a body delivered outside it would reach a session the reflection never declared\",\n"
                "\t\t\tp.plan.Agent, addr.AgentID)\n"
                "\t}\n"
            ),
            new="",
        ),
        test_package="./tests/integration/execution",
        test_file="tests/integration/execution/send_cross_session_gates_test.go",
        test_name="TestSendCrossSessionGates",
        test_selector="^TestSendCrossSessionGates$",
        expect_assertion="want the white list refusal naming",
        expect_assertion_site="tests/integration/execution/send_cross_session_gates_test.go#TestSendCrossSessionGates",
    ),
    Mutation(
        id="MUT-EVID-04-position-boundary",
        basis="EVID-04-notice-visibility-loss",
        evidence_kind="confirmed_defect",
        evidence_ref=(
            "00.3-frozen-contract.json defects[EVID-04-notice-visibility-loss]; "
            "negative anchor TestEventIntegrityProjection/notice_visible_under_fold; "
            "boundary identity anchor TestEventIntegrityProjection/range_head_survives_fold_rewrite"
        ),
        description=(
            "Restore the boundary of the range in flight as the position it appended "
            "at: the age fold rewrites the sequence the commits recorded, so a stored "
            "position names other material or falls past the shortened sequence and is "
            "clamped there by AssembleBudgetPrefix, which moves the in-flight notice "
            "into the ended prefix and presents it as one folded summary line instead "
            "of its material."
        ),
        replacement=Replacement(
            file="assembly/request.go",
            old=(
                "\t// endedAt names the first entry of the range in flight: everything before\n"
                "\t// it is the ended material a request presents. The boundary is recorded by\n"
                "\t// identity, never by position, because the age fold rewrites the sequence\n"
                "\t// the commits record — a position read after a fold names other material,\n"
                "\t// or falls past the shortened sequence and silently moves the in-flight\n"
                "\t// notice into the budgeted prefix. An empty identity names a chain with no\n"
                "\t// range in flight, so every entry is ended material.\n"
                "\tendedAt event.EventID\n"
            ),
            new="\tended           int\n",
        ),
        companion=(
            Replacement(
                file="assembly/request.go",
                old=(
                    "// endedPrefix reports the position the ended material of the registered sequence\n"
                    "// ends at, resolved against the sequence as it stands now: the position of the\n"
                    "// entry the boundary names by identity — inclusive for the agent_output that\n"
                    "// closed a segment (its answer is ended material the next segment presents),\n"
                    "// exclusive for the input that opened the next range (it belongs to that range\n"
                    "// and the drive or loop of it carries it). A boundary naming no entry — a\n"
                    "// chain that admitted no range yet, or one whose opening entry a fold has\n"
                    "// already consumed — reports the end of the sequence, so every entry is ended\n"
                    "// material and none of the range in flight is presented twice.\n"
                    "func (h replayEntry) endedPrefix() int {\n"
                    "\tif h.projection == nil {\n"
                    "\t\treturn 0\n"
                    "\t}\n"
                    "\tif h.endedAt == \"\" {\n"
                    "\t\treturn h.projection.Len()\n"
                    "\t}\n"
                    "\tfor at, entry := range h.projection.Entries() {\n"
                    "\t\tif entry.Key == h.endedAt {\n"
                    "\t\t\tif entry.Type == event.TypeAgentOutput {\n"
                    "\t\t\t\treturn at + 1\n"
                    "\t\t\t}\n"
                    "\t\t\treturn at\n"
                    "\t\t}\n"
                    "\t}\n"
                    "\treturn h.projection.Len()\n"
                    "}\n"
                    "\n"
                ),
                new="",
            ),
            Replacement(
                file="assembly/request.go",
                # Anchor migrated per the clearance ruling (G4 full-address
                # index key): same registration site, the full addr key now.
                old=(
                    "\tr.replays[addr] = replayEntry{projection: projection, window: window}\n"
                ),
                new=(
                    "\tr.replays[addr] = replayEntry{projection: projection, "
                    "window: window, ended: projection.Len()}\n"
                ),
            ),
            Replacement(
                file="assembly/request.go",
                # Anchor migrated per the clearance ruling (the projected
                # segment carries the run handle now).
                old=(
                    "\treturn projectedSegment(ctx, a.run, a.sequence.projection, a.sequence.window, "
                    "a.sequence.endedPrefix(), a.boundary.ProjectionBudget)\n"
                ),
                new=(
                    "\treturn projectedSegment(ctx, a.run, a.sequence.projection, a.sequence.window, "
                    "a.sequence.ended, a.boundary.ProjectionBudget)\n"
                ),
            ),
            Replacement(
                file="assembly/facts.go",
                # Anchor re-taken after the U2 g(x) unification: the live
                # commit and the replay fold share one append-plus-trim step,
                # so the mutant records the position the entry lands at in the
                # sequence as it stood before that step.
                # Anchor re-taken again after 12.R2.2 (foldMapped only moves the
                # boundary for the chain's newest fact): the boundary the mutant
                # overwrites is unchanged, but the step it sits in now states a
                # landing beside the fold, so the position is recorded ahead of
                # both and the anchor names the whole block.
                old=(
                    "\tvar landed bool\n"
                    "\theld.projection, landed = foldIntoProjection(ctx, held.projection, entry, held.window)\n"
                    "\tif !landed {\n"
                    "\t\tr.replays[slot] = held\n"
                    "\t\treturn\n"
                    "\t}\n"
                    "\tif evt.Type == event.TypeAgentOutput {\n"
                    "\t\theld.endedAt = entry.Key\n"
                    "\t\theld.rangeOpen = false\n"
                    "\t}\n"
                    "\tif admitsRange(evt) && !held.rangeOpen {\n"
                    "\t\theld.endedAt = entry.Key\n"
                    "\t\theld.rangeOpen = true\n"
                    "\t}\n"
                ),
                new=(
                    "\tposition := held.projection.Len()\n"
                    "\tvar landed bool\n"
                    "\theld.projection, landed = foldIntoProjection(ctx, held.projection, entry, held.window)\n"
                    "\tif !landed {\n"
                    "\t\tr.replays[slot] = held\n"
                    "\t\treturn\n"
                    "\t}\n"
                    "\tif evt.Type == event.TypeAgentOutput {\n"
                    "\t\theld.ended = position\n"
                    "\t\theld.rangeOpen = false\n"
                    "\t}\n"
                    "\tif admitsRange(evt) && !held.rangeOpen {\n"
                    "\t\theld.ended = position\n"
                    "\t\theld.rangeOpen = true\n"
                    "\t}\n"
                ),
            ),
            Replacement(
                file="assembly/facts.go",
                old="\treturn held.endedPrefix(), true\n",
                new="\treturn held.ended, true\n",
            ),
            # The notice read resolves its fence from the same identity: the
            # mutant keeps the position too, so the read compiles and stays
            # wrong in the same way as the boundary it follows.
            Replacement(
                file="assembly/facts.go",
                old="\tboundary := held.endedPrefix()\n",
                new="\tboundary := held.ended\n",
            ),
            # Anchor added 2026-11-22 (04.402b). The fold-time registration
            # grew a third read of the boundary in assembly/facts.go —
            # chainAlreadyPresented, the commit asking where the identity stands
            # against the material the chain already presented — and it resolved
            # the boundary through endedPrefix like the two reads above. Left
            # unrelocated, the mutation renamed the field and deleted the method
            # while that read still named the method, so the package failed to
            # build and the mutant counted as a compile_failure instead of a
            # kill. Relocating this read restores the build and keeps the fault
            # whole: the stored position this read follows carries the same
            # stale value after a fold, so the already-presented judgment drifts
            # with the boundary it reads.
            Replacement(
                file="assembly/facts.go",
                old="\tprefix := held.endedPrefix()\n",
                new="\tprefix := held.ended\n",
            ),
            # The presentation read resolves its prefix from the same position:
            # the mutant keeps the position here too, so the read compiles and
            # stays wrong in the same way as the boundary it follows.
            Replacement(
                file="assembly/request.go",
                old="\tprefix := held.endedPrefix()\n",
                new="\tprefix := held.ended\n",
            ),
        ),
        test_package="./tests/integration/history",
        test_file="tests/integration/history/event_integrity_test.go",
        test_name="TestEventIntegrityProjection",
        test_selector="^TestEventIntegrityProjection$",
        expect_assertion="copies of the range-opening notice in its own request",
        expect_assertion_site="tests/integration/history/event_integrity_test.go#runRangeHeadSurvivesFoldRewrite",
    ),
    # MUT-EVID-05-restart-flat-restore retired: see RETIRED_DEFECTS (equivalent
    # mutant post-purification; the defect is structurally resolved).
    Mutation(
        id="MUT-CAND-01.8-storage-success",
        basis="storage_failure_reported_as_success",
        evidence_kind="named_assertion",
        evidence_ref=(
            "01.8 negative anchor TestEventIntegrityPersistence/"
            "storage_failure_not_reported_as_success (existing named business assertion); "
            "registered by 计划回写③ (task-graph 04.4 gate)"
        ),
        description=(
            "Report a segment the storage tier refused as a committed fact: every "
            "storage-tier refusal in the persist path answers as committed, so the "
            "turn keeps going beside a refused commit and the barrier failure is "
            "answered as success instead of reported by name."
        ),
        replacement=Replacement(
            file="assembly/facts.go",
            # The storage-tier refusal exits of the persist path answer as
            # committed. The first registry form swallowed the segment store error
            # alone; the live persist path splits one refusal over several exits,
            # so one swallow left the turn failing beside an unnamed error or hid
            # the mutant entirely.
            # Anchors re-taken 2026-11-22 (F2 closing gate, leaf 13.103). The old
            # `default: return err` exit belonged to `persistFact`, whose answer was
            # one error; H02b made the write step answer the store's own five-value
            # outcome, so the same exit now reads `return outcome, err` and the
            # mutant answers the tier's refusal as `ledger.FactCommitted, nil` —
            # the refused segment reported as a committed fact, answered as success
            # instead of reported by name. The old presence-read exit
            # (`stored, held, heldErr := …GetEventInPartition`) and the old
            # conflict-refusal exit (`if !held { return err }`) have no counterpart
            # in this function any more: the presence read moved out of the write
            # step into `chainRecord`, a pre-write read whose refusal still
            # propagates under the mutant, and the conflict refusal now carries the
            # store's named outcome beside its error. Both were the same refusal
            # this `default` exit carries, not a second one.
            old=(
                "\tdefault:\n"
                "\t\treturn outcome, err\n"
                "\t}\n"
                "}\n\n"
                "// chainRecord"
            ),
            new=(
                "\tdefault:\n"
                "\t\treturn ledger.FactCommitted, nil\n"
                "\t}\n"
                "}\n\n"
                "// chainRecord"
            ),
        ),
        companion=(
            Replacement(
                file="assembly/facts.go",
                # The conflict-branch read refusal answers as committed: the tier
                # refused the read behind its own conflict answer, and the mutant
                # reports the segment as committed instead of naming the read's
                # failure.
                old=(
                    "\t\tprior, held, getErr := tier.Segments.GetEventInPartition(ctx, evt.Key, partition)\n"
                    "\t\tif getErr != nil {\n"
                    "\t\t\treturn ledger.FactConflict, getErr\n"
                    "\t\t}\n"
                ),
                new=(
                    "\t\tprior, held, getErr := tier.Segments.GetEventInPartition(ctx, evt.Key, partition)\n"
                    "\t\tif getErr != nil {\n"
                    "\t\t\treturn ledger.FactCommitted, nil\n"
                    "\t\t}\n"
                ),
            ),
        ),
        test_package="./tests/integration/history",
        test_file="tests/integration/history/event_integrity_test.go",
        test_name="TestEventIntegrityPersistence",
        test_selector="^TestEventIntegrityPersistence$",
        expect_assertion="the turn beside a failing storage tier returned no failure",
        expect_assertion_site="tests/integration/history/event_integrity_test.go#runPersistenceStorageFailure",
    ),
    Mutation(
        id="MUT-CAND-01.2-handover-confirm",
        basis="confirm_before_handover",
        evidence_kind="named_assertion",
        evidence_ref=(
            "01.2 supplement negative anchor TestEventIntegrityConsumptionRecovery/"
            "handover_before_confirmation (existing named business assertion); "
            "registered by 计划回写③ (task-graph 04.4 gate)"
        ),
        description=(
            "Record the completed consumption before the settled result is handed back "
            "into the intake: a stop inside the handover then leaves the notification no "
            "longer owed, so the restart has no result to hand over again."
        ),
        replacement=Replacement(
            file="assembly/facts.go",
            # Patch 1 — the completed consumption is recorded before the settled
            # result is handed back into the intake, so a stop inside the handover
            # leaves the notification no longer owed and the restart has no result
            # to hand over again.
            # Anchor migrated 2026-11-22 (F2 closing gate, leaf 13.103). The old
            # anchor named the refusal branch releasing the claim
            # (`return r.retainNoticeOwed(…)`) and the consume as the tail; H05
            # keeps the claim with the process that already handed the result on,
            # so that branch now logs and returns nil. The two statements the fault
            # reorders — `r.loop.Backflow(…)` before `r.store.Consume(…)` — stand
            # unchanged in `sink`, which is what this mutation swaps.
            old=(
                "\tif refusal := r.loop.Backflow(frame.Identity.InvocationID, notice); "
                "refusal != nil {\n"
                "\t\tassemblyDebug(\"tagent: the notice of task %s stays owed under the claim of this process\", notice.TaskID)\n"
                "\t\treturn nil\n"
                "\t}\n"
                "\treturn r.store.Consume(ctx, frame.Address, frame.Identity.InstanceID, seq)\n"
            ),
            new=(
                "\tif err := r.store.Consume(ctx, frame.Address, frame.Identity.InstanceID, seq); "
                "err != nil {\n"
                "\t\treturn err\n"
                "\t}\n"
                "\tif refusal := r.loop.Backflow(frame.Identity.InvocationID, notice); "
                "refusal != nil {\n"
                "\t\tassemblyDebug(\"tagent: the notice of task %s stays owed under the claim of this process\", notice.TaskID)\n"
                "\t\treturn nil\n"
                "\t}\n"
                "\treturn nil\n"
            ),
        ),
        test_package="./tests/integration/history",
        test_file="tests/integration/history/event_integrity_test.go",
        test_name="TestEventIntegrityConsumptionRecovery",
        test_selector="^TestEventIntegrityConsumptionRecovery$",
        expect_assertion="not recoverable after the restart",
        expect_assertion_site="tests/integration/history/event_integrity_test.go#runHandoverBeforeConfirmation",
    ),
    Mutation(
        id="MUT-CAND-01.10-alive-process-complete",
        basis="alive_process_reported_complete",
        evidence_kind="named_assertion",
        evidence_ref=(
            "01.10a negative anchors TestEventIntegrityExecLifecycle/"
            "partial_output_is_not_completion and stable_is_not_success share "
            "eventIntegrityAssertNoCompletionVerdict; the mutated answer first fails "
            "returned_call_does_not_prove_exit (event_integrity_test.go:52) with "
            "'no query face may report success or completion'; "
            "registered by 计划回写④ (task-graph 04.4 gate)"
        ),
        description=(
            "Report a run whose process is still alive as complete: answer the settled "
            "output with a completion verdict while the admitted process still runs."
        ),
        replacement=Replacement(
            file="tool/exec/tool.go",
            old='\treceipt := fmt.Sprintf("executed %s: %s", key, output)\n',
            new=(
                '\treceipt := fmt.Sprintf("executed %s: completed successfully: %s", '
                'key, output)\n'
            ),
        ),
        test_package="./tool/exec",
        test_file="tool/exec/event_integrity_test.go",
        test_name="TestEventIntegrityExecLifecycle",
        test_selector="^TestEventIntegrityExecLifecycle$",
        expect_assertion="no query face may report success or completion",
        expect_assertion_site="tool/exec/event_integrity_test.go#eventIntegrityAssertNoCompletionVerdict",
        anchor_subtest="partial_output_is_not_completion",
    ),
    Mutation(
        id="MUT-FOLD-01-segment-age-window",
        basis="FOLD-CONTRACT-segment-age-window",
        evidence_kind="named_assertion",
        evidence_ref=(
            "golden/runtime/fold_contract.json segment_age; golden vector golden/runtime/fold_plan_golden.json; anchor TestFoldPlanGolden"
        ),
        description=(
            "Widen the newest age window by one segment, so a segment the ladder reads L1 is recorded as L0 material and its level moves."
        ),
        replacement=Replacement(
            file="memory/replay/age_segments.go",
            old="\tcase age < keepRecent:\n",
            new="\tcase age < keepRecent+1:\n",
        ),
        test_package="./memory/replay",
        test_file="memory/replay/age_plan_test.go",
        test_name="TestFoldPlanGolden",
        test_selector="^TestFoldPlanGolden$",
        expect_assertion="the fold plan golden drifted",
        expect_assertion_site="memory/replay/age_plan_test.go#TestFoldPlanGolden",
    ),
    Mutation(
        id="MUT-FOLD-02-mid-level-window",
        basis="FOLD-CONTRACT-mid-level-window",
        evidence_kind="named_assertion",
        evidence_ref=(
            "golden/runtime/fold_contract.json segment_age levels L1 and L2; anchor TestFoldPlanGolden"
        ),
        description=(
            "Widen the L1 window by one segment, so a segment the ladder reads L2 reads L1 instead."
        ),
        replacement=Replacement(
            file="memory/replay/age_segments.go",
            old="\tcase age < keepRecent*2:\n",
            new="\tcase age < keepRecent*2+1:\n",
        ),
        test_package="./memory/replay",
        test_file="memory/replay/age_plan_test.go",
        test_name="TestFoldPlanGolden",
        test_selector="^TestFoldPlanGolden$",
        expect_assertion="the fold plan golden drifted",
        expect_assertion_site="memory/replay/age_plan_test.go#TestFoldPlanGolden",
    ),
    Mutation(
        id="MUT-FOLD-03-inflight-fold",
        basis="FOLD-CONTRACT-inflight-never-folded",
        evidence_kind="named_assertion",
        evidence_ref=(
            "golden/runtime/fold_contract.json segment_age in_flight; anchor TestAgeFoldKeepsTheSegmentInProgressWhole"
        ),
        description=(
            "Fold the segment still in flight: drop the completion guard from the band rule, so the tail run no agent output closed reads as middle-band material and its process detail folds into a product."
        ),
        replacement=Replacement(
            file="memory/replay/age_segments.go",
            old="\tif !segment.Complete || segment.Index >= total-keepRecent {\n",
            new="\tif segment.Complete && segment.Index >= total-keepRecent {\n",
        ),
        test_package="./memory/replay",
        test_file="memory/replay/age_segments_test.go",
        test_name="TestAgeFoldKeepsTheSegmentInProgressWhole",
        test_selector="^TestAgeFoldKeepsTheSegmentInProgressWhole$",
        expect_assertion="want the segment in progress beside the whole new band",
        expect_assertion_site="memory/replay/age_segments_test.go#TestAgeFoldKeepsTheSegmentInProgressWhole",
    ),
    Mutation(
        id="MUT-FOLD-04-product-type",
        basis="FOLD-CONTRACT-product-type",
        evidence_kind="named_assertion",
        evidence_ref=(
            "golden/runtime/fold_contract.json products segment_fold.type; anchor TestFoldPlanGolden"
        ),
        description=(
            "Mint the middle-band product as a history summary entry, so the product the middle band folds into carries the wrong type."
        ),
        replacement=Replacement(
            file="memory/replay/age_segments.go",
            old="\t\tType:    ProjectionTypeSegmentFold,\n",
            new="\t\tType:    ProjectionTypeHistorySummary,\n",
        ),
        test_package="./memory/replay",
        test_file="memory/replay/age_plan_test.go",
        test_name="TestFoldPlanGolden",
        test_selector="^TestFoldPlanGolden$",
        expect_assertion="the fold plan golden drifted",
        expect_assertion_site="memory/replay/age_plan_test.go#TestFoldPlanGolden",
    ),
    Mutation(
        id="MUT-DOD-P0-concrete-mount",
        basis="DOD-P0-concrete-mount",
        evidence_kind="named_assertion",
        evidence_ref=(
            "golden/runtime/dod_predicates.json P0 (measure mountedBuiltinFeatures); "
            "scripts/gates/check_dod_coverage.py; anchor "
            "assembly/plugin_catalog_test.go#TestBuiltinCatalogMountsEveryFeature"
        ),
        description=(
            "Mount a builtin by a construction of the assembly root instead of the "
            "catalog: drop the final-round feature from the catalog beside its now "
            "unused import, so the boot serves a run-level set the catalog does not "
            "mount."
        ),
        replacement=Replacement(
            file="assembly/plugin_catalog.go",
            old=(
                "\t{\n"
                "\t\tname:  finalRoundPluginName,\n"
                "\t\tscope: runPluginScope,\n"
                "\t\trun: func(r *Run) ([]plugin.Instance, error) {\n"
                "\t\t\tr.finalRound = agent.NewFinalRoundPlanner(finalRoundChain{run: r}, r.finalRoundPolicyOf)\n"
                "\t\t\treturn []plugin.Instance{r.finalRound}, nil\n"
                "\t\t},\n"
                "\t},\n"
            ),
            new="",
        ),
        companion=(
            Replacement(
                file="assembly/plugin_catalog.go",
                old="\t\"github.com/SpellingDragon/tagent/runtime/agent\"\n",
                new="",
            ),
        ),
        test_package="./assembly",
        test_file="assembly/plugin_catalog_test.go",
        test_name="TestBuiltinCatalogMountsEveryFeature",
        test_selector="^TestBuiltinCatalogMountsEveryFeature$",
        expect_assertion="want the 7 builtin plugins",
        expect_assertion_site="assembly/plugin_catalog_test.go#TestBuiltinCatalogMountsEveryFeature",
    ),
    Mutation(
        id="MUT-DOD-P1-identity-overwrite",
        basis="DOD-P1-identity-overwrite",
        evidence_kind="named_assertion",
        evidence_ref=(
            "golden/runtime/dod_predicates.json P1 (measure committedIdentityFacts); "
            "scripts/gates/check_dod_coverage.py; anchor "
            "memory/ledger/segment_store_test.go#TestFactCommitAnswersNewExistingConflictUncertain"
        ),
        description=(
            "Answer other bytes under an identity the chain already holds as the "
            "duplicate it holds: drop the byte comparison that refuses the conflicting "
            "commit, so one identity names whichever bytes were written last."
        ),
        replacement=Replacement(
            file="memory/ledger/segment_store.go",
            old=(
                "\t\tif !bytes.Equal(existing, rec.Body) {\n"
                "\t\t\treturn nil, ErrContentConflict\n"
                "\t\t}\n"
            ),
            new=(
                "\t\tif !bytes.Equal(existing, rec.Body) {\n"
                "\t\t\treturn &segmentEventPlan{duplicate: true}, nil\n"
                "\t\t}\n"
            ),
        ),
        test_package="./memory/ledger",
        test_file="memory/ledger/segment_store_test.go",
        test_name="TestFactCommitAnswersNewExistingConflictUncertain",
        test_selector="^TestFactCommitAnswersNewExistingConflictUncertain$",
        expect_assertion="want the byte conflict the engine names",
        expect_assertion_site="memory/ledger/segment_store_test.go#TestFactCommitAnswersNewExistingConflictUncertain",
    ),
    Mutation(
        id="MUT-DOD-P2-reducer-bypass",
        basis="DOD-P2-reducer-bypass",
        evidence_kind="named_assertion",
        evidence_ref=(
            "golden/runtime/dod_predicates.json P2 (measure bootFoldedFacts); "
            "scripts/gates/check_dod_coverage.py; anchor "
            "assembly/checkpoint_test.go#TestSeedFactReusesTheReducer"
        ),
        description=(
            "Recover through a reader of the boot's own: seedFact lands the replayed "
            "fact in the map and never folds it into the mounted state slices, so the "
            "boot derives none of the state the live commit derives."
        ),
        replacement=Replacement(
            file="assembly/run.go",
            old=(
                "\tif err := states.Reduce(ctx, evt); err != nil {\n"
                "\t\treturn fmt.Errorf(\"tagent: fold the replayed fact %s into the state of its consumers: %w\", evt.Key, err)\n"
                "\t}\n"
                "\treturn nil\n"
            ),
            new="\treturn nil\n",
        ),
        test_package="./assembly",
        test_file="assembly/checkpoint_test.go",
        test_name="TestSeedFactReusesTheReducer",
        test_selector="^TestSeedFactReusesTheReducer$",
        expect_assertion="want the reducer the live commit drives",
        expect_assertion_site="assembly/checkpoint_test.go#TestSeedFactReusesTheReducer",
    ),
    Mutation(
        id="MUT-DOD-P3-slice-bypass",
        basis="DOD-P3-slice-bypass",
        evidence_kind="named_assertion",
        evidence_ref=(
            "golden/runtime/dod_predicates.json P3 (measure "
            "durablePrefixScansOfASeatedAccount); scripts/gates/check_dod_coverage.py; "
            "anchor runtime/state/durable_test.go#TestDurableLoadReadsTheTaskSlice"
        ),
        description=(
            "Seat a task the checkpoint states and read its chain anyway: add every "
            "seated task to the identities read by prefix, so a boot pays the durable "
            "prefixes of a task it already holds whole."
        ),
        replacement=Replacement(
            file="runtime/state/durable.go",
            old=(
                "\t\tif err := d.seatTaskLocked(entry); err != nil {\n"
                "\t\t\treturn err\n"
                "\t\t}\n"
            ),
            new=(
                "\t\tif err := d.seatTaskLocked(entry); err != nil {\n"
                "\t\t\treturn err\n"
                "\t\t}\n"
                "\t\tyoung[entry.TaskID] = struct{}{}\n"
            ),
        ),
        test_package="./runtime/state",
        test_file="runtime/state/durable_test.go",
        test_name="TestDurableLoadReadsTheTaskSlice",
        test_selector="^TestDurableLoadReadsTheTaskSlice$",
        expect_assertion="want no durable prefix read",
        expect_assertion_site="runtime/state/durable_test.go#TestDurableLoadReadsTheTaskSlice",
    ),
    Mutation(
        id="MUT-DOD-P4-window-unbounded",
        basis="DOD-P4-window-unbounded",
        evidence_kind="named_assertion",
        evidence_ref=(
            "golden/runtime/dod_predicates.json P4 (measure wallClock); "
            "scripts/gates/check_dod_coverage.py; anchor "
            "assembly/symptom_anchor_recovery_test.go"
            "#TestSymptomAnchorBootCostFlatAcrossGenerations"
        ),
        description=(
            "Rebuild from the whole index instead of the window past the cut: drop "
            "the bounded walk the newest checkpoint's cut drives, so every boot walks "
            "the history the chain holds and the entries it names grow with it."
        ),
        replacement=Replacement(
            file="assembly/run.go",
            old=(
                "\tcut := r.checkpointCut(checkpointSliceProjection)\n"
                "\tif len(cut) > 0 {\n"
                "\t\trefs, landed, past, bounded := r.walkIndexPastCut(ctx, tier, cut)\n"
                "\t\tif bounded && past == tier.Segments.EventCount()-coveredEntries(cut) {\n"
                "\t\t\treturn refs, landed, nil\n"
                "\t\t}\n"
                "\t}\n"
                "\treturn r.walkIndexWhole(ctx, tier)\n"
            ),
            new="\treturn r.walkIndexWhole(ctx, tier)\n",
        ),
        test_package="./assembly",
        test_file="assembly/symptom_anchor_recovery_test.go",
        test_name="TestSymptomAnchorBootCostFlatAcrossGenerations",
        test_selector="^TestSymptomAnchorBootCostFlatAcrossGenerations$",
        expect_assertion="the walk follows the history the chain holds instead of stopping at the cut its newest checkpoint states",
        expect_assertion_site="assembly/symptom_anchor_recovery_test.go#TestSymptomAnchorBootCostFlatAcrossGenerations",
    ),
    Mutation(
        id="MUT-DOD-P7-midband-summary-feed",
        basis="DOD-P7-midband-summary-feed",
        evidence_kind="named_assertion",
        evidence_ref=(
            "golden/runtime/dod_predicates.json P7 (measure "
            "eventKeyTicketsInThePresentedRequest); scripts/gates/check_dod_coverage.py; "
            "anchor tests/integration/history/symptom_anchor_midband_test.go"
            "#TestSymptomAnchorMidbandProcessRecalledByTicket"
        ),
        description=(
            "Fold the process-detail entry into its product line with no handle on its "
            "own event key: drop the ticket from the one-line read the fold writes into "
            "the product, so the request built after the fold carries the product's "
            "summary and no address that recalls the process text the fold consumed."
        ),
        replacement=Replacement(
            file="memory/replay/age_segments.go",
            old=(
                "\tticket := TicketOf(entry.Key, entry.Type)\n"
                "\tif ticket == \"\" {\n"
                "\t\treturn foldLineOf(entry)\n"
                "\t}\n"
                "\treturn provenancePrefixOf(entry) + ticket + \" \" + entry.Type + \": \" + entry.Summary\n"
            ),
            new="\treturn foldLineOf(entry)\n",
        ),
        test_package="./tests/integration/history",
        test_file="tests/integration/history/symptom_anchor_midband_test.go",
        test_name="TestSymptomAnchorMidbandProcessRecalledByTicket",
        test_selector="^TestSymptomAnchorMidbandProcessRecalledByTicket$",
        expect_assertion="the request carries no ticket",
        expect_assertion_site="tests/integration/history/symptom_anchor_midband_test.go#TestSymptomAnchorMidbandProcessRecalledByTicket",
    ),
    Mutation(
        id="MUT-DOD-P8-fixed-budget",
        basis="DOD-P8-fixed-budget",
        evidence_kind="named_assertion",
        evidence_ref=(
            "golden/runtime/dod_predicates.json P8 (measure "
            "summaryTextBytesUnderTheDeclaredBudget); scripts/gates/check_dod_coverage.py; "
            "anchor memory/replay/age_segments_test.go"
            "#TestHistorySummaryObeysThePlanBudget"
        ),
        description=(
            "Fold every history summary under the policy default instead of the plan's "
            "declared bound: the fold reads the hardcoded limit and ignores the budget "
            "the plan states."
        ),
        replacement=Replacement(
            file="memory/replay/age_plan.go",
            old=(
                "\tif plan.SummaryBudgetBytes <= 0 {\n"
                "\t\treturn DefaultAgeSummaryTextLimit\n"
                "\t}\n"
                "\treturn plan.SummaryBudgetBytes\n"
            ),
            new="\treturn DefaultAgeSummaryTextLimit\n",
        ),
        test_package="./memory/replay",
        test_file="memory/replay/age_segments_test.go",
        test_name="TestHistorySummaryObeysThePlanBudget",
        test_selector="^TestHistorySummaryObeysThePlanBudget$",
        expect_assertion="the bound comes from the plan",
        expect_assertion_site="memory/replay/age_segments_test.go#TestHistorySummaryObeysThePlanBudget",
    ),
    Mutation(
        id="MUT-DOD-P5-legacy-clause",
        basis="DOD-P5-legacy-clause",
        evidence_kind="named_assertion",
        evidence_ref=(
            "golden/runtime/dod_predicates.json P5 (measure legacyRequirementText); "
            "scripts/gates/check_dod_coverage.py; anchor "
            "scripts/gates/check_spec_hygiene.py#main"
        ),
        description=(
            "State an implementation detail in a main spec's requirement prose: "
            "append a sentence naming a .go file to the live requirement that retired "
            "the lifecycle scanner, so the retired mechanism returns as a legacy clause "
            "the hygiene rule refuses."
        ),
        replacement=Replacement(
            file="openspec/specs/storage-lifecycle-completion/spec.md",
            old="只拒销毁，不得以「先标墓碑、压实再删」的形态复活。",
            new=(
                "只拒销毁，不得以「先标墓碑、压实再删」的形态复活。"
                "TTL 扫描由 ttl_scanner.go 内的周期性全量扫描实现。"
            ),
        ),
        gate_path="scripts/gates/check_spec_hygiene.py",
        gate_selftest="scripts/gates/test_check_spec_hygiene.py",
        expect_assertion="R4 go-file in requirement '保留保护与回收协调'",
        expect_assertion_site="scripts/gates/check_spec_hygiene.py#main",
    ),
    Mutation(
        id="MUT-DOD-P6-dead-producer",
        basis="DOD-P6-dead-producer",
        evidence_kind="named_assertion",
        evidence_ref=(
            "golden/runtime/dod_predicates.json P6 (measure deadProducerCalls); "
            "scripts/gates/check_dod_coverage.py; anchor "
            "scripts/gates/check_dead_producers.py#main"
        ),
        description=(
            "Add a production function no consumer reaches: declare DeadProducerProbe "
            "in tool/spec beside the operation set, so the package carries a producer "
            "without a consumer."
        ),
        replacement=Replacement(
            file="tool/spec/spec.go",
            old='const allowedOps = "init, new, status, validate, archive, instructions, list"\n',
            new=(
                'const allowedOps = "init, new, status, validate, archive, instructions, list"\n'
                "\n"
                "// DeadProducerProbe is a probe no consumer reaches.\n"
                "func DeadProducerProbe() {}\n"
            ),
        ),
        gate_path="scripts/gates/check_dead_producers.py",
        gate_selftest="scripts/gates/test_check_dead_producers.py",
        expect_assertion=(
            "tool/spec/DeadProducerProbe has no consumer (declared in tool/spec/spec.go)"
        ),
        expect_assertion_site="scripts/gates/check_dead_producers.py#main",
    ),
)

# MUT-DOD-P4-cold-unbounded is retired rather than disabled: its one edit replaced a
# case over the index-cut struct the retirement pass deleted, and its anchor
# `cut.Offset < 0 || cut.Size > size` is absent from memory/ledger/journal.go
# (grep: no match), so the patch can never be applied or run. Its basis is a
# named_assertion, not a confirmed defect, so RETIRED_DEFECTS does not carry it; the
# registry has no disabled state to move it to. golden/runtime/dod_predicates.json P4
# binds MUT-DOD-P4-window-unbounded in its place: that counterexample drops the bounded
# index walk of assembly/run.go and is killed by the walk face of
# TestSymptomAnchorBootCostFlatAcrossGenerations.

# 五类候选形态（04 design D5）：每一类都必须由至少一条已启用变异承载；
# 未证实形态不得为凑数开启。忙时忽略一类含随动驱动与冥想驱动两个落点。
CANDIDATE_FORMS: Tuple[Dict[str, object], ...] = (
    {"form": "dual_material_body",
     "carried_by": ["MUT-EVID-02-dual-material-input"]},
    {"form": "busy_notice_ignore",
     "carried_by": ["MUT-EVID-03-busy-notice-drop", "MUT-G1-meditation-busy-drop"]},
    {"form": "identity_destruction",
     "carried_by": ["MUT-CAND-01.1-conflicting-body"]},
    {"form": "early_confirm",
     "carried_by": ["MUT-CAND-01.2-early-confirm"]},
    {"form": "wrong_routing",
     "carried_by": ["MUT-CAND-02.2-wrong-target"]},
)

# 由 00.3 之后的正式测试复现并修复的缺陷（G1），不冒充 00.3 defects[]。
REPRODUCED_DEFECTS: Dict[str, Dict[str, str]] = {
    "G1-meditation-busy-drop": {
        "leaf": "02.1",
        "evidence": "ORCHESTRATION.md ledger W1首批: 02.1 证实并修复 driver 忙时丢观察（retained+rearm）",
        "anchor": "TestEventIntegrityMeditationSchedule/busy_observation_holds_watermark",
    },
}
# Defects resolved structurally by the purification: the fold form on restart is
# re-derived from the committed chain, so RestoreProducts / NoteProduct are
# redundant and no mutation removing them can kill (equivalent mutants). Retired
# with reason instead of keeping a false gate; re-confirm if the chain-derived
# rebuild ever regresses.
RETIRED_DEFECTS = {
    "EVID-05-restart-refold-regression": "fold form chain-derived post-purification; restore/note redundant (2026-11-09)",
}



class GateError(RuntimeError):
    """A gate-level failure that stops the run before any verdict counts."""


# ---------------------------------------------------------------------------
# go test -json 判决（纯函数：自测用合成事件，不真跑 go）
# ---------------------------------------------------------------------------


@dataclass
class RunResult:
    """One `go test -json` execution, parsed into events plus raw text."""

    test_name: str
    exit_code: int
    events: List[dict]
    raw_text: str
    log_ref: str = ""
    command: Tuple[str, ...] = ()
    timed_out: bool = False


def parse_go_test_output(raw_stdout: str, raw_stderr: str, test_name: str,
                         exit_code: int, log_ref: str = "",
                         command: Sequence[str] = (),
                         timed_out: bool = False) -> RunResult:
    """Parse a `go test -json` stream; non-JSON lines stay as raw text."""
    events: List[dict] = []
    raw_lines: List[str] = []
    for line in raw_stdout.splitlines():
        stripped = line.strip()
        if not stripped:
            continue
        try:
            payload = json.loads(stripped)
        except ValueError:
            raw_lines.append(line)
            continue
        if isinstance(payload, dict) and "Action" in payload:
            events.append(payload)
        else:
            raw_lines.append(line)
    if raw_stderr.strip():
        raw_lines.append(raw_stderr)
    return RunResult(test_name=test_name, exit_code=exit_code, events=events,
                     raw_text="\n".join(raw_lines), log_ref=log_ref,
                     command=tuple(command), timed_out=timed_out)


def _ran_test(result: RunResult) -> bool:
    return any(event.get("Action") == "run" and event.get("Test") == result.test_name
               for event in result.events)


def _in_selector(result: RunResult, name: str) -> bool:
    """Whether a `go test -json` test name belongs to the declared selector."""
    return name == result.test_name or name.startswith(result.test_name + "/")


def _skipped_cases(result: RunResult) -> List[str]:
    """Every case of the selector the stream records as skipped."""
    return [event.get("Test", "") for event in result.events
            if event.get("Action") == "skip"
            and _in_selector(result, event.get("Test", ""))]


def _passed_selector(result: RunResult) -> bool:
    """Whether the declared selector itself reached a `pass` terminal."""
    return any(event.get("Action") == "pass" and event.get("Test") == result.test_name
               for event in result.events)


def _failing_tests(result: RunResult) -> List[str]:
    return [event.get("Test", "") for event in result.events
            if event.get("Action") == "fail" and event.get("Test")]


def _test_output(result: RunResult) -> str:
    """The output lines that belong to the selector or its failing children."""
    failing = set(_failing_tests(result))
    parts: List[str] = []
    for event in result.events:
        if event.get("Action") not in ("output", "build-output"):
            continue
        test = event.get("Test", "")
        if not test or test in failing or test == result.test_name \
                or test.startswith(result.test_name + "/"):
            parts.append(event.get("Output", ""))
    if result.raw_text:
        parts.append(result.raw_text)
    return "".join(parts)


def _failing_cases(result: RunResult) -> List[str]:
    """The failing cases of the selector that failed for themselves.

    `go test -json` puts the aggregate `fail` on a parent whose case failed, so a
    parent's own `fail` states that something below it failed, not that the parent
    asserted anything. A test that still has a failing descendant is therefore not
    a place a named assertion can be attributed to.
    """
    failing = [name for name in _failing_tests(result) if _in_selector(result, name)]
    return [name for name in failing
            if not any(other != name and other.startswith(name + "/")
                       for other in failing)]


def assertion_report_sites(test_text: str, site_line: int) -> frozenset:
    """Every source location a run may print when the registered assertion fires.

    `site_line` is the line of the anchored function's body that states the
    literal. `go test` prints the position of the first frame above the failure
    that is not a `t.Helper()`, so an assertion written inside a helper reports the
    *caller's* line: the sites a kill may print are that line plus the call sites of
    every helper that encloses it, resolved transitively because a helper may call a
    helper.
    """
    lines = test_text.splitlines()
    if not 1 <= site_line <= len(lines):
        return frozenset()
    sites = {site_line}
    pending = [site_line]
    seen_functions = set()
    while pending and len(seen_functions) < _REPORT_SITE_DEPTH_LIMIT:
        line = pending.pop()
        name, definition = _enclosing_function(lines, line - 1)
        if not name or name in seen_functions:
            continue
        seen_functions.add(name)
        if not _declares_helper(lines, definition):
            continue
        for call_line in _call_sites(lines, name, definition):
            caller, caller_definition = _enclosing_function(lines, call_line - 1)
            if caller and _declares_helper(lines, caller_definition):
                # A call inside another helper bubbles past that frame too.
                pending.append(call_line)
                continue
            sites.add(call_line)
    return frozenset(sites)


# A helper chain deeper than this is not a shape this suite writes; the bound
# keeps the transitive walk finite beside a recursive helper.
_REPORT_SITE_DEPTH_LIMIT = 16
_FUNCTION_HEAD = re.compile(r"^func\s")
_LOCATED_OUTPUT = re.compile(r"^[ \t]*(?P<file>[^\s:]+\.go):(?P<line>\d+):(?P<text>.*)$")
# The constructs that fail a test and print a location: only these can report a
# kill, so a registered site naming anything else is a stale anchor.
FAILING_CONSTRUCTS = ("t.Fatalf(", "t.Fatal(", "t.Errorf(", "t.Error(")


def _enclosing_function(lines: List[str], index: int) -> Tuple[str, int]:
    """The name and 0-based definition line of the function holding `index`."""
    for cursor in range(index, -1, -1):
        if not _FUNCTION_HEAD.match(lines[cursor]):
            continue
        match = re.match(r"^func\s+(?:\([^)]*\)\s*)?(?P<name>\w+)", lines[cursor])
        if match:
            return match.group("name"), cursor
    return "", -1


def _declares_helper(lines: List[str], definition: int) -> bool:
    """Whether the function defined at `definition` marks itself with `t.Helper()`."""
    if definition < 0:
        return False
    for cursor in range(definition + 1, len(lines)):
        if _FUNCTION_HEAD.match(lines[cursor]):
            return False
        if "t.Helper()" in lines[cursor]:
            return True
    return False


def _call_sites(lines: List[str], name: str, definition: int) -> List[int]:
    """The 1-based lines calling `name`, excluding its own definition."""
    call = re.compile(r"\b" + re.escape(name) + r"\s*\(")
    return [number for number, line in enumerate(lines, 1)
            if number != definition + 1 and call.search(line)]


def failure_records(result: RunResult) -> List[Dict[str, object]]:
    """Each failure report a failing case printed, keyed by its source location.

    `go test` prefixes a report with `<file>:<line>: ` and continues a multi-line
    message on the lines after it, which carry no location of their own. The
    record is therefore the located line plus the continuation lines up to the
    next located one: the position discriminates two assertions of one case,
    while the text keeps a message whose literal sits on a later line.
    """
    cases = set(_failing_cases(result))
    if not cases:
        return []
    records: List[Dict[str, object]] = []
    current: Optional[Dict[str, object]] = None
    for event in result.events:
        if event.get("Action") != "output" or event.get("Test", "") not in cases:
            current = None
            continue
        text = event.get("Output", "")
        match = _LOCATED_OUTPUT.match(text)
        if match:
            current = {"file": os.path.basename(match.group("file")),
                       "line": int(match.group("line")),
                       "test": event.get("Test", ""), "text": match.group("text")}
            records.append(current)
        elif current is not None:
            current["text"] = str(current["text"]) + text
    return records


def parse_assertion_site(site: str) -> Tuple[str, str]:
    """Split a registered `<test file>#<func>` site; `("", "")` when unparseable."""
    site_file, _, site_func = (site or "").partition("#")
    if not site_file or not site_func:
        return "", ""
    return site_file, site_func


# String and rune literals of one source line. Braces inside them open or close no
# block, so the body walk counts a line only after they are blanked.
_GO_LITERAL = re.compile(r'`[^`]*`|"(?:[^"\\]|\\.)*"|\'(?:[^\'\\]|\\.)*\'')


def _block_code(line: str) -> str:
    """The line as the block walk reads it: literals and trailing comment blanked."""
    return _GO_LITERAL.sub('""', line).split("//")[0]


def _function_span(lines: List[str], name: str) -> Tuple[int, int]:
    """The 0-based declaration and close lines of the top-level `func name(...)`.

    `(-1, -1)` when the file declares no such function. The declaration has to
    start at column 0: a closure inside a test is indented, so a function literal
    is never mistaken for the function the registry names.
    """
    head = re.compile(r"^func\s+(?:\([^)]*\)\s*)?" + re.escape(name) + r"\s*\(")
    for index, line in enumerate(lines):
        if not head.match(line):
            continue
        depth, opened = 0, False
        for cursor in range(index, len(lines)):
            code = _block_code(lines[cursor])
            opened = opened or "{" in code
            depth += code.count("{") - code.count("}")
            if opened and depth <= 0:
                return index, cursor
        return index, len(lines) - 1
    return -1, -1


def assertion_body_lines(lines: List[str], name: str, literal: str) -> List[int]:
    """The 1-based lines inside `name`'s body that state `literal`.

    The body, never the file: a neighbour test stating the same literal is no
    evidence that this function's failing statement carries it.
    """
    start, end = _function_span(lines, name)
    if start < 0:
        return []
    return [number for number in range(start + 1, end + 2)
            if literal in lines[number - 1]]


def assertion_sites_of(mutation: Mutation, root: Optional[Path] = None) -> frozenset:
    """The (file basename, line) locations a run may print for this assertion."""
    if mutation.gate_path:
        return gate_assertion_sites(mutation, root)
    site_file, site_func = parse_assertion_site(mutation.expect_assertion_site)
    if not site_file:
        return frozenset()
    source = Path(root or DEFAULT_ROOT) / site_file
    try:
        text = source.read_text(encoding="utf-8")
    except OSError:
        return frozenset()
    lines = text.splitlines()
    basename = os.path.basename(site_file)
    sites = set()
    for number in assertion_body_lines(lines, site_func, mutation.expect_assertion):
        if any(call in lines[number - 1] for call in FAILING_CONSTRUCTS):
            sites |= assertion_report_sites(text, number)
    return frozenset((basename, number) for number in sites)


def assertion_site_problem(mutation: Mutation, root: Path) -> str:
    """Why the registered assertion site cannot be the report of a kill.

    The site is a shape anchor — the test file and the function inside it — like a
    patch anchor: the function has to exist, and its body has to state the literal
    inside a construct that fails the test. A function the file does not declare, or
    a body that states no such literal, is stale; and a site the run could never
    print would let a true kill go uncounted. A gate mutation's site is judged by
    the gate's own source instead.
    """
    if mutation.gate_path:
        return gate_site_problem(mutation, root)
    site_file, site_func = parse_assertion_site(mutation.expect_assertion_site)
    if not site_file:
        return "an enabled mutation must register the source site of its assertion"
    if site_file != mutation.test_file:
        return "the assertion site {} is outside the test file {}".format(
            site_file, mutation.test_file)
    source = root / site_file
    if not source.is_file():
        return "the assertion site file is missing: {}".format(site_file)
    lines = source.read_text(encoding="utf-8").splitlines()
    if _function_span(lines, site_func)[0] < 0:
        return ("the assertion site {}#{} names no function the file declares"
                .format(site_file, site_func))
    inside = assertion_body_lines(lines, site_func, mutation.expect_assertion)
    if not inside:
        found = [number for number, line in enumerate(lines, 1)
                 if mutation.expect_assertion in line]
        return ("the assertion site {}#{} states no {!r} inside that function body; "
                "the literal is stated at line(s) {}"
                .format(site_file, site_func, mutation.expect_assertion,
                        ", ".join(str(number) for number in found) or "nowhere"))
    failing = [number for number in inside
               if any(call in lines[number - 1] for call in FAILING_CONSTRUCTS)]
    if not failing:
        return ("the assertion site {}#{} states {!r} at line(s) {} where no failing "
                "construct is asserted, so a run never reports a failure there"
                .format(site_file, site_func, mutation.expect_assertion,
                        ", ".join(str(number) for number in inside)))
    return ""


def gate_function(tree, name: str):
    """The named function of a parsed gate, or None."""
    for node in ast.walk(tree):
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)) and node.name == name:
            return node
    return None


def gate_states_literal(func, body_text: str, literal: str) -> bool:
    """Whether the function's source states the registered rejection text.

    A gate renders its report through format templates, so the literal a run
    prints need not stand verbatim in the source: it is stated when it appears
    verbatim, or when it carries the longest static fragment of a template the
    function formats. The fragment is the tie that survives interpolation; a
    fragment shorter than four characters names no report.
    """
    if literal in body_text:
        return True
    for node in ast.walk(func):
        if not isinstance(node, ast.Constant) or not isinstance(node.value, str):
            continue
        fragments = [part for part in re.split(r"\{[^{}]*\}", node.value) if part.strip()]
        if not fragments:
            continue
        longest = max(fragments, key=len)
        if len(longest.strip()) >= 4 and longest in literal:
            return True
    return False


def gate_text_states(text: str, literal: str) -> bool:
    """Whether a gate's source states a rejection text, verbatim or through a
    template one of its functions formats."""
    if literal in text:
        return True
    try:
        tree = ast.parse(text)
    except SyntaxError:
        return False
    lines = text.splitlines()
    for node in ast.walk(tree):
        if not isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
            continue
        body = "\n".join(lines[node.lineno - 1:node.end_lineno])
        if gate_states_literal(node, body, literal):
            return True
    return False


def gate_site_problem(mutation: Mutation, root: Path) -> str:
    """Why the registered gate site cannot be the report of a kill.

    The gate-anchor form names the gate file and the function whose body states
    the rejection text; the function has to exist and state the literal, so a
    registration the gate's own source no longer supports is stale.
    """
    site_file, site_func = parse_assertion_site(mutation.expect_assertion_site)
    if not site_file:
        return "an enabled gate mutation must register the gate site of its assertion"
    if site_file != mutation.gate_path:
        return "the assertion site {} is outside the anchor gate {}".format(
            site_file, mutation.gate_path)
    source = root / site_file
    if not source.is_file():
        return "the assertion site file is missing: {}".format(site_file)
    text = source.read_text(encoding="utf-8")
    try:
        tree = ast.parse(text)
    except SyntaxError as exc:
        return "the anchor gate {} does not parse: {}".format(site_file, exc)
    func = gate_function(tree, site_func)
    if func is None:
        return "the assertion site {}#{} names no function the gate declares".format(
            site_file, site_func)
    lines = text.splitlines()
    body = "\n".join(lines[func.lineno - 1:func.end_lineno])
    if not gate_states_literal(func, body, mutation.expect_assertion):
        return ("the assertion site {}#{} states no {!r} inside that function body"
                .format(site_file, site_func, mutation.expect_assertion))
    return ""


def gate_assertion_sites(mutation: Mutation, root: Optional[Path] = None) -> frozenset:
    """The gate declaration line the registered assertion site names."""
    site_file, site_func = parse_assertion_site(mutation.expect_assertion_site)
    if not site_file:
        return frozenset()
    source = Path(root or DEFAULT_ROOT) / site_file
    try:
        func = gate_function(ast.parse(source.read_text(encoding="utf-8")), site_func)
    except (OSError, SyntaxError):
        return frozenset()
    if func is None:
        return frozenset()
    return frozenset(((os.path.basename(site_file), func.lineno),))


def _assertion_hit(result: RunResult, mutation: Mutation,
                   sites: frozenset) -> bool:
    """The failure record at the assertion's own location carries the literal.

    `go test -json` attributes every line to the test that printed it, so a line
    printed by an aggregating parent, by a passing sibling, or by a test outside
    the selector is no evidence that this test's assertion failed. Nor is another
    assertion of the same failing case: its record prints its own location, and
    the registered location is what ties the report to this assertion. The
    registry separately refuses a literal a logging call in the test file could
    print (`emitter_sites`).

    Declared residual: two assertions written on ONE source line report the same
    location, so a literal of the second still reads as a hit. Closing that would
    need the column, which neither the registry nor `go test` states.
    """
    for record in failure_records(result):
        if (record["file"], record["line"]) not in sites:
            continue
        if mutation.expect_assertion in str(record["text"]):
            return True
    return False


def classify(result: RunResult, mutation: Mutation,
             sites: Optional[frozenset] = None) -> Dict[str, object]:
    """Classify one mutated run: only a named-assertion failure is a kill."""
    text = _test_output(result)
    built_failed = any(event.get("Action") == "build-fail" for event in result.events)
    if result.timed_out or "panic: test timed out" in text:
        return {"verdict": NOT_KILLED, "reason": "timeout", "assertion_hit": False}
    if built_failed or (not _ran_test(result) and result.exit_code != 0
                        and any(marker in text for marker in COMPILE_MARKERS)):
        return {"verdict": NOT_KILLED, "reason": "compile_failure", "assertion_hit": False}
    if not _ran_test(result):
        reason = "zero_match" if result.exit_code == 0 else "no_test_run"
        return {"verdict": NOT_KILLED, "reason": reason, "assertion_hit": False}
    if sites is None:
        sites = assertion_sites_of(mutation)
    hit = _assertion_hit(result, mutation, sites)
    if result.exit_code == 0:
        return {"verdict": NOT_KILLED, "reason": "test_green", "assertion_hit": hit}
    if hit:
        return {"verdict": KILLED, "reason": "named_assertion", "assertion_hit": True}
    if "panic:" in text:
        return {"verdict": NOT_KILLED, "reason": "panic_without_assertion",
                "assertion_hit": False}
    return {"verdict": NOT_KILLED, "reason": "assertion_not_matched", "assertion_hit": False}


def classify_gate(result: RunResult, mutation: Mutation) -> Dict[str, object]:
    """Classify one mutated gate run: a non-zero exit carrying the literal is a kill.

    A Python gate states its verdict as one process exit beside its own output,
    so the failure text is the whole output rather than a located test record.
    A gate that stays green, crashes, or reports a rejection the registration
    does not name is a verification failure, never a kill.
    """
    if result.timed_out:
        return {"verdict": NOT_KILLED, "reason": "timeout", "assertion_hit": False}
    hit = bool(mutation.expect_assertion) and mutation.expect_assertion in result.raw_text
    if result.exit_code == 0:
        return {"verdict": NOT_KILLED, "reason": "gate_green", "assertion_hit": hit}
    if hit:
        return {"verdict": KILLED, "reason": "named_assertion", "assertion_hit": True}
    return {"verdict": NOT_KILLED, "reason": "assertion_not_matched", "assertion_hit": False}


def classify_gate_control(result: RunResult) -> Dict[str, object]:
    """The unmutated control gate must end green on the same tree face."""
    if result.timed_out:
        return {"verdict": "red", "reason": "timeout", "exit_code": result.exit_code}
    if result.exit_code != 0:
        return {"verdict": "red", "reason": "gate_failed", "exit_code": result.exit_code}
    return {"verdict": "green", "reason": "green", "exit_code": result.exit_code}


# CONTROL_RACE_WAIVERS mirrors the RACE_WAIVER_SIGNATURES table of
# scripts/gates/race_check.sh (single source of upstream race rulings): a
# control whose only failures are races whose top frames match a registered
# waiver stays green for this gate exactly as the race gate already rules.
CONTROL_RACE_WAIVERS = (
    r'trpc-agent-go.*steer',
    r'trpc-agent-go.*invocation',
    r'trpc-agent-go/session.*inmemory',
    r'trpc-agent-go.*session/session\.go',
    r'trpc-mcp-go.*streamableHTTPClientTransport',
    r'trpc-mcp-go.*streamable_client\.go',
)


def _race_waived(result: RunResult) -> bool:
    text = result.raw_text or ""
    text += "".join(str(ev.get("Output") or "") for ev in result.events)
    if "WARNING: DATA RACE" not in text:
        return False
    frames = re.findall(
        r"WARNING: DATA RACE.*?==================", text, re.S)
    for frame in frames:
        if not any(re.search(w, frame) for w in CONTROL_RACE_WAIVERS):
            return False
    return bool(frames)


# RACE_FAILURE_MARKER is the testing package's own statement that a failure is a
# detected race: `testing.go` logs it on the racing test's own record and keeps it
# off the parents, which is the per-test attribution a waiver can rest on.
RACE_FAILURE_MARKER = "race detected during execution of test"


def _race_failed(result: RunResult, name: str) -> bool:
    """Whether the testing package logged this test as failed by a race."""
    return any(RACE_FAILURE_MARKER in str(event.get("Output") or "")
               for event in result.events
               if event.get("Action") in ("output", "build-output")
               and event.get("Test", "") == name)


def _failures_beside_the_race(result: RunResult) -> List[str]:
    """The failures a waived race does not explain, so a waived race cannot hide them.

    A test the race detector failed states that reason on its own record, and a
    parent that failed for such a child states nothing of its own because the
    testing package keeps the race off the parents: those are explained. A case
    that failed for an assertion carries no such marker, and a race elsewhere in
    the same run never explains it.
    """
    failures = _failing_tests(result)
    raced = {name for name in failures if _race_failed(result, name)}
    return [name for name in failures
            if name not in raced
            and not any(other != name and other.startswith(name + "/")
                        for other in failures)]


def control_record(mutation: Mutation, result: RunResult,
                   verdict: Dict[str, object]) -> Dict[str, object]:
    """One control's evidence: the selector it drove and what the rule ruled."""
    return {
        "test": {
            "module": ".",
            "package": mutation.test_package,
            "file": mutation.test_file,
            "name": mutation.test_name,
            "selector": mutation.test_selector,
        },
        "gate": gate_anchor_record(mutation),
        "verdict": verdict["verdict"],
        "reason": verdict["reason"],
        "exit_code": result.exit_code,
        "timed_out": result.timed_out,
        "skipped": list(verdict.get("skipped", [])),
        "command": list(result.command),
        "log_ref": result.log_ref,
    }


def classify_control(result: RunResult) -> Dict[str, object]:
    """The unmutated control must run the selector and end green.

    A green exit code is not enough: the selector has to reach a `pass` terminal
    with no skipped case, because a control tree of start-only, skipped or
    zero-hit cases proves nothing about the run the mutations are compared to and
    would let every later verdict count on no execution at all.

    A race the race gate already waives (registered upstream signatures whose
    frames are the whole race report) stays green here: the two gates share one
    ruling table, so a control never flips on a waived upstream race. That ruling
    rests on an actual race report, which only a real run emits, so it is read
    before the pass-terminal rule instead of being overridden by it. The waiver
    covers the failures the race explains and nothing else: an ordinary assertion
    failure beside the same report leaves the control red, because a control that
    really asserted is not the execution the mutations are compared to."""
    if result.timed_out:
        return {"verdict": "red", "reason": "timeout", "exit_code": result.exit_code}
    if not _ran_test(result):
        return {"verdict": "red", "reason": "zero_match", "exit_code": result.exit_code}
    skipped = _skipped_cases(result)
    if skipped:
        return {"verdict": "red", "reason": "skipped", "exit_code": result.exit_code,
                "skipped": skipped}
    failures = _failing_tests(result)
    if (result.exit_code != 0 or failures) and _race_waived(result) \
            and not _failures_beside_the_race(result):
        return {"verdict": "green", "reason": "green_race_waived", "exit_code": result.exit_code}
    if result.exit_code != 0 or failures:
        return {"verdict": "red", "reason": "test_failed", "exit_code": result.exit_code,
                "failing": failures}
    if not _passed_selector(result):
        return {"verdict": "red", "reason": "no_pass_terminal",
                "exit_code": result.exit_code}
    return {"verdict": "green", "reason": "green", "exit_code": result.exit_code}


def evidence_status_for(basis: str, confirmed_ids: Iterable[str],
                        reproduced_ids: Iterable[str],
                        named_assertion_ids: Iterable[str]) -> str:
    if basis in set(confirmed_ids):
        return "confirmed_defect"
    if basis in set(reproduced_ids):
        return "reproduced"
    if basis in set(named_assertion_ids):
        return "named_assertion"
    return "not_evidence_backed"


def gate_anchor_record(mutation: Mutation) -> Optional[Dict[str, str]]:
    """The gate a verdict is attributed to; None for a Go-test mutation."""
    if not mutation.gate_path:
        return None
    return {"path": mutation.gate_path, "selftest": mutation.gate_selftest}


def judge_mutation(mutation: Mutation, result: RunResult,
                   evidence_status: str,
                   source_root: Optional[Path] = None) -> Dict[str, object]:
    """Build the per-mutation record; unbacked candidates never count as kills."""
    sites = assertion_sites_of(mutation, source_root)
    outcome = (classify_gate(result, mutation) if mutation.gate_path
               else classify(result, mutation, sites))
    # A test-sensitivity mutation is evidenced by the kill itself: if the named
    # assertion does not kill it, the mutation stays unbacked instead of being
    # reported as if the suite already covered the form.
    if evidence_status == "named_assertion" and outcome["verdict"] != KILLED:
        evidence_status = "not_evidence_backed"
    counted = outcome["verdict"] == KILLED and evidence_status != "not_evidence_backed"
    targets = [replacement.file for replacement in mutation.patch_set()]
    return {
        "id": mutation.id,
        "basis": mutation.basis,
        "evidence_kind": mutation.evidence_kind,
        "evidence_status": evidence_status,
        "evidence_ref": mutation.evidence_ref,
        "description": mutation.description,
        "target": targets[0] if targets else "",
        "targets": targets,
        "test": {
            "module": ".",
            "package": mutation.test_package,
            "file": mutation.test_file,
            "name": mutation.test_name,
            "selector": mutation.test_selector,
        },
        "gate": gate_anchor_record(mutation),
        "expect_assertion": mutation.expect_assertion,
        "assertion_site": mutation.expect_assertion_site,
        "assertion_locations": [
            "{}:{}".format(name, line) for name, line in sorted(sites)],
        "status": outcome["verdict"],
        "verdict": outcome["verdict"],
        "reason": outcome["reason"],
        "assertion_hit": outcome["assertion_hit"],
        "counted_killed": counted,
        "exit_code": result.exit_code,
        "timed_out": result.timed_out,
        "command": list(result.command),
        "log_ref": result.log_ref,
        "patch_digest": patch_digest(mutation),
    }


def evaluate_gate(records: Sequence[Dict[str, object]],
                  controls: Sequence[Dict[str, object]],
                  confirmed_ids: Sequence[str],
                  require_confirmed_mapping: bool = True) -> Dict[str, object]:
    """Aggregate verdicts into the gate decision the bundle consumes."""
    failures: List[str] = []
    if not controls:
        failures.append("no control run was recorded")
    for control in controls:
        if control.get("verdict") != "green":
            source = ((control.get("test") or {}).get("selector")
                      or (control.get("gate") or {}).get("path"))
            failures.append(
                "control {} is {} ({})".format(
                    source, control.get("verdict"), control.get("reason")))
    if not records:
        failures.append("no mutation was run")
    for record in records:
        if record.get("evidence_status") == "not_evidence_backed":
            failures.append(
                "mutation {} is not evidence-backed ({})".format(
                    record.get("id"), record.get("basis")))
        if record.get("verdict") != KILLED:
            detail = record.get("patch_face")
            failures.append(
                "mutation {} status {!r} is not 'killed' ({}{})".format(
                    record.get("id"), record.get("verdict"), record.get("reason"),
                    "" if not detail else ": " + str(detail)))
    for defect_id in (confirmed_ids if require_confirmed_mapping else ()):
        hits = [record for record in records if record.get("basis") == defect_id]
        if len(hits) != 1:
            failures.append(
                "confirmed defect {} must map to exactly one mutation, found {}".format(
                    defect_id, len(hits)))
    killed = [record.get("id") for record in records if record.get("counted_killed")]
    not_backed = [record.get("id") for record in records
                  if record.get("evidence_status") == "not_evidence_backed"]
    return {
        "ok": not failures,
        "failures": failures,
        "killed": killed,
        "not_evidence_backed": not_backed,
    }


# ---------------------------------------------------------------------------
# 冻结清单校验
# ---------------------------------------------------------------------------


def load_json(path: Path) -> Dict[str, object]:
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        raise GateError("cannot read {}: {}".format(path, exc))
    if not isinstance(payload, dict):
        raise GateError("{} must hold a JSON object".format(path))
    return payload


def confirmed_defect_ids(root: Path, contract_path: Optional[str]) -> List[str]:
    relative = contract_path or DEFAULT_FROZEN_CONTRACT
    path = Path(relative)
    if not path.is_absolute():
        path = root / relative
    if not path.is_file():
        raise GateError("frozen contract is missing: {}".format(path))
    payload = load_json(path)
    defects = payload.get("defects")
    if not isinstance(defects, list):
        raise GateError("{}: defects must be a list".format(path))
    ids: List[str] = []
    for defect in defects:
        if not isinstance(defect, dict):
            raise GateError("{}: every defect must be an object".format(path))
        if defect.get("confirmed") is True:
            defect_id = defect.get("id")
            if isinstance(defect_id, str) and defect_id in RETIRED_DEFECTS:
                continue
            if not isinstance(defect_id, str) or not defect_id:
                raise GateError("{}: a confirmed defect carries no id".format(path))
            ids.append(defect_id)
    if not ids:
        raise GateError("{}: no confirmed defect is frozen".format(path))
    return sorted(ids)


def anchor_landed(root: Path, mutation: Mutation) -> bool:
    """Report whether the anchor a pending entry waits on has been implemented."""
    path = root / mutation.test_file
    if not path.is_file():
        return False
    text = path.read_text(encoding="utf-8")
    if "func {}(".format(mutation.test_name) in text:
        return True
    leaf = mutation.anchor_subtest
    return bool(leaf) and "t.Run(\"{}\")".format(leaf) in text


# Constructs that print text without failing the test. A recorded kill is judged
# by the literal appearing on a failing case, and a line printed by one of these
# has no failure behind it, so a registered literal reachable from one of them
# would let a mention pass as a kill.
NON_FAILING_EMITTERS = (
    "t.Log(", "t.Logf(", "t.Logln(",
    "fmt.Print(", "fmt.Printf(", "fmt.Println(", "fmt.Fprint",
    "log.Print", "log.Printf", "log.Println", "println(", "os.Stdout", "os.Stderr",
)


def emitter_sites(text: str, literal: str) -> List[int]:
    """The 1-based lines where a logging call could print the registered literal."""
    return [number for number, line in enumerate(text.splitlines(), 1)
            if literal in line and any(token in line for token in NON_FAILING_EMITTERS)]


def gate_registration_problems(root: Path, mutation: Mutation) -> List[str]:
    """The static anchors of one gate mutation: the gate and its selftest."""
    problems: List[str] = []
    if (mutation.test_package or mutation.test_file or mutation.test_name
            or mutation.test_selector):
        problems.append(
            "a gate mutation declares a Go test beside its gate, so it states "
            "two measurements")
    if not mutation.gate_path.endswith(".py"):
        problems.append("the anchor gate {} is not a Python gate".format(mutation.gate_path))
    if not (root / mutation.gate_path).is_file():
        problems.append("the anchor gate is missing: {}".format(mutation.gate_path))
    if not mutation.gate_selftest:
        problems.append("the anchor gate names no selftest")
    elif not (root / mutation.gate_selftest).is_file():
        problems.append(
            "the anchor gate's selftest is missing: {}".format(mutation.gate_selftest))
    return problems


def validate_registry(root: Path, mutations: Sequence[Mutation]) -> List[str]:
    """Static anchors: no go run, but every patch and assertion must exist once."""
    problems: List[str] = []
    enabled = [mutation for mutation in mutations if mutation.enabled]
    ids = [mutation.id for mutation in mutations]
    if len(ids) != len(set(ids)):
        problems.append("mutation ids are not unique")
    forms = [entry["form"] for entry in CANDIDATE_FORMS]
    if len(forms) != 5 or len(set(forms)) != 5:
        problems.append("exactly the five candidate forms must be frozen")
    by_id = {mutation.id: mutation for mutation in mutations}
    for entry in CANDIDATE_FORMS:
        for carrying in entry["carried_by"]:
            mutation = by_id.get(carrying)
            if mutation is None:
                problems.append(
                    "candidate form {} points at an unknown mutation {}".format(
                        entry["form"], carrying))
            elif not mutation.enabled:
                problems.append(
                    "candidate form {} is carried by the pending mutation {}".format(
                        entry["form"], carrying))
    for mutation in mutations:
        patches = mutation.patch_set()
        if not mutation.enabled:
            if patches:
                problems.append(
                    "{}: a pending mutation must not declare a patch".format(mutation.id))
            if anchor_landed(root, mutation):
                problems.append(
                    "{}: the anchor {} has landed; enable the mutation instead of "
                    "leaving it pending".format(
                        mutation.id, mutation.test_name))
            continue
        if not patches:
            problems.append("{}: an enabled mutation declares no patch".format(mutation.id))
        if not mutation.expect_assertion:
            problems.append(
                "{}: an enabled mutation must name the assertion that kills it".format(
                    mutation.id))
        if mutation.gate_path:
            for problem in gate_registration_problems(root, mutation):
                problems.append("{}: {}".format(mutation.id, problem))
        site_problem = assertion_site_problem(mutation, root)
        if site_problem:
            problems.append("{}: {}".format(mutation.id, site_problem))
        for replacement in patches:
            if replacement.file.endswith("_test.go"):
                problems.append("{} targets a test file".format(mutation.id))
            source = root / replacement.file
            if not source.is_file():
                problems.append("{}: target is missing: {}".format(
                    mutation.id, replacement.file))
            elif source.read_text(encoding="utf-8").count(replacement.old) != 1:
                problems.append(
                    "{}: stale mutation, the patch anchor occurs {} times in {}".format(
                        mutation.id,
                        source.read_text(encoding="utf-8").count(replacement.old),
                        replacement.file))
        if mutation.gate_path:
            continue
        target = root / mutation.test_file
        if not target.is_file():
            problems.append("{}: test file is missing: {}".format(
                mutation.id, mutation.test_file))
        else:
            test_text = target.read_text(encoding="utf-8")
            if mutation.expect_assertion not in test_text:
                problems.append(
                    "{}: expect_assertion is absent from {}".format(
                        mutation.id, mutation.test_file))
            printed = emitter_sites(test_text, mutation.expect_assertion)
            if printed:
                problems.append(
                    "{}: the registered literal is emittable by a logging call in {} "
                    "at line(s) {}; a mention is not a failed assertion".format(
                        mutation.id, mutation.test_file,
                        ", ".join(str(line) for line in printed)))
        if "^{}$".format(mutation.test_name) != mutation.test_selector:
            problems.append(
                "{}: test_selector must anchor the parent test name".format(mutation.id))
    if not enabled:
        problems.append("no mutation is enabled")
    return problems


def patch_digest(mutation: Mutation) -> str:
    digest = hashlib.sha256()
    for replacement in mutation.patch_set():
        for part in (replacement.file, replacement.old, replacement.new):
            digest.update(part.encode("utf-8"))
            digest.update(b"\0")
    return "sha256:" + digest.hexdigest()


# ---------------------------------------------------------------------------
# 隔离副本树
# ---------------------------------------------------------------------------


def _git(root: Path, *args: str) -> bytes:
    proc = subprocess.run(["git", "-C", str(root)] + list(args),
                          capture_output=True)
    if proc.returncode != 0:
        raise GateError("git {} failed: {}".format(
            " ".join(args), proc.stderr.decode("utf-8", "replace").strip()))
    return proc.stdout


def source_files(root: Path, include_examples: bool = False) -> List[str]:
    """Tracked plus untracked-but-not-ignored files, minus nested modules.

    A Go selector never compiles the nested example modules, so its copy leaves
    them out; a Python gate reads them as part of its corpus (the dead-producer
    consumer face walks every Go file under the root), so a run carrying gate
    mutations copies them too.
    """
    raw = _git(root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
    names = []
    for chunk in raw.split(b"\0"):
        if not chunk:
            continue
        name = chunk.decode("utf-8")
        if not include_examples and name.startswith(COPY_EXCLUDED_PREFIXES):
            continue
        names.append(name)
    return sorted(set(names))


def copy_working_tree(root: Path, dest: Path, files: Sequence[str]) -> None:
    """Copy the current working tree (tracked modifications included), not HEAD."""
    dest.mkdir(parents=True, exist_ok=True)
    for name in files:
        source = root / name
        target = dest / name
        if source.is_symlink():
            target.parent.mkdir(parents=True, exist_ok=True)
            if target.exists() or target.is_symlink():
                target.unlink()
            os.symlink(os.readlink(source), target)
        elif source.is_file():
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, target)


def tree_digest(root: Path, files: Sequence[str]) -> str:
    """Content digest of the copied source set: the baseline an evidence names."""
    digest = hashlib.sha256()
    for name in sorted(files):
        path = root / name
        if not path.is_file():
            continue
        digest.update(name.encode("utf-8"))
        digest.update(b"\0")
        digest.update(hashlib.sha256(path.read_bytes()).digest())
    return "sha256:" + digest.hexdigest()


def file_digest(path: Path) -> str:
    return "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()


def git_head(root: Path) -> str:
    try:
        return _git(root, "rev-parse", "HEAD").decode("utf-8").strip()
    except GateError:
        return ""


def git_dirty(root: Path) -> bool:
    try:
        return bool(_git(root, "status", "--porcelain").strip())
    except GateError:
        return False


def _runner_module():
    """The scenario runner that owns the baseline vocabulary.

    `scripts/scenarios/run_runtime_scenarios.py` writes the contract digest and
    the per-module fingerprint every runner evidence carries. Reusing its
    `module_fingerprint` keeps one definition, so the mutation report and the
    runs it is tied to cannot drift apart through two implementations.
    """
    scenarios = str(SCRIPTS.parent / "scenarios")
    if scenarios not in sys.path:
        sys.path.insert(0, scenarios)
    try:
        import run_runtime_scenarios
    except ImportError as exc:
        raise GateError(
            "cannot load the scenario runner for the baseline fingerprint: {}"
            .format(exc))
    return run_runtime_scenarios


MUTATION_TOOL_NAME = "check_event_integrity_mutations"


def tested_source_binding(root: Path) -> Dict[str, object]:
    """The closure these mutations compile, resolved from the tree they run on.

    Every registered mutation drives `go -C <root> test`, so the tested module is
    the root module — but a selected module is not the whole compiled tree: a local
    `replace` in the root's go.mod and a go.work `use` list both add sources, and a
    report that named only the root would leave those free to change.
    """
    runner = _runner_module()
    try:
        return runner.source_binding(str(root), [{"dir": ".", "module_path": runner.MODULE_PATH}])
    except runner.RunnerError as exc:
        raise GateError("cannot bind the tested sources: {}".format(exc))


def binding_window(binding: Dict[str, object]) -> Dict[str, object]:
    """One end of the run: the closure digest and how many modules it covered."""
    return {"digest": binding["digest"], "modules": len(binding["modules"])}


def baseline_evidence(root: Path, contract_path: Optional[str]) -> Dict[str, object]:
    """The baseline a `--bundle` ties this mutation report to.

    The report only counts when it names the same contract digest and the same
    per-module source fingerprint as the evidence the runner produced. The
    digest is the sha256 of the schema-v2 event contract — the very file the
    runner digests — and the fingerprint comes from the runner's own
    `source_binding`. Every registered mutation drives `go -C <root> test`,
    so the tested module is the root module.

    The closure and the tool identity are recorded beside it because a mutation
    verdict is only as good as the tree and the gate that produced it: the same
    check that walks the sources also names the script whose bytes decide what
    "killed" means.
    """
    relative = contract_path or DEFAULT_EVENT_CONTRACT
    path = Path(relative)
    if not path.is_absolute():
        path = root / relative
    if not path.is_file():
        raise GateError("event contract is missing: {}".format(path))
    contract = load_json(path)
    if contract.get("schema_version") != 2 or contract.get("profile") != PROFILE:
        raise GateError(
            "{} is not the schema v2 {} contract; its digest cannot tie the "
            "mutations to the runner baseline".format(path, PROFILE))
    runner = _runner_module()
    binding = tested_source_binding(root)
    try:
        gate_tool = runner.tool_identity(str(root), name=MUTATION_TOOL_NAME,
                                         entrypoint=os.path.relpath(
                                             os.path.abspath(__file__), str(root)))
    except runner.RunnerError as exc:
        raise GateError("cannot fingerprint the mutation gate: {}".format(exc))
    return {
        "contract_digest": file_digest(path),
        "module_fingerprints": binding["modules"],
        "source_binding": dict(binding, before=binding_window(binding),
                               after=binding_window(binding)),
        "gate_tool": gate_tool,
    }


# ---------------------------------------------------------------------------
# 运行器
# ---------------------------------------------------------------------------


def go_test_command(tree: Path, mutation: Mutation) -> List[str]:
    return ["go", "-C", str(tree), "test", mutation.test_package,
            "-run", mutation.test_selector, "-count=1", "-race", "-json"]


def gate_command(tree: Path, mutation: Mutation) -> List[str]:
    return [sys.executable, str(tree / mutation.gate_path), "--root", str(tree)]


def run_gate_check(tree: Path, mutation: Mutation, timeout: int,
                   log_path: Path) -> RunResult:
    """Run the named Python gate on the copy tree; its exit is the verdict."""
    command = gate_command(tree, mutation)
    timed_out = False
    try:
        proc = subprocess.run(command, cwd=str(tree), capture_output=True, text=True,
                              encoding="utf-8", errors="replace", timeout=timeout)
        exit_code = proc.returncode
        stdout, stderr = proc.stdout, proc.stderr
    except subprocess.TimeoutExpired as exc:
        timed_out = True
        exit_code = -1
        stdout = exc.stdout or ""
        stderr = exc.stderr or ""
        if isinstance(stdout, bytes):
            stdout = stdout.decode("utf-8", "replace")
        if isinstance(stderr, bytes):
            stderr = stderr.decode("utf-8", "replace")
    log_path.parent.mkdir(parents=True, exist_ok=True)
    log_path.write_text(stdout + ("\n" + stderr if stderr else ""), encoding="utf-8")
    raw = stdout + ("\n" + stderr if stderr else "")
    return RunResult(test_name="", exit_code=exit_code, events=[], raw_text=raw,
                     log_ref=str(log_path), command=tuple(command), timed_out=timed_out)


def run_go_test(tree: Path, mutation: Mutation, timeout: int,
                log_path: Path) -> RunResult:
    command = go_test_command(tree, mutation)
    timed_out = False
    try:
        proc = subprocess.run(command, capture_output=True, text=True,
                              encoding="utf-8", errors="replace", timeout=timeout)
        exit_code = proc.returncode
        stdout, stderr = proc.stdout, proc.stderr
    except subprocess.TimeoutExpired as exc:
        timed_out = True
        exit_code = -1
        stdout = exc.stdout or ""
        stderr = exc.stderr or ""
        if isinstance(stdout, bytes):
            stdout = stdout.decode("utf-8", "replace")
        if isinstance(stderr, bytes):
            stderr = stderr.decode("utf-8", "replace")
    log_path.parent.mkdir(parents=True, exist_ok=True)
    log_path.write_text(stdout + ("\n" + stderr if stderr else ""), encoding="utf-8")
    return parse_go_test_output(stdout, stderr, mutation.test_name, exit_code,
                                log_ref=str(log_path), command=command,
                                timed_out=timed_out)


def replay_result(log_path: Path, mutation: Mutation) -> RunResult:
    """Parse one recorded `go test -json` stream into the run the gate judges.

    A recording carries no process status, so the exit code is the package
    outcome the stream states: a package-level `fail` or `build-fail` is the
    non-zero exit `go test` would have returned. A missing recording is a gate
    error, never a green run.
    """
    if not log_path.is_file():
        raise GateError("replay log is missing: {}".format(log_path))
    result = parse_go_test_output(log_path.read_text(encoding="utf-8"), "",
                                  mutation.test_name, 0, log_ref=str(log_path),
                                  command=("replay", str(log_path)))
    failed = any(event.get("Action") in ("fail", "build-fail")
                 and not event.get("Test") for event in result.events)
    result.exit_code = 1 if failed else 0
    return result


def control_result(tree: Path, mutation: Mutation, timeout: int, out_dir: Path,
                   replay: Optional[Path]) -> RunResult:
    """One control run: live `go test` or gate, or its recording under `--replay`."""
    if mutation.gate_path:
        if replay is not None:
            raise GateError(
                "{}: a gate verdict is a live process exit; --replay carries no gate "
                "recordings".format(mutation.id))
        return run_gate_check(tree, mutation, timeout,
                              out_dir / "control" / (Path(mutation.gate_path).name + ".log"))
    if replay is None:
        return run_go_test(tree, mutation, timeout,
                           out_dir / "control" / (mutation.test_name + ".jsonl"))
    return replay_result(replay / "control" / (mutation.test_name + ".jsonl"), mutation)


def mutation_result(tree: Path, mutation: Mutation, timeout: int, out_dir: Path,
                    replay: Optional[Path]) -> RunResult:
    """One mutated run: live `go test` or gate, or its recording under `--replay`."""
    if mutation.gate_path:
        if replay is not None:
            raise GateError(
                "{}: a gate verdict is a live process exit; --replay carries no gate "
                "recordings".format(mutation.id))
        return run_gate_check(tree, mutation, timeout,
                              out_dir / "mutations" / mutation.id / "run.log")
    if replay is None:
        return run_go_test(tree, mutation, timeout,
                           out_dir / "mutations" / mutation.id / "run.jsonl")
    return replay_result(replay / "mutations" / mutation.id / "run.jsonl", mutation)


def evidence_name(replay: Optional[Path]) -> str:
    """The report name; a replay report must stay outside every `--bundle`."""
    return "evidence.json" if replay is None else "evidence.replay.json"


def apply_patch(tree: Path, mutation: Mutation) -> Dict[str, str]:
    """Apply every edit of one mutation; return the pristine text per file."""
    pristine: Dict[str, str] = {}
    current: Dict[str, str] = {}
    for replacement in mutation.patch_set():
        path = tree / replacement.file
        text = current.get(replacement.file, pristine.get(replacement.file))
        if text is None:
            text = path.read_text(encoding="utf-8")
            pristine[replacement.file] = text
        count = text.count(replacement.old)
        if count != 1:
            raise GateError(
                "{}: patch anchor occurs {} times in {}; a stale mutation cannot count".format(
                    mutation.id, count, replacement.file))
        current[replacement.file] = text.replace(replacement.old, replacement.new, 1)
        path.write_text(current[replacement.file], encoding="utf-8")
    return pristine


def restore_patch(tree: Path, pristine: Dict[str, str]) -> None:
    for name, text in pristine.items():
        (tree / name).write_text(text, encoding="utf-8")


# ---------------------------------------------------------------------------
# 编译面：补丁必须落在该选择器真正编译的包、构建标签、模块里
# ---------------------------------------------------------------------------


GOOS_NAMES = frozenset((
    "aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos",
    "ios", "js", "linux", "nacl", "netbsd", "openbsd", "plan9", "solaris",
    "wasip1", "windows", "zos",
))
GOARCH_NAMES = frozenset((
    "386", "amd64", "arm", "arm64", "loong64", "mips", "mips64", "mips64le",
    "mipsle", "ppc64", "ppc64le", "riscv64", "s390x", "wasm",
))
# The registered command never passes `-tags`; these are the only tags a `go
# test -race` invocation sets by itself besides the platform and release tags.
CONSTRAINT_TAGS = frozenset(("gc", "race"))
_GO_BUILD_PATTERN = re.compile(r"^//go:build\s+(.+)$")
_GO_LEGACY_PATTERN = re.compile(r"^//\s*\+build\s+(.+)$")
_IMPORT_ONE = re.compile(r'^import\s+(?:[\w.]+\s+|_\s+|\.\s+)?"([^"]+)"\s*$')
_IMPORT_IN_BLOCK = re.compile(r'^\s*(?:[\w.]+\s+|_\s+|\.\s+)?"([^"]+)"\s*$')
_FACE_CACHE: Dict[Tuple[str, str], frozenset] = {}


def platform_tags() -> frozenset:
    """The platform tags the gate's own `go test` invocation compiles under."""
    system = platform.system().lower()
    goos = {"darwin": "darwin", "linux": "linux", "windows": "windows"}.get(system, system)
    machine = platform.machine().lower()
    goarch = {"arm64": "arm64", "aarch64": "arm64", "x86_64": "amd64",
              "amd64": "amd64", "i386": "386", "i686": "386"}.get(machine, machine)
    return frozenset((goos, goarch))


def release_tags(go_mod_text: str) -> frozenset:
    """The `go1.N` release tags a module at the declared version satisfies."""
    match = re.search(r"^go\s+(\d+)\.(\d+)", go_mod_text, re.M)
    if match is None:
        return frozenset()
    major, minor = int(match.group(1)), int(match.group(2))
    if major != 1:
        return frozenset()
    return frozenset("go1.{}".format(level) for level in range(1, minor + 1))


def _split_constraint_tokens(expr: str) -> List[str]:
    return re.findall(r"\(|\)|!|&&|\|\||[A-Za-z0-9_.]+", expr)


def _eval_constraint(expr: str, tags: frozenset) -> bool:
    """Evaluate one `//go:build` expression; an unset tag is false, as in Go."""
    tokens = _split_constraint_tokens(expr)
    position = [0]

    def peek() -> str:
        return tokens[position[0]] if position[0] < len(tokens) else ""

    def eat(text: str) -> bool:
        if peek() == text:
            position[0] += 1
            return True
        return False

    def parse_or() -> bool:
        value = parse_and()
        while eat("||"):
            right = parse_and()
            value = value or right
        return value

    def parse_and() -> bool:
        value = parse_unary()
        while eat("&&"):
            right = parse_unary()
            value = value and right
        return value

    def parse_unary() -> bool:
        if eat("!"):
            return not parse_unary()
        if eat("("):
            value = parse_or()
            eat(")")
            return value
        token = peek()
        if not token:
            return False
        position[0] += 1
        return token in tags

    if not tokens:
        return False
    return parse_or()


def build_constraint_excludes(path: Path, tags: frozenset) -> str:
    """Why the build constraints keep this file out of the gate's build, if any."""
    try:
        lines = path.read_text(encoding="utf-8").splitlines()[:40]
    except OSError as exc:
        raise GateError("cannot read {}: {}".format(path, exc))
    expressions = [match.group(1) for match in map(_GO_BUILD_PATTERN.match, lines) if match]
    legacy = [match.group(1) for match in map(_GO_LEGACY_PATTERN.match, lines) if match]
    for expression in expressions:
        if not _eval_constraint(expression, tags):
            return "//go:build {} is false for the tags the gate sets ({})".format(
                expression, " ".join(sorted(tags)))
    for line in legacy:
        # `// +build` lines AND together; each line ORs its comma-separated terms.
        terms = [term.strip() for term in line.split() if term.strip()]
        allowed = False
        for term in terms:
            options = [option for option in term.split(",") if option]
            if options and all((option.startswith("!") and option[1:] not in tags)
                               or (not option.startswith("!") and option in tags)
                               for option in options):
                allowed = True
                break
        if terms and not allowed:
            return "// +build {} is false for the tags the gate sets ({})".format(
                line, " ".join(sorted(tags)))
    # A `name_GOOS.go`, `name_GOARCH.go` or `name_GOOS_GOARCH.go` file only
    # enters the build whose platform it names.
    stem = path.name[:-3] if path.name.endswith(".go") else path.name
    parts = stem.split("_")
    platform_set = platform_tags()
    named = parts[-1:]
    if len(parts) >= 3 and parts[-2] in GOOS_NAMES and parts[-1] in GOARCH_NAMES:
        named = parts[-2:]
    for part in named:
        if (part in GOOS_NAMES or part in GOARCH_NAMES) and part not in platform_set:
            return "{} names the {} build, which the gate does not compile".format(
                path.name, part)
    return ""


def module_root_of(path: Path) -> Path:
    """The nearest ancestor holding a go.mod, i.e. the module a file belongs to."""
    for parent in [path] + list(path.parents):
        if (parent / "go.mod").is_file():
            return parent
    return path.parent


def _file_imports(path: Path) -> List[str]:
    """The import paths one Go file names."""
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError:
        return []
    found: List[str] = []
    in_block = False
    for line in lines:
        stripped = line.strip()
        if in_block:
            if stripped.startswith(")"):
                in_block = False
                continue
            match = _IMPORT_IN_BLOCK.match(line)
            if match:
                found.append(match.group(1))
            continue
        if stripped.startswith("import ("):
            in_block = True
            continue
        match = _IMPORT_ONE.match(stripped)
        if match:
            found.append(match.group(1))
    return found


def compiled_packages(tree: Path, test_package: str) -> frozenset:
    """Every package directory the selector's package compiles, transitively.

    The set is the transitive import closure of the test package, read from the
    source files the build would compile. A file whose package is outside it is
    never built by `go test <test_package>`, so a patch to it cannot change what
    the selector runs.
    """
    key = (str(tree.resolve()), test_package)
    cached = _FACE_CACHE.get(key)
    if cached is not None:
        return cached
    go_mod = tree / "go.mod"
    if not go_mod.is_file():
        raise GateError("no go.mod under {}".format(tree))
    module_text = go_mod.read_text(encoding="utf-8")
    match = re.search(r"^module\s+(\S+)", module_text, re.M)
    if match is None:
        raise GateError("{} names no module path".format(go_mod))
    module_path = match.group(1)
    tags = frozenset(CONSTRAINT_TAGS | platform_tags() | release_tags(module_text))
    start = (tree / test_package).resolve()
    if not start.is_dir():
        raise GateError("test package is missing: {}".format(start))
    seen = {start}
    queue = [start]
    while queue:
        directory = queue.pop()
        for source in sorted(directory.glob("*.go")):
            if build_constraint_excludes(source, tags):
                continue
            for imported in _file_imports(source):
                if imported == module_path:
                    target = tree
                elif imported.startswith(module_path + "/"):
                    target = (tree / imported[len(module_path) + 1:]).resolve()
                else:
                    continue
                if target.is_dir() and target not in seen:
                    seen.add(target)
                    queue.append(target)
    result = frozenset(seen)
    _FACE_CACHE[key] = result
    return result


def gate_face_problem(tree: Path, mutation: Mutation) -> str:
    """Why a gate mutation's patch cannot enter the gate's read face.

    A Python gate's read face is observable only through its verdict, so the
    static check names the files that must stand in the copy; the causal tie is
    the green control beside the named rejection, which only the patch can have
    changed. A gate that reads the patch and stays green is a surviving
    mutation, never an unapplied one.
    """
    if not (tree / mutation.gate_path).is_file():
        return "the anchor gate is missing from the isolated tree: {}".format(
            mutation.gate_path)
    for replacement in mutation.patch_set():
        if not (tree / replacement.file).is_file():
            return "the patch target is missing from the isolated tree: {}".format(
                replacement.file)
    return ""


def compiled_face_problem(tree: Path, mutation: Mutation) -> str:
    """Why the mutation's patch would not enter the selector's compiled face.

    A patch that changes no file the selector builds leaves the run unmutated, so
    whatever the run then says — green or red — is not a kill of this mutation.
    Empty string means every patched file is compiled by `go test <test_package>`.
    A gate mutation is judged by its gate's read face instead.
    """
    if mutation.gate_path:
        return gate_face_problem(tree, mutation)
    modules = {module_root_of((tree / replacement.file).resolve())
               for replacement in mutation.patch_set()}
    if len(modules) > 1 or (modules and next(iter(modules)) != tree.resolve()):
        return "the patch spans modules {} while the selector compiles {}".format(
            sorted(str(module) for module in modules), tree.resolve())
    tags = frozenset(CONSTRAINT_TAGS | platform_tags()
                     | release_tags((tree / "go.mod").read_text(encoding="utf-8")))
    compiled = None
    for replacement in mutation.patch_set():
        path = tree / replacement.file
        excluded = build_constraint_excludes(path, tags)
        if excluded:
            return "{}: {}".format(replacement.file, excluded)
        if compiled is None:
            compiled = compiled_packages(tree, mutation.test_package)
        if path.resolve().parent not in compiled:
            return ("{} is outside the import closure of the {} selector, so the "
                    "selector never compiles it".format(replacement.file,
                                                        mutation.test_package))
    return ""


def not_applied_record(mutation: Mutation, problem: str,
                       evidence_status: str) -> Dict[str, object]:
    """One mutation whose patch never entered the compiled face: not a kill."""
    targets = [replacement.file for replacement in mutation.patch_set()]
    return {
        "id": mutation.id,
        "basis": mutation.basis,
        "evidence_kind": mutation.evidence_kind,
        "evidence_status": evidence_status,
        "evidence_ref": mutation.evidence_ref,
        "description": mutation.description,
        "target": targets[0] if targets else "",
        "targets": targets,
        "test": {
            "module": ".",
            "package": mutation.test_package,
            "file": mutation.test_file,
            "name": mutation.test_name,
            "selector": mutation.test_selector,
        },
        "gate": gate_anchor_record(mutation),
        "expect_assertion": mutation.expect_assertion,
        "assertion_site": mutation.expect_assertion_site,
        "status": NOT_APPLIED,
        "verdict": NOT_APPLIED,
        "reason": "not_in_compiled_face",
        "patch_face": problem,
        "assertion_hit": False,
        "counted_killed": False,
        "patch_digest": patch_digest(mutation),
    }


def controls_for(mutations: Sequence[Mutation]) -> List[Mutation]:
    """One control per unique verdict source; the first mutation carries it."""
    seen = set()
    out: List[Mutation] = []
    for mutation in mutations:
        key = (mutation.gate_path, mutation.test_package, mutation.test_selector)
        if key in seen:
            continue
        seen.add(key)
        out.append(mutation)
    return out


def select_mutations(mutations: Sequence[Mutation],
                     requested: Sequence[str]) -> List[Mutation]:
    enabled = [mutation for mutation in mutations if mutation.enabled]
    if not requested:
        return enabled
    by_id = {mutation.id: mutation for mutation in enabled}
    unknown = [name for name in requested if name not in by_id]
    if unknown:
        raise GateError("unknown or disabled mutation id(s): {}".format(", ".join(unknown)))
    return [by_id[name] for name in requested]


# ---------------------------------------------------------------------------
# 证据汇总
# ---------------------------------------------------------------------------


def pending_record(mutation: Mutation, landed: bool) -> Dict[str, object]:
    """One registered counterexample whose anchor has not landed yet."""
    return {
        "id": mutation.id,
        "basis": mutation.basis,
        "evidence_kind": mutation.evidence_kind,
        "evidence_ref": mutation.evidence_ref,
        "description": mutation.description,
        "status": PENDING,
        "anchor": {
            "module": ".",
            "package": mutation.test_package,
            "file": mutation.test_file,
            "name": mutation.test_name,
            "selector": mutation.test_selector,
            "subtest": mutation.anchor_subtest,
            "landed": landed,
        },
        "expect_assertion": mutation.expect_assertion,
        "pending_reason": mutation.pending_reason,
    }


def summarize(records: Sequence[Dict[str, object]],
              controls: Sequence[Dict[str, object]],
              gate: Dict[str, object],
              root: Path, source_digest: str,
              baseline: Optional[Dict[str, object]] = None,
              replay: Optional[Path] = None) -> Dict[str, object]:
    payload = {
        "schema_version": SCHEMA_VERSION,
        "profile": PROFILE,
        "phase": "04.4",
        # A live run is the only evidence a bundle may consume; a replay report
        # names the recordings it judged instead of a process it drove.
        "mode": "live" if replay is None else "replay",
        "replay": None if replay is None else str(replay),
        "control_tree_green": bool(
            controls and all(control.get("verdict") == "green" for control in controls)),
        "control": list(controls),
        "mutations": list(records),
        "pending": [pending_record(mutation, anchor_landed(root, mutation))
                    for mutation in MUTATIONS if not mutation.enabled],
        "not_evidence_backed": list(gate.get("not_evidence_backed", [])),
        "gate": gate,
        "source": {
            "root": str(root),
            "git_head": git_head(root),
            "working_tree_dirty": git_dirty(root),
            "source_digest": source_digest,
        },
    }
    payload.update(baseline_evidence(root, None) if baseline is None else baseline)
    return payload


def write_evidence(out_dir: Path, payload: Dict[str, object],
                   name: str = "evidence.json") -> Path:
    out_dir.mkdir(parents=True, exist_ok=True)
    path = out_dir / name
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n",
                    encoding="utf-8")
    return path


def run_gate(root: Path, out_dir: Path, timeout: int,
             requested: Sequence[str], keep_tree: bool,
             frozen_contract: Optional[str],
             event_contract: Optional[str] = None,
             replay: Optional[Path] = None) -> Dict[str, object]:
    if replay is None and shutil.which("go") is None:
        raise GateError("go toolchain is not on PATH")
    problems = validate_registry(root, MUTATIONS)
    if problems:
        raise GateError("mutation registry is stale or incomplete: " + "; ".join(problems))
    confirmed = confirmed_defect_ids(root, frozen_contract)
    selected = select_mutations(MUTATIONS, requested)
    if not requested:
        mapping_problems = []
        for defect_id in confirmed:
            hits = [mutation for mutation in selected if mutation.basis == defect_id]
            if len(hits) != 1:
                mapping_problems.append(
                    "confirmed defect {} maps to {} mutations".format(defect_id, len(hits)))
        for basis in REPRODUCED_DEFECTS:
            hits = [mutation for mutation in selected if mutation.basis == basis]
            if len(hits) != 1:
                mapping_problems.append(
                    "reproduced defect {} maps to {} mutations".format(basis, len(hits)))
        if mapping_problems:
            raise GateError("; ".join(mapping_problems))

    # The report must name the runner's baseline even when a control or a
    # mutation later fails: a red report from another tree is still another tree.
    baseline = baseline_evidence(root, event_contract)
    files = source_files(root, include_examples=any(m.gate_path for m in selected))
    tree_root = Path(tempfile.mkdtemp(prefix="event-integrity-mutations-"))
    tree = tree_root / "tree"
    source_digest = tree_digest(root, files)
    records: List[Dict[str, object]] = []
    controls: List[Dict[str, object]] = []
    try:
        copy_working_tree(root, tree, files)
        enabled = [mutation for mutation in MUTATIONS if mutation.enabled]
        for control_mutation in controls_for(selected):
            result = control_result(tree, control_mutation, timeout, out_dir, replay)
            verdict = (classify_gate_control(result) if control_mutation.gate_path
                       else classify_control(result))
            controls.append(control_record(control_mutation, result, verdict))
        not_backed = [mutation.id for mutation in enabled
                      if evidence_status_for(
                          mutation.basis, confirmed, REPRODUCED_DEFECTS,
                          [m.basis for m in enabled
                           if m.evidence_kind == "named_assertion"]) == "not_evidence_backed"]
        control_green = bool(controls) and all(c.get("verdict") == "green" for c in controls)
        if control_green and not not_backed:
            for mutation in selected:
                status = evidence_status_for(
                    mutation.basis, confirmed, REPRODUCED_DEFECTS,
                    [m.basis for m in enabled if m.evidence_kind == "named_assertion"])
                face_problem = compiled_face_problem(tree, mutation)
                if face_problem:
                    records.append(not_applied_record(mutation, face_problem, status))
                    continue
                pristine = apply_patch(tree, mutation)
                try:
                    result = mutation_result(tree, mutation, timeout, out_dir, replay)
                finally:
                    restore_patch(tree, pristine)
                for name in pristine:
                    if file_digest(tree / name) != file_digest(root / name):
                        raise GateError(
                            "{}: the isolated tree was not restored to the baseline ({})"
                            .format(mutation.id, name))
                records.append(judge_mutation(mutation, result, status, tree))
        gate = evaluate_gate(records, controls, confirmed,
                             require_confirmed_mapping=not requested)
        if not control_green:
            gate["failures"].append("control tree is not green; mutated verdicts do not count")
            gate["ok"] = False
        if not_backed:
            gate["failures"].append(
                "not_evidence_backed mutations cannot count: " + ", ".join(not_backed))
            gate["ok"] = False
        # The closure is re-resolved on the far side of the mutants. A report that
        # names one tree while the mutations ran against another describes neither,
        # so the drift is refused instead of being written down as one baseline.
        after = tested_source_binding(root)
        if after["digest"] != baseline["source_binding"]["digest"]:
            raise GateError(
                "the tested source closure changed while the mutations ran: {} -> {}; the "
                "report would describe a tree that never stood still".format(
                    baseline["source_binding"]["digest"], after["digest"]))
        baseline = dict(baseline, source_binding=dict(
            baseline["source_binding"],
            before=binding_window(baseline["source_binding"]),
            after=binding_window(after)))
        payload = summarize(records, controls, gate, root, source_digest,
                            baseline=baseline, replay=replay)
        write_evidence(out_dir, payload, evidence_name(replay))
        return payload
    finally:
        if keep_tree:
            print("kept isolated tree: {}".format(tree), file=sys.stderr)
        else:
            shutil.rmtree(tree_root, ignore_errors=True)


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Run the event-integrity fault mutations on an isolated copy tree.")
    parser.add_argument("--all", action="store_true",
                        help="run every enabled mutation")
    parser.add_argument("--mutation", action="append", default=[],
                        metavar="ID", help="run one named mutation (repeatable)")
    parser.add_argument("--json", action="store_true",
                        help="print the machine-readable evidence summary")
    parser.add_argument("--out", default=None,
                        help="evidence directory outside the repository")
    parser.add_argument("--root", default=str(DEFAULT_ROOT),
                        help="repository root (default: two levels above scripts/gates/)")
    parser.add_argument("--frozen-contract", default=None,
                        help="override the 00.3 frozen contract path")
    parser.add_argument("--contract", default=DEFAULT_EVENT_CONTRACT,
                        help="schema v2 event contract whose sha256 ties this report "
                             "to the runner evidence (default: {})".format(
                                 DEFAULT_EVENT_CONTRACT))
    parser.add_argument("--timeout", type=int, default=DEFAULT_TIMEOUT_SECONDS,
                        help="per go test timeout in seconds")
    parser.add_argument("--keep-tree", action="store_true",
                        help="keep the isolated copy tree for inspection")
    parser.add_argument("--replay", default=None, metavar="DIR",
                        help="judge recorded test-log streams under DIR "
                             "(control/<test>.jsonl, mutations/<id>/run.jsonl) "
                             "instead of driving go test; writes "
                             "evidence.replay.json, which no --bundle consumes")
    parser.add_argument("--list", action="store_true",
                        help="list the frozen mutation registry and exit")
    return parser


def main(argv: Optional[Sequence[str]] = None) -> int:
    args = build_parser().parse_args(argv)
    root = Path(args.root).resolve()
    if not (root / "go.mod").is_file():
        print("not a Go module root: {}".format(root), file=sys.stderr)
        return 2
    if args.list:
        payload = [{
            "id": mutation.id, "basis": mutation.basis,
            "evidence_kind": mutation.evidence_kind,
            "targets": [replacement.file for replacement in mutation.patch_set()],
            "test": mutation.test_package + " " + mutation.test_selector,
            "gate": mutation.gate_path,
            "expect_assertion": mutation.expect_assertion,
            "status": mutation.status,
            "anchor_subtest": mutation.anchor_subtest,
            "pending_reason": mutation.pending_reason,
        } for mutation in MUTATIONS]
        print(json.dumps(payload, ensure_ascii=False, indent=2))
        return 0
    if not args.all and not args.mutation:
        print("pass --all or --mutation <id>", file=sys.stderr)
        return 2
    if args.out:
        out_dir = Path(args.out).resolve()
        try:
            out_dir.relative_to(root)
        except ValueError:
            pass
        else:
            print("--out must stay outside the repository: {}".format(out_dir),
                  file=sys.stderr)
            return 2
    else:
        out_dir = Path(tempfile.mkdtemp(prefix="event-integrity-mutations-out-"))
    replay = Path(args.replay).resolve() if args.replay else None
    if replay is not None and not replay.is_dir():
        print("--replay directory does not exist: {}".format(replay), file=sys.stderr)
        return 2
    try:
        payload = run_gate(root, out_dir, args.timeout, args.mutation,
                           args.keep_tree, args.frozen_contract, args.contract,
                           replay)
    except GateError as exc:
        failure = {
            "schema_version": SCHEMA_VERSION,
            "profile": PROFILE,
            "phase": "04.4",
            "control_tree_green": False,
            "mutations": [],
            "pending": [pending_record(mutation, anchor_landed(root, mutation))
                        for mutation in MUTATIONS if not mutation.enabled],
            "not_evidence_backed": [],
            "gate": {"ok": False, "failures": [str(exc)]},
        }
        try:
            failure.update(baseline_evidence(root, args.contract))
        except GateError:
            # The failure itself may be the missing/unreadable baseline; report
            # the named gate failure rather than inventing fields.
            pass
        write_evidence(out_dir, failure, evidence_name(replay))
        print("gate failed: {}".format(exc), file=sys.stderr)
        if args.json:
            print(json.dumps(failure, ensure_ascii=False, indent=2))
        return 1
    if args.json:
        print(json.dumps(payload, ensure_ascii=False, indent=2))
    else:
        for control in payload["control"]:
            source = (control["test"]["selector"]
                      or (control.get("gate") or {}).get("path") or "")
            print("control {}: {} ({})".format(source, control["verdict"], control["reason"]))
        for mutation in payload["mutations"]:
            print("{}: {} ({})".format(mutation["id"], mutation["status"],
                                       mutation["reason"]))
        print("evidence: {}".format(out_dir / evidence_name(replay)))
        print("gate: {}".format("ok" if payload["gate"]["ok"] else "FAILED"))
        for failure in payload["gate"]["failures"]:
            print("  - {}".format(failure))
    return 0 if payload["gate"]["ok"] else 1


if __name__ == "__main__":
    sys.exit(main())
