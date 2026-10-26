## Decisions
以 ../../design.md D4/D5 确切签名为准，只写 `.qoder/staging/prototype-reset/03-adapters/` 下plugins.go/memory.go/model.go。
Kinds明确选择；Before/After闭包可变更Data/Meta。ResultMemoryPlugin只存eligible的ToolResult，首次原文供模型，调用后CompactResults仅处理memory_key结果，输入不动；registry配置控制eligible，不在core解析票据。
Memory为锁保护简单map+顺序key，JSON快照恢复续号；Recall直接给原文/miss。CallModel只调用已确定实例并记录含成功/错误的完整request/response，不持状态、不重新选模型或组装system。
域内gofmt语法，集成后行为验证；不新增单测或helper框架。
