#!/usr/bin/env node

/**
 * URL Fetcher Skill - 基于Playwright的URL信息获取工具
 * 
 * 功能:
 * - 获取网页完整HTML内容
 * - 提取纯文本内容
 * - 页面截图
 * - 元数据提取
 * - 自定义请求头和User-Agent
 */

const { chromium } = require('playwright');
const path = require('path');
const fs = require('fs');

// 解析命令行参数
function parseArgs() {
  const args = process.argv.slice(2);
  const params = {
    url: null,
    screenshot: false,
    metadata: false,
    wait: 2000,
    userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
    headers: {},
    proxy: null,
    timeout: 30000,
    full: false,
    output: 'json'
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
      case '--metadata':
        params.metadata = true;
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
          console.error('Invalid headers JSON format');
        }
        break;
      case '--proxy':
        params.proxy = args[++i];
        break;
      case '--timeout':
        params.timeout = parseInt(args[++i]);
        break;
      case '--full':
        params.full = true;
        params.screenshot = true;
        params.metadata = true;
        break;
      case '--output':
        params.output = args[++i];
        break;
      case '--help':
      case '-h':
        console.log(`
URL Fetcher Skill - 基于Playwright的URL信息获取工具

用法: node url_fetcher.js --url <URL> [选项]

必需参数:
  --url <URL>              要获取的URL地址

可选参数:
  --screenshot             获取页面截图
  --metadata               提取页面元数据
  --wait <ms>              等待时间，默认2000ms
  --user-agent <string>    自定义User-Agent
  --headers <json>         自定义请求头，JSON格式
  --proxy <url>            代理服务器地址
  --timeout <ms>           超时时间，默认30000ms
  --full                   完整模式（内容+截图+元数据）
  --output <format>        输出格式: json/text，默认json
  --help, -h               显示帮助信息

示例:
  # 基本用法
  node url_fetcher.js --url "https://example.com"

  # 完整模式
  node url_fetcher.js --url "https://example.com" --full

  # 自定义User-Agent
  node url_fetcher.js --url "https://example.com" --user-agent "Mozilla/5.0..."

  # 设置请求头
  node url_fetcher.js --url "https://example.com" --headers '{"Authorization":"Bearer token"}'
`);
        process.exit(0);
    }
  }

  return params;
}

// 提取页面元数据
async function extractMetadata(page) {
  return await page.evaluate(() => {
    const getMetaContent = (name) => {
      const meta = document.querySelector(`meta[name="${name}"], meta[property="${name}"]`);
      return meta ? meta.getAttribute('content') : null;
    };

    return {
      title: document.title,
      description: getMetaContent('description'),
      keywords: getMetaContent('keywords'),
      author: getMetaContent('author'),
      'og:title': getMetaContent('og:title'),
      'og:description': getMetaContent('og:description'),
      'og:image': getMetaContent('og:image'),
      'og:url': getMetaContent('og:url'),
      'twitter:card': getMetaContent('twitter:card'),
      'twitter:title': getMetaContent('twitter:title'),
      'twitter:description': getMetaContent('twitter:description'),
      'twitter:image': getMetaContent('twitter:image'),
      canonical: document.querySelector('link[rel="canonical"]')?.href,
      favicon: document.querySelector('link[rel="icon"]')?.href || 
               document.querySelector('link[rel="shortcut icon"]')?.href
    };
  });
}

// 主函数
async function fetchUrl(params) {
  if (!params.url) {
    console.error('❌ 错误: 必须提供 --url 参数');
    console.log('使用 --help 查看帮助信息');
    process.exit(1);
  }

  console.error(`🔄 正在获取: ${params.url}`);
  
  const startTime = Date.now();
  let browser = null;
  const result = {
    success: false,
    url: params.url,
    timestamp: new Date().toISOString()
  };

  try {
    // 启动浏览器
    const browserOptions = {
      headless: true,
      args: [
        '--no-sandbox',
        '--disable-setuid-sandbox',
        '--disable-dev-shm-usage',
        '--disable-accelerated-2d-canvas',
        '--disable-gpu'
      ]
    };

    if (params.proxy) {
      browserOptions.proxy = {
        server: params.proxy
      };
    }

    browser = await chromium.launch(browserOptions);
    
    // 创建页面
    const context = await browser.newContext({
      userAgent: params.userAgent,
      viewport: { width: 1920, height: 1080 },
      extraHTTPHeaders: params.headers
    });

    const page = await context.newPage();
    page.setDefaultTimeout(params.timeout);

    // 导航到URL
    const navigationStart = Date.now();
    const response = await page.goto(params.url, {
      waitUntil: 'networkidle',
      timeout: params.timeout
    });

    const navigationTime = Date.now() - navigationStart;
    result.statusCode = response.status();
    result.statusText = response.statusText();

    // 等待额外时间确保内容完全加载
    if (params.wait > 0) {
      await page.waitForTimeout(params.wait);
    }

    // 获取标题
    result.title = await page.title();
    console.error(`📄 页面标题: ${result.title}`);

    // 获取HTML内容
    result.content = {
      html: await page.content()
    };

    // 获取纯文本
    result.content.text = await page.evaluate(() => {
      return document.body.innerText;
    });

    // 提取元数据
    if (params.metadata) {
      result.metadata = await extractMetadata(page);
      console.error(`📊 已提取元数据`);
    }

    // 截图
    if (params.screenshot) {
      const screenshotPath = `/tmp/screenshot_${Date.now()}.png`;
      await page.screenshot({
        path: screenshotPath,
        fullPage: true
      });
      result.screenshot = screenshotPath;
      console.error(`📸 截图已保存: ${screenshotPath}`);
    }

    // 计算时间
    const totalTime = Date.now() - startTime;
    result.timing = {
      total: totalTime,
      navigation: navigationTime,
      content: totalTime - navigationTime
    };

    result.success = true;
    console.error(`✅ 完成! 总耗时: ${totalTime}ms`);

  } catch (error) {
    result.success = false;
    result.error = {
      message: error.message,
      name: error.name
    };
    console.error(`❌ 错误: ${error.message}`);
  } finally {
    // 关闭浏览器
    if (browser) {
      await browser.close();
    }
  }

  return result;
}

// 执行主函数
(async () => {
  const params = parseArgs();
  const result = await fetchUrl(params);

  // 输出结果
  if (params.output === 'json') {
    console.log(JSON.stringify(result, null, 2));
  } else {
    // 文本格式输出
    console.log('\n========== URL Fetcher Result ==========\n');
    console.log(`URL: ${result.url}`);
    console.log(`标题: ${result.title}`);
    console.log(`状态码: ${result.statusCode}`);
    console.log(`耗时: ${result.timing?.total}ms`);
    
    if (result.screenshot) {
      console.log(`截图: ${result.screenshot}`);
    }
    
    console.log('\n--- 内容预览 (前500字符) ---\n');
    console.log(result.content?.text?.substring(0, 500) + '...');
    
    if (result.metadata) {
      console.log('\n--- 元数据 ---\n');
      console.log(JSON.stringify(result.metadata, null, 2));
    }
  }

  process.exit(result.success ? 0 : 1);
})();
