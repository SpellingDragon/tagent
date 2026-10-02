# agent-mail-send — Agent Mail 发信 SOP

## 解决什么痛点
agently-cli 发信语法已在 09-16 / 09-30 / 10-01 三个会话各重学一遍（裸 `+send` 报 unknown command、`--from` 不存在、body-file 绝对路径被拒、漏确认令牌静默不出队）。本 skill 固化唯一可用形态，一次到位。

## 权威用法（2026-10-01 实测 ok:true）

```bash
# nvm 升级会换 node 版本目录，勿写死版本号；glob 取最新 + bin 目录回退（2026-10-02 补）
AG=$(ls /home/lighthouse/.local/lib/node-*/lib/node_modules/@tencent-qqmail/agently-cli/node_modules/@tencent-qqmail/agently-cli-linux-x64/bin/agently-cli 2>/dev/null | tail -1)
[ -z "$AG" ] && AG=$(ls /home/lighthouse/.local/lib/node-*/bin/agently-cli | tail -1)

# 第一步：写正文到已存在的相对路径（body-file 拒绝绝对路径）
mkdir -p .tagent-workspace/outbox
cat > .tagent-workspace/outbox/xx.md <<'EOF'
正文...
EOF

# 第二步：首发 → 返回 JSON + confirmation-token
"$AG" message +send --to codingweiye@agent.qq.com \
  --subject "[tagent] 主题" \
  --body-file .tagent-workspace/outbox/xx.md

# 第三步：带令牌重发同一命令 → ok:true, queued:true
"$AG" message +send ... --confirmation-token <上步返回的 ctk_xxx>
```

## 坑清单（每条都真踩过）
1. 子命令是 `message +send`，裸 `+send` 不存在；`--from` 参数不存在
2. `--body-file` 只收相对路径（base 目录下），绝对路径报 invalid path
3. **两段式确认**：首发只返回 `confirmation-token` 不入队，必须带 `--confirmation-token` 重跑才真正 `queued:true`——漏这步 = 静默未发
4. AG 二进制不在 PATH，按上面 nvm 全路径；丢了用 `find /home/lighthouse/.local/lib -name agently-cli -path "*bin*"` 重新定位
5. 语法不确定时 `"$AG" --help` 是权威源（examples 段含全部子命令）
6. `message +list` 的目录旗标是 `--dir`（inbox/sent/trash/spam），`--folder` 不存在（2026-10-02 实踩）

## 发日志/证据包前的强制检查（凭证卫生）
- `grep -rn "hf_\|sk-\|api[_-]key" <将发送的正文/日志>` —— 命中即打码（`hf_tTct…前缀+长度`），确认 0 命中再发
- 背景：2026-09-30 证据包曾把 HF token 明文带出本机，远端已要求轮换并立此纪律
- token 传递只走环境变量（如 HF_TOKEN），任何邮件/日志/工具输出不得出现明文

## 附件（2026-10-02 实测修正）
直接随信挂：`message +send ... --attachment ./x.tar.gz`（相对 CWD 路径，旗标可重复挂多个）；无需先走 `attachment +upload`（那是拿 file_id 复用/账号超限场景的两步流，详见其 --help）。正文文件同理：`--body-file ./x.md`（相对路径，.md 自动按 Markdown 发）。

## 发后回执实证（queued:true ≠ 对端已收）
`{"ok":true,"queued":true}` 只代表入队。实证查 sent 侧：
`"$AG" message +list --dir sent --limit 3 --has-attachments` —— 见到本信 message_id 即已落地。
注意：`message +search` 对刚发出的信有索引滞后（2026-10-02 实测刚发即搜为空），sent +list 才是权威回执。
