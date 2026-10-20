#!/usr/bin/env python3
"""check_concept_budget.py — 概念预算静态上限（S1–S4），不是产品验证。

判定对象与口径（与 openspec/changes/runtime-rebuild/concept-slim-plan.md §一逐条对应）：

* S1 iface-methods：多态接口 ≤2 方法；消费者侧窄口 ≤3 方法。「多态」由静态实现者
  计数判定——某接口的方法名集合（同包嵌入接口展开后）被 ≥2 个一方接收者类型完整
  覆盖即视为多态端口，否则视为消费者侧窄口。
* S2 struct-fields：导出结构 ≤5 个导出字段。只数结构体顶层的导出字段名：嵌入类型
  不算、未导出字段不算、多行函数类型算一个字段。
* S3 ctx-bare：**调用点**裸 `ctx.Value(` = 0；跨缝关系入签名或类型化 accessor（accessor 实现体的单次 ctx.Value 是合法落点，按文件具名登记）。按文件计数，
  baseline 中点名的类型化 accessor 实现是唯一合法落点（仍然 warn，不静默）。
* S4 contract-surface：每个导出接口都要有 canonical 范例文件
  `examples/contract/<pkg_slug>/<Iface>_example.go`（含 `func Example<Iface>()`）
  与 Godoc 红线清单（`// MUST ...` / `// MUST NOT ...` 合计 ≤6 条，且 doc 块出现
  「红线」标记）。pkg_slug：根包为 `root`，其余把 `/` 换成 `_`（接口重名跨包存在，
  故按包分目录）。

棘轮（只降不升）：现状大面积超标，故 BASELINE 是文件内显式债务清单，每行
  ("<rule>", "<file>:<Symbol>", <observed>)，observed 为登记时的实测值。
  * 命中豁免 → warn（禁静默放行），计入 counts.baseline_exempted；
  * 实测 > observed → error（棘轮：只降不升）；
  * 当前不再超标 / 符号消失 → warn「stale，删除该行」。
  S4 的豁免是布尔（observed=None）：范例与红线都补齐后该行即 stale。

规则可单独运行（--rule S1|S2|S3|S4，或规则名），下游收敛叶按规则对账。

局限（如实登记，不冒充 type-check）：
  * 纯正则＋括号匹配的静态解析，不做 Go 类型检查；方法集按名字匹配，跨包同名类型
    可能虚增实现者数——偏差方向只会让接口更易判为「多态」而更严格，不会放宽；
  * 只扫 golden/runtime/architecture.json 登记的一方包（未登记的新包由
    check_runtime_architecture.py 拦），跳过 *_test.go（测试不是契约面）；
  * 不判语义：字段是否真该拆、红线是否切题仍需评审。

productVerified 恒为 null：静态上限通过不等于产品成立。

用法：
  python3 scripts/check_concept_budget.py [--rule S1] [--json]
  python3 scripts/check_concept_budget.py --self-check
退出码：0=通过（含豁免 warn）；1=有 error 或输入不可用。
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys
import tempfile

import runtime_checklib as lib

# ---- 标尺常数（与 concept-slim-plan.md §一 一致；改这里等于改验收面）----
POLYMORPHIC_METHOD_LIMIT = 2
CONSUMER_METHOD_LIMIT = 3
STRUCT_FIELD_LIMIT = 5
CTX_VALUE_LIMIT = 0
REDLINE_LIMIT = 6
# 实现者数达到此值即视为「多态接口」（S1 用严口径）。
POLYMORPHIC_MIN_IMPLEMENTERS = 2
EXAMPLE_ROOT = "examples/contract"
REDLINE_MARKERS = ("红线", "redline", "REDLINE")

RULE_IFACE = "iface-methods"
RULE_STRUCT = "struct-fields"
RULE_CTX = "ctx-bare"
RULE_CONTRACT = "contract-surface"
RULES = (RULE_IFACE, RULE_STRUCT, RULE_CTX, RULE_CONTRACT)
RULE_LABELS = {
    RULE_IFACE: "S1",
    RULE_STRUCT: "S2",
    RULE_CTX: "S3",
    RULE_CONTRACT: "S4",
}
RULE_ALIASES = {label: rule for rule, label in RULE_LABELS.items()}
NUMERIC_RULES = (RULE_IFACE, RULE_STRUCT, RULE_CTX)

# ---- 债务清单（baseline）：现状超标件，只降不升；每清一户删一行 ----
# 生成口径：本检查器对当前树的实测值。S1 的 observed 是接口方法数，S2 是导出字段数，
# S3 是该文件的裸 ctx.Value 调用数；S4 是布尔债务（observed=None）。
#
# 键一律是**当前树路径**：包迁移（query/→memory/query/、agent/→runtime/agent/、
# state/→runtime/state/、task/→runtime/task/、event/→api/event/、config/→api/config/、
# governance/→evolution/governance/、host_bridge.go→cli/host_face.go、diagnostics.go→host_face.go）只做键正名，
# observed 不动——路径对齐不是豁免扩张。已清零的条目按棘轮生命周期删除，不得回填。
BASELINE = (
    ("struct-fields", "runtime/agent/delegate.go:Delegation", 9),
    ("struct-fields", "runtime/task/spec.go:Spec", 9),
    ("struct-fields", "tool/subagent/builder.go:Builder", 7),
    ("iface-methods", "tool/recall/table.go:Table", 3),
    ("struct-fields", "memory/replay/age_segments.go:Segment", 6),
    ("struct-fields", "memory/replay/age_segments.go:AgeFoldReport", 7),
    ("struct-fields", "runtime/task/registry.go:Obligations", 6),
    ("struct-fields", "tool/subagent/target.go:TargetOptions", 8),
    # S1 iface-methods — 10 项（observed=实测接口方法数）
    ("iface-methods", "runtime/agent/delegate.go:DelegationTasks", 4),
    ("iface-methods", "runtime/agent/loop.go:Recorder", 3),
    ("iface-methods", "rl/produce.go:Recorder", 3),
    ("iface-methods", "runtime/state/durable.go:FactLog", 4),
    ("iface-methods", "runtime/state/store.go:Store", 4),
    ("iface-methods", "runtime/task/registry.go:Manager", 4),
    ("iface-methods", "runtime/task/registry.go:Registry", 4),
    ("iface-methods", "tool/exec/stop.go:Executor", 4),
    ("iface-methods", "tool/mcp/adapter.go:Registry", 5),
    ("iface-methods", "memory/replica/cleaner.go:Cleaner", 5),
    # S2 struct-fields — 43 项（observed=实测导出字段数）
    ("struct-fields", "runtime/agent/delegate.go:Delegation", 9),
    ("struct-fields", "tool/subagent/subagent.go:Definition", 7),
    # Declaration 6f（声明面本体）：组织声明=入口/代理/引用/基线/MCP 服务器代际/原始文档，MCP 面随 mcp_servers 声明下沉各成一格，拆开会让声明面出现影子结构。
    ("struct-fields", "api/config/config.go:Declaration", 6),
    # AgentSpec 15f（声明面本体）：单个 agent 的声明格＝核心（身份/模型/引用/参数/预算/请求开关）＋ memory/meditation/file/exec 四个域段平铺在同一格；段是声明字段，不是影子结构，拆开会让声明面重新长出机制层。
    ("struct-fields", "api/config/config.go:AgentSpec", 15),
    # MeditationSpec 6f：冥想运行声明本体，六个格分别是启用、节奏两格、提示、观察面与回流白名单，字段即声明形状。
    ("struct-fields", "api/config/config.go:MeditationSpec", 6),
    # event.ParsedMeta 9f（00.25 具名登记）：ParseEventMeta 的解析结果本体＝数据面，
    # 七个冻结键各自成一格，前缀透传与未建模条目必须原样留存（PassThrough/Unknown）——
    # 字段即被解析元数据的原样形状，瘦身会丢掉“不丢条目”的保真承诺，故不瘦身。
    ("struct-fields", "api/event/meta.go:ParsedMeta", 9),
    # event.Event 16f（S2 类别豁免先例，具名）：事件事实本体＝数据面，16 个字段是事件
    # 身份/内容/归属/账目四块的数据语义（序列化保真面），不拆构造、不拆块；可读性由
    # api/event/types.go 的语义块文档分组承担。
    ("struct-fields", "api/event/types.go:Event", 16),
    ("struct-fields", "evolution/posterior.go:Evaluation", 6),
    ("struct-fields", "cli/host_face.go:ApprovalDecision", 8),
    ("struct-fields", "memory/event_body.go:EventBody", 8),
    ("struct-fields", "memory/replay/projection.go:ChainFact", 6),
    ("struct-fields", "memory/replay/projection.go:RebuildReport", 9),
    ("struct-fields", "memory/query_events.go:EventRecord", 7),
    ("struct-fields", "memory/query_events.go:QueryOptions", 9),
    ("struct-fields", "memory/replay.go:DeliveryRecord", 8),
    ("struct-fields", "memory/search/port.go:Query", 9),
    ("struct-fields", "memory/ledger/segment_store.go:SegmentRecord", 6),
    ("struct-fields", "evolution/governance/denial.go:DenialRecord", 9),
    ("struct-fields", "evolution/governance/goal.go:Goal", 6),
    ("struct-fields", "memory/query/inventory.go:InventoryCounts", 6),
    ("struct-fields", "memory/query/inventory.go:NoticeEntry", 9),
    ("struct-fields", "memory/query/inventory.go:SessionEntry", 6),
    ("struct-fields", "memory/query/snapshot.go:DetailState", 6),
    ("struct-fields", "memory/query/subscribe.go:Update", 6),
    ("struct-fields", "memory/query/types.go:ObjectRef", 7),
    ("struct-fields", "rl/capture.go:CaptureManifest", 9),
    ("struct-fields", "rl/capture.go:RequestSnapshot", 11),
    # 数据保真面（记录 schema＝序列化载体，字段为数据语义本体非配置选择面——
    # S2 类别豁免，同 event.Event 16f 先例）。装配期配置/选择面不入本块：配置面
    # 零债硬拦。以下逐项具名引入叶与保真理由。
    # 06.8 加性 ModelVersion：替换记录携带版本归属通路的证据。
    ("struct-fields", "rl/replacement.go:ReplacementRecord", 6),
    # 08.4 新产数据面（诊断读出报告＝数据保真面非配置选择面，S2 类别豁免；
    # 引入叶 08.4——七段读出是报告语义本体，同 Explanation 先例）
    ("struct-fields", "host_face.go:DiagnosticReport", 7),
    ("struct-fields", "rl/record.go:AttemptRecord", 12),
    ("struct-fields", "rl/record.go:DecisionRecord", 7),
    ("struct-fields", "rl/record.go:EpisodeRecord", 9),
    ("struct-fields", "rl/record.go:ModelViewRef", 8),
    # 06.3 采集账目（引入叶）：准入/拒收/写错/在飞四类计数是丢失账本体，
    # Dropped() 由分类计数合成，拆块即审计面需要二次归并。
    ("struct-fields", "rl/sink.go:CaptureLedger", 8),
    ("struct-fields", "runtime/state/resources.go:SkillLoad", 6),
    ("struct-fields", "runtime/state/session.go:MailboxEntry", 6),
    ("struct-fields", "runtime/task/spec.go:Spec", 9),
    ("struct-fields", "runtime/task/wrapper.go:Result", 6),
    # 04.2 执行资源解释（引入叶）：租约/续期/停滞/义务的只读投影，字段即
    # Manager.Explain 的回复本体，拆块即解释面需要二次装配。
    ("struct-fields", "tool/exec/exec.go:Explanation", 10),
    # S3 ctx-bare — accessor 落点具名登记（调用点裸读=0；实现体单次 ctx.Value=合法）
    ("ctx-bare", "evolution/governance/policy.go:*", 1),   # CallPassFrom：派发→受理 pass 传输（锁定工具端口无第二通道）
    ("ctx-bare", "runtime/task/wrapper.go:*", 1),    # CallFrameFrom：per-call 身份经端口唯一通道
    # S4 contract-surface 已出清（叶 00.24）：全部导出接口的 canonical 范例与 Godoc
    # 红线均已落地，本表不再登记 S4 债务。
)
# ---- Go 源码静态扫描（纯 stdlib；不做类型检查，见模块 docstring 的局限）----
IFACE_DECL_RE = re.compile(r"^type\s+(?P<name>[A-Za-z_]\w*)\s+interface\s*\{", re.M)
STRUCT_DECL_RE = re.compile(r"^type\s+(?P<name>[A-Za-z_]\w*)\s+struct\s*\{", re.M)
METHOD_DECL_RE = re.compile(
    r"^func\s*\(\s*(?:[A-Za-z_]\w*\s+)?\*?(?P<recv>[A-Za-z_]\w*)"
    r"(?:\s*\[[^\]]*\])?\s*\)\s*(?P<name>[A-Za-z_]\w*)\s*\(",
    re.M,
)
CTX_VALUE_RE = re.compile(r"\bctx\.Value\(")
IFACE_METHOD_RE = re.compile(r"^(?P<name>[A-Za-z_]\w*)\s*\(")
LOCAL_EMBED_RE = re.compile(r"^(?P<name>[A-Za-z_]\w*)(?:\[[^\]]*\])?$")
FOREIGN_EMBED_RE = re.compile(r"^(?P<pkg>[A-Za-z_]\w*)\.(?P<name>[A-Za-z_]\w*)(?:\[[^\]]*\])?$")
FIELD_RE = re.compile(
    r"^(?P<names>[A-Za-z_]\w*(?:\s*,\s*[A-Za-z_]\w*)*)\s+(?P<rest>\S.*)$", re.S
)
REDLINE_MUST_NOT_RE = re.compile(r"^//\s*MUST\s+NOT\b")
REDLINE_MUST_RE = re.compile(r"^//\s*MUST\b")
EXAMPLE_FUNC_RE = "func Example{name}("


def strip_go(text):
    """返回 (no_comments, code)：注释置空；code 另把字符串/字符面量置空。

    两版都与原文等长（换行保留、字符位置不变），因此行号/括号位置可互相对齐；
    括号匹配只在 code 上做，字段/方法名读取在 no_comments 上做。
    """
    size = len(text)
    no_comments = list(text)
    code = list(text)

    def blank(chars, start, end):
        for pos in range(start, min(end, size)):
            if chars[pos] != "\n":
                chars[pos] = " "

    index = 0
    while index < size:
        char = text[index]
        if char in "\"'`":
            quote = char
            end = index + 1
            while end < size:
                if quote != "`" and text[end] == "\\":
                    end += 2
                    continue
                if text[end] == quote:
                    end += 1
                    break
                end += 1
            blank(code, index, end)
            index = end
            continue
        if char == "/" and index + 1 < size and text[index + 1] == "/":
            end = text.find("\n", index)
            end = size if end < 0 else end
            blank(no_comments, index, end)
            blank(code, index, end)
            index = end
            continue
        if char == "/" and index + 1 < size and text[index + 1] == "*":
            end = text.find("*/", index + 2)
            end = size if end < 0 else end + 2
            blank(no_comments, index, end)
            blank(code, index, end)
            index = end
            continue
        index += 1
    return "".join(no_comments), "".join(code)


def brace_span(code, open_index):
    """返回与 `{` 配对的 `}` 之后的位置；不配对时返回文本末尾。"""
    depth = 0
    for pos in range(open_index, len(code)):
        char = code[pos]
        if char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                return pos + 1
    return len(code)


def split_items(block):
    """按顶层换行/分号切分接口或结构体体；括号、花括号内的换行不切。

    多行方法签名（`Foo(\\n  a int,\\n) error`）因此仍是一个 item。
    """
    items = []
    buffer = []
    paren = 0
    brace = 0
    for char in block:
        if char == "(":
            paren += 1
        elif char == ")":
            paren = max(0, paren - 1)
        elif char == "{":
            brace += 1
        elif char == "}":
            brace = max(0, brace - 1)
        if char in "\n;" and paren == 0 and brace == 0:
            item = "".join(buffer).strip()
            if item:
                items.append(item)
            buffer = []
            continue
        buffer.append(char)
    item = "".join(buffer).strip()
    if item:
        items.append(item)
    return items


def stripped_line_at(code, offset):
    return code.count("\n", 0, offset) + 1


def doc_block(lines, decl_line):
    """紧贴在声明上方（中间不空行）的 `//` 注释块；空行或代码即中断。"""
    collected = []
    cursor = decl_line - 2
    while cursor >= 0:
        text = lines[cursor].strip()
        if not text.startswith("//"):
            break
        collected.append(text)
        cursor -= 1
    return list(reversed(collected))


def parse_interface_items(items):
    """把接口体切成方法名与嵌入项（同包/外包）。"""
    methods = []
    local_embeds = []
    foreign_embeds = []
    for item in items:
        method = IFACE_METHOD_RE.match(item)
        if method:
            methods.append(method.group("name"))
            continue
        local = LOCAL_EMBED_RE.match(item)
        if local:
            local_embeds.append(local.group("name"))
            continue
        foreign = FOREIGN_EMBED_RE.match(item)
        if foreign:
            foreign_embeds.append(item)
    return methods, local_embeds, foreign_embeds


def exported_field_names(item):
    """一个结构体顶层 item 里的导出字段名；嵌入字段与未导出字段不计。"""
    match = FIELD_RE.match(item.strip())
    if not match:
        return []
    names = [part.strip() for part in match.group("names").split(",")]
    return [name for name in names if name[:1].isupper()]


def scan_file(rel_path, text):
    """解析一个 Go 文件里的接口/结构体/方法接收者，返回三元组。"""
    no_comments, code = strip_go(text)
    original_lines = text.splitlines()
    interfaces = []
    structs = []
    for match in IFACE_DECL_RE.finditer(code):
        open_index = code.index("{", match.end() - 1)
        end = brace_span(code, open_index)
        body = no_comments[open_index + 1:end - 1]
        methods, local_embeds, foreign_embeds = parse_interface_items(split_items(body))
        line = stripped_line_at(code, match.start())
        interfaces.append({
            "file": rel_path,
            "pkg": os.path.dirname(rel_path) or ".",
            "line": line,
            "name": match.group("name"),
            "methods": methods,
            "local_embeds": local_embeds,
            "foreign_embeds": foreign_embeds,
            "doc": doc_block(original_lines, line),
            "key": "{}:{}".format(rel_path, match.group("name")),
        })
    for match in STRUCT_DECL_RE.finditer(code):
        open_index = code.index("{", match.end() - 1)
        end = brace_span(code, open_index)
        body = no_comments[open_index + 1:end - 1]
        fields = []
        for item in split_items(body):
            fields.extend(exported_field_names(item))
        structs.append({
            "file": rel_path,
            "pkg": os.path.dirname(rel_path) or ".",
            "line": stripped_line_at(code, match.start()),
            "name": match.group("name"),
            "fields": fields,
            "key": "{}:{}".format(rel_path, match.group("name")),
        })
    receivers = {}
    for match in METHOD_DECL_RE.finditer(code):
        receivers.setdefault(match.group("recv"), set()).add(match.group("name"))
    return interfaces, structs, receivers


def pattern_matches(pattern, pkg):
    """与 check_runtime_architecture 同口径：精确包路径，或 `^...$` 锚定正则。"""
    if pattern == pkg:
        return True
    if pattern.startswith("^") and pattern.endswith("$"):
        return re.match(pattern, pkg) is not None
    return False


def load_policy(arch_path):
    policy = lib.load_json_object(arch_path)
    raw = policy.get("packages")
    if not isinstance(raw, list):
        raise lib.CheckFailure("{}: packages must be a list".format(arch_path))
    patterns = [item.get("pattern") for item in raw
                if isinstance(item, dict) and isinstance(item.get("pattern"), str)]
    if not patterns:
        raise lib.CheckFailure("{}: no package patterns declared".format(arch_path))
    scope = policy.get("scope") if isinstance(policy.get("scope"), dict) else {}
    excludes = scope.get("exclude_dir_names")
    if not isinstance(excludes, list):
        excludes = []
    return patterns, {name for name in excludes if isinstance(name, str)}


def in_scope(rel_path, excludes):
    return not any(segment in excludes for segment in rel_path.split("/"))


def collect_sources(repo_root, arch_path):
    """返回 (sources, metrics)：只含政策登记且非排除目录下的非测试 Go 文件。"""
    if not os.path.isdir(repo_root):
        raise lib.CheckFailure("repo root not found: {}".format(repo_root))
    patterns, excludes = load_policy(arch_path)
    sources = []
    skipped_unregistered = set()
    for current, dirs, files in os.walk(repo_root):
        dirs[:] = sorted(name for name in dirs
                         if not name.startswith(".") and name not in excludes)
        for name in sorted(files):
            if not name.endswith(".go") or name.endswith("_test.go"):
                continue
            absolute = os.path.join(current, name)
            rel_path = os.path.relpath(absolute, repo_root).replace(os.sep, "/")
            if not in_scope(rel_path, excludes):
                continue
            pkg = os.path.dirname(rel_path) or "."
            if not any(pattern_matches(pattern, pkg) for pattern in patterns):
                skipped_unregistered.add(pkg)
                continue
            try:
                with open(absolute, "r", encoding="utf-8") as handle:
                    text = handle.read()
            except OSError as exc:
                raise lib.CheckFailure("cannot read {}: {}".format(rel_path, exc))
            sources.append((rel_path, pkg, text))
    if not sources:
        raise lib.CheckFailure(
            "no registered first-party Go sources under {}: implementation not present".format(
                repo_root))
    metrics = {
        "files": len(sources),
        "packages": len({pkg for _rel, pkg, _text in sources}),
        "skipped_unregistered_packages": sorted(skipped_unregistered),
    }
    return sources, metrics


def build_index(sources):
    index = {"interfaces": [], "structs": [], "receivers": {}, "sources": sources}
    for rel_path, _pkg, text in sources:
        interfaces, structs, receivers = scan_file(rel_path, text)
        index["interfaces"].extend(interfaces)
        index["structs"].extend(structs)
        for recv, names in receivers.items():
            index["receivers"].setdefault(recv, set()).update(names)
    return index


def resolved_methods(iface, by_name, cache, stack=()):
    """展开同包嵌入接口后的方法名集合；环或外包嵌入已在上游报出。"""
    if iface["key"] in cache:
        return cache[iface["key"]]
    if iface["key"] in stack:
        return []
    methods = list(iface["methods"])
    for embed in iface["local_embeds"]:
        for candidate in by_name.get(embed, ()):
            if candidate["pkg"] == iface["pkg"]:
                methods.extend(resolved_methods(candidate, by_name, cache, stack + (iface["key"],)))
    cache[iface["key"]] = methods
    return methods


def implementer_count(methods, receivers):
    if not methods:
        return 0
    need = set(methods)
    return sum(1 for have in receivers.values() if need <= have)


def rule_iface_methods(index):
    """S1：多态接口 ≤2 方法；消费者侧窄口 ≤3 方法。"""
    findings = []
    by_name = {}
    for iface in index["interfaces"]:
        by_name.setdefault(iface["name"], []).append(iface)
    cache = {}
    for iface in index["interfaces"]:
        methods = resolved_methods(iface, by_name, cache)
        count = len(methods)
        implementers = implementer_count(methods, index["receivers"])
        polymorphic = implementers >= POLYMORPHIC_MIN_IMPLEMENTERS
        limit = POLYMORPHIC_METHOD_LIMIT if polymorphic else CONSUMER_METHOD_LIMIT
        if count <= limit:
            continue
        kind = "polymorphic port" if polymorphic else "consumer-side narrow port"
        detail = "{} methods > {} ({}; {} implementer(s))".format(
            count, limit, kind, implementers)
        if iface["foreign_embeds"]:
            detail += "; foreign embeds counted as 1 each: {}".format(
                ", ".join(iface["foreign_embeds"]))
        findings.append({
            "rule": RULE_IFACE,
            "key": iface["key"],
            "file": iface["file"],
            "line": iface["line"],
            "value": count,
            "detail": detail,
        })
    return findings


def rule_struct_fields(index):
    """S2：导出结构 ≤5 个导出字段。"""
    findings = []
    for struct in index["structs"]:
        if not struct["name"][:1].isupper():
            continue
        count = len(struct["fields"])
        if count <= STRUCT_FIELD_LIMIT:
            continue
        findings.append({
            "rule": RULE_STRUCT,
            "key": struct["key"],
            "file": struct["file"],
            "line": struct["line"],
            "value": count,
            "detail": "{} exported fields > {} ({})".format(
                count, STRUCT_FIELD_LIMIT, ", ".join(struct["fields"])),
        })
    return findings


def rule_ctx_bare(index):
    """S3：裸 ctx.Value 调用 = 0（按文件计数）。"""
    findings = []
    for rel_path, _pkg, text in index["sources"]:
        no_comments, _code = strip_go(text)
        count = len(CTX_VALUE_RE.findall(no_comments))
        if count <= CTX_VALUE_LIMIT:
            continue
        findings.append({
            "rule": RULE_CTX,
            "key": "{}:*".format(rel_path),
            "file": rel_path,
            "line": 0,
            "value": count,
            "detail": "{} bare ctx.Value call(s) > {}; use a typed accessor".format(
                count, CTX_VALUE_LIMIT),
        })
    return findings


def package_slug(pkg):
    return "root" if pkg == "." else pkg.replace("/", "_")


def example_path(pkg, name):
    return "{}/{}/{}_example.go".format(EXAMPLE_ROOT, package_slug(pkg), name)


def redline_problem(doc):
    must = sum(1 for line in doc if REDLINE_MUST_RE.match(line))
    must_not = sum(1 for line in doc if REDLINE_MUST_NOT_RE.match(line))
    marked = any(marker in line for line in doc for marker in REDLINE_MARKERS)
    total = must + must_not
    if not marked:
        return "no 红线 marker in Godoc"
    if not must:
        return "no '// MUST ...' redline"
    if not must_not:
        return "no '// MUST NOT ...' redline"
    if total > REDLINE_LIMIT:
        return "{} redline lines > {}".format(total, REDLINE_LIMIT)
    return None


def rule_contract_surface(index, repo_root):
    """S4：每导出接口＝canonical 范例文件＋Godoc MUST/MUST NOT 红线清单。"""
    findings = []
    for iface in index["interfaces"]:
        if not iface["name"][:1].isupper():
            continue
        problems = []
        rel_example = example_path(iface["pkg"], iface["name"])
        absolute = os.path.join(repo_root, rel_example)
        if not os.path.isfile(absolute):
            problems.append("missing canonical example {}".format(rel_example))
        else:
            try:
                with open(absolute, "r", encoding="utf-8") as handle:
                    body = strip_go(handle.read())[0]
            except OSError as exc:
                raise lib.CheckFailure("cannot read {}: {}".format(rel_example, exc))
            if EXAMPLE_FUNC_RE.format(name=iface["name"]) not in body:
                problems.append("{} lacks func Example{}()".format(
                    rel_example, iface["name"]))
        redline = redline_problem(iface["doc"])
        if redline:
            problems.append(redline)
        if not problems:
            continue
        findings.append({
            "rule": RULE_CONTRACT,
            "key": iface["key"],
            "file": iface["file"],
            "line": iface["line"],
            "value": None,
            "detail": "; ".join(problems),
        })
    return findings


def collect_findings(repo_root, arch_path, rules):
    sources, metrics = collect_sources(repo_root, arch_path)
    index = build_index(sources)
    findings = []
    if RULE_IFACE in rules:
        findings.extend(rule_iface_methods(index))
    if RULE_STRUCT in rules:
        findings.extend(rule_struct_fields(index))
    if RULE_CTX in rules:
        findings.extend(rule_ctx_bare(index))
    if RULE_CONTRACT in rules:
        findings.extend(rule_contract_surface(index, repo_root))
    findings.sort(key=lambda item: (item["rule"], item["file"], item["line"], item["key"]))
    metrics["interfaces"] = len(index["interfaces"])
    metrics["exported_interfaces"] = sum(
        1 for iface in index["interfaces"] if iface["name"][:1].isupper())
    metrics["structs"] = len(index["structs"])
    return findings, metrics


def apply_baseline(report, findings, baseline):
    """棘轮：豁免必须 warn；超 baseline 即 error；不超标即 stale。"""
    recorded = {}
    for entry in baseline:
        rule, key, observed = entry
        recorded[(rule, key)] = observed
    hit = 0
    for finding in findings:
        slot = (finding["rule"], finding["key"])
        label = RULE_LABELS[finding["rule"]]
        if slot not in recorded:
            report.error("{} {} {}: {} (not baselined)".format(
                label, finding["rule"], finding["key"], finding["detail"]))
            continue
        allowed = recorded[slot]
        if allowed is None:
            hit += 1
            report.warn("{} {} {}: {} (baselined debt, still open)".format(
                label, finding["rule"], finding["key"], finding["detail"]))
            continue
        if finding["value"] > allowed:
            report.error("{} {} {}: {} exceeds baseline {} (ratchet: only-down)".format(
                label, finding["rule"], finding["key"], finding["detail"], allowed))
            continue
        hit += 1
        report.warn("{} {} {}: {} (baselined at {}, tighten or remove the entry)".format(
            label, finding["rule"], finding["key"], finding["detail"], allowed))
    seen = {(finding["rule"], finding["key"]) for finding in findings}
    stale = 0
    for rule, key, _observed in baseline:
        if (rule, key) in seen:
            continue
        stale += 1
        report.warn("baseline entry {} {}:{} matches no current violation (stale: remove it)"
                    .format(RULE_LABELS[rule], rule, key))
    report.count("baseline_entries", len(baseline))
    report.count("baseline_exempted", hit)
    report.count("baseline_stale", stale)
    return findings
FIXTURE_ARCH_NAME = ".concept-arch.json"
FIXTURE_ARCH = {
    "schema_version": 1,
    "scope": {"exclude_dir_names": ["examples"]},
    "packages": [{"pattern": "."}, {"pattern": "good"}, {"pattern": "bad"}],
}

GOOD_FIXTURE = {
    "good/faces.go": """package good

import "context"

// GoodFace 红线清单（2 条）：
// MUST 在一次调用内保持 id 稳定
// MUST NOT 持有调用方指针
type GoodFace interface {
	Read(ctx context.Context, id string) (string, error)
	Close() error
}

type firstImpl struct{}

func (firstImpl) Read(ctx context.Context, id string) (string, error) { return "", nil }
func (firstImpl) Close() error                                        { return nil }

type secondImpl struct{}

func (*secondImpl) Read(ctx context.Context, id string) (string, error) { return "", nil }
func (*secondImpl) Close() error                                        { return nil }

type GoodConfig struct {
	A int
	B int
	C int
	D int
	E int
}
""",
    "examples/contract/good/GoodFace_example.go": """package good

// ExampleGoodFace 是该窄口的 canonical 范例（一对最小实现）。
func ExampleGoodFace() {}
""",
}

BAD_FIXTURE = {
    "bad/faces.go": """package bad

import "context"

// BadFace 红线清单（2 条）：
// MUST 调用前加锁
// MUST NOT 在回调里重入本接口
type BadFace interface {
	A()
	B()
	C()
}

type implOne struct{}

func (implOne) A() {}
func (implOne) B() {}
func (implOne) C() {}

type implTwo struct{}

func (*implTwo) A() {}
func (*implTwo) B() {}
func (*implTwo) C() {}

type Big struct {
	A1 int
	B1 int
	C1 int
	D1 int
	E1 int
	F1 int
}

func leak(ctx context.Context) interface{} { return ctx.Value("k") }
""",
}

BAD_EXPECTED = {
    RULE_IFACE: "bad/faces.go:BadFace",
    RULE_STRUCT: "bad/faces.go:Big",
    RULE_CTX: "bad/faces.go:*",
    RULE_CONTRACT: "bad/faces.go:BadFace",
}


def write_fixture_tree(root, files):
    for rel_path, text in files.items():
        absolute = os.path.join(root, rel_path)
        parent = os.path.dirname(absolute)
        if parent:
            os.makedirs(parent, exist_ok=True)
        with open(absolute, "w", encoding="utf-8") as handle:
            handle.write(text)


def check_baseline_table(report, repo_root):
    """BASELINE 自身的完整性：形状、规则名、文件存在、observed 必须真超标。"""
    seen = set()
    minimum = {
        RULE_IFACE: POLYMORPHIC_METHOD_LIMIT,
        RULE_STRUCT: STRUCT_FIELD_LIMIT,
        RULE_CTX: CTX_VALUE_LIMIT,
    }
    for index, entry in enumerate(BASELINE):
        where = "BASELINE[{}]".format(index)
        if not isinstance(entry, tuple) or len(entry) != 3:
            report.error("{} must be a (rule, key, observed) tuple".format(where))
            continue
        rule, key, observed = entry
        if rule not in RULES:
            report.error("{} has unknown rule {!r}".format(where, rule))
            continue
        if not isinstance(key, str) or ":" not in key:
            report.error("{} key must be '<file>:<Symbol>', got {!r}".format(where, key))
            continue
        if (rule, key) in seen:
            report.error("{} duplicates baseline entry {}:{}".format(
                where, RULE_LABELS[rule], key))
        seen.add((rule, key))
        file_part, _sep, symbol = key.partition(":")
        if not os.path.isfile(os.path.join(repo_root, file_part)):
            # 文件被拆分/改名后该行已失效：警告而非失败，正常模式也会报 stale。
            report.warn("{} names missing file {} (stale entry: remove or re-point it)".format(
                where, file_part))
        if rule == RULE_CTX:
            if symbol != "*":
                report.error("{} ctx-bare entries are file-wide, symbol must be '*'".format(where))
        elif symbol == "*":
            report.error("{} symbol must name a type, not '*'".format(where))
        if rule == RULE_CONTRACT:
            if observed is not None:
                report.error("{} contract-surface debt is boolean, observed must be None".format(where))
            continue
        if type(observed) is not int or observed <= minimum[rule]:
            report.error("{} recorded value {!r} must be an int above the limit {} "
                         "(a compliant symbol must not sit in the baseline)".format(
                             where, observed, minimum[rule]))
    report.count("baseline_entries", len(BASELINE))


def check_fixture_rules(report, good_root, bad_root):
    """正反样例：合规件零发现、超标件被点名；每条规则必须能单独跑。"""
    good_arch = os.path.join(good_root, FIXTURE_ARCH_NAME)
    bad_arch = os.path.join(bad_root, FIXTURE_ARCH_NAME)
    for rule in RULES:
        label = RULE_LABELS[rule]
        findings, _metrics = collect_findings(good_root, good_arch, (rule,))
        if findings:
            report.error("self-check {}: compliant fixture was flagged: {}".format(
                label, findings[0]["detail"]))
        findings, _metrics = collect_findings(bad_root, bad_arch, (rule,))
        keys = [finding["key"] for finding in findings]
        if keys != [BAD_EXPECTED[rule]]:
            report.error("self-check {}: expected exactly [{}] on the violation fixture, got {}".format(
                label, BAD_EXPECTED[rule], keys))
    report.count("selfcheck_fixture_cases", len(RULES) * 2)


def check_baseline_ratchet(report, bad_root):
    """豁免/棘轮/清零三条路径都在反样例上跑一遍（不是只测过路径）。"""
    findings, _metrics = collect_findings(bad_root, os.path.join(bad_root, FIXTURE_ARCH_NAME), RULES)
    exempt = [(finding["rule"], finding["key"], finding["value"]) for finding in findings]
    probe = lib.Report("selfcheck-exempt")
    apply_baseline(probe, findings, exempt)
    if probe.errors:
        report.error("self-check: recorded baseline values must exempt, got: {}".format(
            probe.errors[0]))
    if probe.counts.get("baseline_exempted") != len(findings):
        report.error("self-check: exempt path counted {} of {} findings".format(
            probe.counts.get("baseline_exempted"), len(findings)))
    regress = [(finding["rule"], finding["key"],
                None if finding["value"] is None else finding["value"] - 1)
               for finding in findings]
    probe = lib.Report("selfcheck-ratchet")
    apply_baseline(probe, findings, regress)
    numeric = sum(1 for finding in findings if finding["value"] is not None)
    if len(probe.errors) != numeric or not any("ratchet" in message for message in probe.errors):
        report.error("self-check: a value above the recorded baseline must be a ratchet error "
                     "(expected {} errors, got {})".format(numeric, len(probe.errors)))
    probe = lib.Report("selfcheck-stale")
    apply_baseline(probe, [], [(RULE_IFACE, "nowhere.go:Ghost", 4)])
    if not any("stale" in message for message in probe.warnings):
        report.error("self-check: a baseline entry without a current violation must warn stale")
    report.count("selfcheck_ratchet_errors", numeric)


def self_check(report, repo_root, arch_path):
    """--self-check：表完整性＋S1–S4 正反样例＋棘轮三路径；不依赖当前树的通过与否。"""
    del arch_path  # 自校只用内置 fixture 架构；真实树由正常模式判定
    check_baseline_table(report, repo_root)
    with tempfile.TemporaryDirectory() as tmp:
        good_root = os.path.join(tmp, "good_repo")
        bad_root = os.path.join(tmp, "bad_repo")
        for root in (good_root, bad_root):
            write_fixture_tree(root, {FIXTURE_ARCH_NAME: _json_text(FIXTURE_ARCH)})
        write_fixture_tree(good_root, GOOD_FIXTURE)
        write_fixture_tree(bad_root, BAD_FIXTURE)
        check_fixture_rules(report, good_root, bad_root)
        check_baseline_ratchet(report, bad_root)
    report.count("selfcheck_rules", len(RULES))
    report.note("self-check runs rule fixtures only; it never claims the real tree passes")


def _json_text(value):
    return json.dumps(value, ensure_ascii=False, indent=2) + "\n"


def parse_args(argv):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", default=".", help="仓库根目录（默认当前目录）")
    parser.add_argument(
        "--architecture",
        default=os.path.join("golden", "runtime", "architecture.json"),
        help="一方包清单政策（相对 --repo-root）",
    )
    parser.add_argument(
        "--rule",
        action="append",
        default=[],
        help="只跑某条规则：S1..S4 或规则名（可重复；默认全部）",
    )
    parser.add_argument("--self-check", action="store_true",
                        help="规则自校：baseline 表完整性＋S1–S4 正反样例＋棘轮三路径")
    parser.add_argument("--json", action="store_true", help="以 JSON 输出报告")
    return parser.parse_args(argv)


def resolve_rules(requested):
    if not requested:
        return RULES
    chosen = []
    for name in requested:
        key = name.strip()
        rule = RULE_ALIASES.get(key.upper())
        if rule is None and key in RULES:
            rule = key
        if rule is None:
            raise lib.CheckFailure("unknown rule {!r}; use S1..S4 or one of {}".format(
                name, ", ".join(RULES)))
        if rule not in chosen:
            chosen.append(rule)
    return tuple(chosen)


def run(argv):
    args = parse_args(argv)
    report = lib.Report("check_concept_budget")
    repo_root = os.path.abspath(args.repo_root)
    arch_path = args.architecture
    if not os.path.isabs(arch_path):
        arch_path = os.path.join(repo_root, arch_path)
    try:
        if args.self_check:
            self_check(report, repo_root, arch_path)
            return report.emit(args.json)
        rules = resolve_rules(args.rule)
        findings, metrics = collect_findings(repo_root, arch_path, rules)
        report.count("rules_run", len(rules))
        report.count("go_files", metrics["files"])
        report.count("packages", metrics["packages"])
        report.count("interfaces", metrics["interfaces"])
        report.count("exported_interfaces", metrics["exported_interfaces"])
        report.count("structs", metrics["structs"])
        for rule in RULES:
            report.count("findings." + rule, sum(1 for f in findings if f["rule"] == rule))
        report.count("findings", len(findings))
        if metrics["skipped_unregistered_packages"]:
            report.warn("packages not registered in {} were not scanned (the architecture gate "
                        "owns registration): {}".format(
                            args.architecture, ", ".join(metrics["skipped_unregistered_packages"])))
        scope_baseline = [entry for entry in BASELINE if entry[0] in rules]
        apply_baseline(report, findings, scope_baseline)
        report.extra["findings"] = findings
        report.note("static limits only: field/redline semantics still need review; "
                    "productVerified stays null")
    except lib.CheckFailure as exc:
        report.error(str(exc))
    return report.emit(args.json)


if __name__ == "__main__":
    sys.exit(run(sys.argv[1:]))
