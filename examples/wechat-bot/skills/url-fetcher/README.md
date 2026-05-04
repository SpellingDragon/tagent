# URL Fetcher - 智能网页内容获取工具

一个基于 Playwright 的智能网页内容获取工具，支持渐进式策略和自动网站适配。

## ✨ 特性

### 🎯 核心能力
- **自动网站识别**：自动检测 URL 类型（微信公众号、Medium、Twitter、知乎等）
- **渐进式策略**：从轻量级到重量级，智能选择最佳提取方式
- **智能内容提取**：自动识别正文区域，支持多种选择器 fallback
- **懒加载支持**：自动滚动触发动态内容加载
- **元数据提取**：自动提取标题、描述、作者、发布时间等

### 🔄 渐进式策略

| 策略 | 等待时间 | 滚动 | 适用场景 |
|------|---------|------|---------|
| lightweight | 1秒 | ❌ | 简单静态页面 |
| standard | 3秒 | ✅ | 标准网页、SPA |
| heavyweight | 5秒 | ✅ | 复杂SPA、微信公众号 |

### 🌐 特殊网站支持

| 网站 | 专用配置 | 内容选择器 |
|------|---------|-----------|
| 微信公众号 | 微信 UA、长等待、多次滚动 | `#js_content` |
| Medium | 标准等待、滚动 | `article`, `main` |
| Twitter | 快速等待、滚动 | `[data-testid="tweet"]` |
| 掘金 | 标准等待 | `.article-content` |
| 知乎专栏 | 标准等待 | `.Post-RichTextContainer` |

## 📦 安装

```bash
cd skills/url-fetcher
npm install
```

## 🚀 使用方法

### 基本用法

```bash
# 获取网页内容
node smart_fetcher.js --url "https://example.com"

# 获取微信公众号文章
node smart_fetcher.js --url "https://mp.weixin.qq.com/s/xxxxx"

# 获取并截图
node smart_fetcher.js --url "https://example.com" --screenshot

# 文本格式输出
node smart_fetcher.js --url "https://example.com" --output text
```

### 高级选项

```bash
# 自定义等待时间
node smart_fetcher.js --url "https://example.com" --wait 5000

# 自定义 User-Agent
node smart_fetcher.js --url "https://example.com" --user-agent "Mozilla/5.0..."

# 设置请求头
node smart_fetcher.js --url "https://example.com" \
  --headers '{"Authorization":"Bearer token"}'

# 手动指定策略
node smart_fetcher.js --url "https://example.com" --strategy heavyweight

# 使用代理
node smart_fetcher.js --url "https://example.com" --proxy "http://proxy:8080"
```

### 查看帮助

```bash
node smart_fetcher.js --help
```

## 📤 输出格式

### JSON 格式（默认）

```json
{
  "success": true,
  "url": "https://example.com",
  "title": "页面标题",
  "statusCode": 200,
  "strategy": "standard",
  "urlInfo": {
    "type": "special",
    "domain": "mp.weixin.qq.com",
    "config": { ... }
  },
  "content": {
    "article": {
      "success": true,
      "selector": "#js_content",
      "text": "文章正文...",
      "html": "<div>...</div>",
      "length": 1234
    },
    "metadata": {
      "title": "...",
      "description": "...",
      "author": "...",
      "publishDate": "...",
      "image": "..."
    },
    "fullText": "完整页面文本",
    "fullHtml": "完整HTML"
  },
  "timing": {
    "total": 3456
  }
}
```

### 文本格式

```
========== Smart Fetcher Result ==========

URL: https://example.com
标题: 页面标题
策略: standard
状态码: 200
耗时: 3456ms

--- 文章正文 (使用选择器: #js_content) ---

文章正文内容...

--- 元数据 ---

{ ... }
```

## 🔧 技术架构

### 核心组件

```
smart_fetcher.js
├── CONFIG           # 配置管理
├── URLDetector      # URL类型检测
├── ContentExtractor # 内容提取器
│   ├── extractArticle()      # 文章提取
│   ├── extractMetadata()     # 元数据提取
│   └── extractSmart()        # 智能提取
└── SmartFetcher     # 核心获取器
    ├── init()           # 初始化
    ├── launchBrowser()  # 启动浏览器
    ├── createPage()     # 创建页面
    ├── navigate()       # 导航
    ├── waitForContent() # 等待内容
    ├── scrollToLoad()   # 滚动加载
    └── extractContent() # 提取内容
```

### 工作流程

```
1. URL检测 → 识别网站类型
2. 策略选择 → 根据网站类型选择策略
3. 浏览器启动 → 配置浏览器参数
4. 页面导航 → 访问目标URL
5. 等待内容 → 根据策略等待
6. 滚动加载 → 触发懒加载（如需要）
7. 内容提取 → 提取正文和元数据
8. 返回结果 → JSON或文本格式
```

## 🛠️ 扩展新网站

在 `CONFIG.siteConfigs` 中添加新配置：

```javascript
'newsite.com': {
  name: '新网站',
  wait: 3000,
  scroll: true,
  scrollTimes: 3,
  contentSelectors: ['.article', 'main', 'article'],
  minContentLength: 100
}
```

## 📝 示例场景

### 场景1：获取微信公众号文章

```bash
node smart_fetcher.js --url "https://mp.weixin.qq.com/s/xxxxx"
```

自动识别为微信文章，使用：
- 微信 User-Agent
- 5秒等待时间
- 5次滚动
- `#js_content` 选择器

### 场景2：获取Medium文章

```bash
node smart_fetcher.js --url "https://medium.com/@user/article"
```

自动识别为Medium，使用：
- 标准 User-Agent
- 4秒等待时间
- 5次滚动
- `article` 选择器

### 场景3：获取普通网页

```bash
node smart_fetcher.js --url "https://example.com/blog"
```

使用轻量级策略：
- 标准 User-Agent
- 1秒等待时间
- 无滚动
- 通用选择器 fallback

## ⚡ 性能优化

- **按需等待**：根据网站类型智能选择等待时间
- **选择器优先级**：优先尝试最可能的选择器
- **内容长度验证**：避免提取到无效内容
- **浏览器复用**：可以考虑复用浏览器实例（未来优化）

## 🐛 错误处理

- 多重 fallback 机制
- 选择器逐个尝试
- 超时自动退出
- 详细的错误信息

## 📄 License

MIT
