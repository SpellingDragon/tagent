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

写树纪律（R26）：只在 `TMPDIR` 下的副本树内替换产品文件；仓库工作树零写入、
零 index 变更；原始日志只写 `--out` 指定的仓库外目录。
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, Iterable, List, Optional, Sequence, Tuple

SCRIPTS = Path(__file__).resolve().parent
DEFAULT_ROOT = SCRIPTS.parent
PROFILE = "event-integrity"
SCHEMA_VERSION = 2
DEFAULT_TIMEOUT_SECONDS = 900
DEFAULT_FROZEN_CONTRACT = (
    "openspec/changes/archive/2026-10-25-event-consumption-integrity/changes/"
    "00-contract-evidence/evidence/00.3-frozen-contract.json"
)
# examples/ holds two nested Go modules (1.3G); the root-module tests never
# build them, so the isolated copy keeps only the module under test.
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
    test_package: str
    test_file: str
    test_name: str
    test_selector: str
    # The literal text of the named business assertion that must appear on the
    # failing test: a pending entry may leave it empty until its anchor lands.
    expect_assertion: str = ""
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
            file="host.go",
            # Anchor migrated per the clearance ruling (G5/G6b follow-up turn
            # checking turn.Terminal): the call assigns the returned turn now.
            # The injected defect is unchanged — the notice material re-enters
            # the follow-up range as a second input fact beside its notice fact.
            old=(
                "\tturn, err := h.driveLocked(ctx, InvokeRequest{\n"
                "\t\tInvocationID:  invocationID,\n"
                "\t\tAddress:       addr,\n"
                "\t\tTriggerSource: \"task\",\n"
                "\t})\n"
            ),
            new=(
                "\tturn, err := h.driveLocked(ctx, InvokeRequest{\n"
                "\t\tInvocationID:  invocationID,\n"
                "\t\tAddress:       addr,\n"
                "\t\tTriggerSource: \"task\",\n"
                "\t\tInput:         []model.Message{{Role: model.RoleUser, Content: material}},\n"
                "\t})\n"
            ),
        ),
        test_package="./tests/integration/host",
        test_file="tests/integration/host/event_integrity_test.go",
        test_name="TestEventIntegrityTaskContinuation",
        test_selector="^TestEventIntegrityTaskContinuation$",
        expect_assertion="new user-input facts carrying",
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
            file="settle_turn.go",
            # Patch 1 — the observation of a notice arriving while the pump
            # drives (busy) wakes nothing: the busy notice is dropped instead of
            # kept on the account for a later round.
            old=(
                "\tif !terminalSettleKinds[noticeKindOf(fact)] {\n"
                "\t\treturn nil\n"
                "\t}\n"
                "\td.Wake()\n"
                "\treturn nil\n"
                "}\n"
            ),
            new=(
                "\tif !terminalSettleKinds[noticeKindOf(fact)] {\n"
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
                file="settle_turn.go",
                # Patch 2 — the driving flag the busy swallow reads.
                old="\tmu      sync.Mutex\n\tstopped bool\n",
                new="\tmu      sync.Mutex\n\tstopped bool\n\tbusy    bool\n",
            ),
            Replacement(
                file="settle_turn.go",
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
                file="settle_turn.go",
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
            file="evolution/meditation/driver.go",
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
    ),
    Mutation(
        id="MUT-CAND-02.2-wrong-target",
        basis="CAND-02.2-wrong-target",
        evidence_kind="named_assertion",
        evidence_ref=(
            "02.2 negative anchor TestEventIntegrityReflectionDelivery/"
            "wrong_target_no_extra_write (existing named business assertion)"
        ),
        description=(
            "Let a deliver_summary argument name a target outside deliver_to, "
            "so the declaration no longer refuses the argument."
        ),
        replacement=Replacement(
            file="evolution/meditation/deliver.go",
            old=(
                "\tfor key := range probe {\n"
                "\t\tif key != \"content\" {\n"
                "\t\t\tout = append(out, \"'\"+key+\"'\")\n"
                "\t\t}\n"
                "\t}\n"
            ),
            new=(
                "\tfor key := range probe {\n"
                "\t\tif key != \"content\" && key != \"target\" {\n"
                "\t\t\tout = append(out, \"'\"+key+\"'\")\n"
                "\t\t}\n"
                "\t}\n"
            ),
        ),
        test_package="./tests/integration/host",
        test_file="tests/integration/host/event_integrity_test.go",
        test_name="TestEventIntegrityReflectionDelivery",
        test_selector="^TestEventIntegrityReflectionDelivery$",
        expect_assertion="want the named refusal of the target argument",
    ),
    Mutation(
        id="MUT-EVID-04-position-boundary",
        basis="EVID-04-notice-visibility-loss",
        evidence_kind="confirmed_defect",
        evidence_ref=(
            "00.3-frozen-contract.json defects[EVID-04-notice-visibility-loss]; "
            "negative anchor TestEventIntegrityProjection/notice_visible_under_fold"
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
            file="request.go",
            old=(
                "\t// endedAt names the first entry of the range in flight: everything before\n"
                "\t// it is the ended material a request presents. The boundary is recorded by\n"
                "\t// identity, never by position, because the age fold rewrites the sequence\n"
                "\t// the commits record — a position read after a fold names other material,\n"
                "\t// or falls past the shortened sequence and silently moves the in-flight\n"
                "\t// notice into the budgeted prefix. An empty identity names a chain with no\n"
                "\t// range in flight, so every entry is ended material.\n"
                "\tendedAt         event.EventID\n"
            ),
            new="\tended           int\n",
        ),
        companion=(
            Replacement(
                file="request.go",
                old=(
                    "// endedPrefix reports the position the ended material of the registered sequence\n"
                    "// ends at, resolved against the sequence as it stands now: the position of the\n"
                    "// entry the boundary names by identity. A boundary naming no entry — a chain\n"
                    "// that admitted no range yet, or one whose opening entry a fold has already\n"
                    "// consumed — reports the end of the sequence, so every entry is ended material\n"
                    "// and none of the range in flight is presented twice.\n"
                    "func (h replayEntry) endedPrefix() int {\n"
                    "\tif h.projection == nil {\n"
                    "\t\treturn 0\n"
                    "\t}\n"
                    "\tif h.endedAt == \"\" {\n"
                    "\t\treturn h.projection.Len()\n"
                    "\t}\n"
                    "\tfor at, entry := range h.projection.Entries() {\n"
                    "\t\tif entry.Key == h.endedAt {\n"
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
                file="request.go",
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
                file="request.go",
                # Anchor migrated per the clearance ruling (the projected
                # segment carries the run handle now).
                old=(
                    "\treturn projectedSegment(ctx, a.run, a.sequence.projection, a.sequence.window, "
                    "a.sequence.endedPrefix())\n"
                ),
                new=(
                    "\treturn projectedSegment(ctx, a.run, a.sequence.projection, a.sequence.window, "
                    "a.sequence.ended)\n"
                ),
            ),
            Replacement(
                file="facts.go",
                old=(
                    "\theld.projection.Append(context.Background(), entry)\n"
                    "\tif admitsRange(evt) && held.rangeInvocation != evt.InvocationID {\n"
                    "\t\theld.endedAt = evt.Key\n"
                    "\t}\n"
                ),
                new=(
                    "\tposition := held.projection.Len()\n"
                    "\theld.projection.Append(context.Background(), entry)\n"
                    "\tif admitsRange(evt) && held.rangeInvocation != evt.InvocationID {\n"
                    "\t\theld.ended = position\n"
                    "\t}\n"
                ),
            ),
            Replacement(
                file="facts.go",
                old="\treturn held.endedPrefix(), true\n",
                new="\treturn held.ended, true\n",
            ),
            # The notice read resolves its fence from the same identity: the
            # mutant keeps the position too, so the read compiles and stays
            # wrong in the same way as the boundary it follows.
            Replacement(
                file="facts.go",
                old="\tboundary := held.endedPrefix()\n",
                new="\tboundary := held.ended\n",
            ),
        ),
        test_package="./tests/integration/history",
        test_file="tests/integration/history/event_integrity_test.go",
        test_name="TestEventIntegrityProjection",
        test_selector="^TestEventIntegrityProjection$",
        expect_assertion="want the notice identity and its material",
    ),
    Mutation(
        id="MUT-EVID-05-restart-flat-restore",
        basis="EVID-05-restart-refold-regression",
        evidence_kind="confirmed_defect",
        evidence_ref=(
            "00.3-frozen-contract.json defects[EVID-05-restart-refold-regression]; "
            "restore anchor TestEventIntegrityProjection/restart_restores_fold_form"
        ),
        description=(
            "Restore the replay of the chain flat: seedReplay registers the raw mapped "
            "sequence without re-folding it at the commit boundaries the chain recorded, "
            "so a restart presents material the fold had already consumed and the fold "
            "rule mints a second set of products over it instead of restoring the form "
            "the process left."
        ),
        replacement=Replacement(
            file="run.go",
            old=(
                "\t\tseed.projection.Append(ctx, entry)\n"
                "\t\tif err := restoreFoldForm(ctx, seed.projection, seed.window); err != nil {\n"
                "\t\t\treturn err\n"
                "\t\t}\n"
            ),
            new=(
                "\t\tseed.projection.Append(ctx, entry)\n"
            ),
        ),
        test_package="./tests/integration/history",
        test_file="tests/integration/history/event_integrity_test.go",
        test_name="TestEventIntegrityProjection",
        test_selector="^TestEventIntegrityProjection$",
        expect_assertion="the restart restored the fold products",
        anchor_subtest="restart_restores_fold_form",
    ),
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
            "storage-tier refusal in the persist path is answered as a committed "
            "fact, the fact enters the live ledger and the turn keeps going, so the "
            "barrier failure is answered as success instead of reported by name."
        ),
        replacement=Replacement(
            file="facts.go",
            # The refusal exits of the persist path all answer as committed. The
            # first registry form swallowed the segment store error alone; the
            # live persist path splits one refusal over several exits (the read
            # that flushes staged writes, the originals put, the conflict read),
            # so one swallow left the turn failing beside an unnamed error or hid
            # the mutant entirely. The defect claim is unchanged — a refused
            # segment is reported as a committed fact and the barrier failure is
            # answered as success.
            old=(
                "\tif err != nil {\n"
                "\t\treturn err\n"
                "\t}\n"
                "\tif supply == nil {\n"
            ),
            new=(
                "\tif err != nil {\n"
                "\t\treturn nil\n"
                "\t}\n"
                "\tif supply == nil {\n"
            ),
        ),
        companion=(
            Replacement(
                file="facts.go",
                # The read that flushes staged writes answers as committed.
                old=(
                    "\tstored, held, err := tier.Segments.GetEvent(ctx, evt.Key)\n"
                    "\tif err != nil {\n"
                    "\t\treturn err\n"
                    "\t}\n"
                ),
                new=(
                    "\tstored, held, err := tier.Segments.GetEvent(ctx, evt.Key)\n"
                    "\tif err != nil {\n"
                    "\t\treturn nil\n"
                    "\t}\n"
                ),
            ),
            Replacement(
                file="facts.go",
                # The originals write answers as committed.
                old=(
                    "\tif err := tier.Originals.Put(ctx, body.Ref(), []byte(body.Content)); err != nil {\n"
                    "\t\treturn err\n"
                    "\t}\n"
                ),
                new=(
                    "\tif err := tier.Originals.Put(ctx, body.Ref(), []byte(body.Content)); err != nil {\n"
                    "\t\treturn nil\n"
                    "\t}\n"
                ),
            ),
            Replacement(
                file="facts.go",
                # The conflict-branch read answers as committed.
                old=(
                    "\t\tprior, held, getErr := tier.Segments.GetEvent(ctx, evt.Key)\n"
                    "\t\tif getErr != nil {\n"
                    "\t\t\treturn getErr\n"
                    "\t\t}\n"
                ),
                new=(
                    "\t\tprior, held, getErr := tier.Segments.GetEvent(ctx, evt.Key)\n"
                    "\t\tif getErr != nil {\n"
                    "\t\t\treturn nil\n"
                    "\t\t}\n"
                ),
            ),
            Replacement(
                file="facts.go",
                # The conflict-branch refusal answers as committed.
                old="\t\tif !held {\n\t\t\treturn err\n\t\t}\n",
                new="\t\tif !held {\n\t\t\treturn nil\n\t\t}\n",
            ),
        ),
        test_package="./tests/integration/history",
        test_file="tests/integration/history/event_integrity_test.go",
        test_name="TestEventIntegrityPersistence",
        test_selector="^TestEventIntegrityPersistence$",
        expect_assertion="the turn beside a failing storage tier returned no failure",
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
            file="facts.go",
            old=(
                "\tr.loop.Backflow(frame.Identity.InvocationID, notice)\n"
                "\treturn r.store.Consume(ctx, frame.Address, frame.Identity.InstanceID, seq)\n"
            ),
            new=(
                "\tif err := r.store.Consume(ctx, frame.Address, frame.Identity.InstanceID, seq); "
                "err != nil {\n"
                "\t\treturn err\n"
                "\t}\n"
                "\tr.loop.Backflow(frame.Identity.InvocationID, notice)\n"
                "\treturn nil\n"
            ),
        ),
        test_package="./tests/integration/history",
        test_file="tests/integration/history/event_integrity_test.go",
        test_name="TestEventIntegrityConsumptionRecovery",
        test_selector="^TestEventIntegrityConsumptionRecovery$",
        expect_assertion="not recoverable after the restart",
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
            old='\treturn fmt.Sprintf("executed %s: %s", key, output), nil\n',
            new=(
                '\treturn fmt.Sprintf("executed %s: completed successfully: %s", '
                'key, output), nil\n'
            ),
        ),
        test_package="./tool/exec",
        test_file="tool/exec/event_integrity_test.go",
        test_name="TestEventIntegrityExecLifecycle",
        test_selector="^TestEventIntegrityExecLifecycle$",
        expect_assertion="no query face may report success or completion",
        anchor_subtest="partial_output_is_not_completion",
    ),
)

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


def _assertion_hit(result: RunResult, expect_assertion: str) -> bool:
    """The assertion must appear on a failing test, not in a passing log."""
    failing = _failing_tests(result)
    if not failing:
        return False
    for event in result.events:
        if event.get("Action") != "output":
            continue
        test = event.get("Test", "")
        if not test:
            continue
        related = test in failing or any(name.startswith(test + "/") for name in failing)
        if related and expect_assertion in event.get("Output", ""):
            return True
    return False


def classify(result: RunResult, expect_assertion: str) -> Dict[str, object]:
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
    hit = _assertion_hit(result, expect_assertion)
    if result.exit_code == 0:
        return {"verdict": NOT_KILLED, "reason": "test_green", "assertion_hit": hit}
    if hit:
        return {"verdict": KILLED, "reason": "named_assertion", "assertion_hit": True}
    if "panic:" in text:
        return {"verdict": NOT_KILLED, "reason": "panic_without_assertion",
                "assertion_hit": False}
    return {"verdict": NOT_KILLED, "reason": "assertion_not_matched", "assertion_hit": False}


def classify_control(result: RunResult) -> Dict[str, object]:
    """The unmutated control must run the selector and end green."""
    if result.timed_out:
        return {"verdict": "red", "reason": "timeout", "exit_code": result.exit_code}
    if not _ran_test(result):
        return {"verdict": "red", "reason": "zero_match", "exit_code": result.exit_code}
    failures = _failing_tests(result)
    if result.exit_code != 0 or failures:
        return {"verdict": "red", "reason": "test_failed", "exit_code": result.exit_code,
                "failing": failures}
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


def judge_mutation(mutation: Mutation, result: RunResult,
                   evidence_status: str) -> Dict[str, object]:
    """Build the per-mutation record; unbacked candidates never count as kills."""
    outcome = classify(result, mutation.expect_assertion)
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
        "expect_assertion": mutation.expect_assertion,
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
            failures.append(
                "control {} is {} ({})".format(
                    control.get("test", {}).get("selector"), control.get("verdict"),
                    control.get("reason")))
    if not records:
        failures.append("no mutation was run")
    for record in records:
        if record.get("evidence_status") == "not_evidence_backed":
            failures.append(
                "mutation {} is not evidence-backed ({})".format(
                    record.get("id"), record.get("basis")))
        if record.get("verdict") != KILLED:
            failures.append(
                "mutation {} status {!r} is not 'killed' ({})".format(
                    record.get("id"), record.get("verdict"), record.get("reason")))
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
            carrier = by_id.get(carrying)
            if carrier is None:
                problems.append(
                    "candidate form {} points at an unknown mutation {}".format(
                        entry["form"], carrying))
            elif not carrier.enabled:
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
        target = root / mutation.test_file
        if not target.is_file():
            problems.append("{}: test file is missing: {}".format(
                mutation.id, mutation.test_file))
        elif mutation.expect_assertion not in target.read_text(encoding="utf-8"):
            problems.append(
                "{}: expect_assertion is absent from {}".format(
                    mutation.id, mutation.test_file))
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


def source_files(root: Path) -> List[str]:
    """Tracked plus untracked-but-not-ignored files, minus nested modules."""
    raw = _git(root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
    names = []
    for chunk in raw.split(b"\0"):
        if not chunk:
            continue
        name = chunk.decode("utf-8")
        if name.startswith(COPY_EXCLUDED_PREFIXES):
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


# ---------------------------------------------------------------------------
# 运行器
# ---------------------------------------------------------------------------


def go_test_command(tree: Path, mutation: Mutation) -> List[str]:
    return ["go", "-C", str(tree), "test", mutation.test_package,
            "-run", mutation.test_selector, "-count=1", "-race", "-json"]


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


def controls_for(mutations: Sequence[Mutation]) -> List[Mutation]:
    """One control per unique (package, selector); the first mutation carries it."""
    seen = set()
    out: List[Mutation] = []
    for mutation in mutations:
        key = (mutation.test_package, mutation.test_selector)
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
              root: Path, source_digest: str) -> Dict[str, object]:
    return {
        "schema_version": SCHEMA_VERSION,
        "profile": PROFILE,
        "phase": "04.4",
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


def write_evidence(out_dir: Path, payload: Dict[str, object]) -> Path:
    out_dir.mkdir(parents=True, exist_ok=True)
    path = out_dir / "evidence.json"
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n",
                    encoding="utf-8")
    return path


def run_gate(root: Path, out_dir: Path, timeout: int,
             requested: Sequence[str], keep_tree: bool,
             contract_path: Optional[str]) -> Dict[str, object]:
    if shutil.which("go") is None:
        raise GateError("go toolchain is not on PATH")
    problems = validate_registry(root, MUTATIONS)
    if problems:
        raise GateError("mutation registry is stale or incomplete: " + "; ".join(problems))
    confirmed = confirmed_defect_ids(root, contract_path)
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

    files = source_files(root)
    tree_root = Path(tempfile.mkdtemp(prefix="event-integrity-mutations-"))
    tree = tree_root / "tree"
    source_digest = tree_digest(root, files)
    records: List[Dict[str, object]] = []
    controls: List[Dict[str, object]] = []
    try:
        copy_working_tree(root, tree, files)
        enabled = [mutation for mutation in MUTATIONS if mutation.enabled]
        for control_mutation in controls_for(selected):
            result = run_go_test(tree, control_mutation, timeout,
                                 out_dir / "control" / (
                                     control_mutation.test_name + ".jsonl"))
            verdict = classify_control(result)
            controls.append({
                "test": {
                    "module": ".",
                    "package": control_mutation.test_package,
                    "file": control_mutation.test_file,
                    "name": control_mutation.test_name,
                    "selector": control_mutation.test_selector,
                },
                "verdict": verdict["verdict"],
                "reason": verdict["reason"],
                "exit_code": result.exit_code,
                "timed_out": result.timed_out,
                "command": list(result.command),
                "log_ref": result.log_ref,
            })
        not_backed = [mutation.id for mutation in enabled
                      if evidence_status_for(
                          mutation.basis, confirmed, REPRODUCED_DEFECTS,
                          [m.basis for m in enabled
                           if m.evidence_kind == "named_assertion"]) == "not_evidence_backed"]
        control_green = bool(controls) and all(c.get("verdict") == "green" for c in controls)
        if control_green and not not_backed:
            for mutation in selected:
                pristine = apply_patch(tree, mutation)
                try:
                    result = run_go_test(tree, mutation, timeout,
                                         out_dir / "mutations" / mutation.id / "run.jsonl")
                finally:
                    restore_patch(tree, pristine)
                for name in pristine:
                    if file_digest(tree / name) != file_digest(root / name):
                        raise GateError(
                            "{}: the isolated tree was not restored to the baseline ({})"
                            .format(mutation.id, name))
                status = evidence_status_for(
                    mutation.basis, confirmed, REPRODUCED_DEFECTS,
                    [m.basis for m in enabled if m.evidence_kind == "named_assertion"])
                records.append(judge_mutation(mutation, result, status))
        gate = evaluate_gate(records, controls, confirmed,
                             require_confirmed_mapping=not requested)
        if not control_green:
            gate["failures"].append("control tree is not green; mutated verdicts do not count")
            gate["ok"] = False
        if not_backed:
            gate["failures"].append(
                "not_evidence_backed mutations cannot count: " + ", ".join(not_backed))
            gate["ok"] = False
        payload = summarize(records, controls, gate, root, source_digest)
        write_evidence(out_dir, payload)
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
                        help="repository root (default: parent of scripts/)")
    parser.add_argument("--frozen-contract", default=None,
                        help="override the 00.3 frozen contract path")
    parser.add_argument("--timeout", type=int, default=DEFAULT_TIMEOUT_SECONDS,
                        help="per go test timeout in seconds")
    parser.add_argument("--keep-tree", action="store_true",
                        help="keep the isolated copy tree for inspection")
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
    try:
        payload = run_gate(root, out_dir, args.timeout, args.mutation,
                           args.keep_tree, args.frozen_contract)
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
        write_evidence(out_dir, failure)
        print("gate failed: {}".format(exc), file=sys.stderr)
        if args.json:
            print(json.dumps(failure, ensure_ascii=False, indent=2))
        return 1
    if args.json:
        print(json.dumps(payload, ensure_ascii=False, indent=2))
    else:
        for control in payload["control"]:
            print("control {test[selector]}: {verdict} ({reason})".format(**control))
        for mutation in payload["mutations"]:
            print("{}: {} ({})".format(mutation["id"], mutation["status"],
                                       mutation["reason"]))
        print("evidence: {}".format(out_dir / "evidence.json"))
        print("gate: {}".format("ok" if payload["gate"]["ok"] else "FAILED"))
        for failure in payload["gate"]["failures"]:
            print("  - {}".format(failure))
    return 0 if payload["gate"]["ok"] else 1


if __name__ == "__main__":
    sys.exit(main())
