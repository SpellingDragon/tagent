# mail-poller — 微信助手邮件入站渠道

**一句话架构**：`agently-cli message +watch`（QQ Agent 邮箱官方长轮询，NDJSON 逐行输出）→ `mail_poller.py` 解析并按 `message_id` 去重 → `POST /task` 注入本机 tagent 消费，回复自动走微信通道。

## 部署

```bash
# 0. 前置: agently-cli 已安装且 auth login 授权过 (token 在 ~/.agently-cli/, 本服务不经手密钥)
# 1. (可选) 自定义配置
cp mail-poller/.env.example mail-poller/.env   # 编辑后取消 service 中 EnvironmentFile= 注释
# 2. 安装常驻服务 (systemd, 崩溃 5s 自愈 + 开机自启)
sudo cp deploy/tagent-mail-poller.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now tagent-mail-poller
```

## 验证

```bash
systemctl is-active tagent-mail-poller
# 发一封主题带 [e2e-test] 的邮件到授权邮箱, 然后看:
tail -f data/mail-poller/poller.log          # 出现 "已注入 msg_xxx"
cat data/mail-poller/seen.json               # message_id 落盘 (重启去重依据)
# 微信侧收到 [email-inbound] 开头的回显即全链路贯通
```

## 回滚（bot 零改动零回滚）

```bash
sudo systemctl disable --now tagent-mail-poller   # 停止并取消自启
sudo rm /etc/systemd/system/tagent-mail-poller.service && sudo systemctl daemon-reload
# 去重状态与日志在 data/mail-poller/, 可一并删除; wechat-bot 本体无任何侵入
```

## 已知限制

- `POST /task` 无 metadata 通道：邮件触发回复 fallback 到**最近活跃微信会话**（design D6/Open Question，定向回执待后续）
- 发件需两步确认（CLI 返回 confirmation_token 后原样重放），脚本自动化发件需处理该机制
