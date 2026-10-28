#!/usr/bin/env python3
"""test_convert_trajectories.py — v2 strict 双流转换回归（O6.1/O6.4/O6.5 + 6.6 的 Python 半）

fixture 与 rl/ 的真实写出形状对齐，三条流分开构造（旧版把事实字段混写进记录，正是
run_strict 拿 --events 当主记录源、整批判 not_v2_schema 的集成缺口的温床）：

  1. capture 流（**主记录源**，--input）：rl.CaptureRecord 一行一次模型调用，
     含顶层 response_id/request_digest/binding_status/missing_reasons 与平铺 owner；
  2. facts 流（**关联索引层**，--events）：rl.ExportedFact = memory.FullEvent + parent_key，
     feedback 行为 memory.FeedbackPayload 的 JSON content，按 parent→parent.metadata.call_id 关联；
  3. manifest（--manifest）：rl.MarshalCaptureManifest 的 {"runs": {run_id: {平铺计数}}}。

不依赖 transformers：strict 模板路径经注入式 tokenizer 适配器 TemplateEncoder，
测试用确定性 FakeTokenizer 替身，只证明逻辑正确，不冒充真实 tokenizer 验收。

运行: python3 -m unittest discover -s scripts -p test_convert_trajectories.py -v
"""
from __future__ import annotations

import contextlib
import hashlib
import io
import json
import os
import tempfile
import unittest

import convert_trajectories as ct

# rl.CaptureToolDeclaration: registry_key/name/description + inputSchema (framework spelling).
TOOL_DECL = {
    "registry_key": "nonce",
    "name": "nonce",
    "description": "nonce tool",
    "inputSchema": {"type": "object", "properties": {"token": {"type": "string"}}},
}


class FakeTokenizer:
    """Minimal tool-aware tokenizer stand-in implementing only the adapter protocol.

    modes:
      ok        -> apply_chat_template(tokenize=True, return_assistant_tokens_mask=True)
                   returns {input_ids, assistant_tokens_mask} with a 1-run per assistant.
      no_kwarg  -> rejects return_assistant_tokens_mask (TypeError), like a template
                   without generation-mask support.
      zero_mask -> accepts it but reports an all-zero assistant mask.
    """

    def __init__(self, mode="ok"):
        self.mode = mode
        self.version = "fake-1"
        self.name_or_path = "fake/tokenizer"
        self.chat_template = "fake tool-aware chatml template"
        self.eos_token_id = 0
        self.encode_calls = 0
        self.seen_messages = []
        self.seen_tools = []

    @staticmethod
    def _ids(text):
        return [(i * 13 + ord(c)) % 99999 + 1 for i, c in enumerate(text)]

    def apply_chat_template(self, messages, tools=None, tokenize=True,
                            return_assistant_tokens_mask=False, return_dict=False,
                            add_generation_prompt=False):
        self.encode_calls += 1
        self.seen_messages.append([dict(m) for m in messages])
        self.seen_tools.append(tools)

        if not tokenize:  # capability probe renders text
            chunks = [str(m.get("content", "")) for m in messages]
            if tools:
                chunks.append(" ".join(t["function"]["name"] for t in tools))
            return "\n".join(chunks)

        if self.mode == "no_kwarg" and return_assistant_tokens_mask:
            raise TypeError("this chat template does not support return_assistant_tokens_mask")

        ids, masks = [], []
        for m in messages:
            role = m.get("role", "user")
            head = self._ids("<|%s|>" % role)
            ids += head
            masks += [0] * len(head)

            if role == "assistant":
                parts = []
                if m.get("content"):
                    parts.append(str(m["content"]))
                for call in m.get("tool_calls") or []:
                    fn = call.get("function") or {}
                    parts.append("call:%s(%s)(%s)" % (call.get("id"), fn.get("name"),
                                                      fn.get("arguments")))
                body = " ".join(parts) or "<none>"
                tokens = self._ids(body)
                ids += tokens
                if self.mode == "zero_mask":
                    masks += [0] * len(tokens)
                else:
                    masks += [1] * len(tokens)
            elif role == "tool":
                tokens = self._ids("%s=%s" % (m.get("tool_call_id", ""), m.get("content", "")))
                ids += tokens
                masks += [0] * len(tokens)
            else:  # system / user
                tokens = self._ids(str(m.get("content", "")))
                ids += tokens
                masks += [0] * len(tokens)

            term = self._ids("<|end|>")
            ids += term
            masks += [0] * len(term)

        if return_dict and return_assistant_tokens_mask:
            return {"input_ids": ids, "assistant_tokens_mask": masks}
        return ids


# ---------------------------------------------------------------------------
# stream 1 fixture: the capture record (rl.CaptureRecord, schema_version=2)
# ---------------------------------------------------------------------------

def _event_key_for(call_id):
    """A deterministic snowflake-shaped event key for one call_id."""
    return int(hashlib.sha256(("evt:%s" % call_id).encode("utf-8")).hexdigest()[:15], 16)


def capture_record(call_id="runA-1", run_id="runA", ns="7", root="sess-root", *,
                   messages=None, target=None, tools=None, finish="stop", binding="bound",
                   incomplete=False, response_id=None, digest=None, partition_id=None,
                   owner=None, line=1, **overrides):
    """One v2 capture line, shaped exactly as rl/trajectory_capture.go writes it."""
    if messages is None:
        messages = [{"role": "system", "content": "SYS"},
                    {"role": "user", "content": "need a nonce"}]
    if target is None:
        target = {"role": "assistant", "content": "here is the answer"}
    if response_id is None:
        response_id = "resp-" + call_id
    owner_block = owner if owner is not None else {
        "capture_namespace": ns,
        "root_session_id": root,
        "agent_name": "entry",
        "partition_id": ns if partition_id is None else partition_id,
        "session_id": root,
        "user_id": "user-1",
        "invocation_id": "inv-" + call_id,
        "parent_invocation_id": "",
        "task_id": "",
        "purpose": "chat",
        "input_event_keys": [],
    }
    record = {
        # v1 fields, unchanged in name or meaning
        "timestamp": "2026-10-08T00:00:00Z",
        "session_id": root,
        "user_id": "user-1",
        "batch_index": 0,
        "llm_call": {
            "request": {"messages": messages, "model": "glm-5",
                        "generation_config": {"max_tokens": 128},
                        "tools": [TOOL_DECL] if tools is None else tools},
            "response": {"choices": [{"index": 0, "message": target,
                                      "finish_reason": finish or None}],
                         "usage": {"prompt_tokens": 7, "completion_tokens": 5,
                                   "total_tokens": 12},
                         "finish_reason": finish},
            "response_fragments": [],
            "terminal_status": {"kind": "done" if finish else "closed_without_terminal",
                                "finish_reason": finish, "response_id": response_id,
                                "fragments": 1},
            "response_incomplete": incomplete,
        },
        "metadata": {"duration_ms": 12, "model_endpoint": "https://example.invalid/v4"},
        # v2 additions
        "schema_version": 2,
        "capture_scope": "sdk_request",
        "run_id": run_id,
        "call_id": call_id,
        "owner": owner_block,
        "binding_status": binding,
        "missing_reasons": [],
        "request_digest": digest or ("digest-" + call_id),
        "response_incomplete": incomplete,
        "response_id": response_id,
    }
    record.update(overrides)
    record["_source"] = "capture.jsonl:%d" % line
    return record


# ---------------------------------------------------------------------------
# stream 2 fixture: the authorised fact export (rl.ExportedFact) + feedback lines
# ---------------------------------------------------------------------------

def fact_row(call_id="runA-1", event_key=None, partition_id=7, event_type="agent_output",
             parent_key="", metadata=None, timestamp=1762500000000, line=1, **overrides):
    """One exported fact line: memory.FullEvent copy + parent_key (absent = "")."""
    if event_key is None:
        event_key = _event_key_for(call_id)
    meta = {"call_id": call_id, "agent_name": "entry", "rollout_id": "sess-root"}
    meta.update(metadata or {})
    row = {
        "event_key": event_key,
        "partition_id": partition_id,
        "event_type": event_type,
        "event_summary": "%s: %s" % (event_type, call_id),
        "timestamp": timestamp,
        "content": json.dumps({"text": "committed fact for %s" % call_id}),
        "tool_calls": [],
        "tool_results": {},
        "metadata": meta,
        "parent_key": parent_key,
    }
    row.update(overrides)
    row["_source"] = "facts.jsonl:%d" % line
    return row


def fact_for(record, **overrides):
    """The committed-fact line the export writes for one captured call (same partition)."""
    owner = record.get("owner") or {}
    partition = overrides.pop("partition_id", None)
    if partition is None:
        for key in ("partition_id", "capture_namespace"):
            value = str(owner.get(key) or "").strip()
            if value.isdigit():
                partition = int(value)
                break
        else:
            partition = 1
    call_id = record.get("call_id")
    return fact_row(call_id=call_id, partition_id=partition,
                    event_key=overrides.pop("event_key", _event_key_for(call_id)), **overrides)


def feedback_row(parent_key="runA-1", event_key=None, verdict="good", note=None, rating=None,
                 source="user", partition_id=7, timestamp=1762500001000, line=99, **overrides):
    """One feedback fact: memory.FeedbackPayload JSON, bound through ITS parent.

    ``parent_key`` accepts an event key in any spelling the export writes (or, as
    fixture shorthand, a call_id); pass None for task-level feedback, which carries
    no parent pointer at all and therefore never attaches to "the nearest" call.
    """
    if event_key is None:
        event_key = _event_key_for("feedback:" + str(parent_key))
    parent = ""
    if parent_key is not None:
        parent = ct.canonical_event_key(parent_key)
        if not parent:
            # fixture shorthand: a call_id stands for the event key fact_row gives it.
            parent = ct.canonical_event_key(_event_key_for(str(parent_key)))
    payload = {"verdict": verdict, "source": source, "timestamp": timestamp}
    if note is not None:
        payload["note"] = note
    if rating is not None:
        payload["rating"] = rating
    if parent:
        payload["parent_key"] = parent
    row = {
        "event_key": event_key,
        "partition_id": partition_id,
        "event_type": "feedback",
        "event_summary": "feedback[%s]: %s → %s" % (source, verdict, parent),
        "timestamp": timestamp,
        "content": json.dumps(payload, ensure_ascii=False),
        "tool_calls": [],
        "tool_results": {},
        "metadata": {"subtype": source, "verdict": verdict},
        "parent_key": parent,
    }
    row.update(overrides)
    row["_source"] = "facts.jsonl:%d" % line
    return row


def feedback_for(record, **overrides):
    """A feedback line bound to one captured call, exactly as BindFeedback writes it."""
    return feedback_row(parent_key=_event_key_for(record.get("call_id")), **overrides)


# ---------------------------------------------------------------------------
# stream 3 fixture: the capture seal (rl.MarshalCaptureManifest; flat counters)
# ---------------------------------------------------------------------------

def sealed_manifest(*run_ids, written=12):
    runs = {}
    for run_id in run_ids:
        runs[run_id] = {
            "run_id": run_id, "cutoff_seq": 5, "status": "sealed",
            "started": written, "enqueued": written, "written": written,
            "dropped_full": 0, "dropped_closed": 0, "dropped_disk_limit": 0,
            "oversized": 0, "serialize_failed": 0, "write_failed": 0, "sync_failed": 0,
            "unbound_no_evidence": 0, "unbound_no_response_id": 0, "unbound_capacity": 0,
            "unbound_released": 0, "ambiguous": 0,
            "inflight": 0, "pending": 0, "pending_bytes": 0,
            "max_pending_bytes_observed": 0, "run_bytes": 4096, "open_files": 1,
            "synchronized": True, "sealed": True, "complete": True,
            "files": [{"session_id": "sess-root", "path": "sess-root.jsonl",
                       "run_bytes": 4096, "records": written, "sha256": "FILE-SHA",
                       "cutoff_seq": 5}],
        }
    return {"runs": runs, "manifest_sha256": "MANIFEST-HASH",
            "complete": True, "synchronized": True}


def opts(**kw):
    base = {"content_policy": "synthetic", "train_ratio": 0.9, "split_seed": 0,
            "redact_rules_path": None, "tokenizer_id": "fake", "tools_required": True}
    base.update(kw)
    return base


def index_of(*rows):
    """Association index over fact rows, as run_strict builds it from --events."""
    return ct.build_fact_index(list(rows))[0]


def convert(records, manifest=None, tokenizer=None, options=None, facts=None,
            load_rejections=None):
    """Strict conversion over BOTH streams: records are primary, facts only index."""
    return ct.convert_strict(list(records), index_of(*(facts or [])),
                             manifest if manifest is not None else sealed_manifest("runA"),
                             tokenizer or FakeTokenizer(), options or opts(),
                             load_rejections=load_rejections)


def reasons(result):
    return [r["reason"] for r in result["rejections"]]


def assistant_tool_call_target(arguments='{"token": "abc", "note": "keep  spaces"}',
                               call_id="call-target"):
    return {"role": "assistant", "content": "",
            "tool_calls": [{"id": call_id, "type": "function",
                            "function": {"name": "nonce", "arguments": arguments}}]}


def _runs(mask):
    out, start = [], None
    for i, v in enumerate(mask):
        if v == 1 and start is None:
            start = i
        elif v == 0 and start is not None:
            out.append((start, i))
            start = None
    if start is not None:
        out.append((start, len(mask)))
    return out


class TestToolRoundtrip(unittest.TestCase):
    def test_tool_call_target_and_history_pairing_accepted(self):
        # history: system, user, assistant(call-0), tool(call-0) — a closed roundtrip.
        # target: assistant(empty body + call-target) — its result has not happened yet.
        history = [
            {"role": "system", "content": "SYS"},
            {"role": "user", "content": "call the nonce"},
            {"role": "assistant", "content": "", "tool_calls": [
                {"id": "call-0", "type": "function",
                 "function": {"name": "nonce", "arguments": '{"token":"seed"}'}}]},
            {"role": "tool", "tool_id": "call-0", "content": "seed ok"},
        ]
        rec = capture_record(messages=history, target=assistant_tool_call_target(),
                             finish="tool_calls")
        tok = FakeTokenizer()
        res = convert([rec], sealed_manifest("runA"), tok, opts())
        self.assertEqual(len(res["samples"]), 1, res["rejections"])
        self.assertEqual(tok.encode_calls, 1)  # ONE template call, never re-encode

        sample = res["samples"][0]
        self.assertEqual(sample["loss_mask"], sample["loss_mask"])
        # raw arguments original string and call id are preserved verbatim
        last_msg = sample["messages"][-1]
        self.assertEqual(last_msg["content"], "")
        self.assertEqual(last_msg["tool_calls"][0]["function"]["arguments"],
                         '{"token": "abc", "note": "keep  spaces"}')
        self.assertEqual(last_msg["tool_calls"][0]["id"], "call-target")
        # the tool observation in the encoded history keeps its tool_call_id
        tool_rows = [m for m in sample["messages"] if m.get("role") == "tool"]
        self.assertEqual(tool_rows[0]["tool_call_id"], "call-0")
        # declared tool schema reached the template
        self.assertIn("nonce", [t["function"]["name"] for t in tok.seen_tools[0]])

    def test_labels_ignore_index_and_only_target_counts_loss(self):
        history = [
            {"role": "system", "content": "SYS"},
            {"role": "user", "content": "q1"},
            {"role": "assistant", "content": "a1"},        # history assistant: must be loss 0
            {"role": "user", "content": "q2"},
        ]
        rec = capture_record(messages=history,
                             target={"role": "assistant", "content": "final action"})
        tok = FakeTokenizer()
        res = convert([rec], sealed_manifest("runA"), tok, opts())
        self.assertEqual(len(res["samples"]), 1, res["rejections"])
        self.assertEqual(tok.encode_calls, 1)  # one whole-sequence encode, never split
        sample = res["samples"][0]
        ids, labels, mask = sample["input_ids"], sample["labels"], sample["loss_mask"]
        self.assertEqual(len(ids), len(labels))
        self.assertEqual(len(ids), len(mask))
        self.assertEqual(len(ids), len(sample["attention_mask"]))
        # labels == input_ids on loss positions, -100 elsewhere
        for iid, lab, m in zip(ids, labels, mask):
            self.assertEqual(lab, iid if m == 1 else ct.LOSS_IGNORE_INDEX)
        # Independently recompute the template assistant mask and derive the trailing
        # (target) assistant span WITHOUT touching _target_loss_mask, so a regression
        # that truncates the span cannot pass silently.
        raw = tok.apply_chat_template(tok.seen_messages[0], tools=tok.seen_tools[0],
                                      tokenize=True, return_assistant_tokens_mask=True,
                                      return_dict=True, add_generation_prompt=False)
        am = raw["assistant_tokens_mask"]
        self.assertGreaterEqual(len(_runs(am)), 2)  # history assistant + target
        end = len(am)
        while end > 0 and am[end - 1] == 0:
            end -= 1
        start = end
        while start > 0 and am[start - 1] == 1:
            start -= 1
        expected = [0] * len(am)
        for i in range(start, end):
            expected[i] = 1
        self.assertEqual(mask, expected)
        self.assertGreater(sum(mask), 1)     # target body is more than a single token
        self.assertEqual(len(_runs(mask)), 1)  # only the target assistant carries loss

    def test_empty_body_tool_call_target_has_nonempty_mask(self):
        rec = capture_record(messages=[{"role": "user", "content": "do it"}],
                             target=assistant_tool_call_target(), finish="tool_calls")
        res = convert([rec])
        self.assertEqual(len(res["samples"]), 1, res["rejections"])
        self.assertGreater(sum(res["samples"][0]["loss_mask"]), 0)

    def test_target_without_content_or_calls_is_empty_target(self):
        rec = capture_record(messages=[{"role": "user", "content": "x"}],
                             target={"role": "assistant", "content": ""})
        res = convert([rec])
        self.assertEqual(reasons(res), [ct.REASON_EMPTY_TARGET])

    def test_missing_tools_rejected(self):
        rec = capture_record(messages=[{"role": "user", "content": "x"}], tools=[])
        res = convert([rec])
        self.assertEqual(reasons(res), [ct.REASON_MISSING_TOOLS])
        # tools_required=False lets it through
        res2 = convert([rec], options=opts(tools_required=False))
        self.assertEqual(len(res2["samples"]), 1)


class TestMaskUnsupported(unittest.TestCase):
    def test_template_without_mask_support_is_refused(self):
        rec = capture_record(messages=[{"role": "user", "content": "x"}],
                             target={"role": "assistant", "content": "y"})
        res = convert([rec], tokenizer=FakeTokenizer(mode="no_kwarg"))
        self.assertEqual(reasons(res), [ct.REASON_MASK_UNSUPPORTED])

    def test_template_reporting_zero_mask_is_refused(self):
        rec = capture_record(messages=[{"role": "user", "content": "x"}],
                             target={"role": "assistant", "content": "y"})
        res = convert([rec], tokenizer=FakeTokenizer(mode="zero_mask"))
        self.assertEqual(reasons(res), [ct.REASON_TARGET_MASK_EMPTY])


class TestHistoryPairing(unittest.TestCase):
    def _with_history(self, history):
        rec = capture_record(messages=history, target={"role": "assistant", "content": "done"})
        return convert([rec])

    def test_unanswered_history_call_breaks_pairing(self):
        history = [
            {"role": "user", "content": "go"},
            {"role": "assistant", "content": "", "tool_calls": [
                {"id": "call-9", "type": "function",
                 "function": {"name": "nonce", "arguments": "{}"}}]},
            {"role": "user", "content": "still waiting"},  # broke: no tool result for call-9
        ]
        self.assertEqual(reasons(self._with_history(history)), [ct.REASON_BROKEN_PAIRING])

    def test_orphan_tool_result_rejected(self):
        history = [
            {"role": "user", "content": "go"},
            {"role": "tool", "tool_id": "call-ghost", "content": "nobody asked"},
        ]
        self.assertEqual(reasons(self._with_history(history)), [ct.REASON_ORPHAN_RESULT])

    def test_unbound_tool_name_rejected(self):
        history = [
            {"role": "user", "content": "go"},
            {"role": "assistant", "content": "", "tool_calls": [
                {"id": "call-1", "type": "function",
                 "function": {"name": "not_declared", "arguments": "{}"}}]},
            {"role": "tool", "tool_id": "call-1", "content": "ok"},
        ]
        self.assertEqual(reasons(self._with_history(history)), [ct.REASON_UNBOUND_CALL])

    def test_invalid_arguments_json_rejected(self):
        rec = capture_record(messages=[{"role": "user", "content": "x"}],
                             target={"role": "assistant", "content": "", "tool_calls": [
                                 {"id": "call-t", "type": "function",
                                  "function": {"name": "nonce", "arguments": "{not json}"}}]},
                             finish="tool_calls")
        res = convert([rec])
        self.assertEqual(reasons(res), [ct.REASON_INVALID_ARGS_JSON])

    def test_ambiguous_tool_call_id_rejected(self):
        history = [
            {"role": "user", "content": "go"},
            {"role": "assistant", "content": "", "tool_calls": [
                {"id": "call-dup", "type": "function",
                 "function": {"name": "nonce", "arguments": "{}"}},
                {"id": "call-dup", "type": "function",
                 "function": {"name": "nonce", "arguments": "{}"}},
            ]},
        ]
        self.assertEqual(reasons(self._with_history(history)),
                         [ct.REASON_AMBIGUOUS_TOOL_CALL_ID])


class TestRecordValidation(unittest.TestCase):
    def _reason(self, rec):
        return reasons(convert([rec]))

    def test_legacy_record_not_v2(self):
        rec = capture_record()
        del rec["schema_version"]
        # still carries call_id, so it is not a v1 dump line: it fails the v2 schema
        self.assertEqual(self._reason(rec), [ct.REASON_NOT_V2])

    def test_unsealed_run_rejected(self):
        rec = capture_record(run_id="runZ")
        self.assertEqual(self._reason(rec)[0].split(":")[0], ct.REASON_UNSEALED)

    def test_intermediate_flush_not_sealed_rejected(self):
        # MarshalCaptureManifest carries rl.CaptureManifest.Sealed per run: an
        # intermediate FlushAndWait (sealed=false) must not license any sample.
        manifest = sealed_manifest("runA")
        manifest["runs"]["runA"]["sealed"] = False
        rec = capture_record()
        self.assertEqual(reasons(convert([rec], manifest))[0].split(":")[0],
                         ct.REASON_UNSEALED)

    def test_disk_limit_drop_rejected(self):
        manifest = sealed_manifest("runA")
        manifest["runs"]["runA"]["dropped_disk_limit"] = 1
        self.assertEqual(reasons(convert([capture_record()], manifest))[0].split(":")[0],
                         ct.REASON_UNSEALED)

    def test_response_incomplete_rejected(self):
        rec = capture_record(incomplete=True)
        self.assertEqual(self._reason(rec), [ct.REASON_NONTERMINAL])

    def test_llm_call_level_incomplete_rejected(self):
        # the writer sets response_incomplete BOTH at the top level and in llm_call
        rec = capture_record()
        rec["response_incomplete"] = False
        rec["llm_call"]["response_incomplete"] = True
        self.assertEqual(self._reason(rec), [ct.REASON_NONTERMINAL])

    def test_missing_terminal_finish_rejected(self):
        rec = capture_record(finish="")
        self.assertEqual(self._reason(rec), [ct.REASON_NONTERMINAL])

    def test_finish_reason_only_in_terminal_status_accepted(self):
        # a v2 record may state the finish reason only in llm_call.terminal_status
        rec = capture_record(finish="stop")
        rec["llm_call"]["response"].pop("finish_reason")
        rec["llm_call"]["response"]["choices"][0]["finish_reason"] = None
        self.assertEqual(self._reason(rec), [])

    def test_multimodal_parts_rejected(self):
        rec = capture_record(messages=[{"role": "user", "content": [{"type": "image_url"}]}])
        self.assertEqual(self._reason(rec), [ct.REASON_MULTIMODAL])

    def test_unknown_role_rejected(self):
        rec = capture_record(messages=[{"role": "wizard", "content": "hi"}])
        self.assertEqual(self._reason(rec), [ct.REASON_UNKNOWN_ROLE])

    def test_missing_provenance_rejected(self):
        rec = capture_record(owner={"capture_namespace": "p1"})  # no root_session_id
        self.assertEqual(self._reason(rec), [ct.REASON_MISSING_PROVENANCE])

    def test_missing_call_id_rejected(self):
        rec = capture_record()
        rec["call_id"] = ""
        self.assertEqual(self._reason(rec), [ct.REASON_MISSING_CALL_ID])

    def test_ambiguous_attribution_rejected(self):
        rec = capture_record(binding="ambiguous")
        self.assertEqual(self._reason(rec), [ct.REASON_AMBIGUOUS_ATTRIBUTION])

    def test_missing_choice_rejected(self):
        rec = capture_record()
        rec["llm_call"]["response"]["choices"] = []
        self.assertEqual(self._reason(rec), [ct.REASON_NO_CHOICE])

    def test_unbound_attribution_still_produces_sample(self):
        # unbound (no memory fact linked) does NOT invalidate the SFT target itself
        rec = capture_record(binding="unbound")
        self.assertEqual(self._reason(rec), [])

    def test_choice_role_not_assistant_rejected(self):
        rec = capture_record(target={"role": "tool", "content": "oops"})
        self.assertEqual(self._reason(rec), [ct.REASON_BAD_CHOICE_ROLE])


class TestDuplicateCallId(unittest.TestCase):
    def test_conflicting_duplicate_refuses_both(self):
        a = capture_record(call_id="dup", response_id="resp-1", line=1)
        b = capture_record(call_id="dup", response_id="resp-2", line=2)
        res = convert([a, b])
        self.assertEqual(len(res["samples"]), 0)
        self.assertEqual(reasons(res), [ct.REASON_DUPLICATE_CALL_ID,
                                       ct.REASON_DUPLICATE_CALL_ID])

    def test_identical_reexport_deduped(self):
        a = capture_record(call_id="dup", response_id="same", line=1)
        b = capture_record(call_id="dup", response_id="same", line=2)
        res = convert([a, b])
        self.assertEqual(len(res["samples"]), 1)
        self.assertEqual(res["rejections"], [])


class TestSessionSplit(unittest.TestCase):
    def _samples(self, ratio=0.5, seed=0):
        recs = []
        for idx, root in enumerate(["root-A", "root-B", "root-C", "root-D"]):
            # parent + child share the same root_session_id (child follows the root)
            recs.append(capture_record(call_id="r%d-p" % idx, root=root, ns="ns-%d" % idx,
                                       line=idx * 2 + 1))
            recs.append(capture_record(call_id="r%d-c" % idx, root=root, ns="ns-%d" % idx,
                                        messages=[{"role": "user", "content": "child turn"}],
                                        line=idx * 2 + 2))
        return ct.convert_strict(recs, index_of(*(fact_for(r) for r in recs)),
                                 sealed_manifest("runA"), FakeTokenizer(),
                                 opts(train_ratio=ratio, split_seed=seed))["samples"]

    def test_no_group_leaks_across_splits(self):
        samples = self._samples()
        self.assertEqual(len(samples), 8)
        by_root = {}
        for s in samples:
            by_root.setdefault(s["root_session_id"], set()).add(s["split"])
        for root, splits in by_root.items():
            self.assertEqual(len(splits), 1, "root %s leaked across splits" % root)

    def test_child_follows_root_group(self):
        samples = self._samples()
        parent = [s for s in samples if s["source_call_ids"][0].endswith("-p")]
        child = [s for s in samples if s["source_call_ids"][0].endswith("-c")]
        parent_map = {s["root_session_id"]: s["split"] for s in parent}
        for s in child:
            self.assertEqual(s["split"], parent_map[s["root_session_id"]])

    def test_split_is_deterministic(self):
        self.assertEqual([s["split"] for s in self._samples()],
                         [s["split"] for s in self._samples()])

    def test_ratio_boundaries(self):
        self.assertTrue(all(s["split"] == "train" for s in self._samples(ratio=1.0)))
        self.assertTrue(all(s["split"] == "test" for s in self._samples(ratio=0.0)))


class TestContentPolicy(unittest.TestCase):
    def _write_rules(self, rules):
        fd, path = tempfile.mkstemp(suffix=".json")
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump({"rules": rules}, f)
        self.addCleanup(os.unlink, path)
        return path

    def test_redact_without_rules_is_refused(self):
        rec = capture_record(messages=[{"role": "user", "content": "sekret42"}])
        with self.assertRaises(ct.StrictError) as ctx:
            convert([rec], options=opts(content_policy="redact", redact_rules_path=None))
        self.assertIn(ct.REASON_REDACT_NO_RULES, str(ctx.exception))

    def test_redact_rewrites_content_but_keeps_association(self):
        path = self._write_rules([{"name": "secret",
                                   "pattern": "sekret[0-9]+",
                                   "replacement": "[REDACTED]"}])
        history = [
            {"role": "user", "content": "my sekret42 is secret"},
            {"role": "assistant", "content": "", "tool_calls": [
                {"id": "call-0", "type": "function",
                 "function": {"name": "nonce", "arguments": '{"token":"sekret99"}'}}]},
            {"role": "tool", "tool_id": "call-0", "content": "value sekret77"},
        ]
        rec = capture_record(messages=history, target={"role": "assistant", "content": "done"})
        # feedback arrives through the FACT stream, joined via its parent event
        facts = [fact_for(rec), feedback_for(rec, note="liked sekret10")]
        res = convert([rec], facts=facts,
                      options=opts(content_policy="redact", redact_rules_path=path))
        self.assertEqual(len(res["samples"]), 1, res["rejections"])
        blob = json.dumps(res["samples"][0]["messages"], ensure_ascii=False)
        self.assertNotIn("sekret", blob)
        self.assertIn("[REDACTED]", blob)
        self.assertNotIn("sekret", json.dumps(res["samples"][0]["feedback"]))
        # association fields (tool_call_id, event keys) are never redacted
        tool_row = [m for m in res["samples"][0]["messages"] if m.get("role") == "tool"][0]
        self.assertEqual(tool_row["tool_call_id"], "call-0")
        self.assertEqual(res["samples"][0]["source_event_keys"],
                         [ct.format_event_key(facts[0]["event_key"])])
        # before/after digests differ, proving a recorded transformation
        s = res["samples"][0]
        self.assertNotEqual(s["content_digest_before"], s["content_digest_after"])
        self.assertEqual(s["content_policy"], "redact")

    def test_redact_that_breaks_tool_json_is_rejected(self):
        path = self._write_rules([{"name": "strip-quotes", "pattern": '"', "replacement": ""}])
        rec = capture_record(messages=[{"role": "user", "content": "go"}],
                             target={"role": "assistant", "content": "", "tool_calls": [
                                 {"id": "call-t", "type": "function",
                                  "function": {"name": "nonce", "arguments": '{"token":"v"}'}}]},
                             finish="tool_calls")
        res = convert([rec], options=opts(content_policy="redact", redact_rules_path=path))
        self.assertEqual(reasons(res), [ct.REASON_REDACT_BREAKS_JSON])

    def test_retain_and_synthetic_are_passthrough(self):
        rec = capture_record(messages=[{"role": "user", "content": "keep sekret42"}])
        for policy in ("retain", "synthetic"):
            res = convert([rec], options=opts(content_policy=policy))
            self.assertEqual(len(res["samples"]), 1)
            self.assertEqual(res["samples"][0]["content_policy"], policy)
            self.assertIn("sekret42", json.dumps(res["samples"][0]["messages"]))


class TestRejectionLedger(unittest.TestCase):
    def test_every_record_is_accounted_no_silent_skip(self):
        recs = [
            capture_record(call_id="good"),
            {"schema_version": 1, "call_id": "legacy", "_source": "capture.jsonl:2"},
            {"schema_version": 2, "_source": "capture.jsonl:3"},  # missing call_id
            capture_record(call_id="noseal", run_id="unlisted", line=4),
            capture_record(call_id="noterm", finish="", line=5),
        ]
        for r in recs:
            r.setdefault("_source", "capture.jsonl:?")
        res = convert(recs)
        accepted_sources = [s["sample_id"] for s in res["samples"]]
        self.assertEqual(len(res["samples"]), 1)
        self.assertIn("runA:good", accepted_sources)
        self.assertEqual(len(res["rejections"]), 4)
        for entry in res["rejections"]:
            self.assertIn("source", entry)
            self.assertIn("reason", entry)
            self.assertIn("call_id", entry)
        self.assertEqual(res["stats"]["seen"], 5)
        self.assertEqual(res["stats"]["accepted"], 1)
        self.assertEqual(res["stats"]["rejected"], 4)

    def test_parse_error_line_is_not_dropped_silently(self):
        recs = [{"_source": "capture.jsonl:7", "_parse_error": "invalid_json: boom"}]
        res = convert(recs)
        self.assertEqual(reasons(res), ["invalid_json: boom"])
        self.assertEqual(res["rejections"][0]["source"], "capture.jsonl:7")

    def test_loader_rejections_are_seeded_into_the_ledger(self):
        # lines the loader ruled out of the primary stream still land in rejected.jsonl
        res = convert([capture_record()], load_rejections=[
            {"source": "capture.jsonl:9", "call_id": "old", "reason": ct.REASON_LEGACY_RECORD}])
        self.assertEqual(reasons(res), [ct.REASON_LEGACY_RECORD])
        self.assertEqual(res["stats"]["seen"], 2)
        self.assertEqual(res["stats"]["rejected"], 1)
        self.assertEqual(res["stats"]["accepted"], 1)


class TestSampleMetadata(unittest.TestCase):
    def test_sample_carries_traceability_fields(self):
        rec = capture_record(call_id="meta-1", digest="REQ-DIGEST")
        res = convert([rec])
        s = res["samples"][0]
        self.assertEqual(s["manifest_sha256"], "MANIFEST-HASH")
        self.assertEqual(s["request_digest"], "REQ-DIGEST")
        self.assertEqual(s["tokenizer_version"], "fake-1")
        self.assertEqual(s["template_hash"],
                         ct._sha256_text("fake tool-aware chatml template"))
        self.assertEqual(s["content_policy_version"], "v1")
        self.assertEqual(s["capture_namespace"], "7")
        self.assertEqual(s["root_session_id"], "sess-root")
        self.assertIn("split", s)
        # unrated feedback is explicit, not a default 0 reward
        self.assertEqual(s["feedback"], [{"verdict": "unrated", "call_id": "meta-1"}])
        self.assertEqual(s["feedback_source"], "unrated")
        self.assertTrue(s["quality"]["event_key_missing"])
        self.assertTrue(s["quality"]["fact_unmatched"])


# ---------------------------------------------------------------------------
# output safety (the strict writer must never overwrite or silently vanish work)
# ---------------------------------------------------------------------------

def _dump(row: dict) -> dict:
    """Strip fixture-only underscore keys so the written line is the real shape."""
    return {k: v for k, v in row.items() if not k.startswith("_")}


def _write_jsonl(path: str, rows: list) -> str:
    with open(path, "w", encoding="utf-8") as f:
        for row in rows:
            f.write(json.dumps(_dump(row), ensure_ascii=False) + "\n")
    return path


def _read_jsonl(path: str) -> list:
    out = []
    with open(path, "r", encoding="utf-8") as f:
        for line in f:
            if line.strip():
                out.append(json.loads(line))
    return out


def _rmtree(path: str) -> None:
    if not os.path.isdir(path):
        return
    for root, dirs, files in os.walk(path, topdown=False):
        for name in files:
            os.unlink(os.path.join(root, name))
        for name in dirs:
            os.rmdir(os.path.join(root, name))
    os.rmdir(path)


class _StrictArgs:
    """Stand-in for argparse.Namespace: the strict CLI contract, attribute by attribute."""

    def __init__(self, **kw):
        self.input = None
        self.events = None
        self.manifest = None
        self.output = None
        self.tokenizer = "fake/tokenizer"
        self.format = "jsonl"
        self.content_policy = "synthetic"
        self.train_ratio = 1.0
        self.split_seed = 0
        self.redact_rules = None
        self.allow_tools_free = False
        for key, value in kw.items():
            setattr(self, key, value)


class TestOutputSafety(unittest.TestCase):
    def test_nonempty_output_dir_is_refused(self):
        base = tempfile.mkdtemp(prefix="c5-out-")
        self.addCleanup(_rmtree, base)
        with open(os.path.join(base, "already-here.jsonl"), "w", encoding="utf-8") as f:
            f.write("{}\n")
        with self.assertRaises(ct.StrictError) as ctx:
            ct.check_output_dir(base)
        self.assertIn("not empty", str(ctx.exception))

    def test_output_path_that_is_a_file_is_refused(self):
        fd, path = tempfile.mkstemp(suffix=".jsonl")
        os.close(fd)
        self.addCleanup(os.unlink, path)
        with self.assertRaises(ct.StrictError) as ctx:
            ct.check_output_dir(path)
        self.assertIn("refusing to overwrite", str(ctx.exception))

    def test_missing_stream_path_is_refused_not_treated_as_empty(self):
        base = tempfile.mkdtemp(prefix="c5-missing-")
        self.addCleanup(_rmtree, base)
        with self.assertRaises(ct.StrictError) as ctx:
            ct.load_capture_records(os.path.join(base, "nope"))
        self.assertIn("--input path not found", str(ctx.exception))
        with self.assertRaises(ct.StrictError) as ctx:
            ct.load_facts(os.path.join(base, "nope.jsonl"))
        self.assertIn("--events path not found", str(ctx.exception))

    def test_dir_of_capture_files_without_jsonl_is_refused(self):
        base = tempfile.mkdtemp(prefix="c5-emptydir-")
        self.addCleanup(_rmtree, base)
        with self.assertRaises(ct.StrictError) as ctx:
            ct.load_capture_records(base)
        self.assertIn("no .jsonl files", str(ctx.exception))

    def test_manifest_file_is_required_by_strict(self):
        with self.assertRaises(ct.StrictError) as ctx:
            ct.load_capture_manifest(None)
        self.assertIn("--manifest", str(ctx.exception))
        with self.assertRaises(ct.StrictError) as ctx:
            ct.load_capture_manifest("/nonexistent/capture-manifest.json")
        self.assertIn("not found", str(ctx.exception))

    def test_run_strict_writes_the_ledger_even_when_nothing_is_accepted(self):
        base = tempfile.mkdtemp(prefix="c5-noaccept-")
        self.addCleanup(_rmtree, base)
        capture = _write_jsonl(os.path.join(base, "capture.jsonl"),
                               [{"schema_version": 1, "llm_call": {"request": {}, "response": {}}}])
        facts = _write_jsonl(os.path.join(base, "facts.jsonl"), [])
        manifest = os.path.join(base, "manifest.json")
        with open(manifest, "w", encoding="utf-8") as f:
            json.dump(_dump(sealed_manifest("runA")), f)
        out = os.path.join(base, "out")
        args = _StrictArgs(input=capture, events=facts, manifest=manifest, output=out)
        code = ct.run_strict(args, tokenizer=FakeTokenizer())
        self.assertEqual(code, 1)
        self.assertFalse(os.path.exists(os.path.join(out, "sft_train.jsonl")))
        rejected = _read_jsonl(os.path.join(out, "rejected.jsonl"))
        self.assertEqual([r["reason"] for r in rejected], [ct.REASON_LEGACY_RECORD])


# ---------------------------------------------------------------------------
# legacy mode stays functional but is explicitly labelled non-equivalent
# ---------------------------------------------------------------------------

class TestLegacyMode(unittest.TestCase):
    def test_legacy_run_warns_it_is_not_equivalent_to_strict(self):
        base = tempfile.mkdtemp(prefix="c5-legacy-")
        self.addCleanup(_rmtree, base)
        legacy = {"timestamp": "2026-10-08T00:00:00Z", "session_id": "s", "user_id": "u",
                  "batch_index": 0,
                  "llm_call": {"request": {"messages": [{"role": "user", "content": "hi"}],
                                           "model": "glm-5"},
                               "response": {"choices": [{"message": {"role": "assistant",
                                                                     "content": "ho"}}]}},
                  "metadata": {}}
        capture = _write_jsonl(os.path.join(base, "traj.jsonl"), [legacy])
        err = io.StringIO()
        with contextlib.redirect_stderr(err):
            code = ct.main(["--input", capture, "--output", os.path.join(base, "out"),
                            "--mode", "rl"])
        self.assertEqual(code, 0)
        text = err.getvalue()
        self.assertIn("legacy mode is NOT equivalent to --strict", text)
        self.assertIn("no capture seal is consulted", text)
        # legacy never consults the seal streams
        self.assertNotIn("--events", text)

    def test_strict_without_the_primary_source_is_a_cli_error(self):
        err = io.StringIO()
        with contextlib.redirect_stderr(err):
            with self.assertRaises(SystemExit) as ctx:
                ct.main(["--strict", "--output", "/tmp/whatever", "--events", "/tmp/facts.jsonl"])
        self.assertEqual(ctx.exception.code, 2)
        self.assertIn("--input", err.getvalue())

    def test_strict_without_the_association_stream_is_a_cli_error(self):
        err = io.StringIO()
        with contextlib.redirect_stderr(err):
            with self.assertRaises(SystemExit) as ctx:
                ct.main(["--strict", "--input", "/tmp/capture.jsonl",
                         "--output", "/tmp/whatever"])
        self.assertEqual(ctx.exception.code, 2)
        self.assertIn("--events", err.getvalue())

    def test_strict_without_the_seal_manifest_is_a_cli_error(self):
        """The seal is a CLI-level requirement, not a deep runtime surprise.

        --input and --events are what make a row a sample; --manifest is what makes
        the whole set claimable as complete. Missing any of the three must fail at
        argument parsing (exit 2) before a single stream is read, otherwise a caller
        can half-run a conversion and get a StrictError only after file IO.
        """
        err = io.StringIO()
        with contextlib.redirect_stderr(err):
            with self.assertRaises(SystemExit) as ctx:
                ct.main(["--strict", "--input", "/tmp/capture.jsonl",
                         "--events", "/tmp/facts.jsonl", "--output", "/tmp/whatever"])
        self.assertEqual(ctx.exception.code, 2)
        self.assertIn("--manifest", err.getvalue())

    def test_legacy_mode_still_requires_its_own_arguments(self):
        err = io.StringIO()
        with contextlib.redirect_stderr(err):
            with self.assertRaises(SystemExit) as ctx:
                ct.main(["--output", "/tmp/whatever"])
        self.assertEqual(ctx.exception.code, 2)
        self.assertIn("--mode is required unless --strict is set", err.getvalue())


# ---------------------------------------------------------------------------
# tokenizer capability probe (recorded by the acceptance path, never assumed)
# ---------------------------------------------------------------------------

class TestHfSingleSidedAndLedger(unittest.TestCase):

    def _out(self, name):
        base = tempfile.mkdtemp(prefix="c5-rev-")
        self.addCleanup(_rmtree, base)
        return os.path.join(base, name)

    def test_write_hf_one_sided_split_writes_flat_dataset(self):
        saved = []

        class _Flat:
            def __init__(self, rows):
                self.rows = rows

            def save_to_disk(self, path):
                saved.append((path, list(self.rows)))

        calls = {"n": 0}

        def _build(rows):
            calls["n"] += 1
            return _Flat(rows)

        orig = ct.build_dataset
        ct.build_dataset = _build
        try:
            ct.write_hf([{"split": "train", "x": 1}, {"split": "train", "x": 2}],
                        self._out("hf-flat"))
        finally:
            ct.build_dataset = orig
        self.assertEqual(calls["n"], 1, "one-sided output builds exactly one Dataset")
        self.assertEqual(len(saved), 1, "flat layout: a single save_to_disk at the root")

    def test_poisoned_call_id_slot_rejects_every_later_claim(self):
        first = capture_record(call_id="dup-1", response_id="resp-a")
        second = capture_record(call_id="dup-1", response_id="resp-b")
        third = capture_record(call_id="dup-1", response_id="resp-c")
        fact_index, _stats = ct.build_fact_index([fact_for(first)])
        res = ct.convert_strict([first, second, third], fact_index,
                                sealed_manifest("runA"), FakeTokenizer(), opts())
        dup = [r for r in res["rejections"] if r["reason"] == ct.REASON_DUPLICATE_CALL_ID]
        self.assertEqual(len(dup), 3,
                         "both contradictory records plus every later claim join the ledger")

    def test_new_capture_ledger_keys_break_the_seal(self):
        man = sealed_manifest("runA")
        man["runs"]["runA"]["pending_exhausted"] = 1
        status, detail = ct._manifest_run_status(man, "runA")
        self.assertEqual(status, "incomplete")
        self.assertIn("pending_exhausted=1", detail)
        man["runs"]["runA"]["pending_exhausted"] = 0
        man["runs"]["runA"]["oversized_dropped"] = 2
        status, detail = ct._manifest_run_status(man, "runA")
        self.assertEqual(status, "incomplete")
        self.assertIn("oversized_dropped=2", detail)


class TestTokenizerCapabilityProbe(unittest.TestCase):
    def test_tool_aware_mask_capable_template_reports_both(self):
        cap = ct.detect_local_tokenizer_capability(FakeTokenizer("ok"))
        self.assertTrue(cap["available"])
        self.assertTrue(cap["apply_chat_template"])
        self.assertTrue(cap["tools"])
        self.assertTrue(cap["assistant_tokens_mask"])

    def test_template_rejecting_the_mask_kwarg_is_reported_unsupported(self):
        cap = ct.detect_local_tokenizer_capability(FakeTokenizer("no_kwarg"))
        self.assertTrue(cap["tools"])
        self.assertFalse(cap["assistant_tokens_mask"])

    def test_template_reporting_an_all_zero_mask_is_reported_unsupported(self):
        cap = ct.detect_local_tokenizer_capability(FakeTokenizer("zero_mask"))
        self.assertFalse(cap["assistant_tokens_mask"])

    def test_absent_tokenizer_is_reported_absent(self):
        cap = ct.detect_local_tokenizer_capability(None)
        self.assertFalse(cap["available"])
        self.assertFalse(cap["apply_chat_template"])
        self.assertIsNone(cap["eos_token_id"])


# ---------------------------------------------------------------------------
# the two streams, kept apart (task 6.6: capture is the record source, the fact
# export is only the association index)
# ---------------------------------------------------------------------------

class TestDualStream(unittest.TestCase):
    def _write_streams(self, records, facts, manifest=None, extra=None, out_name="out"):
        base = tempfile.mkdtemp(prefix="c5-stream-")
        self.addCleanup(_rmtree, base)
        capture_dir = os.path.join(base, "capture")
        os.mkdir(capture_dir)
        capture = _write_jsonl(os.path.join(capture_dir, "sess-root.jsonl"), records)
        facts_path = _write_jsonl(os.path.join(base, "facts.jsonl"), facts)
        manifest_path = os.path.join(base, "manifest.json")
        with open(manifest_path, "w", encoding="utf-8") as f:
            json.dump(_dump(manifest if manifest is not None else sealed_manifest("runA")), f)
        out = os.path.join(base, out_name)
        kw = {"input": capture_dir, "events": facts_path, "manifest": manifest_path, "output": out}
        kw.update(extra or {})
        args = _StrictArgs(**kw)
        code = ct.run_strict(args, tokenizer=FakeTokenizer())
        return code, out, manifest_path

    # ---- stream 1: what may be a record ------------------------------------

    def test_fact_lines_pointed_at_the_primary_stream_are_rejected_not_consumed(self):
        """The integrated bug: rl.ExportedFact has no schema_version, so it is not a record."""
        rec = capture_record(call_id="mix-1")
        rows = [fact_for(rec), feedback_for(rec), rec]
        base = tempfile.mkdtemp(prefix="c5-mixed-")
        self.addCleanup(_rmtree, base)
        path = _write_jsonl(os.path.join(base, "capture.jsonl"), rows)
        records, rejections = ct.load_capture_records(path)
        self.assertEqual(len(records), 1)
        self.assertEqual(records[0]["call_id"], "mix-1")
        self.assertEqual([r["reason"] for r in rejections],
                         [ct.REASON_NOT_V2, ct.REASON_NOT_V2])
        for entry in rejections:
            self.assertTrue(entry["source"].startswith("capture.jsonl:"))

    def test_v1_recorder_dump_is_classified_legacy_record(self):
        v1 = {"timestamp": "2026-10-08T00:00:00Z", "session_id": "s", "user_id": "u",
              "batch_index": 0,
              "llm_call": {"request": {"messages": [{"role": "user", "content": "hi"}]},
                           "response": {"choices": [{"message": {"role": "assistant",
                                                                 "content": "ho"}}]}},
              "metadata": {}}
        self.assertEqual(ct.classify_capture_row(v1), (False, ct.REASON_LEGACY_RECORD))
        # a half-migrated line that does carry v2 identity is a schema problem, not
        # the legacy recorder: the two have different fixes, so the codes differ.
        v1["call_id"] = "half-migrated"
        self.assertEqual(ct.classify_capture_row(v1), (False, ct.REASON_NOT_V2))

    def test_fact_row_as_a_record_yields_no_sample(self):
        res = convert([fact_row(call_id="runA-1")])
        self.assertEqual(res["samples"], [])
        self.assertEqual(reasons(res), [ct.REASON_NOT_V2])

    # ---- stream 2: additive association only ------------------------------

    def test_record_without_any_fact_is_accepted_and_marked_missing(self):
        rec = capture_record(call_id="nofact-1")
        res = convert([rec], facts=[])
        self.assertEqual(len(res["samples"]), 1, res["rejections"])
        s = res["samples"][0]
        self.assertEqual(s["source_event_keys"], [])
        self.assertTrue(s["quality"]["event_key_missing"])
        self.assertTrue(s["quality"]["fact_unmatched"])
        self.assertFalse(s["quality"]["feedback_ambiguous"])
        self.assertEqual(res["stats"]["quality"]["event_key_missing"], 1)
        self.assertEqual(res["stats"]["quality"]["fact_unmatched"], 1)

    def test_fact_stream_supplies_the_event_key(self):
        rec = capture_record(call_id="evt-1")
        fact = fact_for(rec)
        res = convert([rec], facts=[fact])
        s = res["samples"][0]
        self.assertEqual(s["source_event_keys"], [ct.format_event_key(fact["event_key"])])
        self.assertFalse(s["quality"]["event_key_missing"])
        self.assertFalse(s["quality"]["fact_unmatched"])
        self.assertEqual(s["feedback_source"], "fact_index")
        self.assertEqual(res["stats"]["fact_index_unmatched"], 0)

    def test_facts_for_calls_with_no_record_never_become_samples(self):
        res = convert([capture_record(call_id="real-1")],
                      facts=[fact_row(call_id="ghost-1"), fact_row(call_id="ghost-2")])
        self.assertEqual([s["source_call_ids"][0] for s in res["samples"]], ["real-1"])
        self.assertEqual(res["stats"]["fact_index_unmatched"], 2)

    def test_several_fact_rows_on_one_call_aggregate_event_keys(self):
        rec = capture_record(call_id="multi-1")
        rows = [fact_row(call_id="multi-1", event_key=0x1000, partition_id=7),
                fact_row(call_id="multi-1", event_key=0x2000, partition_id=7)]
        res = convert([rec], facts=rows)
        index = index_of(*rows)
        self.assertEqual(index["multi-1"]["rows"], 2)
        self.assertEqual(index["multi-1"]["event_keys"],
                         [ct.format_event_key(0x1000), ct.format_event_key(0x2000)])
        self.assertEqual(res["samples"][0]["source_event_keys"],
                         index["multi-1"]["event_keys"])

    def test_feedback_arrives_through_the_parent_and_all_judgements_are_kept(self):
        rec = capture_record(call_id="fb-1")
        rows = [fact_for(rec),
                feedback_for(rec, event_key=0x9001, verdict="good", note="clear"),
                feedback_for(rec, event_key=0x9002, verdict="bad", note="too long")]
        index, stats = ct.build_fact_index(rows)
        self.assertEqual(stats["feedback_total"], 2)
        self.assertEqual(stats["feedback_bound"], 2)
        self.assertEqual(len(index["fb-1"]["feedback"]), 2)
        res = convert([rec], facts=rows)
        s = res["samples"][0]
        self.assertEqual(s["feedback_source"], "fact_index")
        self.assertEqual([f["verdict"] for f in s["feedback"]], ["good", "bad"])
        self.assertEqual([f["note"] for f in s["feedback"]], ["clear", "too long"])
        # no numeric reward is invented anywhere on the sample
        self.assertNotIn("reward", json.dumps(s))
        self.assertFalse(s["quality"]["feedback_ambiguous"])

    def test_contradictory_parents_are_ambiguous_and_spread_nothing(self):
        rec = capture_record(call_id="amb-1")
        parents = [fact_row(call_id="amb-1", event_key=0x1111, partition_id=7),
                   fact_row(call_id="amb-1", event_key=0x2222, partition_id=7)]
        rows = parents + [feedback_row(parent_key=0x1111, event_key=0x9001, verdict="good"),
                          feedback_row(parent_key=0x2222, event_key=0x9002, verdict="bad")]
        index, stats = ct.build_fact_index(rows)
        self.assertTrue(index["amb-1"]["ambiguous"])
        self.assertEqual(index["amb-1"]["feedback"], [])
        self.assertEqual(stats["feedback_ambiguous"], 2)
        self.assertEqual(stats["feedback_bound"], 0)
        res = convert([rec], facts=rows)
        s = res["samples"][0]
        self.assertTrue(s["quality"]["feedback_ambiguous"])
        self.assertEqual(s["feedback_source"], "fact_index_ambiguous")
        self.assertEqual(s["feedback"], [{"verdict": "unrated", "call_id": "amb-1"}])
        self.assertEqual(res["stats"]["quality"]["feedback_ambiguous"], 1)

    def test_task_level_feedback_without_a_parent_stays_unbound(self):
        rec = capture_record(call_id="task-1")
        rows = [fact_for(rec), feedback_row(parent_key=None, event_key=0x9003, verdict="meh")]
        index, stats = ct.build_fact_index(rows)
        self.assertEqual(stats["feedback_unbound"], 1)
        self.assertEqual(stats["join_missing"], 1)
        self.assertEqual(index["task-1"]["feedback"], [])
        res = convert([rec], facts=rows)
        self.assertEqual(res["samples"][0]["feedback"],
                         [{"verdict": "unrated", "call_id": "task-1"}])

    def test_feedback_whose_parent_is_absent_is_expired_or_missing(self):
        rec = capture_record(call_id="gone-1")
        rows = [fact_for(rec), feedback_row(parent_key=0x7777, event_key=0x9004)]
        _index, stats = ct.build_fact_index(rows)
        self.assertEqual(stats["join_expired_or_missing"], 1)
        self.assertEqual(stats["feedback_bound"], 0)
        res = convert([rec], facts=rows)
        self.assertEqual(len(res["samples"]), 1)
        self.assertEqual(res["samples"][0]["feedback_source"], "fact_index")

    def test_the_record_carried_feedback_is_only_a_fallback_without_a_fact(self):
        rec = capture_record(call_id="own-1", feedback=[{"verdict": "self_reported"}])
        res = convert([rec])
        self.assertEqual(res["samples"][0]["feedback_source"], "record")
        self.assertEqual(res["samples"][0]["feedback"], [{"verdict": "self_reported"}])
        # once the fact stream has this call, the association layer is authoritative
        res = convert([rec], facts=[fact_for(rec)])
        self.assertEqual(res["samples"][0]["feedback_source"], "fact_index")
        self.assertEqual(res["samples"][0]["feedback"],
                         [{"verdict": "unrated", "call_id": "own-1"}])

    # ---- cross-stream partition consistency -------------------------------

    def test_partition_conflict_between_the_streams_is_rejected(self):
        rec = capture_record(call_id="part-1", ns="7")
        fact = fact_row(call_id="part-1", partition_id=9)
        res = convert([rec], facts=[fact])
        self.assertEqual(res["samples"], [])
        self.assertEqual(reasons(res), [ct.REASON_OWNER_PARTITION_CONFLICT])
        self.assertEqual(res["rejections"][0]["call_id"], "part-1")

    def test_a_non_numeric_namespace_never_invents_a_conflict(self):
        rec = capture_record(call_id="part-2", ns="part-seven", partition_id="")
        res = convert([rec], facts=[fact_row(call_id="part-2", partition_id=7)])
        self.assertEqual(len(res["samples"]), 1, res["rejections"])
        self.assertEqual(res["samples"][0]["capture_namespace"], "part-seven")

    def test_owner_partition_and_fact_partition_agree_across_spellings(self):
        # owner spells the partition as a STRING, the export as a JSON NUMBER
        rec = capture_record(call_id="part-3", ns="42")
        res = convert([rec], facts=[fact_row(call_id="part-3", partition_id=42)])
        self.assertEqual(len(res["samples"]), 1, res["rejections"])

    # ---- event key spelling normalisation ---------------------------------

    def test_event_key_spellings_normalise_to_one_canonical_key(self):
        for spelling in (500, "1f4", "0x1f4", "0X1F4", "evt_1f4",
                         "[evt_1f4|agent_output]", "1f4|agent_output", " 1f4 "):
            self.assertEqual(ct.canonical_event_key(spelling), "1f4", spelling)
        self.assertEqual(ct.canonical_event_key(""), "")
        self.assertEqual(ct.canonical_event_key("not-a-key"), "")
        self.assertEqual(ct.canonical_event_key(None), "")

    def test_strings_are_hex_not_decimal_because_that_is_what_the_writer_emits(self):
        # event.ParseEventKey is strconv.ParseInt(s, 16, 64). A digits-only canonical
        # key is hex: reading "1000" as decimal would silently bind a feedback join to
        # a different event, which is worse than failing to bind it at all.
        self.assertEqual(ct.parse_event_key("1000"), 0x1000)
        self.assertEqual(ct.canonical_event_key("1000"), "1000")
        self.assertEqual(ct.canonical_event_key(ct.format_event_key(0x1000)), "1000")

    def test_large_snowflake_keys_survive_without_float_rounding(self):
        big = 7234567890123456789
        text = ct.format_event_key(big)
        self.assertEqual(ct.parse_event_key(text), big)
        self.assertEqual(ct.parse_event_key(big), big)

    # ---- end to end over both streams -------------------------------------

    def test_run_strict_wires_both_streams_and_seals_the_provenance(self):
        rec = capture_record(call_id="e2e-1", ns="7")
        code, out, manifest_path = self._write_streams([_dump(rec)], [_dump(fact_for(rec))])
        self.assertEqual(code, 0, os.listdir(out))
        samples = _read_jsonl(os.path.join(out, "sft_train.jsonl"))
        self.assertEqual(len(samples), 1)
        s = samples[0]
        self.assertEqual(s["source_call_ids"], ["e2e-1"])
        self.assertEqual(s["source_event_keys"], [ct.format_event_key(_event_key_for("e2e-1"))])
        self.assertEqual(len(s["labels"]), len(s["input_ids"]))
        self.assertEqual(sum(s["loss_mask"]), sum(1 for l in s["labels"] if l != ct.LOSS_IGNORE_INDEX))
        self.assertGreater(sum(s["loss_mask"]), 0)
        with open(os.path.join(out, "conversion_manifest.json"), encoding="utf-8") as f:
            doc = json.load(f)
        self.assertEqual(doc["streams"]["record_source"], "capture (--input)")
        self.assertEqual(doc["streams"]["association_index"], "facts (--events)")
        self.assertEqual(doc["fact_index"]["indexed_calls"], 1)
        with open(manifest_path, encoding="utf-8") as f:
            manifest_text = f.read()
        self.assertEqual(doc["capture_manifest_sha256"], ct._sha256_text(manifest_text))
        self.assertEqual(doc["stats"]["accepted"], 1)
        self.assertTrue(os.path.isfile(os.path.join(out, "rejected.jsonl")))

    def test_run_strict_rejects_the_primary_source_it_cannot_seal(self):
        rec = capture_record(call_id="e2e-2", ns="7", run_id="runZ")
        code, out, _ = self._write_streams([_dump(rec)], [_dump(fact_row(call_id="e2e-2"))])
        self.assertEqual(code, 1)
        rejected = _read_jsonl(os.path.join(out, "rejected.jsonl"))
        self.assertTrue(rejected[0]["reason"].startswith(ct.REASON_UNSEALED))

    def test_run_strict_keeps_tools_required_unless_turned_off(self):
        rec = capture_record(call_id="tf-1", tools=[])
        code, out, _ = self._write_streams([_dump(rec)], [_dump(fact_for(rec))])
        self.assertEqual(code, 1)
        self.assertEqual([r["reason"] for r in _read_jsonl(os.path.join(out, "rejected.jsonl"))],
                         [ct.REASON_MISSING_TOOLS])
        code, out, _ = self._write_streams([_dump(rec)], [_dump(fact_for(rec))],
                                           extra={"allow_tools_free": True},
                                           out_name="out-loose")
        self.assertEqual(code, 0)
        self.assertEqual(len(_read_jsonl(os.path.join(out, "sft_train.jsonl"))), 1)


if __name__ == "__main__":
    unittest.main(verbosity=2)
