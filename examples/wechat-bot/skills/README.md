# Skills 技能仓库

本目录包含所有已安装和创建的技能。

## 📂 目录结构

```
skills/
├── url-fetcher/          # URL信息获取技能
│   ├── SKILL.md          # 技能描述文件
│   ├── package.json      # Node.js依赖配置
│   ├── url_fetcher.js    # 主执行脚本
│   ├── README.md         # 使用说明
│   └── node_modules/     # 依赖目录
└── README.md             # 本文件
```

## 🎯 已安装技能

### 1. URL Fetcher (url-fetcher)

基于Playwright的无头浏览器URL信息获取工具。

**功能特点**:
- ✅ 获取网页完整HTML内容
- ✅ 提取纯文本内容
- ✅ 页面截图（全页面）
- ✅ 元数据提取（标题、描述、关键词等）
- ✅ 自定义User-Agent
- ✅ 自定义请求头
- ✅ 代理支持
- ✅ 超时控制

**快速使用**:
```bash
# 基本用法
node skills/url-fetcher/url_fetcher.js --url "https://example.com"

# 完整模式
node skills/url-fetcher/url_fetcher.js --url "https://example.com" --full

# 查看帮助
node skills/url-fetcher/url_fetcher.js --help
```

**测试状态**: ✅ 已通过测试
- 内容获取: ✅
- 元数据提取: ✅
- 截图功能: ✅
- JSON输出: ✅
- 文本输出: ✅

---

## 📝 技能开发规范

### 必需文件

每个技能应包含以下文件：

1. **SKILL.md** - 技能描述文件
   - 技能概述
   - 功能说明
   - 使用方法
   - 参数说明
   - 输出格式
   - 注意事项

2. **执行脚本** - 主程序
   - Python: `main.py` 或 `<skill_name>.py`
   - Node.js: `index.js` 或 `<skill_name>.js`
   - Shell: `run.sh`

3. **依赖配置**
   - Python: `requirements.txt`
   - Node.js: `package.json`
   - 其他: 安装说明文档

4. **README.md** - 使用说明
   - 快速开始
   - 使用示例
   - 配置说明
   - 开发说明

### 推荐结构

```
<skill-name>/
├── SKILL.md              # 必需: 技能描述
├── package.json          # Node.js项目必需
├── <skill_name>.js       # 必需: 主执行脚本
├── README.md             # 必需: 使用说明
├── lib/                  # 可选: 库文件
├── tests/                # 可选: 测试文件
└── examples/             # 可选: 示例文件
```

---

## 🔧 技能安装

### 从SkillHub安装

```bash
# 方法1: 使用tagent命令（如果有）
tagent skill install <skill-name>

# 方法2: 手动克隆
cd skills/
git clone <skill-repo-url>
cd <skill-name>
npm install  # Node.js技能
# 或
pip install -r requirements.txt  # Python技能
```

### 创建新技能

```bash
# 1. 创建技能目录
mkdir -p skills/<skill-name>
cd skills/<skill-name>

# 2. 创建必要文件
touch SKILL.md package.json <skill-name>.js README.md

# 3. 编写代码
# ...

# 4. 安装依赖
npm install

# 5. 测试
node <skill-name>.js --help
```

---

## 📚 参考资料

- [Playwright官方文档](https://playwright.dev/)
- [Node.js官方文档](https://nodejs.org/)
- [SkillHub社区](https://skillhub.tencent.com/)

---

**最后更新**: 2026-04-21  
**维护者**: AI Assistant

---

## 📂 新增技能（2026-07-15 增补）

### 6. Path Map (path-map)
项目路径拓扑与访问规范（**本 skill 未随仓库分发，此节仅存档说明**）：
- 主知识库唯一位置：**ima 云端知识库**（经 `skills/ima/` 访问，KBID 见 `~/.config/ima/wiki_kbid`；2026-09-14 起本地零沉淀——拆条即上传，临时件即删，删除前 tar 备份）
- skill 真实位置：`examples/wechat-bot/skills/`（**非 repo 根**）
- 详见 `path-map/README.md`（如存在）

### 7. Knowledge Base Manager（已退役，并入 ima）
知识库管理已收敛为**云端单源**模式（2026-09-10 用户拍板，2026-09-14 完成本地清零）：
- SSoT = ima 云端知识库（54+ 条），操作入口 = `skills/ima/SKILL.md`
- 旧的本地目录 SOP（抓取→归档→关联→评估）已废弃；原 knowledge-base-manager/ 目录不存在

---

