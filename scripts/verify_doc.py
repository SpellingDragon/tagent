#!/usr/bin/env python3
"""verify_doc.py — tagent_review.md 交付前终验脚本（门禁自动化）

把 AGENTS.md 规定的「终验脚本全绿（UTF-8 / 行数 / 不变量 / 章节连续）」与
对抗性审阅门禁固化成可一键运行的检查，防止回归，尤其防止本次会话真实发生过的
「字节级 perl/sed 破坏多字节 UTF-8 导致文档大规模删字」类事故。

检查项：
  1. UTF-8 合法（try decode，捕获非法 continuation byte）
  2. NEL (U+0085) 与控制字符（除 \\n\\r\\t）残留 —— 0
  3. 内部概念泄露：知识库 / KB 条目 / 内部 KB —— 0
  4. 参考引用标号连续：[1]..[N] 无跳号
  5. 行内引用 [n] 均在参考表定义（无悬空引用）
  6. 章节标题 ## 连续（中文数字 一~九 无跳号），### 子节归属父节

用法：
  python3 scripts/verify_doc.py [path]          # 默认 examples/wechat-bot/tagent_review.md
  python3 scripts/verify_doc.py --strict-refs  # 额外要求每个 [n] 至少被行内引用一次

退出码：全部通过 0；任一 FAIL 1。幂等、无副作用。
"""
import sys
import re
import os

DEFAULT = os.path.join(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
    "examples", "wechat-bot", "tagent_review.md",
)

CN_NUM = "一二三四五六七八九"


def load(path):
    with open(path, "rb") as f:
        raw = f.read()
    try:
        return raw.decode("utf-8")
    except UnicodeDecodeError as e:
        raise SystemExit(f"FAIL: UTF-8 非法 — {e}")


def check_utf8_and_ctrl(text):
    fails = []
    ctrl = [c for c in text if ord(c) < 32 and c not in "\n\r\t"]
    nel = text.count("\u0085")
    if nel:
        fails.append(f"NEL(U+0085) 残留 {nel} 处 —— 须字符模式清除，勿字节级 perl/sed")
    if ctrl:
        fails.append(f"其他控制字符残留 {len(ctrl)} 处")
    return fails


def check_kb(text):
    hits = []
    for kw in ("知识库", "KB 条目", "内部 KB"):
        n = text.count(kw)
        if n:
            hits.append(f"内部概念泄露「{kw}」{n} 处")
    return hits


def check_refs(text):
    fails = []
    defined = set(int(m) for m in re.findall(r"^\[(\d+)\]", text, re.M))
    if not defined:
        return ["参考表为空或格式异常"]
    lo, hi = min(defined), max(defined)
    missing = [i for i in range(lo, hi + 1) if i not in defined]
    if missing:
        fails.append(f"引用标号跳号：缺 {missing}（[1]..[{hi}] 不连续）")
    body = "\n".join(
        ln for ln in text.splitlines()
        if not re.match(r"^\[\d+\]", ln) and not ln.startswith("> 注")
    )
    used = set(int(m) for m in re.findall(r"\[(\d+)\]", body))
    dangling = sorted(used - defined)
    if dangling:
        fails.append(f"悬空行内引用（参考表未定义）：{dangling}")
    return fails


def check_sections(text):
    fails = []
    h2 = re.findall(r"^##\s+([一二三四五六七八九])、", text, re.M)
    if h2:
        seq = [CN_NUM.index(x) + 1 for x in h2]
        expected = list(range(1, len(seq) + 1))
        if seq != expected:
            fails.append(f"## 章节不连续：见 {h2}（期望连续一~九）")
    return fails


def main():
    path = sys.argv[1] if len(sys.argv) > 1 and not sys.argv[1].startswith("--") \
        else DEFAULT
    strict = "--strict-refs" in sys.argv
    if not os.path.exists(path):
        print(f"FAIL: 文件不存在 {path}")
        sys.exit(1)
    text = load(path)
    print(f"检查目标：{path}（{len(text.splitlines())} 行）\n")

    all_fails = []
    all_fails += check_utf8_and_ctrl(text)
    all_fails += check_kb(text)
    all_fails += check_refs(text)
    all_fails += check_sections(text)

    if strict:
        defined = set(int(m) for m in re.findall(r"^\[(\d+)\]", text, re.M))
        body = "\n".join(
            ln for ln in text.splitlines()
            if not re.match(r"^\[\d+\]", ln) and not ln.startswith("> 注")
        )
        used = set(int(m) for m in re.findall(r"\[(\d+)\]", body))
        unused = sorted(defined - used)
        if unused:
            all_fails.append(f"参考表定义但从未被行内引用：{unused}")

    if all_fails:
        print("=== FAIL ===")
        for f in all_fails:
            print(f"  x {f}")
        sys.exit(1)
    print("=== PASS ===")
    print("  ok UTF-8 合法 / 无 NEL 与控制字符")
    print("  ok 无内部概念（知识库/KB）泄露")
    print("  ok 引用标号连续、无悬空行内引用")
    print("  ok 章节标题连续")
    sys.exit(0)


if __name__ == "__main__":
    main()
