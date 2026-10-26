# design：host-layer

## D1 分层
- `tagent.Boot(configPath)` → 应用宿主（一行启动）
- `tagent.Assemble(decl, agents, opts...)` → 框架扩展者（手动组装）
- Boot 内部走 Assemble——两层共享底层

## D2 Host 结构
```go
type Host struct {
    run       *Run
    meditation *meditationDriver  // 归核
    face       *faceServer        // 标准注入面
    config     *BootConfig        // 原始配置
}
func Boot(path string, opts ...BootOption) (*Host, error)
func (h *Host) Invoke(ctx, req) / Close(ctx) / ServeFace(port)
```

## D3 装配自动化
- 工具面：send（回调注入）→subagent（agent 引用递归）→preset→file/exec/mcp/spec/recall/knowledge 按声明自动
- 模型：provider→model 按声明解析
- 冥想：声明→驱动→定时器→投递端口

## D4 验收
wechat.go ≤900 行；tagent import ≤5 包；行为等价（golden match＋wechat test 全绿）。
