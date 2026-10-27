#!/usr/bin/env python3
from __future__ import annotations
"""
Convert tagent trajectory JSONL files to AReaL-compatible datasets.

Usage:
    # SFT mode (legacy): convert to {input_ids, loss_mask} format
    python3 convert_trajectories.py \
        --input data/trajectories/ \
        --output data/sft/ \
        --tokenizer Qwen/Qwen2.5-1.5B-Instruct \
        --mode sft

    # RL mode (legacy): convert to {messages} format (prompt-only)
    python3 convert_trajectories.py \
        --input data/trajectories/ \
        --output data/rl/ \
        --mode rl

    # v2 strict mode: tool-aware chat-template SFT with a verifiable loss mask.
    # --strict reads TWO streams: --input is the capture stream (the primary record
    # source: rl.CaptureRecord, one v2 JSONL line per model call), --events is the
    # authorised fact export (rl.ExportedFact) used as the ASSOCIATION INDEX only —
    # it supplies event keys, causal parent keys and the feedback bound to a call —
    # and --manifest is the capture seal that proves the primary stream is complete.
    python3 convert_trajectories.py \
        --strict \
        --input data/capture/ \
        --events data/export/facts.jsonl \
        --manifest data/export/capture-manifest.json \
        --tokenizer /models/qwen2.5-tokenizer \
        --output data/sft-strict/ \
        --format jsonl \
        --content-policy synthetic \
        --train-ratio 0.9 --split-seed 0

Legacy input format (tagent JSONL, one per line):
    {
        "timestamp": "2026-06-20T10:30:00Z",
        "session_id": "wechat-session",
        "user_id": "wechat-user",
        "batch_index": 0,
        "llm_call": {
            "request": {"messages": [{"role": "user", "content": "..."}],
                       "model": "glm-5", "generation_config": {...}},
            "response": {"choices": [{"message": {"role": "assistant", "content": "..."}}],
                         "usage": {...}, "finish_reason": "stop"}
        },
        "metadata": {"duration_ms": 1234, "model_endpoint": "https://..."}
    }

Legacy output formats:
    SFT: {input_ids, loss_mask}  - prompt 0, completion 1 (boundary assumed)
    RL:  {messages}              - prompt-only

Strict input formats (the shapes rl/ actually writes; field names are the contract):
    capture record (schema_version=2, one line per model call)
        {"schema_version": 2, "capture_scope": "sdk_request", "run_id": "...",
         "call_id": "...", "response_id": "...", "request_digest": "...",
         "binding_status": "bound|unbound|ambiguous", "missing_reasons": [...],
         "owner": {"capture_namespace": "7", "root_session_id": "...", "agent_name": "...",
                   "partition_id": "7", "session_id": "...", "invocation_id": "...",
                   "purpose": "...", "input_event_keys": [...]},
         "llm_call": {"request": {"messages": [...], "model": "...", "tools":
                       [{"registry_key": "...", "name": "...", "description": "...",
                         "inputSchema": {...}}]},
                      "response": {"choices": [...], "finish_reason": "stop"},
                      "terminal_status": {"kind": "done", "finish_reason": "stop",
                                          "response_id": "...", "fragments": 1},
                      "response_incomplete": false}}
    exported fact (the association index, rl.ExportedFact = memory.FullEvent + parent_key)
        {"event_key": 730118678880927744, "partition_id": 7, "event_type": "agent_output",
         "event_summary": "...", "timestamp": 1762500000000, "content": "...",
         "tool_calls": [...], "tool_results": {}, "metadata": {"call_id": "...", ...},
         "parent_key": "a2b3..."}
        feedback lines are the same shape with "event_type": "feedback" and a
        memory.FeedbackPayload JSON in "content"; they join to a call through THEIR
        parent (parent_key -> that event's metadata.call_id), never by proximity.
    capture seal (--manifest, rl.MarshalCaptureManifest; counters stay flat because a
        nested object would be read as zero and turn an unsealed run into a complete one)
        {"runs": {"<run_id>": {"run_id": "...", "written": 12, "inflight": 0, "pending": 0,
                    "dropped_full": 0, "dropped_closed": 0, "dropped_disk_limit": 0,
                    "oversized": 0, "serialize_failed": 0, "write_failed": 0,
                    "sync_failed": 0, "sealed": true, "synchronized": true,
                    "complete": true, "files": [...]}},
         "complete": true, "synchronized": true}
"""

import argparse
import glob
import hashlib
import json
import os
import re
import sys
from pathlib import Path


def load_trajectories(input_path: str) -> list[dict]:
    """Load all trajectory records from JSONL files (legacy mode)."""
    records = []

    if os.path.isdir(input_path):
        jsonl_files = sorted(glob.glob(os.path.join(input_path, "*.jsonl")))
    elif os.path.isfile(input_path) and input_path.endswith(".jsonl"):
        jsonl_files = [input_path]
    else:
        print(f"Error: {input_path} is not a .jsonl file or directory", file=sys.stderr)
        sys.exit(1)

    if not jsonl_files:
        print(f"Error: No .jsonl files found in {input_path}", file=sys.stderr)
        sys.exit(1)

    for fpath in jsonl_files:
        with open(fpath, "r", encoding="utf-8") as f:
            for line_num, line in enumerate(f, 1):
                line = line.strip()
                if not line:
                    continue
                try:
                    record = json.loads(line)
                    record["_source_file"] = os.path.basename(fpath)
                    record["_line_number"] = line_num
                    records.append(record)
                except json.JSONDecodeError as e:
                    print(f"Warning: skipping invalid JSON in {fpath}:{line_num}: {e}", file=sys.stderr)

    print(f"Loaded {len(records)} trajectory records from {len(jsonl_files)} file(s)", file=sys.stderr)
    return records


def extract_prompt_completion(record: dict) -> tuple[str, str] | None:
    """Extract prompt text and completion text from a trajectory record (legacy)."""
    llm_call = record.get("llm_call", {})
    request = llm_call.get("request", {})
    response = llm_call.get("response", {})

    messages = request.get("messages", [])
    choices = response.get("choices", [])

    if not messages or not choices:
        return None

    prompt_parts = []
    for msg in messages:
        role = msg.get("role", "user")
        content = msg.get("content", "")
        prompt_parts.append(f"{role}: {content}")
    prompt_text = "\n".join(prompt_parts)

    message = choices[0].get("message", {})
    completion_text = message.get("content", "")

    if not completion_text:
        return None

    return prompt_text, completion_text


def extract_user_message(record: dict) -> str | None:
    """Extract the initial user message from a trajectory record (legacy)."""
    messages = record.get("llm_call", {}).get("request", {}).get("messages", [])
    for msg in messages:
        if msg.get("role") == "user":
            return msg.get("content", "")
    return None


def convert_to_sft(records: list[dict], tokenizer) -> list[dict]:
    """Convert records to AReaL SFT {input_ids, loss_mask} (legacy plain-text path)."""
    from transformers import AutoTokenizer

    samples = []
    skipped = 0

    for record in records:
        result = extract_prompt_completion(record)
        if result is None:
            skipped += 1
            continue

        prompt_text, completion_text = result
        prompt_ids = tokenizer.encode(prompt_text, add_special_tokens=False)
        completion_ids = tokenizer.encode(completion_text, add_special_tokens=False)

        eos_id = tokenizer.eos_token_id
        if eos_id is not None:
            completion_ids = completion_ids + [eos_id]

        input_ids = prompt_ids + completion_ids
        loss_mask = [0] * len(prompt_ids) + [1] * len(completion_ids)
        samples.append({"input_ids": input_ids, "loss_mask": loss_mask})

    print(f"SFT conversion: {len(samples)} samples, {skipped} skipped", file=sys.stderr)
    return samples


def convert_to_rl(records: list[dict]) -> list[dict]:
    """Convert records to AReaL RL prompt dataset {messages} (legacy)."""
    samples = []
    skipped = 0
    for record in records:
        user_msg = extract_user_message(record)
        if user_msg is None:
            skipped += 1
            continue
        samples.append({"messages": [{"role": "user", "content": user_msg}]})
    print(f"RL conversion: {len(samples)} samples, {skipped} skipped", file=sys.stderr)
    return samples


def save_dataset(samples: list[dict], output_path: str, mode: str):
    """Save samples as a HuggingFace Dataset (legacy: JSONL fallback if no datasets)."""
    output_path = Path(output_path)
    output_path.mkdir(parents=True, exist_ok=True)
    try:
        from datasets import Dataset
        ds = Dataset.from_list(samples)
        ds.save_to_disk(str(output_path))
        print(f"Saved HuggingFace Dataset to {output_path}", file=sys.stderr)
    except ImportError:
        jsonl_path = output_path / f"{mode}_dataset.jsonl"
        with open(jsonl_path, "w", encoding="utf-8") as f:
            for sample in samples:
                f.write(json.dumps(sample, ensure_ascii=False) + "\n")
        print(f"datasets library not available, saved JSONL to {jsonl_path}", file=sys.stderr)


# ---------------------------------------------------------------------------
# v2 strict export (tool-aware chat-template SFT)
# ---------------------------------------------------------------------------
# Legacy path concatenates 'role: content' as text, silently drops records and
# encodes prompt/completion separately (loss boundary assumed). --strict is the
# explicit v2 consumer contract: fail-loud rejections (source/call_id/reason), one
# apply_chat_template over the whole conversation (never a separate re-encode), loss
# on the target assistant span only (labels -100 elsewhere), an empty-body tool-call
# stays a valid target, and an unverifiable boundary is refused, not guessed. It talks
# to an injected adapter (TemplateEncoder) needing only apply_chat_template/eos_token_id,
# so unit tests run without transformers while real acceptance still uses a local
# tool-aware tokenizer.
SCHEMA_VERSION_V2 = 2
LOSS_IGNORE_INDEX = -100
VALID_SDK_ROLES = ("system", "user", "assistant", "tool")
TERMINAL_FINISH_REASONS = frozenset(["stop", "length", "tool_calls", "content_filter"])
CONTENT_POLICIES = ("synthetic", "retain", "redact")

# Spelling of the association layer as the Go writer emits it (event.TypeFeedback,
# event.MetaKeyCallID): a fact-export line carries its call attribution in
# metadata["call_id"], and a feedback line is bound through its causal parent.
EVENT_TYPE_FEEDBACK = "feedback"
FACT_META_CALL_ID = "call_id"

# Rejection reason codes are part of the output contract (downstream acceptance
# greps for them); renaming one is a breaking change.
REASON_NOT_V2 = "not_v2_schema"
REASON_LEGACY_RECORD = "legacy_record"
REASON_UNSEALED = "capture_not_sealed"
REASON_MISSING_CALL_ID = "missing_call_id"
REASON_DUPLICATE_CALL_ID = "duplicate_call_id"
REASON_MISSING_TOOLS = "missing_tools"
REASON_NONTERMINAL = "response_not_terminal"
REASON_NO_CHOICE = "response_missing_choice"
REASON_BAD_CHOICE_ROLE = "choice_role_not_assistant"
REASON_AMBIGUOUS_ATTRIBUTION = "ambiguous_attribution"
REASON_MISSING_PROVENANCE = "missing_root_group_provenance"
REASON_UNKNOWN_ROLE = "unknown_message_role"
REASON_MULTIMODAL = "multimodal_unsupported"
REASON_UNBOUND_CALL = "unbound_tool_call"
REASON_BROKEN_PAIRING = "history_tool_pairing_broken"
REASON_ORPHAN_RESULT = "orphan_tool_result"
REASON_AMBIGUOUS_TOOL_CALL_ID = "ambiguous_tool_call_id"
REASON_INVALID_ARGS_JSON = "invalid_tool_arguments_json"
REASON_EMPTY_TARGET = "empty_assistant_target"
REASON_MASK_UNSUPPORTED = "template_mask_unsupported"
REASON_TARGET_MASK_EMPTY = "template_mask_unsupported:empty_target_span"
REASON_REDACT_NO_RULES = "content_policy_redact_rules_missing"
REASON_REDACT_BREAKS_JSON = "redaction_corrupts_tool_json"
# Cross-stream contradiction: the capture record's own owner partition and the
# partition the authorised fact export read this call's committed event from do
# not agree. One of the two streams is lying about which partition this call is in,
# so the sample is refused instead of being attributed to either side.
REASON_OWNER_PARTITION_CONFLICT = "owner_partition_conflict"

# Per-sample quality flags: things a consumer must be able to count without
# re-reading the streams, reported but NOT rejected (an unlinked association is a
# weaker sample, not a broken one).
QUALITY_EVENT_KEY_MISSING = "event_key_missing"
QUALITY_FACT_UNMATCHED = "fact_unmatched"
QUALITY_FEEDBACK_AMBIGUOUS = "feedback_ambiguous"
QUALITY_FLAGS = (QUALITY_EVENT_KEY_MISSING, QUALITY_FACT_UNMATCHED, QUALITY_FEEDBACK_AMBIGUOUS)

# Where a sample's feedback list came from, so "no feedback" is never confused with
# "feedback the join refused to spread".
FEEDBACK_SOURCE_FACT_INDEX = "fact_index"
FEEDBACK_SOURCE_RECORD = "record"
FEEDBACK_SOURCE_UNRATED = "unrated"
FEEDBACK_SOURCE_AMBIGUOUS = "fact_index_ambiguous"


class StrictError(Exception):
    """Fatal strict violation: bad CLI contract, unsafe output dir, or missing dep."""


class _MaskUnsupported(Exception):
    def __init__(self, reason: str):
        super().__init__(reason)
        self.reason = reason


def _sha256_text(text: str) -> str:
    return hashlib.sha256(text.encode("utf-8")).hexdigest()


def _jsonl_files(path: str, flag: str) -> list:
    """Resolve one strict stream to its JSONL files (a dir of them, or one file)."""
    if os.path.isdir(path):
        jsonl_files = sorted(glob.glob(os.path.join(path, "*.jsonl")))
    elif os.path.isfile(path):
        jsonl_files = [path]
    else:
        raise StrictError("%s path not found: %s" % (flag, path))
    if not jsonl_files:
        raise StrictError("no .jsonl files found under %s %s" % (flag, path))
    return jsonl_files


def read_jsonl_rows(path: str, flag: str) -> list:
    """Read one strict stream, tagging every row with its exact source location.

    A malformed line is NOT silently skipped: it becomes a poison record carrying
    ``_parse_error`` so the strict pipeline reports it in the rejection list with
    its exact source location.
    """
    rows = []
    for fpath in _jsonl_files(path, flag):
        base = os.path.basename(fpath)
        with open(fpath, "r", encoding="utf-8") as f:
            for line_num, line in enumerate(f, 1):
                source = "%s:%d" % (base, line_num)
                stripped = line.strip()
                if not stripped:
                    continue
                try:
                    parsed = json.loads(stripped)
                except json.JSONDecodeError as exc:
                    rows.append({"_source": source, "_parse_error": "invalid_json: %s" % exc})
                    continue
                if not isinstance(parsed, dict):
                    rows.append({"_source": source, "_parse_error": "record_is_not_object"})
                    continue
                parsed["_source"] = source
                rows.append(parsed)
    return rows


def load_facts(events_path: str) -> list:
    """Load the authorised fact export (rl.ExportedFact) rows: the association layer.

    These rows are NOT training records — they carry no SDK request/response. They
    index a committed event to the call that produced it (metadata.call_id), point at
    its causal parent (parent_key) and carry the feedback events bound to that parent.
    """
    return read_jsonl_rows(events_path, "--events")


def classify_capture_row(record: dict) -> tuple:
    """(keep, reason) for one primary-source line: only schema_version==2 is kept.

    A line that declares an older ``schema_version``, or a v1 dump line (the
    request/response envelope with no v2 identity fields and no declaration at all), is
    ``legacy_record``; anything else that is not a v2 capture record — a fact-export
    line mistakenly pointed at --input, a foreign JSON object, a line declaring a schema
    this reader does not know — is ``not_v2_schema``. Telling the two apart matters
    because "you pointed the legacy recorder at strict" and "you pointed the wrong
    stream at strict" have different fixes.
    """
    if record.get("_parse_error"):
        return False, record["_parse_error"]
    declared = record.get("schema_version")
    if declared is not None:
        try:
            version = int(declared)
        except (TypeError, ValueError):
            return False, REASON_NOT_V2
        if version == SCHEMA_VERSION_V2:
            return True, None
        # a line that declares an older schema IS the legacy recorder's output;
        # a line declaring a newer one is not something this reader may guess at.
        return False, (REASON_LEGACY_RECORD if version < SCHEMA_VERSION_V2 else REASON_NOT_V2)
    if (record.get("llm_call") and not record.get("call_id")
            and not record.get("run_id")):
        return False, REASON_LEGACY_RECORD
    return False, REASON_NOT_V2


def load_capture_records(input_path: str) -> tuple:
    """Load the primary capture stream -> (v2 records, rejections).

    Only schema_version==2 lines enter the record set that becomes samples; legacy and
    foreign lines land in the rejection ledger with their reason instead of vanishing.
    """
    rows = read_jsonl_rows(input_path, "--input")
    records, rejections = [], []
    for row in rows:
        keep, reason = classify_capture_row(row)
        if keep:
            records.append(row)
        else:
            rejections.append({"source": row.get("_source", "<memory>"),
                               "call_id": row.get("call_id"), "reason": reason})
    return records, rejections


def load_capture_manifest(path: str) -> dict:
    """Read the capture seal manifest -> {"runs": {run_id: entry}, "manifest_sha256"}.

    A missing/invalid manifest is fatal for strict: an unsealed capture cannot be
    claimed complete.
    """
    if not path:
        raise StrictError("--strict requires --manifest <capture-manifest>")
    if not os.path.isfile(path):
        raise StrictError("--manifest file not found: %s" % path)
    with open(path, "r", encoding="utf-8") as f:
        raw = f.read()
    try:
        doc = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise StrictError("--manifest is not valid JSON: %s" % exc)
    if not isinstance(doc, dict):
        raise StrictError("--manifest must be a JSON object")

    runs = {}
    runs_raw = doc.get("runs")
    if isinstance(runs_raw, dict):
        for run_id, entry in runs_raw.items():
            merged = dict(entry) if isinstance(entry, dict) else {}
            merged.setdefault("run_id", run_id)
            runs[str(run_id)] = merged
    elif isinstance(runs_raw, list):
        for entry in runs_raw:
            if isinstance(entry, dict) and entry.get("run_id"):
                runs[str(entry["run_id"])] = entry
    return {
        "runs": runs,
        "manifest_sha256": _sha256_text(raw),
        "complete": bool(doc.get("complete", False)),
        "synchronized": bool(doc.get("synchronized", False)),
    }


# The loss ledger keys of one capture run entry (rl.CaptureStats, flat on purpose).
# A run is quiesced only when every one of them is zero; reading a nested object as
# zero is exactly how an unsealed run would get promoted to a complete one.
_CAPTURE_LEDGER_KEYS = ("inflight", "pending", "dropped_full", "dropped_closed",
                        "dropped_disk_limit", "oversized", "oversized_dropped",
                        "pending_exhausted", "serialize_failed",
                        "write_failed", "sync_failed")


def _manifest_run_status(manifest: dict, run_id):
    """Return ("sealed"|"incomplete"|"unknown", detail) for a record's run_id."""
    if not run_id:
        return "incomplete", "record has no run_id"
    entry = manifest["runs"].get(str(run_id))
    if entry is None:
        return "unknown", "run_id %s absent from capture manifest" % run_id
    complete = bool(entry.get("complete", manifest["complete"]))
    synced = bool(entry.get("synchronized", manifest["synchronized"]))
    sealed = bool(entry.get("sealed", True))
    if not complete:
        return "incomplete", "run_id %s not sealed complete" % run_id
    if not synced:
        return "incomplete", "run_id %s not synchronized" % run_id
    if not sealed:
        return "incomplete", "run_id %s is an intermediate flush, not the final seal" % run_id
    nonzero = ", ".join("%s=%d" % (key, int(entry.get(key, 0) or 0))
                        for key in _CAPTURE_LEDGER_KEYS if int(entry.get(key, 0) or 0))
    if nonzero:
        return "incomplete", "run_id %s not quiesced (%s)" % (run_id, nonzero)
    return "sealed", ""


# ---------------------------------------------------------------------------
# association index (stream 2 -> stream 1)
# ---------------------------------------------------------------------------

def parse_event_key(value):
    """Parse an event key the way event.ParseEventKey does, or None.

    Accepted spellings: the canonical lowercase hex the export writes, an ``0x`` /
    ``evt_`` / ``[evt_<hex>|<type>]`` echo of a ticket, and a JSON number. Strings are
    read as BASE 16 always, because that is what the writer produces
    (strconv.ParseInt(s, 16, 64)): a digits-only canonical key such as "1000" is 0x1000,
    not one thousand. Reading it as decimal would silently point a feedback join at a
    different event — the loudest possible wrongness here would be the cheapest. A
    malformed string is None instead of a guess.
    """
    if value is None or isinstance(value, bool):
        return None
    if isinstance(value, int):
        return value
    if isinstance(value, float):
        return int(value) if value.is_integer() else None
    text = str(value).strip()
    if not text:
        return None
    neg = text.startswith("-")
    if neg:
        text = text[1:]
    if text[:2].lower() == "0x":
        text = text[2:]
    text = text.strip()
    if text.startswith("["):
        text = text[1:]
    if text.startswith("evt_"):
        text = text[4:]
    for sep in ("|", "]"):
        cut = text.find(sep)
        if cut >= 0:
            text = text[:cut]
    text = text.strip()
    if not text:
        return None
    try:
        parsed = int(text, 16)
    except ValueError:
        return None
    return -parsed if neg else parsed


def format_event_key(key) -> str:
    """Render an event key in the canonical lowercase hex form (event.FormatEventKey)."""
    if key is None:
        return ""
    return ("-" + format(-int(key), "x")) if int(key) < 0 else format(int(key), "x")


def canonical_event_key(value) -> str:
    """Any spelling of an event key -> canonical hex text, or "" when there is none."""
    return format_event_key(parse_event_key(value))


def _normalise_partition(value):
    """Normalise a partition claim to int, or None when it is not a partition number.

    The capture owner spells ``partition_id`` as a STRING (rl.CaptureOwner) while the
    export spells it as a JSON NUMBER (memory.FullEvent), and the owner's numeric
    ``capture_namespace`` is the same number rendered as text by the composition root.
    Comparing them as strings would invent conflicts; comparing them as ints is what
    the two writers actually mean. A value that is not a number at all (a hand-made
    fixture namespace like "part-1") yields None, i.e. "this side cannot claim".
    """
    if value is None or isinstance(value, bool):
        return None
    if isinstance(value, int):
        return value
    if isinstance(value, float):
        return int(value) if value.is_integer() else None
    text = str(value).strip()
    if not text:
        return None
    try:
        return int(text, 10)
    except ValueError:
        return None


def _fact_call_id(row: dict) -> str:
    """The call a committed fact belongs to: metadata["call_id"], nothing else."""
    meta = row.get("metadata")
    if not isinstance(meta, dict):
        return ""
    value = meta.get(FACT_META_CALL_ID)
    return str(value).strip() if value else ""


def _feedback_payload(row: dict) -> dict:
    """Decode memory.FeedbackPayload from a feedback event's content JSON."""
    content = row.get("content")
    if isinstance(content, dict):
        return dict(content)
    if not isinstance(content, str) or not content.strip():
        return {}
    try:
        loaded = json.loads(content)
    except json.JSONDecodeError:
        return {}
    return loaded if isinstance(loaded, dict) else {}


def _feedback_item(row: dict) -> dict:
    """One bound feedback, kept as the raw judgement (no numeric reward is invented).

    Only the payload/annotation fields are carried over, never the whole storage row:
    the payload's ``note`` is user-authored text (the content policy redacts it), while
    ``verdict``/``source``/``event_key``/``parent_key`` are association fields that must
    survive redaction untouched.
    """
    payload = _feedback_payload(row)
    meta = row.get("metadata") if isinstance(row.get("metadata"), dict) else {}
    item = {}
    for key in ("verdict", "source", "note", "rating", "reason"):
        value = payload.get(key, meta.get(key))
        if value is not None and value != "":
            item[key] = value
    event_key = canonical_event_key(row.get("event_key"))
    parent_key = canonical_event_key(row.get("parent_key") or payload.get("parent_key"))
    if event_key:
        item["event_key"] = event_key
    if parent_key:
        item["parent_key"] = parent_key
    return item


def _new_index_entry(call_id: str) -> dict:
    return {"call_id": call_id, "event_keys": [], "parent_keys": [], "partitions": [],
            "feedback": [], "rows": 0, "ambiguous": False}


def build_fact_index(rows: list) -> tuple:
    """Index the association stream by call_id -> ({"call_id": entry}, stats).

    The join rules mirror rl/training_export.go, because a converter that disagrees
    with the exporter about what "bound" means would silently re-attribute feedback:
      - a committed fact joins in through its own ``metadata.call_id``;
      - a ``feedback`` line joins through ITS PARENT (parent_key -> that event's
        metadata.call_id), never by proximity or "the nearest call";
      - several feedbacks on one call are all kept (repeated judgement is data), but a
        call reached from contradictory parents is ambiguous and gets NO feedback
        spread across it;
      - a parent that is not in the snapshot is expired_or_missing, and a feedback with
        no parent pointer at all (task-level feedback) stays unbound.

    An entry is {"call_id", "event_keys", "parent_keys", "partitions", "feedback",
    "rows", "ambiguous"}; callers treat a missing call_id as "no association layer",
    which weakens a sample but never invalidates it.
    """
    index = {}
    by_event_key = {}
    stats = {"rows": 0, "parse_errors": 0, "indexed_calls": 0, "indexed_rows": 0,
             "rows_without_call_id": 0, "feedback_total": 0, "feedback_bound": 0,
             "feedback_unbound": 0, "feedback_ambiguous": 0, "ambiguous_calls": 0,
             "join_missing": 0, "join_expired_or_missing": 0, "join_forbidden_parent": 0}

    for row in rows:
        stats["rows"] += 1
        if row.get("_parse_error"):
            stats["parse_errors"] += 1
            continue
        if str(row.get("event_type") or "") == EVENT_TYPE_FEEDBACK:
            continue  # a feedback line indexes through its parent, handled below
        key = parse_event_key(row.get("event_key"))
        if key is not None:
            by_event_key.setdefault(key, row)

    pending = []  # (call_id, parent_key_int, item) — decided once the whole walk is done
    for row in rows:
        if row.get("_parse_error"):
            continue
        if str(row.get("event_type") or "") == EVENT_TYPE_FEEDBACK:
            stats["feedback_total"] += 1
            item = _feedback_item(row)
            parent = parse_event_key(row.get("parent_key") or item.get("parent_key"))
            if parent is None:
                # Task-level feedback: it remains its own line, never attached to a
                # call by guessing.
                stats["feedback_unbound"] += 1
                stats["join_missing"] += 1
                continue
            parent_row = by_event_key.get(parent)
            if parent_row is None:
                stats["join_expired_or_missing"] += 1
                continue
            call_id = _fact_call_id(parent_row)
            if not call_id:
                stats["join_missing"] += 1
                continue
            pending.append((call_id, parent, item))
            continue

        call_id = _fact_call_id(row)
        if not call_id:
            stats["rows_without_call_id"] += 1
            continue
        entry = index.get(call_id)
        if entry is None:
            entry = _new_index_entry(call_id)
            index[call_id] = entry
            stats["indexed_calls"] += 1
        entry["rows"] += 1
        stats["indexed_rows"] += 1
        event_key = canonical_event_key(row.get("event_key"))
        if event_key and event_key not in entry["event_keys"]:
            entry["event_keys"].append(event_key)
        parent_key = canonical_event_key(row.get("parent_key"))
        if parent_key and parent_key not in entry["parent_keys"]:
            entry["parent_keys"].append(parent_key)
        partition = _normalise_partition(row.get("partition_id"))
        if partition is not None and partition not in entry["partitions"]:
            entry["partitions"].append(partition)

    parents_by_call = {}
    for call_id, parent, _item in pending:
        parents_by_call.setdefault(call_id, set()).add(parent)

    for call_id, parent, item in pending:
        entry = index.get(call_id)
        if entry is None:
            entry = _new_index_entry(call_id)
            index[call_id] = entry
            stats["indexed_calls"] += 1
        if len(parents_by_call.get(call_id, ())) > 1:
            # One call reached from contradictory parents: broken attribution, so it
            # is counted and nothing is attached to it.
            entry["ambiguous"] = True
            stats["feedback_ambiguous"] += 1
            continue
        stats["feedback_bound"] += 1
        parent_key = format_event_key(parent)
        if parent_key and parent_key not in entry["parent_keys"]:
            entry["parent_keys"].append(parent_key)
        entry["feedback"].append(item)

    stats["ambiguous_calls"] = sum(1 for e in index.values() if e["ambiguous"])
    return index, stats


def get_owner(record: dict) -> dict:
    owner = record.get("owner")
    return owner if isinstance(owner, dict) else {}


def get_request(record: dict) -> dict:
    llm_call = record.get("llm_call")
    if not isinstance(llm_call, dict):
        return {}
    request = llm_call.get("request")
    return request if isinstance(request, dict) else {}


def get_response(record: dict) -> dict:
    llm_call = record.get("llm_call")
    if not isinstance(llm_call, dict):
        return {}
    response = llm_call.get("response")
    return response if isinstance(response, dict) else {}


def get_terminal_status(record: dict) -> dict:
    """The v2 reduced terminal view: llm_call.terminal_status."""
    llm_call = record.get("llm_call")
    if not isinstance(llm_call, dict):
        return {}
    status = llm_call.get("terminal_status")
    return status if isinstance(status, dict) else {}


def message_text(content):
    """Render a content payload as text, or None if it is not reliably text-encodable."""
    if content is None:
        return ""
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        out = []
        for part in content:
            if isinstance(part, str):
                out.append(part)
            elif isinstance(part, dict) and part.get("type") in ("text", "output_text"):
                out.append(str(part.get("text", "")))
            else:
                return None  # multimodal part: refuse, do not drop into text
        return "".join(out)
    return None


def _call_arguments(raw) -> str:
    """Return tool-call arguments as the ORIGINAL raw string (byte-inspectable)."""
    if raw is None:
        return ""
    if isinstance(raw, str):
        return raw
    if isinstance(raw, (bytes, bytearray)):
        return raw.decode("utf-8", "replace")
    return json.dumps(raw, ensure_ascii=False, sort_keys=True)


def _record_finish_reason(record: dict) -> str:
    """The terminal finish reason wherever the writer put it.

    A v2 record states it three times (llm_call.response.finish_reason, the choice's
    own finish_reason and llm_call.terminal_status.finish_reason); an empty string from
    all of them means the stream never reached a terminal response, which is a
    rejection, not a default.
    """
    response = get_response(record)
    status = get_terminal_status(record)
    choices = response.get("choices") or []
    first = choices[0] if choices and isinstance(choices[0], dict) else {}
    for candidate in (record.get("finish_reason"), response.get("finish_reason"),
                      status.get("finish_reason"), first.get("finish_reason")):
        if candidate:
            return str(candidate)
    return ""


def _record_incomplete(record: dict) -> bool:
    """response_incomplete is written both at the top level and inside llm_call."""
    if record.get("response_incomplete"):
        return True
    llm_call = record.get("llm_call")
    return bool(isinstance(llm_call, dict) and llm_call.get("response_incomplete"))


def extract_tools(request: dict) -> list:
    """Normalise v2 declared tools into the OpenAI-style list apply_chat_template wants."""
    raw = request.get("tools")
    if raw is None:
        return []
    if isinstance(raw, dict):
        items = []
        for key in sorted(raw.keys()):
            value = raw[key]
            merged = dict(value) if isinstance(value, dict) else {}
            merged.setdefault("name", key)
            items.append(merged)
    elif isinstance(raw, (list, tuple)):
        items = list(raw)
    else:
        return []
    declarations = []
    for decl in items:
        if not isinstance(decl, dict):
            continue
        fn = decl.get("function") if isinstance(decl.get("function"), dict) else {}
        name = decl.get("name") or decl.get("declaration_name") or fn.get("name") or decl.get("key")
        if not name:
            continue
        function = {
            "name": str(name),
            "description": decl.get("description") or fn.get("description") or "",
            "parameters": (decl.get("inputSchema") or decl.get("input_schema")
                           or fn.get("parameters") or {"type": "object", "properties": {}}),
        }
        outputs = decl.get("outputSchema") or decl.get("output_schema")
        if outputs:
            function["outputs"] = outputs
        declarations.append({"type": decl.get("type") or "function", "function": function})
    return declarations


def _declared_tool_names(tools: list) -> set:
    names = set()
    for entry in tools:
        fn = entry.get("function") if isinstance(entry, dict) else None
        if isinstance(fn, dict) and fn.get("name"):
            names.add(str(fn["name"]))
    return names


def _tool_result_id(msg: dict):
    """The tool_call_id a tool observation answers (SDK: tool_id / tool_call_id)."""
    return msg.get("tool_call_id") or msg.get("tool_id") or msg.get("id")


class TemplateEncoder:
    """Encode the whole conversation with ONE template call and derive the loss mask."""

    def __init__(self, tokenizer):
        self.tokenizer = tokenizer

    def encode(self, messages: list, tools: list) -> dict:
        try:
            out = self.tokenizer.apply_chat_template(
                messages,
                tools=tools or None,
                tokenize=True,
                return_assistant_tokens_mask=True,
                return_dict=True,
                add_generation_prompt=False,
            )
        except TypeError:
            raise _MaskUnsupported(REASON_MASK_UNSUPPORTED)
        if not isinstance(out, dict):
            raise _MaskUnsupported(REASON_MASK_UNSUPPORTED)
        input_ids = out.get("input_ids")
        assistant_masks = out.get("assistant_tokens_mask")
        if assistant_masks is None:
            assistant_masks = out.get("assistant_masks")
        if input_ids is None or assistant_masks is None:
            raise _MaskUnsupported(REASON_MASK_UNSUPPORTED)
        input_ids = list(input_ids)
        assistant_masks = list(assistant_masks)
        if not input_ids or len(input_ids) != len(assistant_masks):
            raise _MaskUnsupported(REASON_MASK_UNSUPPORTED)

        loss_mask = _target_loss_mask(messages, assistant_masks)
        if not any(loss_mask):
            raise _MaskUnsupported(REASON_TARGET_MASK_EMPTY)
        labels = [iid if m == 1 else LOSS_IGNORE_INDEX for iid, m in zip(input_ids, loss_mask)]
        return {
            "input_ids": input_ids,
            "attention_mask": [1] * len(input_ids),
            "loss_mask": loss_mask,
            "labels": labels,
        }


def _target_loss_mask(messages: list, assistant_masks: list) -> list:
    """Mask only the trailing assistant span (the predicted action); 0 everywhere else.

    The target assistant is the last assistant message, so the trailing contiguous
    1-run of the template's assistant mask is its boundary. Locating it uses only
    boundaries the template reported, never a separate re-encode.
    """
    loss_mask = [0] * len(assistant_masks)
    if not any(m.get("role") == "assistant" for m in messages):
        return loss_mask
    idx = len(assistant_masks) - 1
    while idx >= 0 and assistant_masks[idx] == 0:
        idx -= 1
    if idx < 0:  # template reported no assistant token at all
        return loss_mask
    end = idx + 1
    start = idx
    while start - 1 >= 0 and assistant_masks[start - 1] == 1:
        start -= 1
    for i in range(start, end):
        loss_mask[i] = 1
    return loss_mask


def _validate_history(messages: list, declared_names: set):
    """Return a reason string or None. Every history tool call must be paired."""
    pending = []
    seen_ids = set()
    for msg in messages:
        role = msg.get("role")
        if role not in VALID_SDK_ROLES:
            return REASON_UNKNOWN_ROLE
        if message_text(msg.get("content")) is None or msg.get("content_parts"):
            return REASON_MULTIMODAL
        if role == "assistant":
            for call in msg.get("tool_calls") or []:
                fn = call.get("function") or {}
                if str(fn.get("name", "")) not in declared_names:
                    return REASON_UNBOUND_CALL
                call_id = call.get("id")
                if not call_id:
                    return REASON_BROKEN_PAIRING
                if call_id in seen_ids:
                    return REASON_AMBIGUOUS_TOOL_CALL_ID
                seen_ids.add(call_id)
                pending.append(call_id)
            continue
        if role == "tool":
            call_id = _tool_result_id(msg)
            if not call_id:
                return REASON_BROKEN_PAIRING
            if call_id not in pending:
                return REASON_ORPHAN_RESULT
            pending.remove(call_id)
            continue
        # system / user closes any outstanding call
        if pending:
            return REASON_BROKEN_PAIRING
    if pending:
        return REASON_BROKEN_PAIRING
    return None


def _validate_target(message: dict, declared_names: set):
    """Validate the assistant message to train on; return a reason string or None."""
    if message.get("role") not in (None, "assistant"):
        return REASON_BAD_CHOICE_ROLE
    if message.get("content_parts"):
        return REASON_MULTIMODAL
    if message_text(message.get("content")) is None:
        return REASON_MULTIMODAL
    text = (message_text(message.get("content")) or "").strip()
    calls = message.get("tool_calls") or []
    if not text and not calls:
        return REASON_EMPTY_TARGET
    for call in calls:
        fn = call.get("function") or {}
        if str(fn.get("name", "")) not in declared_names:
            return REASON_UNBOUND_CALL
        if not call.get("id"):
            return REASON_BROKEN_PAIRING
        args = _call_arguments(fn.get("arguments"))
        if args.strip():
            try:
                parsed = json.loads(args)
            except json.JSONDecodeError:
                return REASON_INVALID_ARGS_JSON
            if not isinstance(parsed, (dict, list)):
                return REASON_INVALID_ARGS_JSON
    return None


def apply_content_policy(policy: str, text: str, rules) -> str:
    """Redact text under the compiled rules. Never applied to ids/keys/schema."""
    if policy != "redact" or not rules:
        return text
    for _name, pattern, replacement in rules:
        text = pattern.sub(replacement, text)
    return text


def load_redact_rules(path: str) -> list:
    """Compile local redaction rules from a JSON file: {"rules":[{"pattern","replacement"}]}."""
    if not path:
        return []
    if not os.path.isfile(path):
        raise StrictError("--redact-rules file not found: %s" % path)
    with open(path, "r", encoding="utf-8") as f:
        try:
            loaded = json.loads(f.read())
        except json.JSONDecodeError as exc:
            raise StrictError("--redact-rules is not valid JSON: %s" % exc)
    specs = loaded.get("rules") if isinstance(loaded, dict) else loaded
    compiled = []
    for spec in specs or []:
        if not isinstance(spec, dict) or not spec.get("pattern"):
            continue
        try:
            rx = re.compile(spec["pattern"])
        except re.error as exc:
            raise StrictError("invalid redact pattern %r: %s" % (spec["pattern"], exc))
        compiled.append((str(spec.get("name", "rule")), rx,
                         str(spec.get("replacement", "[REDACTED]"))))
    return compiled


def _redact_message(message: dict, rules, stats) -> bool:
    """Redact content/args of a normalised message in place; False if JSON breaks."""
    ok = True
    if isinstance(message.get("content"), str):
        new = apply_content_policy("redact", message["content"], rules)
        if new != message["content"]:
            message["content"] = new
            stats["redacted_fields"] += 1
    for call in message.get("tool_calls") or []:
        fn = call.get("function")
        if not isinstance(fn, dict) or not isinstance(fn.get("arguments"), str):
            continue
        raw = fn["arguments"]
        new = apply_content_policy("redact", raw, rules)
        if raw.strip():
            try:
                json.loads(new)
            except json.JSONDecodeError:
                ok = False
                stats["broken_json"] += 1
        if new != raw:
            fn["arguments"] = new
            stats["redacted_fields"] += 1
    return ok


def _redact_feedback(feedback: list, rules, stats) -> list:
    out = []
    for item in feedback:
        if isinstance(item, dict):
            copy = dict(item)
            for key in ("note", "comment", "text", "value", "rationale"):
                if isinstance(copy.get(key), str):
                    new = apply_content_policy("redact", copy[key], rules)
                    if new != copy[key]:
                        copy[key] = new
                        stats["redacted_fields"] += 1
            out.append(copy)
        else:
            out.append(item)
    return out


def strict_messages(request: dict, target: dict) -> list:
    """Whole conversation for apply_chat_template: history + the target turn."""
    messages = []
    for msg in request.get("messages") or []:
        if not isinstance(msg, dict):
            continue
        norm = {"role": msg.get("role"), "content": message_text(msg.get("content")) or ""}
        if msg.get("tool_calls"):
            norm["tool_calls"] = msg["tool_calls"]
        call_id = _tool_result_id(msg)
        if msg.get("role") == "tool" and call_id:
            norm["tool_call_id"] = call_id
        messages.append(norm)
    tgt = {"role": "assistant", "content": message_text(target.get("content")) or ""}
    if target.get("tool_calls"):
        tgt["tool_calls"] = target["tool_calls"]
    messages.append(tgt)
    return messages


def _feedback_of(record: dict) -> list:
    feedback = record.get("feedback")
    if feedback is None:
        return [{"verdict": "unrated", "call_id": record.get("call_id")}]
    if isinstance(feedback, dict):
        return [feedback]
    if isinstance(feedback, list):
        kept = [f for f in feedback if isinstance(f, dict)]
        return kept or [{"verdict": "unrated"}]
    return [{"verdict": "unrated"}]


def _unrated_feedback(call_id):
    """An explicit "no judgement was recorded", never a silent 0 reward."""
    return [{"verdict": "unrated", "call_id": call_id}]


def resolve_feedback(record: dict, entry):
    """(feedback list, source) for one call: the fact index is authoritative.

    A v2 capture record carries no feedback at all — feedback is a committed event the
    export joins to this call. When the record itself carries a ``feedback`` field it is
    used only for a call with no fact entry (a hand-made or older stream), and the
    source says so, because that is provenance a consumer must be able to tell apart.
    """
    call_id = record.get("call_id")
    if entry is not None:
        if entry.get("feedback"):
            return list(entry["feedback"]), FEEDBACK_SOURCE_FACT_INDEX
        if entry.get("ambiguous"):
            return _unrated_feedback(call_id), FEEDBACK_SOURCE_AMBIGUOUS
        return _unrated_feedback(call_id), FEEDBACK_SOURCE_FACT_INDEX
    if record.get("feedback") is not None:
        return _feedback_of(record), FEEDBACK_SOURCE_RECORD
    return _unrated_feedback(call_id), FEEDBACK_SOURCE_UNRATED


def _split_of(group_key: str, train_ratio: float, split_seed: int) -> str:
    """Deterministic group-level split: every member of a group lands on one side."""
    ratio = max(0.0, min(1.0, float(train_ratio)))
    if ratio <= 0.0:
        return "test"
    if ratio >= 1.0:
        return "train"
    digest = hashlib.sha256(("%d\x1f%s" % (split_seed, group_key)).encode("utf-8")).hexdigest()
    bucket = int(digest[:8], 16) / float(0xFFFFFFFF)
    return "train" if bucket < ratio else "test"


def tokenizer_template_hash(tokenizer) -> str:
    template = getattr(tokenizer, "chat_template", None)
    if template is None:
        return "unknown"
    if isinstance(template, (dict, list)):
        template = json.dumps(template, sort_keys=True, ensure_ascii=False)
    return _sha256_text(str(template))


def tokenizer_version_of(tokenizer) -> str:
    explicit = getattr(tokenizer, "version", None)
    if explicit:
        return str(explicit)
    return "injected:" + type(tokenizer).__name__


def detect_local_tokenizer_capability(tokenizer) -> dict:
    """Probe an injected tokenizer for the abilities strict conversion depends on.

    Inspects only the in-memory object: nothing is downloaded and no remote code
    runs. Used by the acceptance path to record the local tokenizer's real
    tool / assistant-mask support.
    """
    capability = {
        "available": tokenizer is not None,
        "apply_chat_template": bool(tokenizer is not None
                                    and callable(getattr(tokenizer, "apply_chat_template", None))),
        "tools": False,
        "assistant_tokens_mask": False,
        "eos_token_id": getattr(tokenizer, "eos_token_id", None) if tokenizer else None,
    }
    if not capability["apply_chat_template"]:
        return capability
    probe_tools = [{"type": "function", "function": {"name": "areal_nonce_probe",
                                                    "description": "d",
                                                    "parameters": {"type": "object"}}}]
    try:
        out = tokenizer.apply_chat_template([{"role": "user", "content": "ping"}],
                                            tools=probe_tools, tokenize=False)
        capability["tools"] = "areal_nonce_probe" in str(out)
    except Exception:
        capability["tools"] = False
    try:
        out = tokenizer.apply_chat_template(
            [{"role": "user", "content": "a"}, {"role": "assistant", "content": "b"}],
            tools=None, tokenize=True, return_assistant_tokens_mask=True,
            return_dict=True, add_generation_prompt=False)
        mask = None
        if isinstance(out, dict):
            mask = out.get("assistant_tokens_mask")
            if mask is None:
                mask = out.get("assistant_masks")
        capability["assistant_tokens_mask"] = bool(mask) and 1 in list(mask)
    except Exception:
        capability["assistant_tokens_mask"] = False
    return capability


def build_tokenizer(tokenizer_id: str):
    """Load a local tool-aware tokenizer. Never downloads, never runs remote code."""
    if not tokenizer_id:
        raise StrictError("--strict requires --tokenizer <local-path> for a template encoding")
    try:
        from transformers import AutoTokenizer
    except ImportError:
        raise StrictError(
            "strict conversion needs transformers.AutoTokenizer, which is not installed in this "
            "interpreter; run it under a prepared training environment (no network download here)")
    try:
        return AutoTokenizer.from_pretrained(
            tokenizer_id, local_files_only=True, trust_remote_code=False)
    except TypeError:
        return AutoTokenizer.from_pretrained(tokenizer_id)


def build_dataset(samples: list):
    """Create a HuggingFace Dataset, or fail loudly. Never silently degrades to JSONL."""
    try:
        from datasets import Dataset
    except ImportError as exc:
        raise StrictError("--format hf needs the datasets package (not installed): %s" % exc)
    return Dataset.from_list(samples)


def split_samples(samples: list) -> dict:
    out = {"train": [], "test": []}
    for sample in samples:
        out.setdefault(sample.get("split", "train"), []).append(sample)
    return out


def check_output_dir(output_path: str) -> None:
    """Refuse to write into a non-empty directory (never overwrite an existing dataset)."""
    path = Path(output_path)
    if not path.exists():
        return
    if not path.is_dir():
        raise StrictError("output path is a file, refusing to overwrite: %s" % output_path)
    entries = sorted(os.listdir(path))
    if entries:
        raise StrictError("output directory is not empty, refusing to overwrite: %s (%d entries)"
                          % (output_path, len(entries)))


def convert_strict(records: list, fact_index: dict, manifest: dict, tokenizer, options: dict,
                   load_rejections: list = None) -> dict:
    """Run the strict v2 SFT conversion over BOTH streams.

    ``records`` is the capture stream and the PRIMARY record source: it is the only thing
    that can become a training sample. ``fact_index`` is the association layer built by
    :func:`build_fact_index` from the authorised fact export — it may only ADD an event
    key, bound feedback and a partition cross-check to a record, never stand in for one
    (a fact row has no SDK request/response, so feeding it as the record source produced
    nothing but ``not_v2_schema`` rejections). ``load_rejections`` seeds the ledger with
    lines the loader already ruled out of the primary stream, so every input line is
    accounted for. Returns {"samples","rejections","groups","stats"}.
    """
    options = dict(options or {})
    policy = options.get("content_policy") or "synthetic"
    if policy not in CONTENT_POLICIES:
        raise StrictError("unknown --content-policy: %s" % policy)
    rules = load_redact_rules(options.get("redact_rules_path")) if policy == "redact" else []
    if policy == "redact" and not rules:
        raise StrictError("%s: --content-policy redact needs local rules (--redact-rules)"
                          % REASON_REDACT_NO_RULES)
    tools_required = bool(options.get("tools_required", True))
    fact_index = fact_index or {}

    encoder = TemplateEncoder(tokenizer)
    template_hash = tokenizer_template_hash(tokenizer)
    tokenizer_version = tokenizer_version_of(tokenizer)

    samples = []
    rejections = []
    stats = {"seen": 0, "accepted": 0, "rejected": 0, "redacted_fields": 0, "broken_json": 0,
             "quality": {flag: 0 for flag in QUALITY_FLAGS},
             "fact_index_unmatched": 0}
    group_index = {}

    for seeded in load_rejections or []:
        rejections.append({"source": seeded.get("source", "<memory>"),
                           "call_id": seeded.get("call_id"),
                           "reason": seeded.get("reason")})
        stats["seen"] += 1

    def reject(source, call_id, reason):
        rejections.append({"source": source, "call_id": call_id, "reason": reason})

    prepared = []
    call_by_key = {}
    matched_calls = set()
    for record in records:
        stats["seen"] += 1
        source = record.get("_source", "<memory>")
        if record.get("_parse_error"):
            reject(source, None, record["_parse_error"])
            continue
        keep, reason = classify_capture_row(record)
        if not keep:
            reject(source, record.get("call_id"), reason)
            continue
        call_id = record.get("call_id")
        if not call_id:
            reject(source, None, REASON_MISSING_CALL_ID)
            continue
        status, detail = _manifest_run_status(manifest, record.get("run_id"))
        if status != "sealed":
            reject(source, call_id, "%s:%s" % (REASON_UNSEALED, detail))
            continue
        if record.get("binding_status") == "ambiguous":
            reject(source, call_id, REASON_AMBIGUOUS_ATTRIBUTION)
            continue
        request = get_request(record)
        response = get_response(record)
        tools = extract_tools(request)
        if tools_required and not tools:
            reject(source, call_id, REASON_MISSING_TOOLS)
            continue
        if _record_incomplete(record):
            reject(source, call_id, REASON_NONTERMINAL)
            continue
        finish = _record_finish_reason(record)
        if not finish or str(finish) not in TERMINAL_FINISH_REASONS:
            reject(source, call_id, REASON_NONTERMINAL)
            continue
        choices = response.get("choices") or []
        if not choices:
            reject(source, call_id, REASON_NO_CHOICE)
            continue
        target = choices[0].get("message") or {}
        declared_names = _declared_tool_names(tools)
        reason = _validate_target(target, declared_names)
        if reason:
            reject(source, call_id, reason)
            continue
        reason = _validate_history(request.get("messages") or [], declared_names)
        if reason:
            reject(source, call_id, reason)
            continue
        owner = get_owner(record)
        namespace = owner.get("capture_namespace") or owner.get("partition_id")
        root_session = owner.get("root_session_id") or owner.get("session_id")
        if not namespace or not root_session:
            reject(source, call_id, REASON_MISSING_PROVENANCE)
            continue

        # --- association layer (stream 2): additive only -----------------------
        entry = fact_index.get(str(call_id))
        matched_calls.add(str(call_id))
        quality = {flag: False for flag in QUALITY_FLAGS}
        event_keys = list(entry["event_keys"]) if entry else []
        if not event_keys:
            quality[QUALITY_EVENT_KEY_MISSING] = True
        if entry is None:
            quality[QUALITY_FACT_UNMATCHED] = True
        elif entry.get("ambiguous"):
            quality[QUALITY_FEEDBACK_AMBIGUOUS] = True
        owner_partition = _normalise_partition(owner.get("partition_id"))
        if owner_partition is None:
            owner_partition = _normalise_partition(namespace)
        if entry and owner_partition is not None:
            if any(part != owner_partition for part in entry["partitions"]):
                reject(source, call_id, REASON_OWNER_PARTITION_CONFLICT)
                continue

        # The record's own response identity: two records claiming one call_id while
        # pointing at different responses is contradictory attribution.
        record_identity = record.get("event_key") or record.get("response_id")
        key = "%s\x1f%s" % (record.get("run_id"), call_id)
        if key in call_by_key:
            other = prepared[call_by_key[key]]
            if other is None:
                # the slot was poisoned by an earlier ambiguity: every later
                # claim on the same key joins the rejection ledger instead of
                # vanishing between the accepted and rejected accounts.
                reject(source, call_id, REASON_DUPLICATE_CALL_ID)
                continue
            if other["identity"] != record_identity:
                # same (run_id, call_id) pointing at different events: the
                # attribution is ambiguous, so refuse BOTH instead of guessing.
                reject(other["source"], call_id, REASON_DUPLICATE_CALL_ID)
                reject(source, call_id, REASON_DUPLICATE_CALL_ID)
                prepared[call_by_key[key]] = None
                continue
            # an identical re-export of the same fact is deduplicated, not rejected.
            continue
        call_by_key[key] = len(prepared)
        feedback, feedback_source = resolve_feedback(record, entry)
        prepared.append({
            "record": record, "source": source, "call_id": call_id, "request": request,
            "target": target, "tools": tools, "namespace": namespace,
            "root_session": root_session, "identity": record_identity,
            "event_keys": event_keys, "feedback": feedback,
            "feedback_source": feedback_source, "quality": quality,
        })
    prepared = [item for item in prepared if item is not None]

    for item in prepared:
        record = item["record"]
        source = item["source"]
        call_id = item["call_id"]
        messages = strict_messages(item["request"], item["target"])
        feedback = item["feedback"]
        digest_before = _sha256_text(json.dumps(messages, sort_keys=True, ensure_ascii=False))
        if policy == "redact":
            ok = True
            for msg in messages:
                ok = _redact_message(msg, rules, stats) and ok
            dict_fb = [f for f in feedback if isinstance(f, dict)]
            other_fb = [f for f in feedback if not isinstance(f, dict)]
            feedback = _redact_feedback(dict_fb, rules, stats) + other_fb
            if not ok:
                reject(source, call_id, REASON_REDACT_BREAKS_JSON)
                continue
        try:
            encoded = encoder.encode(messages, item["tools"])
        except _MaskUnsupported as exc:
            reject(source, call_id, exc.reason)
            continue

        group_key = "%s\x1f%s" % (item["namespace"], item["root_session"])
        group_index.setdefault(group_key, len(group_index))
        sample = dict(encoded)
        sample.update({
            "sample_id": "%s:%s" % (record.get("run_id"), call_id),
            "schema_version": SCHEMA_VERSION_V2,
            "run_id": record.get("run_id"),
            "source_call_ids": [call_id],
            "source_event_keys": item["event_keys"],
            "capture_namespace": item["namespace"],
            "root_session_id": item["root_session"],
            "feedback": feedback,
            "feedback_source": item["feedback_source"],
            "quality": dict(item["quality"]),
            "request_digest": record.get("request_digest"),
            "tools_digest": _sha256_text(json.dumps(item["tools"], sort_keys=True)),
            "messages": messages,
            "split": _split_of(group_key, options.get("train_ratio", 0.9),
                               int(options.get("split_seed", 0) or 0)),
            "manifest_sha256": manifest["manifest_sha256"],
            "template_hash": template_hash,
            "tokenizer_version": tokenizer_version,
            "content_policy": policy,
            "content_policy_version": "v1",
            "content_digest_before": digest_before,
            "content_digest_after": _sha256_text(
                json.dumps(messages, sort_keys=True, ensure_ascii=False)),
        })
        for flag, flagged in item["quality"].items():
            if flagged:
                stats["quality"][flag] += 1
        samples.append(sample)
        stats["accepted"] += 1

    stats["rejected"] = len(rejections)
    stats["fact_index_unmatched"] = len([call for call in fact_index if call not in matched_calls])
    return {"samples": samples, "rejections": rejections, "groups": dict(group_index),
            "stats": stats}


def _strip_internal(sample: dict) -> dict:
    return {k: v for k, v in sample.items() if not k.startswith("_")}


def write_jsonl(samples: list, output_path: str) -> None:
    path = Path(output_path)
    path.mkdir(parents=True, exist_ok=True)
    for split, items in split_samples(samples).items():
        with open(path / ("sft_%s.jsonl" % split), "w", encoding="utf-8") as f:
            for sample in items:
                f.write(json.dumps(_strip_internal(sample), ensure_ascii=False) + "\n")


def write_hf(samples: list, output_path: str) -> None:
    path = Path(output_path)
    path.mkdir(parents=True, exist_ok=True)
    parts = {split: build_dataset([_strip_internal(s) for s in items])
             for split, items in split_samples(samples).items() if items}
    if len(parts) == 1:
        # A one-sided split (e.g. train_ratio=1.0) writes a FLAT single Dataset;
        # the DatasetDict layout below is only for genuinely two-sided outputs.
        for dataset in parts.values():
            dataset.save_to_disk(str(path))
        return
    try:
        from datasets import DatasetDict
        DatasetDict(parts).save_to_disk(str(path))
    except ImportError:
        for split, dataset in parts.items():
            dataset.save_to_disk(str(path / split))


def write_rejections(rejections: list, output_path: str) -> None:
    path = Path(output_path)
    path.mkdir(parents=True, exist_ok=True)
    with open(path / "rejected.jsonl", "w", encoding="utf-8") as f:
        for rejection in rejections:
            f.write(json.dumps(rejection, ensure_ascii=False) + "\n")


def write_conversion_manifest(result: dict, output_path: str, manifest: dict, options: dict,
                              fact_index: dict = None) -> dict:
    path = Path(output_path)
    path.mkdir(parents=True, exist_ok=True)
    samples = result["samples"]
    groups = {}
    for sample in samples:
        groups.setdefault(sample["root_session_id"], set()).add(sample["capture_namespace"])
    doc = {
        "schema_version": SCHEMA_VERSION_V2,
        "conversion": "strict",
        "streams": {"record_source": "capture (--input)", "association_index": "facts (--events)"},
        "capture_manifest_sha256": manifest["manifest_sha256"],
        "tokenizer_id": options.get("tokenizer_id"),
        "tokenizer_version": samples[0]["tokenizer_version"] if samples else "unknown",
        "template_hash": samples[0]["template_hash"] if samples else "unknown",
        "content_policy": options.get("content_policy"),
        "content_policy_version": "v1",
        "train_ratio": options.get("train_ratio"),
        "split_seed": options.get("split_seed"),
        "tools_required": bool(options.get("tools_required", True)),
        "stats": result["stats"],
        "split_counts": {k: len(v) for k, v in split_samples(samples).items()},
        "groups": {root: sorted(str(ns) for ns in names) for root, names in groups.items()},
        "rejections": len(result["rejections"]),
    }
    if fact_index is not None:
        doc["fact_index"] = dict(fact_index)
    with open(path / "conversion_manifest.json", "w", encoding="utf-8") as f:
        json.dump(doc, f, ensure_ascii=False, sort_keys=True, indent=2)
        f.write("\n")
    return doc


def run_strict(args, tokenizer=None) -> int:
    """Execute strict conversion over both streams; return the process exit code.

    ``tokenizer`` is injectable so the logic is testable without transformers; the real
    acceptance path passes a local tool-aware tokenizer.
    """
    if not getattr(args, "input", None):
        raise StrictError("--strict requires --input <capture .jsonl file or dir> "
                          "(the primary record source)")
    if not args.events:
        raise StrictError("--strict requires --events <facts.jsonl file or dir> "
                          "(the authorised fact export = association index)")
    records, load_rejections = load_capture_records(args.input)
    fact_rows = load_facts(args.events)
    fact_index, index_stats = build_fact_index(fact_rows)
    manifest = load_capture_manifest(args.manifest)
    check_output_dir(args.output)
    if tokenizer is None:
        tokenizer = build_tokenizer(args.tokenizer)
    options = {
        "content_policy": args.content_policy,
        "train_ratio": args.train_ratio,
        "split_seed": args.split_seed,
        "redact_rules_path": args.redact_rules,
        "tokenizer_id": args.tokenizer,
        "tools_required": not args.allow_tools_free,
    }
    result = convert_strict(records, fact_index, manifest, tokenizer, options,
                            load_rejections=load_rejections)
    write_rejections(result["rejections"], args.output)
    doc = write_conversion_manifest(result, args.output, manifest, options, fact_index=index_stats)

    for line in result["rejections"][:20]:
        print("rejected: source=%s call_id=%s reason=%s"
              % (line["source"], line.get("call_id"), line["reason"]), file=sys.stderr)
    if len(result["rejections"]) > 20:
        print("rejected: ... %d more in %s"
              % (len(result["rejections"]) - 20, Path(args.output) / "rejected.jsonl"),
              file=sys.stderr)
    accepted = len(result["samples"])
    print("strict: capture %d v2 record(s) + %d fact line(s) indexing %d call(s) "
          "(bound feedback %d, ambiguous %d, expired-or-missing %d)"
          % (len(records), index_stats["rows"], index_stats["indexed_calls"],
             index_stats["feedback_bound"], index_stats["feedback_ambiguous"],
             index_stats["join_expired_or_missing"]), file=sys.stderr)
    print("strict: %d samples accepted, %d rejected (split=%s, quality=%s)"
          % (accepted, len(result["rejections"]), doc["split_counts"],
             result["stats"]["quality"]), file=sys.stderr)
    if not accepted:
        print("strict: no accepted sample; refusing to write an empty dataset", file=sys.stderr)
        return 1
    if args.format == "hf":
        write_hf(result["samples"], args.output)
    else:
        write_jsonl(result["samples"], args.output)
    print("strict: wrote %s format=%s" % (args.output, args.format), file=sys.stderr)
    return 0


def _warn_legacy(args) -> None:
    """Legacy entry stays functional but is explicitly not equivalent to strict."""
    print("WARNING: legacy mode is NOT equivalent to --strict", file=sys.stderr)
    print("WARNING: prompt/completion are encoded separately and the loss boundary is assumed",
          file=sys.stderr)
    print("WARNING: multimodal content parts and empty-body tool-call turns are dropped",
          file=sys.stderr)
    print("WARNING: no capture seal is consulted; completeness is unknown/legacy", file=sys.stderr)
    if args.mode == "sft":
        print("WARNING: role/content joined as plain text (no chat template, no tools)",
              file=sys.stderr)
    else:
        print("WARNING: rl mode emits prompt-only messages; not a training sample", file=sys.stderr)


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Convert tagent trajectory JSONL to AReaL SFT/RL dataset format")
    parser.add_argument("--input", default=None,
                        help="Input .jsonl dir/file: legacy mode input, and with --strict the "
                             "capture stream (the primary record source)")
    parser.add_argument("--output", required=True, help="Output directory for the dataset")
    parser.add_argument("--mode", choices=["sft", "rl"], default=None,
                        help="Legacy conversion mode")
    parser.add_argument("--tokenizer", default=None,
                        help="HuggingFace tokenizer name/path (required for SFT/strict)")
    parser.add_argument("--strict", action="store_true",
                        help="Enable v2 strict tool-aware SFT conversion (fails loudly)")
    parser.add_argument("--events", default=None,
                        help="With --strict: the authorised fact export (rl.ExportedFact JSONL), "
                             "used as the call_id -> event_key/parent/feedback index")
    parser.add_argument("--manifest", default=None,
                        help="Capture seal manifest for --strict (proves completeness)")
    parser.add_argument("--format", choices=["jsonl", "hf"], default="jsonl",
                        help="Strict output format; 'hf' requires the datasets package")
    parser.add_argument("--train-ratio", dest="train_ratio", type=float, default=0.9,
                        help="Fraction of root-session groups assigned to train")
    parser.add_argument("--split-seed", dest="split_seed", type=int, default=0,
                        help="Seed for the deterministic group-level split")
    parser.add_argument("--content-policy", dest="content_policy",
                        choices=list(CONTENT_POLICIES), default="synthetic",
                        help="Privacy handling for user content in exported samples")
    parser.add_argument("--redact-rules", dest="redact_rules", default=None,
                        help="Local redaction rules JSON (required with --content-policy redact)")
    parser.add_argument("--allow-tools-free", dest="allow_tools_free", action="store_true",
                        help="Accept records with no declared tools (weakens strict)")

    args = parser.parse_args(argv)

    if args.strict:
        if not args.input:
            parser.error("--strict requires --input <capture.jsonl dir/file> (primary record source)")
        if not args.events:
            parser.error("--strict requires --events <facts.jsonl> (association index stream)")
        if not args.manifest:
            parser.error("--strict requires --manifest <capture-manifest.json> "
                         "(the seal that proves the capture is complete)")
        try:
            return run_strict(args)
        except StrictError as exc:
            print("Error: %s" % exc, file=sys.stderr)
            return 1

    if not args.mode:
        parser.error("--mode is required unless --strict is set")
    if args.input is None:
        parser.error("--input is required unless --strict is set")
    _warn_legacy(args)
    if args.mode == "sft" and not args.tokenizer:
        parser.error("--tokenizer is required for SFT mode")

    records = load_trajectories(args.input)
    if not records:
        print("No valid trajectory records found", file=sys.stderr)
        sys.exit(1)

    if args.mode == "sft":
        from transformers import AutoTokenizer
        tokenizer = AutoTokenizer.from_pretrained(args.tokenizer)
        samples = convert_to_sft(records, tokenizer)
    else:
        samples = convert_to_rl(records)

    if not samples:
        print("No valid samples after conversion", file=sys.stderr)
        sys.exit(1)

    save_dataset(samples, args.output, args.mode)
    return 0


if __name__ == "__main__":
    sys.exit(main())
