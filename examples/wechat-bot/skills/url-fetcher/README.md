# URL Fetcher Skill

> 基于Playwright的无头浏览器URL信息获取技能

## 🚀 快速开始

### 1. 安装依赖

```bash
cd skills/url-fetcher
npm install
```

### 2. 首次运行

```bash
# 安装Playwright浏览器（首次必须）
npx playwright install chromium

# 测试运行
npm test
```

## 📖 使用示例

### 基本用法

```bash
# 获取网页内容
node url_fetcher.js --url "https://example.com"

# 获取内容并截图
node url_fetcher.js --url "https://example.com" --screenshot

# 提取元数据
node url_fetcher.js --url "https://example.com" --metadata

# 完整模式（全部功能）
node url_fetcher.js --url "https://example.com" --full
```

### 高级用法

```bash
# 自定义等待时间
node url_fetcher.js --url "https://example.com" --wait 5000

# 自定义User-Agent
node url_fetcher.js --url "https://example.com" \
  --user-agent "Mozilla/5.0 (iPhone; CPU iPhone OS 14_0 like Mac OS X) AppleWebKit/605.1.15"

# 设置自定义请求头
node url_fetcher.js --url "https://example.com" \
  --headers '{"Authorization":"Bearer your-token-here"}'

# 使用代理
node url_fetcher.js --url "https://example.com" \
  --proxy "http://proxy.example.com:8080"

# 设置超时时间
node url_fetcher.js --url "https://example.com" --timeout 60000
```

## 📊 输出示例

```json
{
  "success": true,
  "url": "https://example.com",
  "title": "Example Domain",
  "statusCode": 200,
  "statusText": "OK",
  "content": {
    "html": "<!doctype html>...",
    "text": "Example Domain..."
  },
  "metadata": {
    "title": "Example Domain",
    "description": "This domain is for use in illustrative examples...",
    "keywords": null
  },
  "screenshot": "/tmp/screenshot_1234567890.png",
  "timing": {
    "total": 2345,
    "navigation": 1234,
    "content": 1111
  }
}
```

## 🔧 配置说明

### 默认配置

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| 等待时间 | 2000ms | 页面加载后等待时间 |
| 超时时间 | 30000ms | 请求超时时间 |
| User-Agent | Chrome | 默认Chrome UA |
| 视口大小 | 1920x1080 | 浏览器视口尺寸 |

### 自定义配置

可以通过命令行参数覆盖默认配置，详见 `--help`。

## 🎯 应用场景

- ✅ 网页内容采集
- ✅ 动态渲染页面抓取
- ✅ 页面截图
- ✅ SEO元数据提取
- ✅ 网页监控
- ✅ 自动化测试

## ⚠️ 注意事项

1. **首次运行**: 需要先安装Playwright浏览器
2. **网络要求**: 需要稳定的网络连接
3. **资源消耗**: 无头浏览器会占用较多内存
4. **合法性**: 请遵守目标网站的使用条款

## 📝 开发说明

### 项目结构

```
url-fetcher/
├── skill.md           # 技能描述文件
├── package.json       # Node.js依赖配置
├── url_fetcher.js     # 主执行脚本
├── README.md          # 本文件
└── node_modules/      # 依赖目录（npm install后生成）
```

### 依赖说明

- **playwright**: ^1.40.0 - 无头浏览器自动化工具

### 扩展开发

可以通过修改 `url_fetcher.js` 来扩展功能：

1. 添加新的元数据提取规则
2. 自定义截图逻辑
3. 添加新的输出格式
4. 集成其他浏览器功能

## 📚 相关资源

- [Playwright文档](https://playwright.dev/)
- [Node.js文档](https://nodejs.org/)
- [skill.md规范](./skill.md)

## 📄 License

MIT

---

**创建时间**: 2026-04-21  
**版本**: 1.0.0  
**维护者**: AI Assistant
