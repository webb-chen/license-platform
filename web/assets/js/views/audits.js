// views/audits.js — 审计日志筛选、分页与明细展示。

import { api } from '../api.js';
import { esc, fmtTime, loading, errorBox, emptyState, pager, bindPager } from '../ui.js';

const PAGE_SIZE = 20;

const ACTIONS = [
  ['', '全部操作'],
  ['admin.login', '登录'],
  ['admin.login_failed', '登录失败'],
  ['admin.password', '修改密码'],
  ['project.create', '新增项目'],
  ['project.update', '更新项目'],
  ['project.delete', '删除项目'],
  ['key.rotate', '轮换密钥'],
  ['license.issue', '签发授权'],
  ['license.download', '下载授权'],
  ['license.revoke', '吊销授权'],
  ['license.renew', '续期授权'],
  ['license.delete', '删除授权'],
];

export async function render(el, ctx) {
  el.replaceChildren(loading());

  const state = {
    page: 1,
    action: ctx.query.get('action') || '',
    operator: '',
    from: '',
    to: '',
  };
  let pageData = null;

  el.innerHTML = `
    <div class="page-head">
      <div>
        <h2>审计日志</h2>
        <p>记录登录、项目、密钥与授权的关键操作，用于追溯与合规检查</p>
      </div>
    </div>

    <div class="card">
      <div class="card-body">
        <form id="audit-filter" class="filters">
          <div class="field wide">
            <label>操作类型</label>
            <select class="select" name="action">
              ${ACTIONS.map(([value, label]) =>
                `<option value="${esc(value)}" ${value === state.action ? 'selected' : ''}>${esc(label)}${value ? '（' + esc(value) + '）' : ''}</option>`).join('')}
            </select>
          </div>
          <div class="field">
            <label>操作人</label>
            <input class="input" name="operator" placeholder="用户名包含…">
          </div>
          <div class="field">
            <label>日期从</label>
            <input class="input" type="date" name="from">
          </div>
          <div class="field">
            <label>至</label>
            <input class="input" type="date" name="to">
          </div>
          <button class="btn primary" type="submit">查询</button>
          <button class="btn" type="button" id="audit-reset">重置</button>
        </form>
      </div>
    </div>

    <div class="card mt-16">
      <div class="card-head">
        <h3>操作记录</h3>
        <span class="hint" id="audit-total">—</span>
      </div>
      <div class="card-body tight" id="audit-body"></div>
      <div id="audit-pager"></div>
    </div>`;

  const form = el.querySelector('#audit-filter');
  const body = el.querySelector('#audit-body');
  const pagerEl = el.querySelector('#audit-pager');
  const totalEl = el.querySelector('#audit-total');

  async function load() {
    body.innerHTML = '<div class="loading-block"><div class="spinner dark"></div><div>加载中…</div></div>';
    try {
      pageData = await api.audits({
        action: state.action,
        operator: state.operator,
        from: state.from,
        to: state.to,
        page: state.page,
        size: PAGE_SIZE,
      });
      paint();
    } catch (err) {
      body.innerHTML = `<div class="card-body">${errorBox(err)}</div>`;
      pagerEl.innerHTML = '';
      totalEl.textContent = '—';
    }
  }

  function paint() {
    const items = pageData.items || [];
    totalEl.textContent = `共 ${pageData.total} 条`;
    if (!items.length) {
      body.innerHTML = emptyState('暂无审计记录', '调整筛选条件后重试');
      pagerEl.innerHTML = '';
      return;
    }
    body.innerHTML = `<div class="table-wrap"><table class="table">
      <thead><tr>
        <th class="nowrap">时间</th><th>操作</th><th>对象</th><th>详情</th><th>操作人</th><th>IP</th>
      </tr></thead>
      <tbody>${items.map((a) => `
        <tr>
          <td class="muted nowrap">${esc(fmtTime(a.created_at))}</td>
          <td><span class="badge neutral mono">${esc(a.action)}</span></td>
          <td class="mono small">${a.target ? esc(a.target) : '<span class="muted">—</span>'}</td>
          <td><div class="audit-detail">${esc(a.detail || '—')}</div></td>
          <td>${esc(a.operator || '—')}</td>
          <td class="mono small muted">${esc(a.ip || '—')}</td>
        </tr>`).join('')}</tbody></table></div>`;
    pagerEl.innerHTML = pager(pageData);
    bindPager(pagerEl, (n) => { state.page = n; load(); });
  }

  form.addEventListener('submit', (ev) => {
    ev.preventDefault();
    state.action = form.elements.action.value;
    state.operator = form.elements.operator.value.trim();
    state.from = form.elements.from.value;
    state.to = form.elements.to.value;
    state.page = 1;
    load();
  });
  el.querySelector('#audit-reset').addEventListener('click', () => {
    form.reset();
    state.action = '';
    state.operator = '';
    state.from = '';
    state.to = '';
    state.page = 1;
    load();
  });

  await load();
}
