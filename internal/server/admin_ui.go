package server

import (
	"net/http"
	"strings"
)

const adminHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>tunnel.ma37.com 管理台</title>
  <style>
    :root {
      --bg: #f5f7fb;
      --surface: #ffffff;
      --surface-soft: #f8fafc;
      --text: #172033;
      --muted: #667085;
      --line: #dde3ee;
      --primary: #2563eb;
      --primary-dark: #1d4ed8;
      --ok: #12805c;
      --warn: #b45309;
      --bad: #c2410c;
      --shadow: 0 10px 28px rgba(15, 23, 42, 0.08);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      color: var(--text);
      background: var(--bg);
      font-family: "Segoe UI", "PingFang SC", "Microsoft YaHei", Arial, sans-serif;
      letter-spacing: 0;
    }
    [hidden] { display: none !important; }
    .page {
      width: min(1440px, calc(100% - 32px));
      margin: 0 auto;
      padding: 20px 0 32px;
    }
    .topbar {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 16px;
      margin-bottom: 16px;
    }
    .eyebrow {
      color: var(--primary);
      font-weight: 700;
      font-size: 13px;
      margin-bottom: 4px;
    }
    h1, h2, h3, p { margin: 0; }
    h1 { font-size: 28px; line-height: 1.2; }
    h2 { font-size: 18px; line-height: 1.35; }
    h3 { font-size: 15px; line-height: 1.35; }
    .muted { color: var(--muted); }
    .mono, code {
      font-family: Consolas, "SFMono-Regular", monospace;
      word-break: break-all;
    }
    .actions, .row-actions, .quick-actions {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
      align-items: center;
    }
    .btn {
      border: 1px solid var(--line);
      background: var(--surface);
      color: var(--text);
      border-radius: 8px;
      padding: 9px 12px;
      font-size: 14px;
      font-weight: 700;
      cursor: pointer;
      text-decoration: none;
      min-height: 38px;
    }
    .btn:hover { border-color: #b7c2d5; }
    .btn-primary {
      background: var(--primary);
      border-color: var(--primary);
      color: #fff;
    }
    .btn-primary:hover { background: var(--primary-dark); }
    .btn-danger {
      color: var(--bad);
      border-color: rgba(194, 65, 12, 0.28);
      background: #fff7ed;
    }
    .btn-small {
      min-height: 32px;
      padding: 7px 10px;
      font-size: 13px;
    }
    .notice {
      border: 1px solid #fed7aa;
      background: #fff7ed;
      color: #7c2d12;
      border-radius: 8px;
      padding: 12px 14px;
      margin-bottom: 14px;
      line-height: 1.55;
    }
    .cards {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 12px;
      margin-bottom: 14px;
    }
    .card, .panel {
      background: var(--surface);
      border: 1px solid var(--line);
      border-radius: 8px;
      box-shadow: var(--shadow);
    }
    .card { padding: 14px; min-height: 104px; }
    .card .label { color: var(--muted); font-size: 13px; margin-bottom: 8px; }
    .card .value { font-size: 28px; line-height: 1.1; font-weight: 800; margin-bottom: 6px; }
    .card .desc { color: var(--muted); font-size: 12px; line-height: 1.5; }
    .grid {
      display: grid;
      grid-template-columns: minmax(0, 1.25fr) minmax(360px, 0.75fr);
      gap: 14px;
      align-items: start;
    }
    .panel { padding: 16px; }
    .panel-head {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      align-items: center;
      margin-bottom: 12px;
    }
    .table-wrap { overflow-x: auto; border: 1px solid var(--line); border-radius: 8px; }
    table { width: 100%; border-collapse: collapse; font-size: 14px; background: var(--surface); }
    th, td { padding: 11px 10px; border-bottom: 1px solid var(--line); text-align: left; vertical-align: middle; }
    th { color: var(--muted); background: var(--surface-soft); font-weight: 700; font-size: 13px; }
    tbody tr { cursor: pointer; }
    tbody tr:hover { background: #f8fbff; }
    tbody tr.selected { background: #eff6ff; }
    tbody tr:last-child td { border-bottom: 0; }
    .badge {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      border-radius: 999px;
      padding: 4px 9px;
      font-size: 12px;
      font-weight: 800;
      white-space: nowrap;
    }
    .badge-online { color: var(--ok); background: #e8f7ef; }
    .badge-offline { color: #475467; background: #eef2f6; }
    .badge-disabled { color: var(--bad); background: #fff1e8; }
    .badge-expired { color: var(--warn); background: #fff7df; }
    .field { margin-bottom: 12px; }
    .field label {
      display: block;
      color: var(--muted);
      font-size: 13px;
      font-weight: 700;
      margin-bottom: 6px;
    }
    input[type="text"], input[type="datetime-local"], textarea {
      width: 100%;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: #fff;
      color: var(--text);
      padding: 10px 11px;
      font-size: 14px;
      outline: none;
    }
    input[type="text"]:focus, input[type="datetime-local"]:focus, textarea:focus {
      border-color: var(--primary);
      box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.12);
    }
    textarea { min-height: 74px; resize: vertical; }
    .detail-line {
      display: grid;
      grid-template-columns: 92px 1fr;
      gap: 10px;
      align-items: center;
      padding: 9px 0;
      border-bottom: 1px solid var(--line);
      font-size: 14px;
    }
    .detail-line:last-child { border-bottom: 0; }
    .detail-label { color: var(--muted); font-weight: 700; font-size: 13px; }
    .detail-box {
      border: 1px solid var(--line);
      border-radius: 8px;
      background: var(--surface-soft);
      padding: 10px;
      margin: 12px 0;
    }
    .copy-list {
      display: grid;
      gap: 8px;
      margin-top: 10px;
    }
    .copy-row {
      display: grid;
      grid-template-columns: 1fr auto;
      gap: 8px;
      align-items: center;
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 8px;
      background: #fff;
    }
    .log-box {
      min-height: 118px;
      margin-top: 12px;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: #0f172a;
      color: #e5e7eb;
      padding: 12px;
      white-space: pre-wrap;
      word-break: break-word;
      font-size: 12px;
      line-height: 1.55;
    }
    .capacity-grid {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 12px;
      margin-top: 14px;
    }
    .mini {
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 12px;
      background: var(--surface-soft);
    }
    .mini .label { color: var(--muted); font-size: 12px; margin-bottom: 6px; }
    .mini .value { font-size: 20px; font-weight: 800; }
    .empty { color: var(--muted); text-align: center; padding: 24px 8px; }
    .hide-mobile { display: table-cell; }
    @media (max-width: 1180px) {
      .cards, .capacity-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
      .grid { grid-template-columns: 1fr; }
    }
    @media (max-width: 720px) {
      .page { width: min(100% - 20px, 1440px); padding-top: 12px; }
      .topbar, .panel-head { align-items: stretch; flex-direction: column; }
      .cards, .capacity-grid { grid-template-columns: 1fr; }
      .hide-mobile { display: none; }
      h1 { font-size: 23px; }
      .copy-row { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <div class="page">
    <header class="topbar">
      <div>
        <div class="eyebrow">tunnel.ma37.com</div>
        <h1>隧道管理台</h1>
        <p class="muted" id="meta-line">正在加载...</p>
      </div>
      <div class="actions">
        <a class="btn" href="/healthz" target="_blank">健康检查</a>
        <button class="btn btn-primary" onclick="loadAll()">刷新</button>
      </div>
    </header>

    <div class="notice" id="security-alert" hidden>
      后台未设置登录账号。上线时请在 tunnel-server.env 配置 TUNNEL_ADMIN_USER 和 TUNNEL_ADMIN_PASS。
    </div>

    <section class="cards">
      <div class="card">
        <div class="label">存储</div>
        <div class="value" id="card-backend">-</div>
        <div class="desc" id="card-security">登录保护加载中</div>
      </div>
      <div class="card">
        <div class="label">代理总数</div>
        <div class="value" id="card-total">0</div>
        <div class="desc">已经接入或登记过的代理</div>
      </div>
      <div class="card">
        <div class="label">在线代理</div>
        <div class="value" id="card-online">0</div>
        <div class="desc">本地客户端正在连接</div>
      </div>
      <div class="card">
        <div class="label">停用/到期</div>
        <div class="value" id="card-blocked">0</div>
        <div class="desc">已被后台限制的代理</div>
      </div>
    </section>

    <section class="grid">
      <div class="panel">
        <div class="panel-head">
          <div>
            <h2>代理列表</h2>
            <p class="muted">点击一行后，在右侧授权、停用、复制给用户。</p>
          </div>
          <input id="proxy-filter" type="text" placeholder="搜索代理ID、设备、备注" oninput="renderTable()">
        </div>
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>状态</th>
                <th>代理ID</th>
                <th>设备</th>
                <th class="hide-mobile">备注</th>
                <th class="hide-mobile">最后在线</th>
              </tr>
            </thead>
            <tbody id="proxy-table-body">
              <tr><td colspan="5" class="empty">暂无代理数据</td></tr>
            </tbody>
          </table>
        </div>
      </div>

      <aside class="panel">
        <div class="panel-head">
          <div>
            <h2 id="detail-title">代理详情</h2>
            <p class="muted" id="detail-subtitle">请选择左侧代理</p>
          </div>
          <span id="detail-status">-</span>
        </div>

        <div class="detail-box">
          <div class="detail-line">
            <div class="detail-label">代理ID</div>
            <div class="mono" id="detail-proxy-id">-</div>
          </div>
          <div class="detail-line">
            <div class="detail-label">绑定设备</div>
            <div id="detail-agent-name">-</div>
          </div>
          <div class="detail-line">
            <div class="detail-label">出口IP</div>
            <div class="mono" id="detail-remote-ip">-</div>
          </div>
          <div class="detail-line">
            <div class="detail-label">有效期</div>
            <div id="detail-expiry-label">-</div>
          </div>
        </div>

        <div class="field">
          <label for="detail-remark">备注</label>
          <textarea id="detail-remark" placeholder="例如：张三电脑 / 7天测试 / 已交付客户"></textarea>
        </div>
        <div class="field">
          <label for="detail-expires">到期时间</label>
          <input type="datetime-local" id="detail-expires">
        </div>
        <div class="row-actions">
          <button class="btn btn-primary" onclick="quickGrant('permanent')">永久授权</button>
          <button class="btn" onclick="quickGrant('7')">授权 7 天</button>
          <button class="btn" onclick="quickGrant('30')">授权 30 天</button>
          <button class="btn btn-danger" onclick="quickGrant('disable')">立即停用</button>
          <button class="btn" onclick="saveProxy()">保存备注/到期</button>
        </div>

        <div class="detail-box">
          <h3>复制给用户</h3>
          <div class="copy-list">
            <div class="copy-row">
              <div class="mono" id="detail-socks-url">-</div>
              <button class="btn btn-small" onclick="copySocksUrl()">复制代理地址</button>
            </div>
            <div class="copy-row">
              <div class="mono" id="detail-status-link">-</div>
              <button class="btn btn-small" onclick="copyStatusLink()">复制状态页</button>
            </div>
            <div class="copy-row">
              <div class="mono" id="detail-agent-command">-</div>
              <button class="btn btn-small" onclick="copyAgentCommand()">复制启动命令</button>
            </div>
          </div>
          <div class="quick-actions" style="margin-top:10px">
            <button class="btn btn-primary" onclick="copyUserText()">复制整段交付信息</button>
            <button class="btn" onclick="runProxyTool('status')">状态检测</button>
            <button class="btn" onclick="runProxyTool('test')">连通测试</button>
          </div>
        </div>

        <div class="log-box" id="proxy-tool-output">等待操作...</div>
      </aside>
    </section>

    <section class="panel" style="margin-top:14px">
      <div class="panel-head">
        <div>
          <h2>服务器容量</h2>
          <p class="muted" id="capacity-note">正在计算容量建议...</p>
        </div>
        <div class="mono muted" id="public-line">-</div>
      </div>
      <div class="capacity-grid">
        <div class="mini">
          <div class="label">CPU</div>
          <div class="value" id="cap-cpu">-</div>
        </div>
        <div class="mini">
          <div class="label">内存</div>
          <div class="value" id="cap-mem">-</div>
        </div>
        <div class="mini">
          <div class="label">稳定在线</div>
          <div class="value" id="cap-online">-</div>
        </div>
        <div class="mini">
          <div class="label">建议并发</div>
          <div class="value" id="cap-active">-</div>
        </div>
      </div>
    </section>
  </div>

  <script>
    const state = {
      overview: {},
      proxies: [],
      selectedProxyId: ''
    };

    async function getJSON(url) {
      const res = await fetch(url, { credentials: 'same-origin' });
      const text = await res.text();
      let data;
      try {
        data = JSON.parse(text);
      } catch (err) {
        throw new Error(text || '返回内容不是 JSON');
      }
      if (!res.ok) {
        throw new Error(data.error || text || '请求失败');
      }
      return data;
    }

    async function postJSON(url, body) {
      const res = await fetch(url, {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body)
      });
      const text = await res.text();
      let data;
      try {
        data = JSON.parse(text);
      } catch (err) {
        throw new Error(text || '返回内容不是 JSON');
      }
      if (!res.ok) {
        throw new Error(data.error || text || '请求失败');
      }
      return data;
    }

    function esc(input) {
      return String(input == null ? '' : input)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
    }

    function statusLabel(status) {
      const map = { online: '在线', offline: '离线', disabled: '已停用', expired: '已到期' };
      return map[status] || status || '-';
    }

    function badge(status) {
      const cls = {
        online: 'badge-online',
        offline: 'badge-offline',
        disabled: 'badge-disabled',
        expired: 'badge-expired'
      }[status] || 'badge-offline';
      return '<span class="badge ' + cls + '">' + esc(statusLabel(status)) + '</span>';
    }

    function formatTime(value) {
      if (!value) return '-';
      const date = new Date(value);
      if (Number.isNaN(date.getTime())) return value;
      return date.toLocaleString();
    }

    function toDatetimeLocal(value) {
      if (!value) return '';
      const date = new Date(value);
      if (Number.isNaN(date.getTime())) return '';
      const year = date.getFullYear();
      const month = String(date.getMonth() + 1).padStart(2, '0');
      const day = String(date.getDate()).padStart(2, '0');
      const hour = String(date.getHours()).padStart(2, '0');
      const minute = String(date.getMinutes()).padStart(2, '0');
      return year + '-' + month + '-' + day + 'T' + hour + ':' + minute;
    }

    function expiryLabel(item) {
      if (!item || item.permanent || !item.expires_at) return '永久有效';
      return formatTime(item.expires_at);
    }

    function selectedItem() {
      return state.proxies.find(function(item) { return item.proxy_id === state.selectedProxyId; }) || null;
    }

    function renderOverview(data) {
      state.overview = data || {};
      document.getElementById('card-backend').textContent = data.backend || '-';
      document.getElementById('card-total').textContent = String(data.proxy_total || 0);
      document.getElementById('card-online').textContent = String(data.proxy_online || 0);
      document.getElementById('card-blocked').textContent = String((data.proxy_disabled || 0) + (data.proxy_expired || 0));
      document.getElementById('card-security').textContent = data.admin_auth_enabled ? '后台登录保护已开启' : '后台登录保护未开启';
      document.getElementById('security-alert').hidden = !!data.admin_auth_enabled;
      document.getElementById('meta-line').textContent = '更新时间：' + formatTime(data.updated_at);
      document.getElementById('public-line').textContent = 'Agent: ' + (data.public_relay_addr || '-') + ' | SOCKS: ' + (data.socks_host || '-') + ':' + String(data.socks_port || '-');

      const cap = data.capacity || {};
      document.getElementById('cap-cpu').textContent = String(cap.cpu_cores || '-') + ' 核';
      document.getElementById('cap-mem').textContent = cap.memory_gb_label || '-';
      document.getElementById('cap-online').textContent = String(cap.recommended_online_agents || '-');
      document.getElementById('cap-active').textContent = String(cap.recommended_active_concurrent || '-');
      document.getElementById('capacity-note').textContent = cap.upgrade_hint || '暂无容量建议';
    }

    function renderTable() {
      const body = document.getElementById('proxy-table-body');
      const keyword = document.getElementById('proxy-filter').value.trim().toLowerCase();
      const rows = state.proxies.filter(function(item) {
        if (!keyword) return true;
        return [item.proxy_id, item.agent_name, item.remark, item.remote_ip].join(' ').toLowerCase().includes(keyword);
      });
      if (!rows.length) {
        body.innerHTML = '<tr><td colspan="5" class="empty">暂无代理数据</td></tr>';
        return;
      }
      body.innerHTML = rows.map(function(item) {
        const selected = item.proxy_id === state.selectedProxyId ? ' selected' : '';
        return '<tr class="' + selected + '" onclick="selectProxy(\'' + esc(item.proxy_id) + '\')">'
          + '<td>' + badge(item.status) + '</td>'
          + '<td><div class="mono">' + esc(item.proxy_id) + '</div></td>'
          + '<td>' + esc(item.agent_name || '-') + '</td>'
          + '<td class="hide-mobile">' + esc(item.remark || '-') + '</td>'
          + '<td class="hide-mobile">' + esc(item.last_seen_at ? formatTime(item.last_seen_at) : '-') + '</td>'
          + '</tr>';
      }).join('');
    }

    function fillDetail(item) {
      if (!item) {
        document.getElementById('detail-title').textContent = '代理详情';
        document.getElementById('detail-subtitle').textContent = '请选择左侧代理';
        return;
      }
      document.getElementById('detail-title').textContent = item.agent_name || '代理详情';
      document.getElementById('detail-subtitle').textContent = item.message || '';
      document.getElementById('detail-status').innerHTML = badge(item.status);
      document.getElementById('detail-proxy-id').textContent = item.proxy_id || '-';
      document.getElementById('detail-agent-name').textContent = item.agent_name || '-';
      document.getElementById('detail-remote-ip').textContent = item.remote_ip || '-';
      document.getElementById('detail-expiry-label').textContent = expiryLabel(item);
      document.getElementById('detail-remark').value = item.remark || '';
      document.getElementById('detail-expires').value = toDatetimeLocal(item.expires_at);
      document.getElementById('detail-socks-url').textContent = item.socks_url || '-';
      document.getElementById('detail-status-link').textContent = item.status_link || '-';
      document.getElementById('detail-agent-command').textContent = item.agent_command || (state.overview.agent_command_sample || '-');
    }

    async function loadOverview() {
      const data = await getJSON('/api/admin/overview');
      renderOverview(data);
    }

    async function loadProxies() {
      const data = await getJSON('/api/admin/proxies');
      state.proxies = Array.isArray(data.items) ? data.items : [];
      if (!state.selectedProxyId && state.proxies.length) {
        state.selectedProxyId = state.proxies[0].proxy_id;
      }
      if (state.selectedProxyId && !state.proxies.some(function(item) { return item.proxy_id === state.selectedProxyId; })) {
        state.selectedProxyId = state.proxies.length ? state.proxies[0].proxy_id : '';
      }
      renderTable();
      fillDetail(selectedItem());
    }

    async function selectProxy(proxyId) {
      state.selectedProxyId = proxyId;
      renderTable();
      const item = await getJSON('/api/admin/proxy?proxy_id=' + encodeURIComponent(proxyId));
      const idx = state.proxies.findIndex(function(row) { return row.proxy_id === proxyId; });
      if (idx >= 0) state.proxies[idx] = item;
      fillDetail(item);
      renderTable();
    }

    function expiryInDays(days) {
      const date = new Date();
      date.setDate(date.getDate() + Number(days));
      return toDatetimeLocal(date.toISOString());
    }

    async function savePayload(payload, okText) {
      if (!state.selectedProxyId) {
        setOutput('请先选择一个代理。');
        return;
      }
      setOutput('正在保存...');
      try {
        const data = await postJSON('/api/admin/proxy/save', payload);
        const item = data.item || {};
        const idx = state.proxies.findIndex(function(row) { return row.proxy_id === item.proxy_id; });
        if (idx >= 0) state.proxies[idx] = item;
        fillDetail(item);
        renderTable();
        await loadOverview();
        setOutput(okText || '保存成功。');
      } catch (err) {
        setOutput('保存失败：' + String(err.message || err));
      }
    }

    async function quickGrant(mode) {
      const item = selectedItem();
      if (!item) {
        setOutput('请先选择一个代理。');
        return;
      }
      const remark = document.getElementById('detail-remark').value;
      if (mode === 'disable') {
        await savePayload({ proxy_id: item.proxy_id, remark: remark, enabled: false, permanent: true, expires_at: '' }, '已停用。用户会立刻无法继续使用这个代理。');
        return;
      }
      if (mode === 'permanent') {
        await savePayload({ proxy_id: item.proxy_id, remark: remark, enabled: true, permanent: true, expires_at: '' }, '已设为永久授权。');
        return;
      }
      const expiresAt = expiryInDays(mode);
      document.getElementById('detail-expires').value = expiresAt;
      await savePayload({ proxy_id: item.proxy_id, remark: remark, enabled: true, permanent: false, expires_at: expiresAt }, '已授权 ' + mode + ' 天。');
    }

    async function saveProxy() {
      const item = selectedItem();
      if (!item) {
        setOutput('请先选择一个代理。');
        return;
      }
      const expiresAt = document.getElementById('detail-expires').value;
      await savePayload({
        proxy_id: item.proxy_id,
        remark: document.getElementById('detail-remark').value,
        enabled: item.enabled !== false,
        permanent: !expiresAt,
        expires_at: expiresAt
      }, '备注和到期时间已保存。');
    }

    async function runProxyTool(kind) {
      const item = selectedItem();
      if (!item) {
        setOutput('请先选择一个代理。');
        return;
      }
      setOutput('正在检测...');
      try {
        const data = await getJSON('/api/demo/tunnel/' + kind + '?proxy_id=' + encodeURIComponent(item.proxy_id));
        const label = data.ok ? '可用' : '不可用';
        setOutput('检测结果：' + label + '\n状态：' + statusLabel(data.status) + '\n说明：' + (data.message || '-'));
        await loadProxies();
      } catch (err) {
        setOutput('检测失败：' + String(err.message || err));
      }
    }

    async function copyText(text, successText) {
      if (!text || text === '-') {
        setOutput('当前没有可复制的内容。');
        return;
      }
      try {
        await navigator.clipboard.writeText(text);
        setOutput(successText || ('已复制：' + text));
      } catch (err) {
        setOutput('复制失败，请手动复制下面内容：\n' + text);
      }
    }

    function copySocksUrl() {
      copyText(document.getElementById('detail-socks-url').textContent, '代理地址已复制。');
    }

    function copyStatusLink() {
      copyText(document.getElementById('detail-status-link').textContent, '状态页已复制。');
    }

    function copyAgentCommand() {
      copyText(document.getElementById('detail-agent-command').textContent, '本地启动命令已复制。');
    }

    function copyUserText() {
      const item = selectedItem();
      if (!item) {
        setOutput('请先选择一个代理。');
        return;
      }
      const text = [
        '代理权限已开通',
        '代理ID：' + (item.proxy_id || '-'),
        '代理地址：' + (item.socks_url || '-'),
        '状态页：' + (item.status_link || '-'),
        '有效期：' + expiryLabel(item),
        '当前状态：' + statusLabel(item.status)
      ].join('\n');
      copyText(text, '交付信息已复制。');
    }

    function setOutput(text) {
      document.getElementById('proxy-tool-output').textContent = text;
    }

    async function loadAll() {
      try {
        await loadOverview();
        await loadProxies();
      } catch (err) {
        document.getElementById('meta-line').textContent = '加载失败：' + String(err.message || err);
      }
    }

    loadAll();
    setInterval(loadOverview, 15000);
    setInterval(loadProxies, 30000);
  </script>
</body>
</html>`

func (s *HTTPServer) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/admin" && r.URL.Path != "/admin/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(adminHTML))
}

func adminWantsAuth(user, pass string) bool {
	return strings.TrimSpace(user) != "" || strings.TrimSpace(pass) != ""
}

func basicAuthOK(r *http.Request, expectedUser, expectedPass string) bool {
	gotUser, gotPass, ok := r.BasicAuth()
	if !ok {
		return false
	}
	return gotUser == expectedUser && gotPass == expectedPass
}
