---
name: interview
description: >
  面试备战助手：驱动 agent-interview-helper 仓库的零依赖 CLI，跨四个题库
  （agent/ai-infra/llm/mygo，共 6766 行题库+计划）做查计划、搜题、按天推题、
  随机模拟面试。触发词：「抽一道题」「模拟面试」「来几道 agent 题」「今天的计划」
  「面试 practice」。输出题目后等用户口述回答，再按四要素（定义/为什么重要/
  工程做法/权衡失败模式）点评补漏。
---

# Interview 面试备战技能（引用型：脚本与题库在 clone 目录）

## 资产位置（单一真相源，勿拷贝脚本）

- **CLI**：`/home/lighthouse/agent-interview-helper/interview_assistant.py`（Python 3.6+，零依赖）
- **题库**：同目录下 `agent/`（主攻，110 题）、`ai-infra/`、`llm/`、`mygo/`
- **更新**：`cd /home/lighthouse/agent-interview-helper && git pull --ff-only`

## 常用命令（一律带 --json 供解析）

```bash
D=/home/lighthouse/agent-interview-helper
# 按主题抽题（数据目录=领域目录）
python3 $D/interview_assistant.py --data-dir $D/agent --topic MCP --count 3 --json
# 随机模拟面试
python3 $D/interview_assistant.py --data-dir $D/agent --mock --count 5 --json
# 搜题
python3 $D/interview_assistant.py --data-dir $D/agent --search Memory --json
# 计划概览 / 某天详情
python3 $D/interview_assistant.py --data-dir $D/agent --overview --json
python3 $D/interview_assistant.py --data-dir $D/agent --day 7 --json
```

## 微信交互范式（主 Agent 执行口径）

1. 用户说抽题/模拟 → 按其指定领域跑上表命令（未指定默认 agent/）
2. **每次只出 1 题**（微信场景），题干 + 「请口述你的回答」
3. 收到回答后按四要素点评：哪些要素覆盖了、漏了什么、给一条追问压力测试
4. 用户说「下一题」再抽新的；同主题连抽避免重复 qid（对照 state 或记忆本轮已出题号）
5. 领域词映射：agent→agent/，infra→ai-infra/，llm→llm/，go→mygo/
