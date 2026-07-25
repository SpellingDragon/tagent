const { chromium } = require('playwright');

(async () => {
  const browser = await chromium.launch({ 
    headless: true,
    args: ['--no-sandbox', '--disable-setuid-sandbox']
  });
  
  const context = await browser.newContext({
    userAgent: 'Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Mobile Safari/537.36 MicroMessenger/8.0.38.2400(0x28002658) Process/tools WeChat/arm64 Weixin NetType/WIFI Language/zh_CN ABI/arm64'
  });
  
  const page = await context.newPage();
  
  console.log('正在打开页面...');
  await page.goto('https://mp.weixin.qq.com/s/fNBFiRr5RYsxywq_sB3XlQ', {
    waitUntil: 'networkidle',
    timeout: 30000
  });
  
  console.log('等待内容加载...');
  await page.waitForTimeout(5000);
  
  // 模拟滚动以触发懒加载
  console.log('模拟滚动...');
  for (let i = 0; i < 5; i++) {
    await page.evaluate(() => {
      window.scrollBy(0, window.innerHeight);
    });
    await page.waitForTimeout(1000);
  }
  
  // 获取文章标题
  const title = await page.title();
  console.log('文章标题:', title);
  
  // 尝试获取文章正文
  const content = await page.evaluate(() => {
    // 微信文章的正文通常在 js_content 这个 div 里
    const contentDiv = document.querySelector('#js_content');
    if (contentDiv) {
      return {
        found: true,
        html: contentDiv.innerHTML,
        text: contentDiv.innerText
      };
    }
    return { found: false };
  });
  
  if (content.found) {
    console.log('\n=== 文章正文 ===\n');
    console.log(content.text.substring(0, 2000)); // 只打印前2000字符
    console.log('\n... (更多内容已省略)');
    
    // 保存完整内容到文件
    const fs = require('fs');
    fs.writeFileSync('/tmp/wechat_article.txt', content.text);
    fs.writeFileSync('/tmp/wechat_article.html', content.html);
    console.log('\n完整内容已保存到 /tmp/wechat_article.txt 和 /tmp/wechat_article.html');
  } else {
    console.log('未能找到文章正文内容');
    // 截图帮助调试
    await page.screenshot({ path: '/tmp/wechat_debug.png', fullPage: true });
    console.log('已保存调试截图到 /tmp/wechat_debug.png');
  }
  
  await browser.close();
})();
