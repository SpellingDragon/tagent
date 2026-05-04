---
name: url-fetcher
description: >
  使用无头浏览器(Playwright)获取网页内容。支持动态页面渲染、JS执行、页面截图、元数据提取。
  适用于：网页内容采集、URL抓取、动态页面获取、截图、网页监控、数据提取。
---

# URL信息获取技能 (URL Fetcher Skill)

## 🎯 技能概述

这是一个基于Playwright的通用URL信息获取技能，能够启动无头浏览器获取网页的完整内容、截图、元数据等信息。

## 📋 技能信息

- **技能名称**: url-fetcher
- **版本**: 1.0.0
- **类型**: 数据采集 / 网页自动化
- **运行环境**: Node.js 18+
- **依赖**: Playwright

## 🔧 核心功能

### 1. 网页内容获取
- 获取完整的HTML内容
- 获取纯文本内容
- 支持动态渲染页面
- 支持JavaScript执行后的内容

### 2. 页面截图
- 全页面截图
- 可视区域截图
- 自定义尺寸截图

### 3. 元数据提取
- 页面标题
- 页面描述
- 关键词
- Open Graph标签
- 自定义元数据

### 4. 高级功能
- 自定义请求头
- Cookie支持
- 代理支持
- 等待策略配置
- 超时控制

## 📖 使用方法

### 基本用法

```bash
# 安装依赖
cd skills/url-fetcher
npm install

# 获取网页内容
node url_fetcher.js --url "https://example.com"

# 获取网页内容并截图
node url_fetcher.js --url "https://example.com" --screenshot

# 获取元数据
node url_fetcher.js --url "https://example.com" --metadata

# 自定义等待时间
node url_fetcher.js --url "https://example.com" --wait 5000
```

### 高级用法

```bash
# 使用自定义User-Agent
node url_fetcher.js --url "https://example.com" --user-agent "Mozilla/5.0..."

# 设置自定义请求头
node url_fetcher.js --url "https://example.com" --headers '{"Authorization": "Bearer token"}'

# 完整模式（内容+截图+元数据）
node url_fetcher.js --url "https://example.com" --full

# 使用代理
node url_fetcher.js --url "https://example.com" --proxy "http://proxy:8080"
```

## 📥 输入参数

| 参数 | 类型 | 必需 | 默认值 | 说明 |
|-----|------|------|--------|------|
| --url | string | ✅ | - | 要获取的URL地址 |
| --screenshot | boolean | ❌ | false | 是否截图 |
| --metadata | boolean | ❌ | false | 是否提取元数据 |
| --wait | number | ❌ | 2000 | 等待时间（毫秒） |
| --user-agent | string | ❌ | Chrome | 自定义User-Agent |
| --headers | json | ❌ | {} | 自定义请求头 |
| --proxy | string | ❌ | - | 代理服务器地址 |
| --timeout | number | ❌ | 30000 | 超时时间（毫秒） |
| --full | boolean | ❌ | false | 完整模式（全部功能） |

## 📤 输出结果

### JSON格式输出

```json
{
  "success": true,
  "url": "https://example.com",
  "title": "Page Title",
  "metadata": {
    "description": "Page description",
    "keywords": "keyword1, keyword2",
    "og:title": "Open Graph Title",
    "og:image": "https://example.com/image.jpg"
  },
  "content": {
    "html": "<html>...</html>",
    "text": "Plain text content..."
  },
  "screenshot": "/tmp/screenshot_xxx.png",
  "timing": {
    "total": 2345,
    "navigation": 1234,
    "content": 1111
  }
}
```

## 🛠️ 技术实现

### 核心依赖
- **playwright**: 无头浏览器自动化
- **chromium**: 浏览器引擎

### 关键技术点
1. **无头浏览器**: 使用Chromium无头模式
2. **等待策略**: 等待网络空闲确保内容完整加载
3. **元数据提取**: 通过DOM API提取meta标签
4. **截图功能**: 使用Playwright截图API

### 执行流程
```
启动浏览器 → 设置参数 → 导航到URL → 等待加载 → 提取内容 → 截图 → 关闭浏览器 → 返回结果
```

## ⚙️ 环境要求

### 系统要求
- Node.js 18.0 或更高版本
- npm 或 yarn
- 系统内存: 建议 512MB+

### 依赖安装

```bash
# 安装Node.js依赖
npm install

# 安装Playwright浏览器（首次运行时自动安装）
npx playwright install chromium
```

## ⚠️ 注意事项

### 使用限制
1. **频率控制**: 避免频繁请求同一域名
2. **资源消耗**: 无头浏览器占用较多内存
3. **网络依赖**: 需要稳定的网络连接
4. **合法性**: 遵守目标网站的robots.txt和使用条款

### 错误处理
- 网络超时: 自动重试机制
- 页面加载失败: 返回详细错误信息
- 截图失败: 继续执行其他功能

## 🚀 性能优化

### 启动速度优化
- 复用浏览器实例
- 禁用不必要的功能
- 使用轻量级配置

### 内存优化
- 及时关闭浏览器
- 清理缓存和cookies
- 限制并发实例数

## 📊 应用场景

### 1. 内容采集
- 网页内容抓取
- 动态页面数据提取
- SEO内容分析

### 2. 网页监控
- 页面变化检测
- 内容更新通知
- 可用性监控

### 3. 自动化测试
- 页面渲染测试
- 功能回归测试
- 性能基准测试

### 4. 数据分析
- 网页结构分析
- 元数据提取
- 内容挖掘

## 🔄 版本历史

- **v1.0.0** (2026-04-21): 初始版本
  - 基础URL获取功能
  - 页面截图功能
  - 元数据提取功能
  - 自定义配置支持

## 📚 参考资料

- Playwright官方文档: https://playwright.dev/
- Node.js官方文档: https://nodejs.org/
- Chromium项目: https://www.chromium.org/

---

**创建时间**: 2026年4月21日  
**维护者**: AI Assistant  
**技能类型**: 数据采集 / 网页自动化 / 无头浏览器
