#!/usr/bin/env python3
"""mail-poller: agently-cli +watch -> 去重 -> 注入 tagent POST /task。

openspec change: agentmail-email-inbound-channel (D2/D3 定案 2026-09-11)
- 官方 `message +watch --msg-format full` 子进程长轮询, NDJSON 逐行解析;
- message_id 去重 seen-store (JSON 原子落盘, 容量上限截断);
- 注入 202 视为成功, 失败按指数退避重试, 成功后才记 seen (at-least-once);
- exit code 分级: 1/4 网络->短退避重启, 7 限频->按 Retry-After, 3 授权->告警降频;
- CLI token 由 agently-cli 自管理, 本进程不经手任何凭据。
"""
from __future__ import annotations

import glob
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from html.parser import HTMLParser
from pathlib import Path

def log(msg: str) -> None:
    print(f"[mail-poller] {time.strftime('%Y-%m-%dT%H:%M:%S')} {msg}", flush=True)

def resolve_cli() -> str:
    env = os.environ.get("AGENTLY_CLI")
    if env:
        return env
    p = shutil.which("agently-cli")
    if p:
        return p
    cands = sorted(glob.glob(str(Path.home() / ".local/lib/node-*/bin/agently-cli")))
    if cands:
        return cands[-1]
    return "agently-cli"

class Cfg:
    def __init__(self) -> None:
        script_dir = Path(__file__).resolve().parent
        self.task_url = os.environ.get("TAGENT_TASK_URL", "http://127.0.0.1:8089/task")
        self.seen_db = Path(os.environ.get(
            "MAIL_POLLER_SEEN_DB",
            str(script_dir.parent / "data/mail-poller/seen.json")))
        self.errlog = Path(os.environ.get(
            "MAIL_POLLER_ERRLOG",
            str(script_dir.parent / "data/mail-poller/watch-stderr.log")))
        self.cli = resolve_cli()
        self.tag = os.environ.get("MAIL_POLLER_TAG", "[email-inbound]")
        self.max_body = int(os.environ.get("MAIL_POLLER_MAX_BODY_CHARS", "60000"))
        self.seen_cap = int(os.environ.get("MAIL_POLLER_SEEN_CAP", "5000"))

class _TextExtract(HTMLParser):
    _BLOCK = {"p", "div", "br", "tr", "li", "h1", "h2", "h3", "h4", "h5", "h6",
              "blockquote", "pre", "table", "ul", "ol", "hr"}

    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.parts: list[str] = []
        self._skip = 0

    def handle_starttag(self, tag, attrs):
        if tag in ("script", "style"):
            self._skip += 1
        elif tag in self._BLOCK:
            self.parts.append("\n")

    def handle_endtag(self, tag):
        if tag in ("script", "style") and self._skip:
            self._skip -= 1
        elif tag in self._BLOCK:
            self.parts.append("\n")

    def handle_data(self, data):
        if not self._skip:
            self.parts.append(data)

def html_to_text(raw: str) -> str:
    p = _TextExtract()
    p.feed(raw)
    p.close()
    text = "".join(p.parts)
    text = re.sub(r"[ \t]+", " ", text)
    text = re.sub(r"\n\s*\n+", "\n\n", text)
    return text.strip()

def body_to_text(body: str, fmt: str) -> str:
    if (fmt or "").upper() == "HTML":
        return html_to_text(body)
    return body

def format_content(msg: dict, tag: str, max_body: int) -> str:
    frm = msg.get("from") or {}
    fr = f"{frm.get('name') or ''} <{frm.get('email') or ''}>".strip()
    lines = [
        tag,
        f"From: {fr}",
        f"Subject: {msg.get('subject') or '(无主题)'}",
        f"Date: {msg.get('created_at') or ''}",
        f"message_id: {msg.get('message_id') or ''}",
    ]
    if msg.get("has_attachments"):
        names = ", ".join((a.get("filename") or "?") for a in (msg.get("attachments") or []))
        lines.append(f"Attachments: {names or '(未列出)'}")
    body = msg.get("body") or ""
    if body:
        text = body_to_text(body, msg.get("body_format") or "")
        if len(text) > max_body:
            text = text[:max_body] + "…[正文截断]"
        lines += ["", text]
    else:
        lines += ["", "(空正文)"]
    return "\n".join(lines)

def format_placeholder(message_hint: str, reason: str, tag: str) -> str:
    return f"{tag}\n(取回正文失败: {reason}; hint={message_hint})"

class SeenStore:
    def __init__(self, path: Path, cap: int = 5000) -> None:
        self.path = path
        self.cap = cap
        self.ids: dict[str, str] = {}
        self._load()

    def _load(self) -> None:
        try:
            self.ids = json.loads(self.path.read_text(encoding="utf-8"))
        except FileNotFoundError:
            self.ids = {}
        except Exception as e:
            log(f"seen-store 读取失败({e})，按空处理")
            self.ids = {}

    def has(self, mid: str) -> bool:
        return mid in self.ids

    def mark(self, mid: str) -> None:
        self.ids[mid] = time.strftime("%Y-%m-%dT%H:%M:%S%z")
        if len(self.ids) > self.cap:
            for k in sorted(self.ids, key=lambda k: self.ids[k])[: len(self.ids) - self.cap]:
                del self.ids[k]
        self._flush()

    def _flush(self) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        fd, tmp = tempfile.mkstemp(dir=self.path.parent, prefix=".seen-")
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as f:
                json.dump(self.ids, f, ensure_ascii=False)
            os.replace(tmp, self.path)
        except BaseException:
            try:
                os.unlink(tmp)
            except OSError:
                pass
            raise

def inject(url: str, content: str, timeout: float = 10.0) -> bool:
    payload = json.dumps({"messages": [{"role": "user", "content": content}]}).encode("utf-8")
    req = urllib.request.Request(url, data=payload,
                                 headers={"Content-Type": "application/json"}, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            ok = r.status == 202
            if not ok:
                log(f"注入非202返回: HTTP {r.status}")
            return ok
    except urllib.error.HTTPError as e:
        log(f"注入 HTTPError: {e.code}")
        return False
    except Exception as e:
        log(f"注入失败: {e}")
        return False

def inject_with_retry(url: str, content: str) -> bool:
    """失败无限退避重试 (bot 宕机补投语义); Ctrl-C 可中断。"""
    wait = 1
    n = 0
    while True:
        if inject(url, content):
            if n:
                log(f"注入成功(第{n}次重试后)")
            return True
        n += 1
        log(f"注入退避重试 #{n}, {wait}s")
        time.sleep(wait)
        wait = min(wait * 2, 60)

def classify_exit(code: int) -> tuple[str, int]:
    """返回 (处置类别, 建议重启退避秒)。"""
    if code in (1, 4):
        return ("network", 5)
    if code == 7:
        return ("rate-limited", 30)
    if code == 3:
        return ("auth", 120)
    return ("unknown", 10)

def retry_after_from_stderr(errlog: Path) -> int | None:
    try:
        tail = errlog.read_text(encoding="utf-8", errors="replace")[-2000:]
    except OSError:
        return None
    m = re.findall(r"Retry-After[:\s]*(\d+)", tail, re.IGNORECASE)
    return int(m[-1]) if m else None

def handle_line(line: str, seen: SeenStore, cfg: Cfg) -> None:
    try:
        obj = json.loads(line)
    except json.JSONDecodeError as e:
        log(f"NDJSON 解析失败({e}): {line[:120]}")
        return
    msg = obj.get("message")
    if msg is not None:
        mid = msg.get("message_id") or ""
        if not mid:
            log("事件缺 message_id，跳过")
            return
        if seen.has(mid):
            log(f"重复事件 {mid}，去重跳过")
            return
        if inject_with_retry(cfg.task_url, format_content(msg, cfg.tag, cfg.max_body)):
            seen.mark(mid)
            subj = (msg.get("subject") or "")[:40]
            log(f"已注入 {mid} ({subj})")
        return
    err = obj.get("fetch_error")
    if err is not None:
        log(f"fetch_error 事件: {err}")
        hint = json.dumps(err, ensure_ascii=False)[:120]
        content = format_placeholder(hint, str(err.get("type") or "unknown"), cfg.tag)
        if inject_with_retry(cfg.task_url, content):
            log("fetch_error 占位符已注入")
        return
    log(f"未知事件形态(键={list(obj)})，跳过")

def build_watch_cmd(cfg: Cfg) -> list[str]:
    return [cfg.cli, "message", "+watch", "--msg-format", "full"]

def run_watch(cfg: Cfg, seen: SeenStore) -> int:
    cmd = build_watch_cmd(cfg)
    log(f"启动: {' '.join(cmd)}")
    cfg.errlog.parent.mkdir(parents=True, exist_ok=True)
    with open(cfg.errlog, "ab") as err_f:
        proc = subprocess.Popen(cmd, stdout=subprocess.PIPE,
                                stderr=err_f, text=True, bufsize=1)
        assert proc.stdout is not None
        try:
            for line in proc.stdout:
                line = line.strip()
                if line:
                    handle_line(line, seen, cfg)
        finally:
            proc.wait()
    code = proc.returncode if proc.returncode is not None else 0
    log(f"+watch 退出 code={code}")
    return code

def main() -> int:
    cfg = Cfg()
    seen = SeenStore(cfg.seen_db, cfg.seen_cap)
    log(f"mail-poller 启动: task_url={cfg.task_url} seen_db={cfg.seen_db} cli={cfg.cli}")
    while True:
        try:
            code = run_watch(cfg, seen)
        except KeyboardInterrupt:
            log("手动中断退出")
            return 0
        disp, wait = classify_exit(code)
        if disp == "rate-limited":
            ra = retry_after_from_stderr(cfg.errlog)
            if ra:
                wait = max(wait, ra)
        log(f"处置={disp}，{wait}s 后重启 +watch")
        try:
            time.sleep(wait)
        except KeyboardInterrupt:
            log("手动中断退出")
            return 0

if __name__ == "__main__":
    sys.exit(main())
