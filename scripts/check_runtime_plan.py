#!/usr/bin/env python3
"""check_runtime_plan.py — OpenSpec 规划期检查（格式/结构有效，不是产品验证）。

检查对象：某个 change 目录（默认 openspec/changes/archive/2026-10-18-runtime-rebuild）。
检查内容：
  * task-graph.json 的形状、id 全覆盖、依赖无重复/无未知/无自环/无环；
  * 两阶段修订硬校验：24 个骨架任务 NN.P0.M 的 phase 必须为 skeleton
    （00.P0.2 工装单例外 tooling）；01–09 域全部非 P0 实施任务 MUST 以骨架评审门
    00.P0.3 为额外前置（缺边即报错，文案含「阶段门」）；00.P0.3 必须存在于图。
  * 十个二级域四件套存在（每域至少一份 spec）；
  * 各域 tasks.md 的叶 checkbox 编号与 graph 精确相等（多/少/重复都报）；
  * graph status 词表 planned/blocked/in_progress/done：与叶 checkbox 双向一致——
    勾选但 status 非 done = 无证据的完成；status=done 但未勾选 = 缺勾选凭据。
    done 必须携带 evidence {run_id,commands,exit_codes,artifact_paths}。本轮 89 图
    （65 实施＋24 骨架）保持全 planned（不继承任何历史 done）；但词表与证据形状
    已为未来完成预留——完成只能以证据如实表达，绝不把本轮 planned 当成“任务
    永不完成”的永久锁（否则每个真实完成都会红）。
  * ORCHESTRATION.md 四层（结论/编排四问/明细/台账）存在且不倒装；
  * spec 按 delta 段分类校验：ADDED/MODIFIED/baseline 的 ### Requirement 须含
    SHALL/MUST 且至少一个 #### Scenario；REMOVED 的要求须含 **Reason** 与
    **Migration**——删除项没有可断言的行为，绝不要求写者给删除项编造场景；
    未识别的 delta 段、段外 Requirement、无 Requirement 的空 spec 一律失败。
    MODIFIED/REMOVED 的要求必须在主 spec 中唯一存在（重复=ambiguity），
    ADDED 不得与主 spec 撞名（应为 MODIFIED），且能力有声明。

profile 分流（04.5）：task-graph.json 顶层 `domain_profile` 声明本图用哪套规则。
  * 无 `domain_profile`（旧十域 runtime-rebuild 图）→ 原有规则逐字保留：固定十域、
    123 叶编号、阶段门、十域 tasks.md 与 spec、120 能力映射，一概不放宽；
  * `domain_profile=event-integrity` → 六域规则：叶数/范围/阶段门从 graph 读取
    （不在 profile 复制对象状态），校验六域 id 覆盖、域归属、counts 自洽、
    depends_on/verify_after 无重复/未知/自环、76 节点展开图 Kahn 无环、
    tasks.md checkbox 与 graph 状态双向一致；旧十域常量仍保留并继续对旧图生效。

本检查只读规划文件，绝不修改任何 md/json。输出里 productVerified 恒为 null：
格式通过不代表产品成立。本检查只判 delta 结构，不声称 120 主规格的全部语义
（约 601 条 Requirement）已逐条判定；能力单写责任登记在 coverage.md 的登记表
（主消费任务 / 唯一 delta 写者分开登记），该表已机检集合一致。120 集合可机检，
但其 601 条 Requirement 的语义未逐条全审——该欠账如实保留，绝不以标题命中冒充
审毕，也不自动恢复旧算法条目。
"""
from __future__ import annotations

import argparse
import os
import re
import sys

import runtime_checklib as lib

DOMAINS = (
    "00-foundation",
    "01-state-binding",
    "02-execution",
    "03-history",
    "04-capabilities",
    "05-inspection",
    "06-learning",
    "07-evolution",
    "08-host",
    "09-acceptance",
)
# 每域叶任务编号范围（新计划编号：00.1..6 / 01.1..6 / 02.1..9 / 03.1..8 /
# 04.1..9 / 05.1..4 / 06.1..8 / 07.1..4 / 08.1..5 / 09.1..6 = 65 实施叶任务）。
ID_RANGES = (
    ("00", 20), ("01", 6), ("02", 9), ("03", 8), ("04", 9),
    ("05", 4), ("06", 8), ("07", 4), ("08", 5), ("09", 6),
)
# 两阶段修订（阶段一骨架）新增的 24 个 P0 骨架叶任务编号范围：
# 00.P0.1..3 / 01.P0.1..2 / 02.P0.1..4 / 03.P0.1..2 / 04.P0.1..4 /
# 05.P0.1..2 / 06.P0.1..2 / 07.P0.1..2 / 08.P0.1..2 / 09.P0.1 = 24。
# 与 ID_RANGES 共同构成 89 个叶任务（65 实施＋24 骨架）。
# 报告响应修订轮（2026-10-14，docs/.dev/REPORT.md 响应）：00.7–00.20 共 14 个
# 跨域合同修订任务（[R1]×5/[R2]×6/[R3]×3，title 前缀标批次），phase=design；
# 批次间串行硬边（R2 全依赖 R1 尾、R3 全依赖 R2 尾），剩余 NN.M 按三档加批次尾前置。
P0_RANGES = (
    ("00", 3), ("01", 2), ("02", 4), ("03", 2), ("04", 4),
    ("05", 2), ("06", 2), ("07", 2), ("08", 2), ("09", 1),
)
# 概念收敛战役（2026-10-17 用户裁定：破坏式收敛/全仓规划按痛点排波/双轨验收标尺，
# 见 openspec/changes/archive/2026-10-18-runtime-rebuild/concept-slim-plan.md）：13 个收敛叶任务
# （00.21–00.25 / 02.21–02.22 / 04.21–04.23 / 05.21 / 06.21 / 09.21），
# 编号非连续故单列显式清单；01–09 域收敛叶同样受 00.P0.3 阶段门约束。
SLIM_IDS = (
    "00.21", "00.22", "00.23", "00.24", "00.25",
    "02.21", "02.22", "04.21", "04.22", "04.23",
    "05.21", "06.21", "07.22", "09.21", "00.26", "09.7", "04.24", "04.25", "09.8", "09.9",
)
# 阶段门：01–09 域的全部非 P0 实施叶任务 MUST 以 00.P0.3 为额外前置。
STAGE_GATE_ID = "00.P0.3"
# 骨架任务的 phase 例外白名单（键=任务 id，值=允许的 phase）。
P0_PHASE_EXCEPTIONS = {"00.P0.2": "tooling"}
TOP_LEVEL_FILES = ("proposal.md", "design.md", "tasks.md", "ORCHESTRATION.md")
ORCH_LAYERS = (
    ("结论", "conclusion"),
    ("编排四问", "dispatch-questions"),
    ("明细", "detail"),
    ("台账", "ledger"),
)
ORCH_QUESTIONS = ("次序", "授权", "凭据", "释放")
# task-graph status 词表。本轮图全为 planned；其余状态存在，是为了让未来完成
# 能以证据如实表达，而不是把检查器永久锁死在 planned（否则每个真实完成都会红）。
STATUSES = ("planned", "blocked", "in_progress", "done")
DONE_EVIDENCE_FIELDS = ("run_id", "commands", "exit_codes", "artifact_paths")
# `## <something> Requirements` 若不是已知 delta 段、也不是 baseline `## Requirements`
# 就是结构错误，不能被静默当成正文。
SECTION_ANY_RE = re.compile(r"^##\s+(?P<text>.+?)\s*$")
BASELINE_SECTION_RE = re.compile(r"^##\s+Requirements\s*$")
REASON_RE = re.compile(r"^\*\*Reason\*\*:\s*\S", re.M)
MIGRATION_RE = re.compile(r"^\*\*Migration\*\*:\s*\S", re.M)
ENUMERATOR_RE = re.compile(
    r"^(?:[（(]?\s*(?:[0-9]+|[一二三四五六七八九十]+|[IVXLCM]+)\s*[)）.、．:：]?\s*)+"
)
HEADING_RE = re.compile(r"^(?P<hashes>#{1,6})\s+(?P<text>.+?)\s*$")
MAIN_TITLE_RE = re.compile(r"^#\s+(?P<cap>.+?)\s+Specification\s*$")
# 中期复审修订（2026-10-15）新增的完成证据三规则之二（对齐规则已在
# runtime_checklib.check_completion_evidence）：点名测试必须真实存在且被跑过。
# 违反历史先例：Roundtrip/RoundTrip 改名漂移导致验收命令零匹配空转（R24），
# 以及验证面承诺两个点名测试、证据只跑一半。规则只看 -run 锚与 verification
# 裸标识，不扫 evidence 叙述散文，避免误报。
RUN_ANCHOR_RE = re.compile(r"-run[= ]+['\"^]*(?P<name>Test[A-Za-z0-9_]+)")
BARE_TEST_NAME_RE = re.compile(r"\b(?P<name>Test[A-Z][A-Za-z0-9_]*)\b")
TEST_FUNC_RE = re.compile(r"^func (?P<name>Test[A-Za-z0-9_]+)\(", re.M)

# 04.5：变更在图里声明 profile，检查器按 profile 选规则集。旧图没有该字段，
# 因此旧规则集（十域常量）是默认路径，不因新增 profile 而放宽一个字符。
EVENT_PROFILE = "event-integrity"
EVENT_DOMAINS = (
    "00-contract-evidence",
    "01-runtime-consumption",
    "02-event-consumers",
    "03-formal-simulation",
    "04-regression-gates",
    "05-mr-integration",
)
EVENT_LEAF_ID_RE = re.compile(r"^(?P<prefix>\d{2})\.(?P<index>\d+)(?P<suffix>[a-z]?)$")
EVENT_LEAF_KINDS = ("product", "non_product")
# 声明为该 scope 的图是“规划物化”，叶 status 记规划态，与 tasks.md 的执行勾选分开。
# 该声明不放宽任一强方向：done 必须勾选并带证据，blocked 不得勾选。
PLANNING_ONLY_SCOPES = ("whole-change-planning-materialization",)


def expected_ids():
    """全部批准叶任务编号：65 实施（NN.M）＋24 骨架（NN.P0.M）＋13 概念收敛（SLIM_IDS）= 102。"""
    ids = []
    for prefix, count in ID_RANGES:
        ids.extend("{}.{}".format(prefix, index) for index in range(1, count + 1))
    for prefix, count in P0_RANGES:
        ids.extend("{}.P0.{}".format(prefix, index) for index in range(1, count + 1))
    ids.extend(SLIM_IDS)
    return ids


def domain_for_prefix(prefix):
    for domain in DOMAINS:
        if domain.startswith(prefix + "-"):
            return domain
    return None


def parse_args(argv):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--change-dir",
        default=os.path.join("openspec", "changes", "runtime-rebuild"),
        help="待检查的 change 目录（默认 openspec/changes/archive/2026-10-18-runtime-rebuild）；"
             "规则集由该目录 task-graph.json 的 domain_profile 决定，缺省走旧十域规则",
    )
    parser.add_argument(
        "--plan-graph",
        default=None,
        help="task-graph.json 路径（默认为 <change-dir>/task-graph.json）",
    )
    parser.add_argument(
        "--main-specs-root",
        default=None,
        help="主规格根目录（openspec/specs 的绝对路径）；MODIFIED/REMOVED 的主源在此查找",
    )
    parser.add_argument("--json", action="store_true", help="以 JSON 输出报告")
    parser.add_argument(
        "--repo-root",
        default=".",
        help="点名测试存在性扫描的仓库根（默认当前目录）",
    )
    return parser.parse_args(argv)


def dedupe(names):
    seen = set()
    out = []
    for name in names:
        if name not in seen:
            seen.add(name)
            out.append(name)
    return out


def promised_test_names(strings):
    """验证面承诺的点名测试：-run 锚＋裸 Test 标识（只扫 verification）。"""
    names = []
    for text in strings:
        names.extend(m.group("name") for m in RUN_ANCHOR_RE.finditer(text))
        names.extend(m.group("name") for m in BARE_TEST_NAME_RE.finditer(text))
    return dedupe(names)


def anchored_test_names(strings):
    """声称跑过的点名测试：仅 -run 锚（evidence 命令面，不扫叙述散文）。"""
    names = []
    for text in strings:
        names.extend(m.group("name") for m in RUN_ANCHOR_RE.finditer(text))
    return dedupe(names)


def index_repo_test_funcs(repo_root):
    """仓库内全部 *_test.go 的顶层 func TestX( 索引；找不到任何测试文件返回空集。"""
    names = set()
    for dirpath, dirnames, filenames in os.walk(repo_root):
        dirnames[:] = [d for d in dirnames if not d.startswith(".")]
        for filename in filenames:
            if not filename.endswith("_test.go"):
                continue
            try:
                with open(os.path.join(dirpath, filename), "r", encoding="utf-8") as handle:
                    text = handle.read()
            except OSError:
                continue
            names.update(m.group("name") for m in TEST_FUNC_RE.finditer(text))
    return names


def check_completion_test_anchors(report, raw_tasks, repo_tests):
    """done 任务的点名测试三规则之二：存在性＋全覆盖（对齐规则在 lib 层）。

    存在性：verification 与 evidence 命令面点名的 Go 测试必须在仓库
    *_test.go 中真实存在——空转（验收正则零匹配）与改名漂移在此拦住。
    全覆盖：verification 承诺的每个点名测试必须出现在 evidence.commands
    ——只跑一半在此拦住。二者共同把「负向锚」变成勾选硬门：M 补差任务的
    负向测试不存在或未跑，就不可能勾选。
    """
    for task in raw_tasks:
        if not isinstance(task, dict) or task.get("status") != "done":
            continue
        tid = task.get("id", "?")
        where = "task {}".format(tid)
        verification = task.get("verification")
        evidence = task.get("evidence")
        v_strings = [v for v in verification if isinstance(v, str)] \
            if isinstance(verification, list) else []
        commands = evidence.get("commands") if isinstance(evidence, dict) else None
        c_strings = [c for c in commands if isinstance(c, str)] \
            if isinstance(commands, list) else []
        promised = promised_test_names(v_strings)
        ran = anchored_test_names(c_strings)
        joined = "\n".join(c_strings)
        for name in promised:
            if name not in repo_tests:
                report.error("{} promises test {} but no *_test.go defines func {} "
                             "(空转或改名漂移)".format(where, name, name))
            if name not in joined:
                report.error("{} verification names test {} but evidence.commands "
                             "never ran it (只跑一半)".format(where, name))
        for name in ran:
            if name not in repo_tests:
                report.error("{} evidence claims test {} but no *_test.go defines it "
                             "(空转或改名漂移)".format(where, name))


def check_done_evidence(report, where, evidence):
    """status='done' must carry machine-checkable evidence, never a bare flag.

    A task is only 'done' once a real run has produced run_id / commands /
    exit_codes / artifact_paths; the matching leaf checkbox must also be ticked
    (checked in collect_checkbox_ids, which enforces the bidirectional match).
    This round's graph stays 'planned' — a historical 'done' is never inherited.
    """
    lib.check_completion_evidence(report, where, evidence)


def check_graph_shape(report, graph, change_name):
    if graph.get("schema_version") != 1:
        report.error("task-graph schema_version must be 1, got {!r}".format(
            graph.get("schema_version")))
    if graph.get("change") != change_name:
        report.error("task-graph change {!r} != change dir name {!r}".format(
            graph.get("change"), change_name))
    tasks = graph.get("tasks")
    if not isinstance(tasks, list):
        report.error("task-graph tasks must be a list")
        return []
    allowed = {"id", "domain", "depends_on", "phase", "status", "verification", "evidence"}
    cleaned = []
    for index, task in enumerate(tasks):
        where = "tasks[{}]".format(index)
        if not isinstance(task, dict):
            report.error("{} must be an object".format(where))
            continue
        unknown = sorted(set(task) - allowed)
        if unknown:
            report.warn("{} has unrecognised fields {}".format(where, unknown))
        tid = task.get("id")
        if not isinstance(tid, str) or not lib.TASK_ID_RE.match(tid):
            report.error("{} has invalid id {!r}".format(where, tid))
            continue
        where = "task {}".format(tid)
        prefix, _, tail = tid.partition(".")
        is_p0 = tail.startswith("P0.")
        domain = task.get("domain")
        want_domain = domain_for_prefix(tid.split(".")[0])
        if domain not in DOMAINS:
            report.error("{} domain {!r} is not one of the 10 domains".format(where, domain))
        elif want_domain is not None and domain != want_domain:
            report.error("{} belongs to {} but domain is {!r}".format(where, want_domain, domain))
        deps = task.get("depends_on")
        if not isinstance(deps, list) or any(not isinstance(d, str) for d in deps):
            report.error("{} depends_on must be a list of task-id strings".format(where))
            deps = []
        phase = task.get("phase")
        if phase not in lib.PHASES:
            report.error("{} phase {!r} not in {}".format(where, phase, lib.PHASES))
        # 两阶段修订硬校验 (a)：24 个骨架任务的 phase 必须是 skeleton，唯一例外
        # 00.P0.2（工装单）为 tooling；白名单外一律拒收。
        elif is_p0:
            expected_phase = P0_PHASE_EXCEPTIONS.get(tid, "skeleton")
            if phase != expected_phase:
                report.error("{} is a skeleton task so its phase must be {!r}, got {!r}"
                             .format(where, expected_phase, phase))
        status = task.get("status")
        if status not in STATUSES:
            report.error("{} status {!r} not in {}".format(where, status, STATUSES))
        elif status == "done":
            check_done_evidence(report, where, task.get("evidence"))
        verification = task.get("verification")
        if (not isinstance(verification, list) or not verification
                or any(not isinstance(v, str) or not v.strip() for v in verification)):
            report.error("{} verification must be a non-empty list of non-empty strings".format(where))
        # 两阶段修订硬校验 (b)：01–09 域的全部非 P0 实施叶任务 MUST 以骨架评审门
        # 00.P0.3 为额外前置（缺失即阶段门未生效）；00 域（含门本身）不适用。
        if not is_p0 and prefix != "00" and STAGE_GATE_ID not in deps:
            report.error("{} is an 01–09 non-P0 task but lacks the 阶段门 prerequisite {}"
                         .format(where, STAGE_GATE_ID))
        cleaned.append({"id": tid, "domain": domain, "depends_on": deps, "status": status})
    return cleaned


def find_cycle(adjacency):
    color = {}
    stack = []

    def visit(node):
        color[node] = 1
        stack.append(node)
        for nxt in adjacency.get(node, ()):
            state = color.get(nxt, 0)
            if state == 1:
                return stack[stack.index(nxt):] + [nxt]
            if state == 0:
                found = visit(nxt)
                if found:
                    return found
        stack.pop()
        color[node] = 2
        return None

    for node in adjacency:
        if color.get(node, 0) == 0:
            found = visit(node)
            if found:
                return found
    return None


def check_dependency_graph(report, tasks):
    ids = [task["id"] for task in tasks]
    duplicates = sorted({tid for tid in ids if ids.count(tid) > 1})
    for tid in duplicates:
        report.error("duplicate task id in graph: {}".format(tid))
    expected = set(expected_ids())
    actual = set(ids)
    for tid in sorted(expected - actual):
        report.error("task {} missing from task-graph".format(tid))
    for tid in sorted(actual - expected):
        report.error("task {} is not part of the approved leaf tasks (65 实施＋24 骨架＋13 收敛)".format(tid))
    adjacency = {task["id"]: [] for task in tasks}
    statuses = {task["id"]: task["status"] for task in tasks}
    for task in tasks:
        if len(task["depends_on"]) != len(set(task["depends_on"])):
            report.error("task {} has duplicate dependency".format(task["id"]))
        for dep in task["depends_on"]:
            if task["status"] == "done" and statuses.get(dep) != "done":
                report.error("done task {} has unfinished dependency {}".format(task["id"], dep))
            if dep == task["id"]:
                report.error("task {} depends on itself".format(task["id"]))
            elif dep not in adjacency:
                report.error("task {} has unknown dependency {}".format(task["id"], dep))
            else:
                adjacency[task["id"]].append(dep)
    cycle = find_cycle(adjacency)
    if cycle:
        report.error("task dependency cycle: {}".format(" -> ".join(cycle)))
    report.count("graph_tasks", len(ids))
    report.count("graph_ids_unique", len(set(ids)))
    report.count("graph_edges", sum(len(task["depends_on"]) for task in tasks))
    return adjacency


def check_artifacts(report, change_dir, domains=DOMAINS):
    for name in TOP_LEVEL_FILES:
        if not os.path.isfile(os.path.join(change_dir, name)):
            report.error("change root missing {}".format(name))
    top_specs = glob_specs(os.path.join(change_dir, "specs"))
    if [cap for cap, _ in top_specs] != ["orchestration"]:
        report.error("top-level specs must contain only orchestration")
    found_domains = 0
    for domain in domains:
        base = os.path.join(change_dir, "changes", domain)
        if not os.path.isdir(base):
            report.error("domain dir missing: changes/{}".format(domain))
            continue
        for name in ("proposal.md", "design.md", "tasks.md"):
            if not os.path.isfile(os.path.join(base, name)):
                report.error("changes/{}/{} missing".format(domain, name))
        specs_root = os.path.join(base, "specs")
        spec_files = glob_specs(specs_root)
        if not spec_files:
            report.error("changes/{}/specs has no <capability>/spec.md".format(domain))
        else:
            found_domains += 1
    report.count("domains_with_spec", found_domains)


def glob_specs(specs_root):
    found = []
    if not os.path.isdir(specs_root):
        return found
    for cap in sorted(os.listdir(specs_root)):
        path = os.path.join(specs_root, cap, "spec.md")
        if os.path.isfile(path):
            found.append((cap, path))
    return found


def collect_checkbox_ids(report, change_dir, status_by_id):
    md_ids = {}
    for domain in DOMAINS:
        path = os.path.join(change_dir, "changes", domain, "tasks.md")
        if not os.path.isfile(path):
            continue
        try:
            with open(path, "r", encoding="utf-8") as handle:
                lines = handle.read().splitlines()
        except OSError as exc:
            report.error("cannot read {}: {}".format(path, exc))
            continue
        for lineno, line in enumerate(lines, start=1):
            match = lib.CHECKBOX_RE.match(line)
            if not match:
                continue
            tid = match.group("id")
            site = "changes/{}/tasks.md:{}".format(domain, lineno)
            if tid in md_ids:
                report.error("duplicate checkbox {} at {} (already at {})".format(
                    tid, site, md_ids[tid][0]))
                continue
            md_ids[tid] = (site, match.group("mark"))
            want = domain_for_prefix(tid.split(".")[0])
            if want != domain:
                report.error("checkbox {} in changes/{}/tasks.md belongs to {}".format(
                    tid, domain, want))
            # Bidirectional match: a tick needs a 'done' task behind it, and a
            # 'done' task needs its leaf box ticked.
            checked = match.group("mark").lower() == "x"
            status = status_by_id.get(tid)
            if checked and status != "done":
                report.error("{}: {} is checked but task-graph status is {!r} "
                             "(completion without evidence)".format(site, tid, status))
            elif not checked and status == "done":
                report.error("{}: {} is 'done' in task-graph but the leaf checkbox "
                             "is unchecked".format(site, tid))
    for tid in sorted(set(expected_ids()) - set(md_ids)):
        report.error("task {} has no leaf checkbox in any domain tasks.md".format(tid))
    for tid in sorted(set(md_ids) - set(expected_ids())):
        report.error("task {} appears in a domain tasks.md but not in task-graph".format(tid))
    report.count("checkboxes", len(md_ids))
    return md_ids


def heading_text(line):
    match = HEADING_RE.match(line)
    if not match:
        return None
    return ENUMERATOR_RE.sub("", match.group("text")).strip()


def check_orchestration(report, change_dir):
    path = os.path.join(change_dir, "ORCHESTRATION.md")
    if not os.path.isfile(path):
        report.error("ORCHESTRATION.md missing (four-layer orchestration doc)")
        return
    try:
        with open(path, "r", encoding="utf-8") as handle:
            lines = handle.read().splitlines()
    except OSError as exc:
        report.error("cannot read {}: {}".format(path, exc))
        return
    positions = {}
    for lineno, line in enumerate(lines, start=1):
        text = heading_text(line)
        if text is None:
            continue
        for keyword, key in ORCH_LAYERS:
            if keyword in text and key not in positions:
                positions[key] = lineno
    for keyword, key in ORCH_LAYERS:
        if key not in positions:
            report.error("ORCHESTRATION.md missing layer {!r} heading".format(keyword))
    ordered_keys = [key for _, key in ORCH_LAYERS if key in positions]
    ordered_lines = [positions[key] for key in ordered_keys]
    if ordered_lines != sorted(ordered_lines):
        report.error("ORCHESTRATION.md four layers are out of order (金字塔不得倒装): {}".format(
            ordered_keys))
    if "dispatch-questions" in positions:
        start = positions["dispatch-questions"]
        later = [positions[key] for key in ordered_keys if positions[key] > start]
        end = min(later) if later else len(lines) + 1
        body = "\n".join(lines[start:end - 1])
        for question in ORCH_QUESTIONS:
            if question not in body:
                report.warn("编排四问 section does not mention {!r}".format(question))
    report.count("orchestration_layers", len(positions))


def parse_spec_file(report, path, label):
    """Return (requirements, total_scenarios) for one delta spec file.

    OpenSpec delta semantics:
      * ADDED / MODIFIED requirements (and plain baseline `## Requirements`)
        carry normative SHALL/MUST prose and at least one #### Scenario;
      * REMOVED requirements carry **Reason** and **Migration** instead — a
        deletion has no behaviour to assert, so no scenario is demanded (never
        instruct a spec author to invent a fake scenario for a removed item);
      * a file with no Requirement at all, a Requirement before any section, or
        an unrecognised `## <x> Requirements` section is invalid.
    """
    try:
        with open(path, "r", encoding="utf-8") as handle:
            lines = handle.read().splitlines()
    except OSError as exc:
        report.error("cannot read {}: {}".format(path, exc))
        return [], 0
    requirements = []
    total_scenarios = 0
    kind = None
    index = 0
    while index < len(lines):
        line = lines[index]
        kind_match = lib.DELTA_KIND_RE.match(line)
        if kind_match:
            kind = kind_match.group("kind")
            index += 1
            continue
        if BASELINE_SECTION_RE.match(line):
            kind = "baseline"
            index += 1
            continue
        other = SECTION_ANY_RE.match(line)
        if other and other.group("text").rstrip().endswith("Requirements"):
            report.error("{}: unrecognised section {!r} (expected ADDED/MODIFIED/"
                         "REMOVED/RENAMED Requirements or baseline Requirements)".format(
                             label, other.group("text").strip()))
            index += 1
            continue
        req_match = lib.REQUIREMENT_RE.match(line)
        if not req_match:
            index += 1
            continue
        title = req_match.group("title")
        if kind is None:
            report.error("{}: requirement {!r} appears before any Requirements "
                         "section".format(label, title))
        body = []
        scenarios = 0
        index += 1
        while index < len(lines):
            nxt = lines[index]
            if lib.REQUIREMENT_RE.match(nxt) or lib.DELTA_KIND_RE.match(nxt) or (
                    nxt.startswith("## ") and not nxt.startswith("### ")):
                break
            if lib.SCENARIO_RE.match(nxt):
                scenarios += 1
            body.append(nxt)
            index += 1
        text = "\n".join(body)
        if kind == "REMOVED":
            if not REASON_RE.search(text):
                report.error("{}: REMOVED requirement {!r} has no **Reason**".format(label, title))
            if not MIGRATION_RE.search(text):
                report.error("{}: REMOVED requirement {!r} has no **Migration**".format(label, title))
        else:
            if not lib.SHALL_RE.search(text):
                report.error("{}: requirement {!r} has no SHALL/MUST statement".format(label, title))
            if scenarios == 0:
                report.error("{}: requirement {!r} has no #### Scenario".format(label, title))
        total_scenarios += scenarios
        requirements.append({"title": title, "kind": kind, "scenarios": scenarios})
    if not requirements:
        report.error("{}: spec declares no ### Requirement".format(label))
    return requirements, total_scenarios


def main_spec_requirement_titles(main_root, cap):
    path = os.path.join(main_root, cap, "spec.md")
    if not os.path.isfile(path):
        return None, []
    try:
        with open(path, "r", encoding="utf-8") as handle:
            lines = handle.read().splitlines()
    except OSError:
        return None, []
    titles = []
    for line in lines:
        match = lib.REQUIREMENT_RE.match(line)
        if match:
            titles.append(match.group("title"))
    return path, titles


def declaration_texts(change_dir, domain):
    texts = []
    for name in ("proposal.md", "design.md"):
        for base in (change_dir, os.path.join(change_dir, "changes", domain)):
            path = os.path.join(base, name)
            if os.path.isfile(path):
                try:
                    with open(path, "r", encoding="utf-8") as handle:
                        texts.append(handle.read())
                except OSError:
                    pass
    return texts


def check_specs(report, change_dir, main_root, domains=DOMAINS):
    spec_units = []
    for cap, path in glob_specs(os.path.join(change_dir, "specs")):
        spec_units.append((cap, path, None, "specs/{}".format(cap)))
    for domain in domains:
        for cap, path in glob_specs(os.path.join(change_dir, "changes", domain, "specs")):
            spec_units.append((cap, path, domain, "changes/{}/specs/{}".format(domain, cap)))
    report.count("spec_files", len(spec_units))
    seen_caps = {}
    for cap, _path, _domain, label in spec_units:
        if cap in seen_caps:
            report.error("capability {!r} has multiple delta writers: {} and {}".format(
                cap, seen_caps[cap], label))
        else:
            seen_caps[cap] = label
    total_requirements = 0
    total_scenarios = 0
    added = 0
    modified = 0
    removed = 0
    for cap, path, domain, label in spec_units:
        requirements, count = parse_spec_file(report, path, label)
        total_requirements += len(requirements)
        total_scenarios += count
        declarations = declaration_texts(change_dir, domain) if domain else declaration_texts(change_dir, "")
        if declarations and not any(cap in text for text in declarations):
            report.error("{}: capability {!r} is not declared in any proposal/design".format(label, cap))
        for req in requirements:
            if req["kind"] == "ADDED":
                added += 1
                # An ADDED requirement must not collide with the main source: a
                # title that already exists there is a MODIFIED, not a new one.
                main_path, titles = main_spec_requirement_titles(main_root, cap)
                if main_path is not None and req["title"] in titles:
                    report.error("{}: ADDED requirement {!r} already exists in main spec "
                                 "(collision; use MODIFIED)".format(label, req["title"]))
                continue
            if req["kind"] not in ("MODIFIED", "REMOVED", "RENAMED"):
                continue
            if req["kind"] == "MODIFIED":
                modified += 1
            elif req["kind"] == "REMOVED":
                removed += 1
            main_path, titles = main_spec_requirement_titles(main_root, cap)
            if main_path is None:
                report.error("{}: {} requirement {!r} but main spec {}/spec.md missing".format(
                    label, req["kind"], req["title"], cap))
                continue
            hits = [t for t in titles if t == req["title"]]
            if not hits:
                report.error("{}: {} requirement {!r} not present in main spec".format(
                    label, req["kind"], req["title"]))
            elif len(hits) > 1:
                report.error("{}: {} requirement {!r} is ambiguous in main spec "
                             "({} matching titles)".format(label, req["kind"], req["title"], len(hits)))
    report.count("requirements", total_requirements)
    report.count("scenarios", total_scenarios)
    report.count("added_requirements", added)
    report.count("modified_requirements", modified)
    report.count("removed_requirements", removed)
    report.note("delta structure only: semantic correctness of all main-spec requirements "
                "still requires human verification")


def check_coverage(report, change_dir, main_root):
    """核对能力映射集合与任务引用，不以标题命中冒充语义验收。"""
    path = os.path.join(change_dir, "coverage.md")
    if not os.path.isfile(path):
        report.error("coverage.md missing")
        return
    with open(path, encoding="utf-8") as handle:
        text = handle.read()
    rows = re.findall(r"^\| `([^`]+)` \| [^|]+ \| (\d{2}\.\d+) \| ([RMHD]) \|", text, re.M)
    names = [cap for cap, _, _ in rows]
    actual = {cap for cap, _ in glob_specs(main_root)}
    if len(names) != len(set(names)):
        report.error("coverage has duplicate capability rows")
    if set(names) != actual:
        report.error("coverage capability set mismatch: missing={}, extra={}".format(
            sorted(actual - set(names)), sorted(set(names) - actual)))
    for cap, tid, _ in rows:
        if tid not in expected_ids():
            report.error("coverage {} has unknown task {}".format(cap, tid))
    report.count("covered_main_capabilities", len(names))


def kahn_order(nodes, adjacency):
    """Kahn 拓扑排序；返回入序节点（长度不足即展开图有环）。"""
    indegree = {node: 0 for node in nodes}
    for node in nodes:
        for nxt in adjacency.get(node, ()):
            indegree[nxt] = indegree.get(nxt, 0) + 1
    ready = [node for node in nodes if indegree[node] == 0]
    order = []
    while ready:
        node = ready.pop()
        order.append(node)
        for nxt in adjacency.get(node, ()):
            indegree[nxt] -= 1
            if indegree[nxt] == 0:
                ready.append(nxt)
    return order


def check_event_graph(report, graph, change_name):
    """六域 event-integrity 图：叶/域/kind/依赖/verify_after 的形状与自洽。

    叶数、域分布与阶段数一律从 graph 自身读取，再从 tasks.md 反向核对；
    profile 不复制对象状态，因此“删一叶”只能在 counts/tasks.md 两侧同时
    留下可判定的差异，而不是被一个手写常量静默放过。
    """
    if graph.get("schema_version") != 1:
        report.error("task-graph schema_version must be 1, got {!r}".format(
            graph.get("schema_version")))
    if graph.get("change") != change_name:
        report.error("task-graph change {!r} != change dir name {!r}".format(
            graph.get("change"), change_name))
    if graph.get("domain_profile") != EVENT_PROFILE:
        report.error("event profile requires domain_profile {!r}, got {!r}".format(
            EVENT_PROFILE, graph.get("domain_profile")))
    leaves = graph.get("leaves")
    if not isinstance(leaves, list) or not leaves:
        report.error("task-graph leaves must be a non-empty list")
        return []
    rows = []
    for position, leaf in enumerate(leaves):
        where = "leaves[{}]".format(position)
        if not isinstance(leaf, dict):
            report.error("{} must be an object".format(where))
            continue
        leaf_id = leaf.get("id")
        if not isinstance(leaf_id, str) or not EVENT_LEAF_ID_RE.match(leaf_id):
            report.error("{} has invalid id {!r}".format(where, leaf_id))
            continue
        where = "leaf {}".format(leaf_id)
        domain = leaf.get("domain")
        if domain not in EVENT_DOMAINS:
            report.error("{} domain {!r} is not one of the six event domains".format(
                where, domain))
        elif not domain.startswith(leaf_id.split(".", 1)[0] + "-"):
            report.error("{} belongs to prefix {} but domain is {!r}".format(
                where, leaf_id.split(".", 1)[0], domain))
        kind = leaf.get("kind")
        if kind not in EVENT_LEAF_KINDS:
            report.error("{} kind {!r} not in {}".format(where, kind, EVENT_LEAF_KINDS))
        depends_on = leaf.get("depends_on")
        if not isinstance(depends_on, list) or any(
                not isinstance(dep, str) for dep in depends_on):
            report.error("{} depends_on must be a list of leaf-id strings".format(where))
            depends_on = []
        verify_after = leaf.get("verify_after")
        if not isinstance(verify_after, list) or any(
                not isinstance(dep, str) for dep in verify_after):
            report.error("{} verify_after must be a list of leaf-id strings".format(where))
            verify_after = []
        status = leaf.get("status")
        if status not in STATUSES:
            report.error("{} status {!r} not in {}".format(where, status, STATUSES))
        elif status == "done":
            check_done_evidence(report, where, leaf.get("evidence"))
        verification = leaf.get("verification")
        if not isinstance(verification, dict):
            report.error("{} verification must be an object".format(where))
        else:
            commands = verification.get("commands")
            if (not isinstance(commands, list) or not commands
                    or any(not isinstance(item, str) or not item.strip()
                           for item in commands)):
                report.error("{} verification.commands must be a non-empty string list".format(
                    where))
        task_file = leaf.get("task_file")
        if not isinstance(task_file, str) or not task_file.strip():
            report.error("{} task_file must be a non-empty string".format(where))
        elif not isinstance(domain, str) or not task_file.endswith(
                "changes/{}/tasks.md".format(domain)):
            report.error("{} task_file {!r} does not point at changes/{}/tasks.md".format(
                where, task_file, domain))
        rows.append({"id": leaf_id, "domain": domain, "kind": kind,
                     "depends_on": depends_on, "verify_after": verify_after,
                     "status": status})
    ids = [row["id"] for row in rows]
    duplicates = sorted({leaf_id for leaf_id in ids if ids.count(leaf_id) > 1})
    for leaf_id in duplicates:
        report.error("duplicate leaf id in graph: {}".format(leaf_id))
    known = set(ids)
    for row in rows:
        where = "leaf {}".format(row["id"])
        if len(row["depends_on"]) != len(set(row["depends_on"])):
            report.error("{} has duplicate depends_on".format(where))
        if len(row["verify_after"]) != len(set(row["verify_after"])):
            report.error("{} has duplicate verify_after".format(where))
        for field in ("depends_on", "verify_after"):
            for dep in row[field]:
                if dep == row["id"]:
                    report.error("{} {} contains itself".format(where, field))
                elif dep not in known:
                    report.error("{} {} has unknown leaf {}".format(where, field, dep))
    # 六域齐全且分布与 counts 一致。
    actual_by_domain = {}
    for row in rows:
        actual_by_domain[row["domain"]] = actual_by_domain.get(row["domain"], 0) + 1
    for domain in EVENT_DOMAINS:
        if not actual_by_domain.get(domain):
            report.error("event domain {} has no leaf in task-graph".format(domain))
    for domain in sorted(set(actual_by_domain) - set(EVENT_DOMAINS)):
        report.error("task-graph leaf domain {!r} is not a declared event domain".format(domain))
    counts = graph.get("counts") if isinstance(graph.get("counts"), dict) else {}
    if counts.get("leaves_total") != len(rows):
        report.error("task-graph counts.leaves_total={} does not match the leaf list length {}".format(
            counts.get("leaves_total"), len(rows)))
    declared_by_domain = counts.get("leaves_by_domain")
    if not isinstance(declared_by_domain, dict):
        report.error("task-graph counts.leaves_by_domain must be an object")
    else:
        for domain in sorted(set(declared_by_domain) | set(actual_by_domain)):
            if declared_by_domain.get(domain) != actual_by_domain.get(domain):
                report.error("counts.leaves_by_domain[{!r}]={} does not match the derived {}".format(
                    domain, declared_by_domain.get(domain), actual_by_domain.get(domain)))
    declared_by_kind = counts.get("leaves_by_kind")
    if isinstance(declared_by_kind, dict):
        actual_by_kind = {}
        for row in rows:
            actual_by_kind[row["kind"]] = actual_by_kind.get(row["kind"], 0) + 1
        for kind in sorted(set(declared_by_kind) | set(actual_by_kind)):
            if declared_by_kind.get(kind) != actual_by_kind.get(kind):
                report.error("counts.leaves_by_kind[{!r}]={} does not match the derived {}".format(
                    kind, declared_by_kind.get(kind), actual_by_kind.get(kind)))
    report.count("graph_leaves", len(ids))
    report.count("graph_leaf_ids_unique", len(set(ids)))
    report.count("graph_edges", sum(len(row["depends_on"]) for row in rows))
    report.count("graph_verify_after_edges", sum(len(row["verify_after"]) for row in rows))
    report.extra["leaves_by_domain"] = {domain: actual_by_domain.get(domain, 0)
                                        for domain in EVENT_DOMAINS}
    return rows


def check_event_phase_graph(report, graph, rows):
    """单一展开图（每叶 ready/verified）必须整体无环，且与声明计数一致。"""
    nodes = []
    for row in rows:
        nodes.append(row["id"] + "/ready")
        nodes.append(row["id"] + "/verified")
    known = {row["id"] for row in rows}
    adjacency = {node: [] for node in nodes}
    for row in rows:
        leaf_id = row["id"]
        adjacency[leaf_id + "/ready"] = [dep + "/ready" for dep in row["depends_on"]
                                         if dep in known]
        adjacency[leaf_id + "/verified"] = [leaf_id + "/ready"] + [
            dep + "/verified" for dep in row["verify_after"] if dep in known]
    edges = sum(len(successors) for successors in adjacency.values())
    order = kahn_order(nodes, adjacency)
    if len(order) != len(nodes):
        report.error("event phase graph has a cycle: only {}/{} nodes sort; schedule is not acyclic"
                     .format(len(order), len(nodes)))
    declared = graph.get("phase_graph")
    if isinstance(declared, dict):
        if declared.get("nodes_total") != len(nodes):
            report.error("phase_graph.nodes_total={} does not match the derived {}".format(
                declared.get("nodes_total"), len(nodes)))
        if declared.get("edges_total") != edges:
            report.error("phase_graph.edges_total={} does not match the derived {}".format(
                declared.get("edges_total"), edges))
        if declared.get("acyclic") is not True:
            report.error("phase_graph.acyclic must be true (a cached false is not evidence)")
        declared_order = declared.get("topological_order")
        if isinstance(declared_order, list) and len(declared_order) != len(nodes):
            report.error("phase_graph.topological_order holds {} nodes, expected {}".format(
                len(declared_order), len(nodes)))
        elif declared_order is not None and not isinstance(declared_order, list):
            report.error("phase_graph.topological_order must be a list when present")
    report.count("phase_nodes", len(nodes))
    report.count("phase_edges", edges)
    report.count("phase_acyclic", 1 if len(order) == len(nodes) else 0)
    return len(order) == len(nodes)


def check_event_checkboxes(report, change_dir, rows, graph):
    """tasks.md 叶 checkbox 与 graph 叶双向一致。

    graph 声明 scope=whole-change-planning-materialization 时，叶 status 是规划态
    （planned=图未记录完成），tasks.md 勾选是执行记录，两者分开。该分开只解除
    “planned ↔ 未勾选”的强制，不放宽 done/blocked 两个强方向：
      * done 必须勾选，且必须携带 run_id/commands/exit_codes/artifact_paths；
      * blocked 不得勾选；
      * 非规划态图仍按旧规则要求勾选必有 done 背书。
    未勾选叶逐一列入 extra，不以总数掩盖具体 id。
    """
    by_id = {row["id"]: row for row in rows}
    planning_only = graph.get("scope") in PLANNING_ONLY_SCOPES
    md_marks = {}
    for domain in EVENT_DOMAINS:
        path = os.path.join(change_dir, "changes", domain, "tasks.md")
        if not os.path.isfile(path):
            continue
        try:
            with open(path, "r", encoding="utf-8") as handle:
                lines = handle.read().splitlines()
        except OSError as exc:
            report.error("cannot read {}: {}".format(path, exc))
            continue
        for lineno, line in enumerate(lines, start=1):
            match = lib.CHECKBOX_RE.match(line)
            if not match:
                continue
            leaf_id = match.group("id")
            site = "changes/{}/tasks.md:{}".format(domain, lineno)
            if leaf_id in md_marks:
                report.error("duplicate checkbox {} at {} (already at {})".format(
                    leaf_id, site, md_marks[leaf_id][0]))
                continue
            md_marks[leaf_id] = (site, match.group("mark").lower() == "x")
            row = by_id.get(leaf_id)
            if row is None:
                report.error("checkbox {} in changes/{}/tasks.md is not a task-graph leaf".format(
                    leaf_id, domain))
            elif row["domain"] != domain:
                report.error("checkbox {} in changes/{}/tasks.md belongs to {}".format(
                    leaf_id, domain, row["domain"]))
    for leaf_id in sorted(set(by_id) - set(md_marks)):
        report.error("leaf {} has no checkbox in any domain tasks.md".format(leaf_id))
    for leaf_id in sorted(set(md_marks) - set(by_id)):
        report.error("checkbox {} has no leaf in task-graph".format(leaf_id))
    checked = []
    unchecked = []
    for leaf_id, (_site, ticked) in sorted(md_marks.items()):
        row = by_id.get(leaf_id)
        if row is None:
            continue
        if ticked:
            checked.append(leaf_id)
        else:
            unchecked.append(leaf_id)
        status = row["status"]
        if status == "done" and not ticked:
            report.error("leaf {} is 'done' in task-graph but the leaf checkbox is unchecked".format(
                leaf_id))
        elif status == "blocked" and ticked:
            report.error("leaf {} is 'blocked' in task-graph but its checkbox is ticked".format(
                leaf_id))
        elif ticked and status != "done" and not planning_only:
            report.error("leaf {} is checked but task-graph status is {!r} "
                         "(completion without evidence)".format(leaf_id, status))
    report.count("checkboxes", len(md_marks))
    report.count("leaves_checked", len(checked))
    report.count("leaves_unchecked", len(unchecked))
    report.count("leaves_graph_done", sum(1 for row in rows if row["status"] == "done"))
    report.extra["checked_leaf_ids"] = sorted(checked)
    report.extra["unchecked_leaf_ids"] = sorted(unchecked)
    report.extra["leaf_status_semantics"] = (
        "planning-only scope: graph status is the planning state, the tasks.md checkbox is the "
        "execution record; done/blocked stay hard" if planning_only else
        "strict: a ticked leaf must be 'done' in task-graph with evidence")
    if unchecked:
        report.note("{} leaf/leaves remain unchecked: {}".format(
            len(unchecked), ", ".join(sorted(unchecked))))
    return planning_only


def check_event_coverage_doc(report, change_dir):
    """event 图的 coverage.md 是说明/映射文档（非旧十域能力表），只判存在与非空。"""
    path = os.path.join(change_dir, "coverage.md")
    if not os.path.isfile(path):
        report.error("coverage.md missing (event graph leaf/test/scenario mapping)")
        return
    try:
        with open(path, "r", encoding="utf-8") as handle:
            text = handle.read()
    except OSError as exc:
        report.error("cannot read {}: {}".format(path, exc))
        return
    if not text.strip():
        report.error("coverage.md is empty")


def run_event_profile(report, graph, change_dir, change_name, main_root, as_json):
    rows = check_event_graph(report, graph, change_name)
    check_event_phase_graph(report, graph, rows)
    check_artifacts(report, change_dir, EVENT_DOMAINS)
    check_orchestration(report, change_dir)
    check_event_checkboxes(report, change_dir, rows, graph)
    check_specs(report, change_dir, main_root, EVENT_DOMAINS)
    check_event_coverage_doc(report, change_dir)
    report.note("domain_profile {!r}: leaf counts/ranges are read from the graph and cross-checked "
                "against the six domain tasks.md files; the legacy ten-domain constants are "
                "neither used nor relaxed on this path".format(EVENT_PROFILE))
    return report.emit(as_json)


def run_legacy_profile(report, graph, change_dir, change_name, args, main_root):
    tasks = check_graph_shape(report, graph, change_name)
    check_dependency_graph(report, tasks)
    raw_tasks = graph.get("tasks") if isinstance(graph.get("tasks"), list) else []
    check_completion_test_anchors(report, raw_tasks, index_repo_test_funcs(args.repo_root))
    check_artifacts(report, change_dir)
    check_orchestration(report, change_dir)
    collect_checkbox_ids(report, change_dir, {t["id"]: t.get("status") for t in tasks})
    check_specs(report, change_dir, main_root)
    check_coverage(report, change_dir, main_root)
    return report.emit(args.json)


def run(argv):
    args = parse_args(argv)
    change_dir = os.path.abspath(args.change_dir)
    change_name = os.path.basename(change_dir.rstrip(os.sep))
    graph_path = args.plan_graph or os.path.join(change_dir, "task-graph.json")
    main_root = args.main_specs_root or os.path.normpath(
        os.path.join(change_dir, os.pardir, os.pardir, "specs"))
    main_root = os.path.abspath(main_root)
    report = lib.Report("check_runtime_plan", change_name)
    if not os.path.isdir(change_dir):
        report.error("change dir not found: {}".format(change_dir))
        return report.emit(args.json)
    report.note("main specs root: {}".format(main_root))
    try:
        graph = lib.load_json_object(graph_path)
    except lib.CheckFailure as exc:
        report.error(str(exc))
        return report.emit(args.json)
    profile = graph.get("domain_profile")
    if profile in (None, ""):
        return run_legacy_profile(report, graph, change_dir, change_name, args, main_root)
    if profile != EVENT_PROFILE:
        report.error("unknown domain_profile {!r}; supported profiles are the legacy "
                     "ten-domain graph (no field) and {!r}".format(profile, EVENT_PROFILE))
        return report.emit(args.json)
    return run_event_profile(report, graph, change_dir, change_name, main_root, args.json)


if __name__ == "__main__":
    sys.exit(run(sys.argv[1:]))
