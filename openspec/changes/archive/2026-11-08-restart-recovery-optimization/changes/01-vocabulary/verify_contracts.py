#!/usr/bin/env python3
"""Verify that lines containing the six frozen strings are identical between
the stage-1 baseline and the working tree. Read-only; prints a machine-checkable
verdict. It compares the ORDERED list of whole lines containing a fixed substring
per file (stricter than a set, but not whole-file byte identity). A regex
character class would misread [转世通报] as a character set, so fixed substrings
are used instead of grep -E."""
import subprocess
from pathlib import Path

FILES = [
    "examples/wechat-bot/wechat_notice.go",
    "examples/wechat-bot/wechat_notice_test.go",
    "examples/wechat-bot/wechat_test.go",
    "examples/wechat-bot/restart-maintenance.sh",
    "examples/wechat-bot/restart-tagent.sh",
    "features/evolution/meditation/judgment.go",
    "features/evolution/meditation/judgment_test.go",
    "tests/integration/host/notice_source_test.go",
    "tests/integration/acceptance/scenario_s05_test.go",
    "golden/runtime/scenarios.json",
]
NEEDLES = [
    '"reincarnation"',
    "[reincarnation]",
    "REINCARNATION_NOTICE",
    "[转世通报]",
    "TestScenario05CloseHandoffReincarnation",
    "TestReincarnationNoticePreserved",
]
BASE = "1c4e165f"


def read(rev: str, path: str) -> str:
    if rev == "WORK":
        return Path(path).read_text(errors="ignore")
    return subprocess.run(["git", "show", f"{rev}:{path}"], capture_output=True, text=True).stdout


def contract_lines(rev: str):
    return {f: [l for l in read(rev, f).splitlines() if any(n in l for n in NEEDLES)] for f in FILES}


def main():
    base, work = contract_lines(BASE), contract_lines("WORK")
    mismatch = []
    for f in FILES:
        if base[f] != work[f]:
            mismatch.append(f)
            for l in set(base[f]) - set(work[f]):
                print(f"  LOST: {f}: {l[:110]}")
            for l in set(work[f]) - set(base[f]):
                print(f"  GAIN: {f}: {l[:110]}")
    print("files_with_contract_lines_base=", sum(1 for f in FILES if base[f]))
    print("total_contract_lines_base=", sum(len(v) for v in base.values()))
    print("MISMATCH_FILES=", len(mismatch), mismatch)
    print("VERDICT:", "PASS (line sets identical)" if not mismatch else "FAIL")
    raise SystemExit(0 if not mismatch else 1)


if __name__ == "__main__":
    main()
