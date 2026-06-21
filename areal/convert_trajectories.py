#!/usr/bin/env python3
from __future__ import annotations
"""
Convert tagent trajectory JSONL files to AReaL-compatible datasets.

Usage:
    # SFT mode: convert to {input_ids, loss_mask} format
    python3 convert_trajectories.py \
        --input data/trajectories/ \
        --output data/sft/ \
        --tokenizer Qwen/Qwen2.5-1.5B-Instruct \
        --mode sft

    # RL mode: convert to {messages} format (prompt-only)
    python3 convert_trajectories.py \
        --input data/trajectories/ \
        --output data/rl/ \
        --mode rl

Input format (tagent JSONL, one per line):
    {
        "timestamp": "2026-06-20T10:30:00Z",
        "session_id": "wechat-session",
        "user_id": "wechat-user",
        "batch_index": 0,
        "llm_call": {
            "request": {
                "messages": [{"role": "user", "content": "..."}],
                "model": "glm-5",
                "generation_config": {...}
            },
            "response": {
                "choices": [{"message": {"role": "assistant", "content": "..."}}],
                "usage": {...},
                "finish_reason": "stop"
            }
        },
        "metadata": {
            "duration_ms": 1234,
            "model_endpoint": "https://open.bigmodel.cn/api/paas/v4"
        }
    }

Output formats:
    SFT: HuggingFace Dataset with columns {input_ids, loss_mask}
         - input_ids: tokenized prompt + completion
         - loss_mask: 0 for prompt tokens, 1 for completion tokens

    RL:  HuggingFace Dataset with columns {messages}
         - messages: [{"role": "user", "content": "..."}] (prompt only)
"""

import argparse
import glob
import json
import os
import sys
from pathlib import Path


def load_trajectories(input_path: str) -> list[dict]:
    """Load all trajectory records from JSONL files.

    Args:
        input_path: Directory containing .jsonl files, or a single .jsonl file.

    Returns:
        List of trajectory record dicts.
    """
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
    """Extract prompt text and completion text from a trajectory record.

    Returns:
        (prompt_text, completion_text) or None if extraction fails.
    """
    llm_call = record.get("llm_call", {})
    request = llm_call.get("request", {})
    response = llm_call.get("response", {})

    messages = request.get("messages", [])
    choices = response.get("choices", [])

    if not messages or not choices:
        return None

    # Build prompt from request messages
    prompt_parts = []
    for msg in messages:
        role = msg.get("role", "user")
        content = msg.get("content", "")
        prompt_parts.append(f"{role}: {content}")
    prompt_text = "\n".join(prompt_parts)

    # Extract completion from first choice
    choice = choices[0]
    message = choice.get("message", {})
    completion_text = message.get("content", "")

    if not completion_text:
        return None

    return prompt_text, completion_text


def extract_user_message(record: dict) -> str | None:
    """Extract the initial user message from a trajectory record.

    Returns:
        The first user message content, or None if not found.
    """
    llm_call = record.get("llm_call", {})
    request = llm_call.get("request", {})
    messages = request.get("messages", [])

    for msg in messages:
        if msg.get("role") == "user":
            return msg.get("content", "")

    return None


def convert_to_sft(records: list[dict], tokenizer) -> list[dict]:
    """Convert trajectory records to AReaL SFT dataset format.

    Each sample contains:
        - input_ids: tokenized prompt + completion
        - loss_mask: 0 for prompt tokens, 1 for completion tokens
    """
    from transformers import AutoTokenizer

    samples = []
    skipped = 0

    for record in records:
        result = extract_prompt_completion(record)
        if result is None:
            skipped += 1
            continue

        prompt_text, completion_text = result

        # Tokenize prompt and completion separately
        prompt_ids = tokenizer.encode(prompt_text, add_special_tokens=False)
        completion_ids = tokenizer.encode(completion_text, add_special_tokens=False)

        # Combine: prompt + completion + EOS
        eos_id = tokenizer.eos_token_id
        if eos_id is not None:
            completion_ids = completion_ids + [eos_id]

        input_ids = prompt_ids + completion_ids
        loss_mask = [0] * len(prompt_ids) + [1] * len(completion_ids)

        samples.append({
            "input_ids": input_ids,
            "loss_mask": loss_mask,
        })

    print(f"SFT conversion: {len(samples)} samples, {skipped} skipped", file=sys.stderr)
    return samples


def convert_to_rl(records: list[dict]) -> list[dict]:
    """Convert trajectory records to AReaL RL prompt dataset format.

    Each sample contains only the initial user message:
        - messages: [{"role": "user", "content": "..."}]
    """
    samples = []
    skipped = 0

    for record in records:
        user_msg = extract_user_message(record)
        if user_msg is None:
            skipped += 1
            continue

        samples.append({
            "messages": [{"role": "user", "content": user_msg}]
        })

    print(f"RL conversion: {len(samples)} samples, {skipped} skipped", file=sys.stderr)
    return samples


def save_dataset(samples: list[dict], output_path: str, mode: str):
    """Save samples as a HuggingFace Dataset.

    Falls back to JSONL if datasets library is not available.
    """
    output_path = Path(output_path)
    output_path.mkdir(parents=True, exist_ok=True)

    try:
        from datasets import Dataset

        ds = Dataset.from_list(samples)
        ds.save_to_disk(str(output_path))
        print(f"Saved HuggingFace Dataset to {output_path}", file=sys.stderr)
    except ImportError:
        # Fallback: save as JSONL
        jsonl_path = output_path / f"{mode}_dataset.jsonl"
        with open(jsonl_path, "w", encoding="utf-8") as f:
            for sample in samples:
                f.write(json.dumps(sample, ensure_ascii=False) + "\n")
        print(f"datasets library not available, saved JSONL to {jsonl_path}", file=sys.stderr)


def main():
    parser = argparse.ArgumentParser(
        description="Convert tagent trajectory JSONL to AReaL SFT/RL dataset format"
    )
    parser.add_argument(
        "--input", required=True,
        help="Input directory containing .jsonl files, or a single .jsonl file"
    )
    parser.add_argument(
        "--output", required=True,
        help="Output directory for the dataset"
    )
    parser.add_argument(
        "--mode", choices=["sft", "rl"], required=True,
        help="Conversion mode: 'sft' for SFT dataset (input_ids + loss_mask), 'rl' for RL prompt dataset (messages)"
    )
    parser.add_argument(
        "--tokenizer", default=None,
        help="HuggingFace tokenizer name/path (required for SFT mode)"
    )

    args = parser.parse_args()

    if args.mode == "sft" and not args.tokenizer:
        parser.error("--tokenizer is required for SFT mode")

    # Load trajectories
    records = load_trajectories(args.input)
    if not records:
        print("No valid trajectory records found", file=sys.stderr)
        sys.exit(1)

    # Convert
    if args.mode == "sft":
        from transformers import AutoTokenizer
        tokenizer = AutoTokenizer.from_pretrained(args.tokenizer)
        samples = convert_to_sft(records, tokenizer)
    else:
        samples = convert_to_rl(records)

    if not samples:
        print("No valid samples after conversion", file=sys.stderr)
        sys.exit(1)

    # Save
    save_dataset(samples, args.output, args.mode)


if __name__ == "__main__":
    main()
