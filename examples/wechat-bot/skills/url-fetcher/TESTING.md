# 测试文档

## 测试场景

### 1. 基础测试 - 普通网站

```bash
node smart_fetcher.js --url "https://example.com" --output text
```

**预期结果**：
- ✅ 自动检测为 standard 类型
- ✅ 使用 lightweight 策略
- ✅ 成功提取页面内容
- ✅ 提取元数据

### 2. 微信公众号文章

```bash
node smart_fetcher.js --url "https://mp.weixin.qq.com/s/hZgFoLFnAq6-yqd4i1v8ng" --output text
```

**预期结果**：
- ✅ 自动检测为 special 类型（微信公众号）
- ✅ 使用 heavyweight 策略
- ✅ 使用微信 User-Agent
- ✅ 正确提取标题（从特殊选择器）
- ✅ 正确提取正文（#js_content）
- ✅ 正确提取元数据（作者、描述、图片）

### 3. 手动指定策略

```bash
node smart_fetcher.js --url "https://example.com" --strategy heavyweight --output text
```

**预期结果**：
- ✅ 使用手动指定的 heavyweight 策略
- ✅ 更长的等待时间
- ✅ 自动滚动

### 4. 自定义选项

```bash
node smart_fetcher.js --url "https://example.com" \
  --wait 5000 \
  --user-agent "Custom Agent" \
  --screenshot \
  --output json
```

**预期结果**：
- ✅ 使用自定义等待时间 5000ms
- ✅ 使用自定义 User-Agent
- ✅ 保存截图
- ✅ JSON 格式输出

## 实际测试结果

### 测试1: 普通网站
- ✅ 通过
- 自动检测：example.com (standard)
- 策略：lightweight
- 耗时：约 2 秒

### 测试2: 微信公众号
- ✅ 通过
- 自动检测：mp.weixin.qq.com (special)
- 策略：heavyweight
- 特殊网站：微信公众号
- 标题：正确提取 "用自然语言替代复杂代码"
- 正文：正确提取完整内容
- 作者：天猫技术团队
- 耗时：约 11 秒

### 测试3: JSON 输出
- ✅ 通过
- 完整的结构化数据
- 包含所有元数据

## 性能对比

| 网站类型 | 策略 | 等待时间 | 滚动次数 | 耗时 | 成功率 |
|---------|------|---------|---------|------|--------|
| 普通网站 | lightweight | 1秒 | 0 | ~2秒 | 高 |
| SPA | standard | 3秒 | 3次 | ~5秒 | 高 |
| 微信公众号 | heavyweight | 5秒 | 5次 | ~11秒 | 高 |

## 优势对比

### 旧版 url_fetcher.js
- ❌ 无法提取微信文章正文（只有预览）
- ❌ 标题提取不正确
- ❌ 没有网站适配
- ❌ 没有智能策略

### 新版 smart_fetcher.js
- ✅ 成功提取微信文章完整内容
- ✅ 正确提取标题
- ✅ 自动网站适配
- ✅ 渐进式智能策略
- ✅ 更好的元数据提取
- ✅ 支持自定义配置

## 已知问题和改进方向

### 当前版本
- ✅ 标题提取正常
- ✅ 正文提取正常
- ✅ 元数据提取正常
- ⚠️ 策略名称显示为 null（不影响功能）

### 未来改进
1. 浏览器实例复用（减少启动时间）
2. 支持更多特殊网站（知乎、掘金等）
3. 添加内容清理和格式化
4. 支持批量URL处理
5. 添加缓存机制
6. 支持登录态的页面获取
