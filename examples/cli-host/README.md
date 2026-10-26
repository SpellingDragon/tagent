# tagent CLI Host — 第二个宿主示例

一个命令行宿主：stdin 一行 → `Assemble` → `Run.Invoke` → stdout 一行回复。默认模型是内置的
`mock`，**不需要任何凭据即可启动**；同一份代码通过环境变量即可切换到真实 OpenAI 兼容模型。

## 六十秒上手

需要 Go 1.24.1 及以上（与主仓 `go.mod` 一致）。

```bash
cd examples/cli-host
go run . -c "hello"          # 单次模式：输出 mock reply: hello
```

对话式 REPL（提示符写到 stderr，回复写到 stdout，两者可分别重定向）：

```bash
go run .                     # 逐行输入；/exit 或 Ctrl-D 退出
printf 'hello\nsecond\n' | go run .
```

可选持久档：把运行期提交的事实落到本地文件存储层（不传 `-data` 时全部在内存中）。纯问答回合若没有待提交的任务事实，目录可能保持为空——它承载事实链，不是聊天记录快照：

```bash
go run . -data /tmp/tagent-cli-data -c "hello"
```

## 架构位置

本示例是**组合根的最小宿主形态**，与主仓、与 `examples/wechat-bot` 走同一个公共装配入口：

```
flags / env
   │
   ▼
parseFlags ──► newNativeModel（mock 或真实模型）
   │
   ▼
config.Declaration + tagent.NativeAgent
   │
   ▼
tagent.Assemble(WithGovernance, [WithStorage])   ← 唯一公共装配入口
   │
   ▼
Run.Invoke(InvokeRequest{显式 Address, Input})
   │
   ▼
Run.SessionHistory(addr) ──► stdout
   │
   ▼
Run.Close(addr)                                  ← 排空已接受义务
```

- 一个进程只装配一个 `Run`；每次输入是一次独立 invocation，地址是显式的
  `{AgentID, SessionID}`，从不回退到"最近会话"。
- `-data` 打开 `memory.StorageLocalFile` 事实层；不传时事实只存在于内存。
- 不依赖微信、HTTP 或任何外部传输；stdin/stdout 就是传输层。

## 与 wechat-bot 宿主的差异

| 维度 | `examples/cli-host` | `examples/wechat-bot` |
|---|---|---|
| 传输 | stdin / stdout（可管道） | 微信消息上行 + 长文/文件下行 |
| 启动凭据 | **零凭据**（内置 mock） | 微信登录态 + LLM key |
| 身份 | 进程内显式 `-agent` / `-session` | 聊天身份经路由表映射为会话地址 |
| 持久化 | 可选 `-data`（localfile 事实层） | 生产级多存储、治理、自进化、可靠性 |
| 工具面 | 无原生工具（最小装配） | send/file/exec/mcp/skill 等完整工具面 |
| 装配选项 | `WithGovernance` 必填，其余默认 | `WithGovernance` + 审批策略 + 存储 + 录制等 |
| 适用 | 首次接触、调试、自动化管道 | 常驻个人助手部署 |

两者都是**独立 module**，经 `replace` 指向主仓，不向主仓 `go.mod` 增加依赖。

## mock 与真实模型切换

默认 `-model mock` 走内置 mock：它把最后一条 user 消息回显为 `mock reply: <输入>`，不发起任何
网络请求，但走的是真实的 `Assemble → Loop → Invoke → SessionHistory` 链路。

切换到真实 OpenAI 兼容模型时，给出模型名，并让密钥出现在环境变量里：

```bash
export OPENAI_API_KEY=sk-...
go run . -model gpt-4o-mini -c "hello"

# 自定义端点与密钥变量名
go run . -model my-model \
  -base-url https://api.example.com/v1 \
  -api-key-env MY_API_KEY \
  -c "hello"
```

| 参数 / 环境变量 | 默认值 | 说明 |
|---|---|---|
| `-model` / `TAGENT_MODEL` | `mock` | `mock` 为零凭据路径；其他值进入真实模型 |
| `-base-url` / `TAGENT_BASE_URL` | 空（OpenAI 官方端点） | OpenAI 兼容端点 |
| `-api-key-env` / `TAGENT_API_KEY_ENV` | `OPENAI_API_KEY` | 保存密钥的环境变量名 |
| `-agent` | `assistant` | 本次装配的 agent 身份 |
| `-session` / `TAGENT_SESSION_ID` | `cli-session` | 会话身份 |
| `-data` | 空 | 持久档目录（localfile 事实层） |
| `-timeout` | `2m` | 单次 invocation 超时 |
| `-c` | 空 | 单次输入；为空时从 stdin 逐行读取 |

真实模型缺少对应环境变量时，宿主以具名错误退出（exit 1），不会静默降级到 mock。

## 测试

```bash
go test ./... -count=1
```

`main_test.go` 在清空凭据环境后以 `-c "hello"` 驱动端到端链路，断言 stdout 为
`mock reply: hello`，证明零凭据路径真实可跑。
