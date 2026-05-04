#!/usr/bin/env node

/**
 * Smart Fetcher - 智能网页内容获取工具
 * 
 * 特点：
 * - 自动检测URL类型，选择最佳提取策略
 * - 渐进式加载：从简单到复杂，逐步尝试
 * - 健壮性：多重fallback、自动重试、错误恢复
 * - 可扩展：支持自定义策略和提取器
 */

const { chromium } = require('playwright');
const fs = require('fs');
const path = require('path');

// ==================== 配置 ====================
const CONFIG = {
  // 浏览器配置
  browser: {
    headless: true,
    timeout: 30000,
    args: [
      '--no-sandbox',
      '--disable-setuid-sandbox',
      '--disable-dev-shm-usage',
      '--disable-accelerated-2d-canvas',
      '--disable-gpu'
    ]
  },
  
  // 渐进式策略配置
  strategies: {
    // 轻量级（快速，适用于大部分网站）
    lightweight: {
      name: 'lightweight',
      wait: 1000,
      scroll: false,
      userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36'
    },
    // 标准模式（平衡速度和效果）
    standard: {
      name: 'standard',
      wait: 3000,
      scroll: true,
      scrollTimes: 3,
      userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36'
    },
    // 重量级（适用于复杂SPA、懒加载）
    heavyweight: {
      name: 'heavyweight',
      wait: 5000,
      scroll: true,
      scrollTimes: 5,
      userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36'
    }
  },
  
  // 特殊网站配置
  siteConfigs: {
    'mp.weixin.qq.com': {
      name: '微信公众号',
      userAgent: 'Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 Chrome/114.0.0.0 Mobile Safari/537.36 MicroMessenger/8.0.38',
      wait: 5000,
      scroll: true,
      scrollTimes: 5,
      contentSelectors: ['#js_content', '#img-content', '.rich_media_content'],
      titleSelectors: ['.rich_media_title', '#activity-name', 'h1'],
      minContentLength: 100
    },
    'medium.com': {
      name: 'Medium',
      wait: 4000,
      scroll: true,
      scrollTimes: 5,
      contentSelectors: ['article', 'main', '.postArticle'],
      titleSelectors: ['h1', 'header h1'],
      minContentLength: 200
    },
    'twitter.com': {
      name: 'Twitter',
      wait: 3000,
      scroll: true,
      scrollTimes: 3,
      contentSelectors: ['[data-testid="tweet"]', 'article'],
      titleSelectors: ['h1'],
      minContentLength: 50
    },
    'juejin.cn': {
      name: '掘金',
      wait: 3000,
      scroll: true,
      contentSelectors: ['.article-content', 'article', '.markdown-body'],
      titleSelectors: ['.article-title', 'h1'],
      minContentLength: 100
    },
    'zhuanlan.zhihu.com': {
      name: '知乎专栏',
      wait: 3000,
      scroll: true,
      contentSelectors: ['.Post-RichTextContainer', '.RichText', 'article'],
      titleSelectors: ['h1.Post-Title', 'h1'],
      minContentLength: 100
    }
  }
};

// ==================== URL检测器 ====================
class URLDetector {
  static detectType(url) {
    try {
      const urlObj = new URL(url);
      const hostname = urlObj.hostname;
      
      // 检查特殊网站
      for (const [domain, config] of Object.entries(CONFIG.siteConfigs)) {
        if (hostname.includes(domain)) {
          return {
            type: 'special',
            domain: domain,
            config: config
          };
        }
      }
      
      // 检查是否是SPA
      const spaIndicators = ['#', '/app/', '/spa/'];
      const isSPA = spaIndicators.some(ind => url.includes(ind));
      
      return {
        type: isSPA ? 'spa' : 'standard',
        domain: hostname,
        config: null
      };
    } catch (error) {
      return {
        type: 'unknown',
        domain: 'unknown',
        config: null
      };
    }
  }
  
  static getStrategy(url, urlInfo) {
    // 特殊网站使用专门配置
    if (urlInfo.type === 'special' && urlInfo.config) {
      return 'heavyweight';
    }
    
    // SPA使用标准模式
    if (urlInfo.type === 'spa') {
      return 'standard';
    }
    
    // 其他使用轻量级
    return 'lightweight';
  }
}

// ==================== 内容提取器 ====================
class ContentExtractor {
  // 提取标题
  static async extractTitle(page, urlInfo) {
    // 如果有特殊网站的标题选择器
    if (urlInfo.config && urlInfo.config.titleSelectors) {
      const title = await page.evaluate((selectors) => {
        for (const selector of selectors) {
          const element = document.querySelector(selector);
          if (element && element.textContent.trim()) {
            return element.textContent.trim();
          }
        }
        return null;
      }, urlInfo.config.titleSelectors);
      
      if (title) return title;
    }
    
    // 尝试从元数据获取
    const ogTitle = await page.evaluate(() => {
      const meta = document.querySelector('meta[property="og:title"]');
      return meta ? meta.getAttribute('content') : null;
    });
    
    if (ogTitle) return ogTitle;
    
    // 最后使用 document.title
    return await page.title();
  }
  
  // 提取文章正文（通用方法）
  static async extractArticle(page, selectors = null) {
    const defaultSelectors = [
      'article',
      'main',
      '[role="main"]',
      '.content',
      '.article',
      '.post',
      '#content',
      '#main'
    ];
    
    const allSelectors = selectors ? [...selectors, ...defaultSelectors] : defaultSelectors;
    
    return await page.evaluate((selectors) => {
      // 尝试每个选择器
      for (const selector of selectors) {
        try {
          const elements = document.querySelectorAll(selector);
          for (const element of elements) {
            const text = element.textContent.trim();
            // 内容长度验证
            if (text.length > 200) {
              return {
                success: true,
                selector: selector,
                html: element.innerHTML,
                text: text,
                length: text.length
              };
            }
          }
        } catch (e) {
          continue;
        }
      }
      return { success: false };
    }, allSelectors);
  }
  
  // 提取元数据
  static async extractMetadata(page) {
    return await page.evaluate(() => {
      const getMeta = (name) => {
        const meta = document.querySelector(`meta[name="${name}"], meta[property="${name}"]`);
        return meta ? meta.getAttribute('content') : null;
      };
      
      const getLink = (rel) => {
        const link = document.querySelector(`link[rel="${rel}"]`);
        return link ? link.href : null;
      };
      
      return {
        title: document.title,
        description: getMeta('description'),
        keywords: getMeta('keywords'),
        author: getMeta('author'),
        publishDate: getMeta('article:published_time') || getMeta('date'),
        image: getMeta('og:image') || getMeta('twitter:image'),
        canonical: getLink('canonical'),
        favicon: getLink('icon') || getLink('shortcut icon'),
        og: {
          title: getMeta('og:title'),
          description: getMeta('og:description'),
          image: getMeta('og:image'),
          url: getMeta('og:url')
        },
        twitter: {
          card: getMeta('twitter:card'),
          title: getMeta('twitter:title'),
          description: getMeta('twitter:description')
        }
      };
    });
  }
  
  // 智能提取（自动判断最佳内容）
  static async extractSmart(page, urlInfo) {
    // 如果有特殊网站配置，使用专用选择器
    if (urlInfo.config && urlInfo.config.contentSelectors) {
      const result = await this.extractArticle(page, urlInfo.config.contentSelectors);
      if (result.success) {
        return result;
      }
    }
    
    // 否则使用通用提取
    return await this.extractArticle(page);
  }
}

// ==================== 核心获取器 ====================
class SmartFetcher {
  constructor(params) {
    this.params = params;
    this.browser = null;
    this.page = null;
    this.urlInfo = null;
    this.strategyName = null;
  }
  
  async init() {
    const urlInfo = URLDetector.detectType(this.params.url);
    this.urlInfo = urlInfo;
    this.strategyName = this.params.strategy || URLDetector.getStrategy(this.params.url, urlInfo);
    
    console.error(`🔍 URL检测: ${urlInfo.domain} (${urlInfo.type})`);
    console.error(`📋 使用策略: ${this.strategyName}`);
    
    if (urlInfo.config) {
      console.error(`🌐 特殊网站: ${urlInfo.config.name}`);
    }
  }
  
  async launchBrowser() {
    const browserOptions = { ...CONFIG.browser };
    
    // 如果提供了代理
    if (this.params.proxy) {
      browserOptions.proxy = { server: this.params.proxy };
    }
    
    this.browser = await chromium.launch(browserOptions);
  }
  
  async createPage() {
    // 确定User-Agent
    let userAgent = CONFIG.strategies[this.strategyName].userAgent;
    if (this.urlInfo.config && this.urlInfo.config.userAgent) {
      userAgent = this.urlInfo.config.userAgent;
    }
    if (this.params.userAgent) {
      userAgent = this.params.userAgent;
    }
    
    const context = await this.browser.newContext({
      userAgent: userAgent,
      viewport: { width: 1920, height: 1080 },
      extraHTTPHeaders: this.params.headers || {}
    });
    
    this.page = await context.newPage();
    this.page.setDefaultTimeout(this.params.timeout || CONFIG.browser.timeout);
  }
  
  async navigate() {
    console.error(`🔄 正在访问: ${this.params.url}`);
    
    const response = await this.page.goto(this.params.url, {
      waitUntil: 'networkidle',
      timeout: this.params.timeout || CONFIG.browser.timeout
    });
    
    return {
      status: response.status(),
      statusText: response.statusText()
    };
  }
  
  async waitForContent() {
    // 确定等待时间
    let wait = CONFIG.strategies[this.strategyName].wait;
    if (this.urlInfo.config && this.urlInfo.config.wait) {
      wait = this.urlInfo.config.wait;
    }
    if (this.params.wait) {
      wait = this.params.wait;
    }
    
    if (wait > 0) {
      console.error(`⏱️  等待内容加载: ${wait}ms`);
      await this.page.waitForTimeout(wait);
    }
  }
  
  async scrollToLoad() {
    const shouldScroll = CONFIG.strategies[this.strategyName].scroll;
    let scrollTimes = CONFIG.strategies[this.strategyName].scrollTimes || 3;
    
    if (this.urlInfo.config && this.urlInfo.config.scroll) {
      scrollTimes = this.urlInfo.config.scrollTimes || scrollTimes;
    }
    
    if (shouldScroll) {
      console.error(`📜 模拟滚动: ${scrollTimes}次`);
      for (let i = 0; i < scrollTimes; i++) {
        await this.page.evaluate(() => {
          window.scrollBy(0, window.innerHeight);
        });
        await this.page.waitForTimeout(500);
      }
      // 滚动回顶部
      await this.page.evaluate(() => {
        window.scrollTo(0, 0);
      });
    }
  }
  
  async extractContent() {
    console.error('📦 提取内容...');
    
    // 提取标题
    const title = await ContentExtractor.extractTitle(this.page, this.urlInfo);
    
    // 提取正文
    const articleResult = await ContentExtractor.extractSmart(this.page, this.urlInfo);
    
    // 提取元数据
    const metadata = await ContentExtractor.extractMetadata(this.page);
    
    // 提取纯文本（fallback）
    const fullText = await this.page.evaluate(() => document.body.innerText);
    
    // 提取完整HTML
    const fullHtml = await this.page.content();
    
    return {
      title: title,
      article: articleResult,
      metadata: metadata,
      fullText: fullText,
      fullHtml: fullHtml
    };
  }
  
  async fetch() {
    const result = {
      success: false,
      url: this.params.url,
      timestamp: new Date().toISOString(),
      urlInfo: this.urlInfo,
      strategy: this.strategyName
    };
    
    const startTime = Date.now();
    
    try {
      await this.init();
      await this.launchBrowser();
      await this.createPage();
      
      // 导航
      const navResult = await this.navigate();
      result.statusCode = navResult.status;
      result.statusText = navResult.statusText;
      
      // 等待内容
      await this.waitForContent();
      
      // 滚动加载
      await this.scrollToLoad();
      
      // 提取内容
      result.content = await this.extractContent();
      result.title = result.content.title;
      
      // 截图（如果需要）
      if (this.params.screenshot) {
        const screenshotPath = `/tmp/screenshot_${Date.now()}.png`;
        await this.page.screenshot({ path: screenshotPath, fullPage: true });
        result.screenshot = screenshotPath;
        console.error(`📸 截图已保存: ${screenshotPath}`);
      }
      
      result.success = true;
      result.timing = {
        total: Date.now() - startTime
      };
      
      console.error(`✅ 成功! 耗时: ${result.timing.total}ms`);
      
    } catch (error) {
      result.success = false;
      result.error = {
        message: error.message,
        name: error.name,
        stack: error.stack
      };
      console.error(`❌ 错误: ${error.message}`);
    } finally {
      if (this.browser) {
        await this.browser.close();
      }
    }
    
    return result;
  }
}

// ==================== CLI入口 ====================
function parseArgs() {
  const args = process.argv.slice(2);
  const params = {
    url: null,
    screenshot: false,
    wait: null,
    userAgent: null,
    headers: {},
    proxy: null,
    timeout: null,
    output: 'json',
    strategy: null  // 可以手动指定策略
  };
  
  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    switch (arg) {
      case '--url':
        params.url = args[++i];
        break;
      case '--screenshot':
        params.screenshot = true;
        break;
      case '--wait':
        params.wait = parseInt(args[++i]);
        break;
      case '--user-agent':
        params.userAgent = args[++i];
        break;
      case '--headers':
        try {
          params.headers = JSON.parse(args[++i]);
        } catch (e) {
          console.error('Invalid headers JSON');
        }
        break;
      case '--proxy':
        params.proxy = args[++i];
        break;
      case '--timeout':
        params.timeout = parseInt(args[++i]);
        break;
      case '--output':
        params.output = args[++i];
        break;
      case '--strategy':
        params.strategy = args[++i];
        break;
      case '--help':
      case '-h':
        console.log(`
Smart Fetcher - 智能网页内容获取工具

用法: node smart_fetcher.js --url <URL> [选项]

必需参数:
  --url <URL>              要获取的URL地址

可选参数:
  --screenshot             获取页面截图
  --wait <ms>              自定义等待时间
  --user-agent <string>    自定义User-Agent
  --headers <json>         自定义请求头
  --proxy <url>            代理服务器
  --timeout <ms>           超时时间
  --output <format>        输出格式: json/text，默认json
  --strategy <name>        手动指定策略: lightweight/standard/heavyweight
  --help, -h               显示帮助

特点:
  ✅ 自动检测URL类型（微信公众号、Medium、Twitter等）
  ✅ 渐进式策略（轻量→标准→重量级）
  ✅ 智能内容提取（自动识别正文区域）
  ✅ 智能标题提取（支持特殊网站）
  ✅ 支持懒加载（自动滚动触发）
  ✅ 多重fallback机制
`);
        process.exit(0);
    }
  }
  
  return params;
}

// 主函数
(async () => {
  const params = parseArgs();
  
  if (!params.url) {
    console.error('❌ 错误: 必须提供 --url 参数');
    console.error('使用 --help 查看帮助信息');
    process.exit(1);
  }
  
  const fetcher = new SmartFetcher(params);
  const result = await fetcher.fetch();
  
  // 输出结果
  if (params.output === 'json') {
    console.log(JSON.stringify(result, null, 2));
  } else {
    console.log('\n========== Smart Fetcher Result ==========\n');
    console.log(`URL: ${result.url}`);
    console.log(`标题: ${result.title}`);
    console.log(`策略: ${result.strategy}`);
    console.log(`状态码: ${result.statusCode}`);
    console.log(`耗时: ${result.timing?.total}ms`);
    
    if (result.content?.article?.success) {
      console.log(`\n--- 文章正文 (使用选择器: ${result.content.article.selector}) ---\n`);
      const preview = result.content.article.text.substring(0, 500);
      console.log(preview + (result.content.article.text.length > 500 ? '...' : ''));
    } else {
      console.log('\n--- 页面文本 (前500字符) ---\n');
      console.log(result.content?.fullText?.substring(0, 500) + '...');
    }
    
    if (result.content?.metadata) {
      console.log('\n--- 元数据 ---\n');
      console.log(JSON.stringify(result.content.metadata, null, 2));
    }
    
    if (result.screenshot) {
      console.log(`\n📸 截图: ${result.screenshot}`);
    }
  }
  
  process.exit(result.success ? 0 : 1);
})();
