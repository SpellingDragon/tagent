#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Generate tests/offline_bench/testdata/token_fixture.json — the pinned
offline tokenizer reference for the chars/token estimator-error benchmark
(resident-remaining-hardening 2.5 / archived design D6: "中英/代码/JSON 数据集
使用已固定版本的离线 tokenizer 输出作为 fixture；不为本期新增运行时 tokenizer").

Deterministic: corpora are built from fixed templates, no randomness, no
network beyond tiktoken's cached BPE. Regenerate with:
    uv run --no-project --with tiktoken python3 gen_token_fixture.py
"""
import json
import hashlib
from pathlib import Path

import tiktoken


def zh_corpus():
    lines = []
    topics = [
        "常驻会话的耐久收件箱在启动期完成收据对齐",
        "任务结算通知折叠为票据卡片后投影字符量显著下降",
        "资源租约的目录锁必须先于文件打开获取避免竞态",
        "端点 allowlist 对每一跳重定向执行精确 host 校验",
    ]
    for i in range(50):
        t = topics[i % len(topics)]
        lines.append(f"[{i:03d}] {t}，事实链记录保持逐条落库不变。分区 {i % 7}，毫秒 {1700000000000 + i * 13}")
    return lines


def en_corpus():
    lines = []
    frags = [
        "The persistent event loop reclaims external input events into a new turn.",
        "Deduplication keys align one-to-one with messages inside an envelope batch.",
        "Compaction anchors the full-render window at the last fold boundary.",
    ]
    for i in range(50):
        f = frags[i % len(frags)]
        lines.append(f"#{i:03d} {f} Partition {i % 7}, timestamp {1700000000000 + i * 13}.")
    return lines


def code_corpus():
    lines = []
    for i in range(50):
        lines.append(
            "func (cc *ContextCompressor) foldSettleRuns%d(refs []memory.EventReference) []memory.EventReference {\n"
            "\tresult := make([]memory.EventReference, 0, len(refs))\n"
            "\tfor j := 0; j < len(refs); j++ {\n"
            "\t\tif isSettleNoticeRef(refs[j]) && j%%2 == %d {\n"
            "\t\t\tresult = append(result, buildSettleFoldRef(refs[j:j+1]))\n"
            "\t\t}\n"
            "\t}\n"
            "\treturn result\n}" % (i, i % 3)
        )
    return lines


def json_corpus():
    lines = []
    for i in range(50):
        lines.append(json.dumps({
            "event_key": 1700000000000 + i,
            "partition_id": i % 7,
            "event_type": "external_input",
            "content": "任务 %d 完成，结果已落盘 /workspace/out/%d.txt" % (i, i),
            "metadata": {"source": "task", "attempt": i % 5},
        }, ensure_ascii=False, sort_keys=True))
    return lines


def main():
    enc = tiktoken.get_encoding("cl100k_base")
    corpora = {"zh": zh_corpus(), "en": en_corpus(), "code": code_corpus(), "json": json_corpus()}
    out = {
        "tokenizer": "tiktoken cl100k_base",
        "tiktoken_version": getattr(tiktoken, "__version__", "unknown"),
        "generated_by": "tests/offline_bench/gen_token_fixture.py",
        "corpora": {},
    }
    for name, lines in corpora.items():
        samples = [{"text": s, "tokens": len(enc.encode(s))} for s in lines]
        out["corpora"][name] = {
            "count": len(samples),
            "samples": samples,
        }
    path = Path(__file__).parent / "testdata" / "token_fixture.json"
    path.parent.mkdir(exist_ok=True)
    raw = json.dumps(out, ensure_ascii=False, indent=1)
    path.write_text(raw, encoding="utf-8")
    digest = hashlib.sha256(raw.encode("utf-8")).hexdigest()[:12]
    print(f"wrote {path} sha256:{digest}")


if __name__ == "__main__":
    main()
