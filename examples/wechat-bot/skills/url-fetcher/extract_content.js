const { chromium } = require('playwright');

(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage();
  
  await page.goto('https://mp.weixin.qq.com/s/hZgFoLFnAq6-yqd4i1v8ng', {
    waitUntil: 'networkidle'
  });
  
  // 等待内容加载
  await page.waitForTimeout(5000);
  
  // 提取文章内容
  const content = await page.evaluate(() => {
    // 尝试多种选择器
    const selectors = ['#js_content', '#img-content', '.rich_media_content', '.article-content'];
    
    for (const selector of selectors) {
      const element = document.querySelector(selector);
      if (element && element.textContent.trim().length > 100) {
        return element.textContent.trim();
      }
    }
    
    return null;
  });
  
  if (content) {
    console.log(content);
  } else {
    console.log('未能提取到文章内容');
  }
  
  await browser.close();
})();
