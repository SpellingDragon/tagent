## 1. yaml 恢复

- [x] 1.1 找到 /tmp/ 下所有 tagent.yaml.bak.* 备份，识别打补丁前（干净）的最新备份，输出备份清单与基准选择
- [x] 1.2 对照基准备份与当前 examples/wechat-bot/tagent.yaml，列出全部差异行（含缩进/注释残留），确认改动范围后再动文件
- [x] 1.3 恢复 tagent.yaml 至干净状态：删除 unified_compress_target 补丁行与抢救注释残留；核对 keep_recent_tasks: 4 为生效态（未被注释），若被误注释则恢复
- [x] 1.4 验证恢复结果：yaml 可被解析（如 python -c "import yaml; yaml.safe_load(open(...))" 或等效手段），并再次 diff 备份确认一致

## 2. 死区修复内化为默认行为

- [x] 2.1 定位 buildCompressorOpts 实现（grep 主框架源码），梳理当前分龄压缩目标与触发线的取值来源
- [x] 2.2 修改 buildCompressorOpts：分龄压缩目标默认对齐触发线（消除死区空转），不引入任何新配置开关
- [x] 2.3 删除 config.go 中 UnifiedCompressTarget 字段
- [x] 2.4 删除 build_agent.go 中对该字段的映射行
- [x] 2.5 删除 agent.go 中的消费分支
- [x] 2.6 grep 全工程确认无 UnifiedCompressTarget / unified_compress_target 残留（代码与配置两处）

## 3. 构建验证

- [x] 3.1 go build 主框架（go build ./... 于主框架根），全绿
- [x] 3.2 go build examples/wechat-bot 模块，全绿

## 4. 拉起机制排查（只读）

- [x] 4.1 检查 crontab（crontab -l 及 /etc/cron.*、/var/spool/cron）中与 tagent/wechat-bot 相关的拉起条目
- [x] 4.2 检查 systemd（systemctl list-units --type=service、相关 unit 文件）中与 tagent/wechat-bot 相关的服务
- [x] 4.3 检查 watchdog/进程守护脚本/ supervisor 等其他拉起机制（ps auxf 查看父进程、/etc/supervisor*、nohup 脚本）
- [x] 4.4 汇总排查结论：为何停止后未被自动拉起（缺失/失效的环节），写入 changes/wechat-bot-yaml-cleanup-deadband-default/findings.md；修复建议单列但不在本计划内执行
- [x] 4.5 确认排查全程未触碰运行中的 bot（PID 702169 未重启、会话未中断）

## 5. 收尾

- [x] 5.1 汇总本次变更：yaml 恢复结果、代码改动清单、构建结果、排查结论，向用户报告并确认下一步（下次计划内重启的时机）
