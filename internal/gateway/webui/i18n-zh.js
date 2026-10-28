/* i18n-zh.js — 中文界面层（Zcode2Api 面板汉化）
 *
 * 设计原则：不改动上游 HTML/JS 里的英文原文，只在本层做「运行时替换」。
 *  - 静态文本：按整串精确匹配词典
 *  - 动态文本：按正则规则替换（"Added 3 account(s)" 之类）
 *  - 动态插入的节点：MutationObserver 兜住，异步渲染/轮询刷新出来的内容也会被翻译
 *  - 切换器：右下角浮层按钮 中文/EN，选择记在 localStorage，切换后重载页面
 *  - 一次性翻译：原文保留在 DOM 中不被破坏，切回英文靠 reload，不做原地回滚
 *
 * 用法：在每页 app.js 之后引入 <script src="/i18n-zh.js?v=1"></script>
 */
(function () {
  'use strict';

  var LS_KEY = 'zcode_ui_lang';
  var saved = null;
  try { saved = localStorage.getItem(LS_KEY); } catch (e) { /* 隐私模式 */ }
  // URL 覆盖：?lang=zh / ?lang=en（同时写回 localStorage，方便分享/排查）
  var forced = null;
  try { forced = new URLSearchParams(location.search).get('lang'); } catch (e) { /* 老浏览器 */ }
  if (forced === 'zh' || forced === 'en') {
    saved = forced;
    try { localStorage.setItem(LS_KEY, forced); } catch (e) { /* 忽略 */ }
  }
  var wantZh = saved ? saved === 'zh' : /^zh/i.test(navigator.language || '');

  // ── 词典：英文原文 → 中文 ──────────────────────────────────────────────
  var DICT = {
    // 通用 / 导航 / 页头
    'Zcode2Api · Account Pool': 'Zcode2Api · 账号池',
    'Zcode2Api · Proxies': 'Zcode2Api · 代理',
    'Zcode2Api · Settings': 'Zcode2Api · 设置',
    'Zcode2Api · Login': 'Zcode2Api · 登录',
    'Account Pool': '账号池',
    'Proxies': '代理线路',
    'Settings': '设置',
    'GitHub': 'GitHub 仓库',
    'Telegram': 'Telegram',
    'Log out': '退出登录',
    'Cancel': '取消',
    'Save': '保存',
    'Add': '添加',
    'Copy': '复制',
    'Open': '打开',
    'Continue': '继续',
    'Import': '导入',
    'Export': '导出',
    'Name': '名称',
    'Token': '令牌',
    'Tokens': '令牌',
    'Status': '状态',
    'Strikes': '风控计数',
    'Quota': '额度',
    'Actions': '操作',

    // 账号池页面
    'Account overview': '账号总览',
    'Account details': '账号明细',
    'Total accounts': '账号总数',
    'Healthy': '健康',
    'Quota exhausted': '额度耗尽',
    'Total quota (remaining)': '总剩余额度',
    'Credential / account': '凭证 / 账号',
    'Claim all': '全部领取',
    'Refresh quotas': '刷新额度',
    'Claim all: preview billing and claim every plan the origin offers, per account': '全部领取：先预览账单，再逐个账号领取上游给出的所有套餐',
    'Account Pool': '账号池',
    'Multi-account rotation · automatic failover on quota · device-warmed identities': '多账号轮转 · 额度耗尽自动切换 · 设备身份预热',
    'loading…': '加载中…',
    'No accounts in this view': '当前筛选下没有账号',
    'No plan data': '无套餐数据',
    'no plan': '无套餐',
    'Credential / account': '凭证 / 账号',
    '3 strikes (final 3012) → 30m cooldown; a clean 200 resets': '累计 3 次风控（3012 终判）→ 冷却 30 分钟；一次正常 200 即清零',
    'Refresh quota': '刷新额度',
    'Claim every offered plan': '领取所有可用套餐',
    'Edit: name, token, egress line, disabled models': '编辑：名称、令牌、出口线路、禁用模型',
    'Delete account': '删除账号',
    'Enable / disable': '启用 / 停用',
    'All': '全部',
    'Active': '在用',
    'Standby': '待用',
    'Cooling': '冷却中',
    'Exhausted': '已耗尽',
    'Revoked': '已吊销',
    'Expired': '已过期',
    'Disabled': '已停用',

    // 添加 / 编辑账号弹窗
    'Add account': '添加账号',
    'Edit account': '编辑账号',
    'Sign in via OAuth or paste credentials, one per line.': '可用 OAuth 登录，也可直接粘贴凭证（一行一个）。',
    'Change the name, credentials, egress line or disable specific models.': '可修改名称、凭证、出口线路，或禁用指定模型。',
    'OAuth login': 'OAuth 登录',
    'Paste credentials': '粘贴凭证',
    'Z.AI only allows the official ZCode callback URL, so you need to copy the browser URL once after the login completes; no username or password is required.':
      'Z.AI 只允许官方 ZCode 的回调地址，因此登录完成后需要手动复制一次浏览器地址栏 URL；全程不需要用户名和密码。',
    'Start login': '开始登录',
    '1. Open the authorization page and complete the Z.AI login:': '1. 打开授权页面并完成 Z.AI 登录：',
    '● Waiting for authorization…': '● 等待授权中…',
    'the server polls automatically; on success the JWT lands in the pool and gets warmed.':
      '服务端会自动轮询；授权成功后 JWT 会进入账号池并自动预热。',
    'One Coding Plan JWT per line — existing ones are skipped automatically':
      '每行一个 Coding Plan JWT —— 已存在的会自动跳过',
    'Egress line': '出口线路',
    'Pool — random from all lines': '线路池 —— 从全部线路中随机',
    'Direct (no proxy)': '直连（不使用代理）',
    'Bulk from file is served by env (ZCODE_KEYS=tokens.txt) at startup — here paste is for quick additions.':
      '批量导入请用环境变量 ZCODE_KEYS=tokens.txt 在启动时注入 —— 这里的粘贴框用于临时补充。',
    'Leave empty to keep unchanged': '留空则不修改',
    'Disabled models': '禁用的模型',
    'Once checked, this account is excluded from dispatching for that model.': '勾选后，该账号不参与对应模型的调度。',

    // 设置页
    'Runtime configuration — applied without a restart': '运行时配置 —— 保存即生效，无需重启',
    'Authentication': '认证',
    'Admin password': '管理口令',
    'leave empty to keep unchanged': '留空则不修改',
    'Used to log in to this admin panel. Applied immediately.': '用于登录本管理面板，保存后立即生效。',
    'Gateway API key (optional)': '网关 API Key（可选）',
    'leave empty to use the admin password': '留空则沿用管理口令',
    'Model & quotas': '模型与额度',
    'Quota refresh interval (minutes)': '额度刷新间隔（分钟）',
    '0 disables the periodic monitor (button-only mode).': '填 0 关闭定时刷新（只保留手动按钮）。',
    'Captcha pre-mint pool': '验证码预生成池',
    'Pool min / max': '池下限 / 上限',
    'Background solver keeps MIN ready tokens (≤ MAX, serial minting). Applies after restart.':
      '后台求解器维持至少下限个可用令牌（不超过上限，串行生成）。重启后生效。',
    'Token TTL': '令牌有效期',
    'Upstream params live ~2 minutes; expired tokens are evicted.': '上游参数有效期约 2 分钟，过期令牌会被淘汰。',
    'Usage notes': '接入说明',
    '• Anthropic:': '• Anthropic：',
    '• OpenAI:': '• OpenAI：',
    '• Async (ticket-SSE):': '• 异步（ticket-SSE）：',
    '• Both chat endpoints accept Bearer auth with the admin password.': '• 两个对话接口都支持用管理口令做 Bearer 鉴权。',
    // 被内联 <span>/<b>/<code> 截断的句子，需要按片段翻译
    'When set, calls to /v1/messages must carry Authorization: Bearer <key> or x-api-key. Currently:':
      '设置后，调用 /v1/messages 必须携带 Authorization: Bearer <key> 或 x-api-key。当前状态：',
    'Lines are just egress addresses. What to use — a specific line or': '这里只登记出口地址。具体用哪条线，还是用',
    '— is chosen per account in Account Pool → Edit → Egress line. Two failed probes auto-pause a line.':
      '，在「账号池 → 编辑 → 出口线路」里按账号选择。连续 2 次探测失败会自动暂停该线路。',
    'Pool (random from all)': '线路池（全随机）',
    'URL': '地址',
    'Admin Panel': '管理面板',
    'Enter the admin password to continue': '输入管理口令以继续',
    'Invalid password': '口令错误',

    // 代理页
    'Proxy Settings': '代理设置',
    'Centralized management of HTTP / SOCKS5 egress lines · bind per account or rotate in the pool':
      '集中管理 HTTP / SOCKS5 出口线路 · 可按账号绑定，也可在线路池中轮转',
    'Test current line': '测试当前线路',
    'Import lines': '导入线路',
    'Add proxy': '添加代理',
    'Current server egress': '当前服务器出口',
    'Not tested yet': '尚未测试',
    'Public IP via ip.sb → ipwho.is → ipapi.is fallback chain': '公网 IP 依次尝试 ip.sb → ipwho.is → ipapi.is',
    'Proxy lines': '代理线路',
    'Clear all': '清空全部',
    'Create here, then select per account in Account Pool (Egress line)': '先在这里创建线路，再到「账号池」里按账号选择出口线路',
    'No proxy lines yet': '还没有代理线路',
    'Connection format': '线路格式',
    'Proxies with authentication are supported': '支持带认证的代理',
    'Lines are just egress addresses. What to use — a specific line or Pool (random from all) is chosen per account in Account Pool → Edit → Egress line. Two failed probes auto-pause a line.':
      '这里只登记出口地址。具体用哪条线、还是用「线路池（全随机）」，在「账号池 → 编辑 → 出口线路」里按账号选择。连续 2 次探测失败会自动暂停该线路。',
    'HTTP, HTTPS, SOCKS4/5 — optionally with user:pass auth': 'HTTP、HTTPS、SOCKS4/5 —— 可选 user:pass 认证',
    'main-dc (optional)': '主用线路（可选）',
    'Bind to account': '绑定到账号',
    'Pool (all accounts)': '线路池（所有账号）',
    'Line vs Pool is chosen per account in Account Pool → Edit → Egress line.':
      '「单条线路」还是「线路池」是在「账号池 → 编辑 → 出口线路」里按账号决定的。',
    'Import proxy lines': '导入代理线路',
    'One line per row — the same format as proxy.txt': '一行一条 —— 与 proxy.txt 格式一致',
    'Duplicates are skipped. New lines start unbound (pool).': '重复项自动跳过。新增线路默认不绑定（走线路池）。',
    'Probe: public IP + latency': '探测：公网 IP 与延迟',
    'Delete line': '删除线路',
    'line': '线路',
    'paused': '已暂停',
    'Show first ': '只显示前 ',
    'Show all ': '显示全部 ',
    ' lines': ' 条线路'
  };

  // ── 动态文本规则：[正则, 替换] ─────────────────────────────────────────
  var RULES = [
    [/^Updated at (\d{2}:\d{2}:\d{2})$/, '更新于 $1'],
    [/^account #(\d+)$/, '账号 #$1'],
    [/^until (\d{2}\/\d{2} \d{2}:\d{2})$/, '至 $1'],
    [/^(\d+) \/ 3$/, '$1 / 3'],
    [/^Added (\d+) account\(s\)$/, '已添加 $1 个账号'],
    [/^Added (\d+)$/, '已添加 $1 个'],
    [/^Imported (\d+), skipped (\d+)$/, '导入 $1 个，跳过 $2 个'],
    [/^Imported (\d+) line\(s\)$/, '已导入 $1 条线路'],
    [/^(\d+) lines$/, '$1 条线路'],
    [/^Account added: (.*)$/, '已添加账号：$1'],
    [/^Failed: (.*)$/, '失败：$1'],
    [/^Failed to load accounts: (.*)$/, '加载账号失败：$1'],
    [/^Import failed: (.*)$/, '导入失败：$1'],
    [/^Save failed: (.*)$/, '保存失败：$1'],
    [/^Probe failed: (.*)$/, '探测失败：$1'],
    [/^Server returned non-JSON from (.*)$/, '服务端返回了非 JSON 内容：$1'],
    [/^IP: (.*) · (\d+)ms$/, 'IP：$1 · $2ms'],
    [/^Delete all (\d+) proxy lines\?$/, '确定删除全部 $1 条代理线路？'],
    [/^No plan data$/, '无套餐数据']
  ];

  // ── 不翻译的容器（纯数据/代码/URL）────────────────────────────────────
  var SKIP_TAGS = { SCRIPT: 1, STYLE: 1, CODE: 1, TEXTAREA: 1, PRE: 1, SVG: 1 };
  var ATTRS = ['title', 'placeholder', 'aria-label'];

  function lookup(text) {
    var t = text.trim();
    if (!t) return null;
    if (Object.prototype.hasOwnProperty.call(DICT, t)) return DICT[t];
    for (var i = 0; i < RULES.length; i++) {
      if (RULES[i][0].test(t)) return t.replace(RULES[i][0], RULES[i][1]);
    }
    return null;
  }

  function skipped(node) {
    var p = node.parentNode;
    while (p && p.nodeType === 1) {
      if (SKIP_TAGS[p.tagName]) return true;
      if (p.hasAttribute && p.hasAttribute('data-no-i18n')) return true;
      p = p.parentNode;
    }
    return false;
  }

  function translateText(node) {
    if (!node || node.nodeType !== 3 || seen.has(node)) return;
    if (skipped(node)) return;
    var zh = lookup(node.nodeValue);
    seen.add(node);
    if (zh === null) return;
    var m = /^(\s*)([\s\S]*?)(\s*)$/.exec(node.nodeValue);
    node.nodeValue = m[1] + zh + m[3];
  }

  function translateAttrs(el) {
    if (!el || el.nodeType !== 1) return;
    for (var i = 0; i < ATTRS.length; i++) {
      var a = ATTRS[i];
      if (!el.hasAttribute || !el.hasAttribute(a)) continue;
      if (seen.has(el)) continue;
      var zh = lookup(el.getAttribute(a));
      if (zh !== null) el.setAttribute(a, zh);
    }
    seen.add(el);
  }

  function walk(root) {
    if (!root) return;
    if (root.nodeType === 3) { translateText(root); return; }
    if (root.nodeType === 1) translateAttrs(root);
    if (!root.querySelectorAll && root.nodeType !== 1) return;
    var els = [];
    if (root.nodeType === 1) els.push(root);
    var list = root.querySelectorAll ? root.querySelectorAll('*') : [];
    for (var i = 0; i < list.length; i++) els.push(list[i]);
    for (var j = 0; j < els.length; j++) {
      translateAttrs(els[j]);
      var kids = els[j].childNodes || [];
      for (var k = 0; k < kids.length; k++) {
        if (kids[k].nodeType === 3) translateText(kids[k]);
      }
    }
  }

  var seen = new WeakSet();
  var observer = null;

  function startObserver() {
    if (observer || !window.MutationObserver) return;
    observer = new MutationObserver(function (records) {
      for (var i = 0; i < records.length; i++) {
        var r = records[i];
        if (r.type === 'childList') {
          for (var j = 0; j < r.addedNodes.length; j++) walk(r.addedNodes[j]);
        } else if (r.type === 'characterData') {
          translateText(r.target);
        }
      }
    });
    observer.observe(document.body, { childList: true, subtree: true, characterData: true });
  }

  // ── 语言切换浮层 ────────────────────────────────────────────────────────
  function mountToggle() {
    if (document.getElementById('zcode-lang-toggle')) return;
    var btn = document.createElement('button');
    btn.id = 'zcode-lang-toggle';
    btn.type = 'button';
    btn.setAttribute('data-no-i18n', '1');
    btn.textContent = wantZh ? 'EN' : '中文';
    btn.title = wantZh ? 'Switch to English' : '切换到中文';
    btn.style.cssText = [
      'position:fixed', 'right:14px', 'bottom:14px', 'z-index:2147483647',
      'padding:6px 12px', 'border-radius:999px', 'cursor:pointer',
      'font:600 12px/1 -apple-system,"Segoe UI",Roboto,sans-serif',
      'color:#e6edf3', 'background:#1c2129', 'border:1px solid #30363d',
      'box-shadow:0 6px 18px rgba(0,0,0,.45)', 'opacity:.85'
    ].join(';');
    btn.onmouseenter = function () { btn.style.opacity = '1'; };
    btn.onmouseleave = function () { btn.style.opacity = '.85'; };
    btn.onclick = function () {
      try { localStorage.setItem(LS_KEY, wantZh ? 'en' : 'zh'); } catch (e) { /* 忽略 */ }
      location.reload();
    };
    document.body.appendChild(btn);
  }

  function apply() {
    if (wantZh) {
      document.documentElement.lang = 'zh-CN';
      walk(document.body);
      startObserver();
      var t = lookup(document.title);
      if (t) document.title = t;
    }
    mountToggle();
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', apply);
  } else {
    apply();
  }
})();
