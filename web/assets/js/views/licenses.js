// views/licenses.js — 授权记录列表、筛选、下载、吊销与续期。

import { api, downloadLicense } from '../api.js';
import {
  esc, fmtTime, loading, statusBadge, shortHash, toast, openModal, pager, bindPager,
  errorBox, toLocalInput, emptyState,
} from '../ui.js';

const PAGE_SIZE = 20;

export async function render(el, ctx) {
  el.replaceChildren(loading());
  const [projectsRes] = await Promise.all([api.projects()]);
  const projects = projectsRes.items || [];

  const state = {
    page: 1,
    project: ctx.query.get('project') || '',
    customer: '',
    status: '',
    from: '',
    to: '',
  };
  let pageData = null;

  el.innerHTML = `
    <div class="page-head">
      <div>
        <h2>授权记录</h2>
        <p>查询、下载、续期与吊销全部签发记录</p>
      </div>
      <div class="actions">
        <a class="btn primary" href="#/issue">签发授权</a>
      </div>
    </div>

    <div class="card">
      <div class="card-body">
        <form id="filter-form" class="filters">
          <div class="field wide">
            <label>项目</label>
            <select class="select" name="project">
              <option value="">全部项目</option>
              ${projects.map((p) => `<option value="${esc(p.id)}" ${p.id === state.project ? 'selected' : ''}>${esc(p.name)}</option>`).join('')}
            </select>
          </div>
          <div class="field">
            <label>客户</label>
            <input class="input" name="customer" placeholder="客户名称包含…">
          </div>
          <div class="field">
            <label>状态</label>
            <select class="select" name="status">
              <option value="">全部状态</option>
              <option value="active">有效</option>
              <option value="grace">宽限期</option>
              <option value="expired">已过期</option>
              <option value="revoked">已吊销</option>
              <option value="superseded">已替代</option>
            </select>
          </div>
          <div class="field">
            <label>签发日期从</label>
            <input class="input" type="date" name="from">
          </div>
          <div class="field">
            <label>至</label>
            <input class="input" type="date" name="to">
          </div>
          <button class="btn primary" type="submit">查询</button>
          <button class="btn" type="button" id="reset-filter">重置</button>
        </form>
      </div>
    </div>

    <div class="card mt-16">
      <div class="card-head">
        <h3>授权列表</h3>
        <span class="hint" id="list-total">—</span>
      </div>
      <div class="card-body tight" id="list-body"></div>
      <div id="list-pager"></div>
    </div>`;

  const form = el.querySelector('#filter-form');
  const body = el.querySelector('#list-body');
  const pagerEl = el.querySelector('#list-pager');
  const totalEl = el.querySelector('#list-total');

  async function load() {
    body.innerHTML = '<div class="loading-block"><div class="spinner dark"></div><div>加载中…</div></div>';
    try {
      pageData = await api.licenses({
        project: state.project,
        customer: state.customer,
        status: state.status,
        from: state.from,
        to: state.to,
        page: state.page,
        size: PAGE_SIZE,
      });
      paint();
    } catch (err) {
      body.innerHTML = `<div class="card-body">${errorBox(err)}</div>`;
      pagerEl.innerHTML = '';
    }
  }

  function paint() {
    const items = pageData.items || [];
    totalEl.textContent = `共 ${pageData.total} 条`;
    if (!items.length) {
      body.innerHTML = emptyState('没有符合条件的授权记录', '调整筛选条件，或前往「授权签发」创建新授权');
      pagerEl.innerHTML = '';
      return;
    }
    body.innerHTML = `<div class="table-wrap"><table class="table">
      <thead><tr>
        <th>授权 ID</th><th>项目</th><th>客户</th><th>到期时间</th><th>状态</th>
        <th>机器绑定</th><th>签发时间</th><th style="text-align:right">操作</th>
      </tr></thead>
      <tbody>${items.map((l) => `
        <tr>
          <td><a class="mono" href="#/licenses/${encodeURIComponent(l.id)}" title="${esc(l.id)}">${esc(shortHash(l.id, 10, 4))}</a></td>
          <td><div>${esc(l.project_name || l.project_id)}</div><div class="mono muted small">${esc(l.project_id)}</div></td>
          <td><div class="truncate" style="max-width:170px" title="${esc(l.customer)}">${esc(l.customer)}</div>
              ${l.customer_contact ? `<div class="muted small truncate" style="max-width:170px">${esc(l.customer_contact)}</div>` : ''}</td>
          <td class="nowrap">${l.expire_time ? esc(fmtTime(l.expire_time)) : '<span class="badge blue">永久</span>'}</td>
          <td>${statusBadge(l.effective_status)}</td>
          <td class="mono small">${l.machine_code === '*' ? '<span class="muted">通用</span>' : esc(shortHash(l.machine_code, 8, 4))}</td>
          <td class="muted nowrap">${esc(fmtTime(l.issued_at))}</td>
          <td class="actions-cell">
            <a class="btn sm" href="#/licenses/${encodeURIComponent(l.id)}">详情</a>
            ${l.status === 'revoked'
              ? '<button class="btn sm" type="button" disabled title="已吊销，禁止下载">下载</button>'
              : `<button class="btn sm" data-download="${esc(l.id)}" type="button">下载</button>`}
            ${l.status === 'revoked'
              ? '<button class="btn sm" type="button" disabled title="已吊销，请重新签发">续期</button>'
              : `<button class="btn sm" data-renew="${esc(l.id)}" type="button">续期</button>`}
            ${l.status === 'active' ? `<button class="btn sm danger" data-revoke="${esc(l.id)}" type="button">吊销</button>` : ''}
          </td>
        </tr>`).join('')}</tbody></table></div>`;
    pagerEl.innerHTML = pager(pageData);
    bindPager(pagerEl, (n) => { state.page = n; load(); });
    bindRowActions(items);
  }

  function bindRowActions(items) {
    body.querySelectorAll('[data-download]').forEach((btn) => {
      btn.addEventListener('click', async () => {
        btn.disabled = true;
        try {
          const name = await downloadLicense(btn.getAttribute('data-download'));
          toast('已下载 ' + name, 'ok');
        } catch (err) {
          toast(err.message, 'error');
        } finally {
          btn.disabled = false;
        }
      });
    });
    body.querySelectorAll('[data-renew]').forEach((btn) => {
      btn.addEventListener('click', () => {
        const lic = items.find((x) => x.id === btn.getAttribute('data-renew'));
        openRenew(lic, () => load());
      });
    });
    body.querySelectorAll('[data-revoke]').forEach((btn) => {
      btn.addEventListener('click', () => {
        const lic = items.find((x) => x.id === btn.getAttribute('data-revoke'));
        openRevoke(lic, () => load());
      });
    });
  }

  form.addEventListener('submit', (ev) => {
    ev.preventDefault();
    state.project = form.elements.project.value;
    state.customer = form.elements.customer.value.trim();
    state.status = form.elements.status.value;
    state.from = form.elements.from.value;
    state.to = form.elements.to.value;
    state.page = 1;
    load();
  });
  el.querySelector('#reset-filter').addEventListener('click', () => {
    form.reset();
    state.project = '';
    state.customer = '';
    state.status = '';
    state.from = '';
    state.to = '';
    state.page = 1;
    load();
  });

  await load();
}

function openRevoke(lic, onDone) {
  const ref = openModal({
    title: '吊销授权',
    subtitle: lic.project_name ? `${lic.project_name} · ${lic.customer}` : lic.customer,
    body: `
      <div class="alert warn" style="margin-bottom:16px">
        <span class="ico">!</span>
        <span><strong>离线端无法感知吊销。</strong>已在客户服务器上安装的授权文件不会自动失效。吊销仅作为平台记录，并阻止后续下载与续期；如需让客户立即停用，需配合人工或远程措施。</span>
      </div>
      <div class="field">
        <label>吊销原因</label>
        <textarea class="textarea" id="revoke-reason" rows="3" placeholder="例如：合同终止 / 客户要求 / 授权信息有误"></textarea>
      </div>`,
    footer: `<button class="btn" data-act="cancel" type="button">取消</button>
             <button class="btn danger" data-act="ok" type="button">确认吊销</button>`,
  });
  ref.root.querySelector('[data-act="cancel"]').addEventListener('click', ref.close);
  const okBtn = ref.root.querySelector('[data-act="ok"]');
  okBtn.addEventListener('click', async () => {
    okBtn.disabled = true;
    okBtn.innerHTML = '<span class="spinner"></span> 处理中…';
    try {
      await api.revokeLicense(lic.id, ref.root.querySelector('#revoke-reason').value.trim());
      toast('授权已吊销', 'ok');
      ref.close();
      onDone();
    } catch (err) {
      toast(err.message, 'error');
      okBtn.disabled = false;
      okBtn.textContent = '确认吊销';
    }
  });
}

export function openRenew(lic, onDone) {
  const ref = openModal({
    title: '续期授权',
    subtitle: '将基于原授权信息签发一份新授权，原授权自动标记为「已替代」',
    body: `
      <div id="renew-alert"></div>
      <dl class="kv" style="margin-bottom:18px">
        <dt>原授权 ID</dt><dd class="mono">${esc(lic.id)}</dd>
        <dt>客户</dt><dd>${esc(lic.customer)}</dd>
        <dt>原到期</dt><dd>${lic.expire_time ? esc(fmtTime(lic.expire_time)) : '永久有效'}</dd>
        <dt>机器绑定</dt><dd class="mono">${esc(lic.machine_code)}</dd>
      </dl>
      <form class="form" id="renew-form" autocomplete="off">
        <div class="field">
          <label>续期方式</label>
          <div class="radio-cards" style="grid-template-columns:1fr 1fr">
            <label class="radio-card">
              <input type="radio" name="mode" value="days" checked>
              <strong>延长天数</strong>
              <span>从本次签发起算</span>
            </label>
            <label class="radio-card">
              <input type="radio" name="mode" value="date">
              <strong>指定到期时间</strong>
              <span>精确到分钟</span>
            </label>
          </div>
        </div>
        <div class="field" id="renew-days">
          <label>有效天数</label>
          <input class="input" type="number" name="days" min="1" max="36500" value="365">
        </div>
        <div class="field" id="renew-date" style="display:none">
          <label>新的到期时间</label>
          <input class="input" type="datetime-local" name="expire_at" min="${toLocalInput(new Date(Date.now() + 60000))}" value="${toLocalInput(new Date(Date.now() + 365 * 86400000))}">
        </div>
        <div class="field">
          <label>宽限期（天）</label>
          <input class="input" type="number" name="grace_days" min="0" max="3650" value="${Number(lic.grace_days) || 0}">
        </div>
        <div class="field">
          <label>备注</label>
          <textarea class="textarea" name="note" rows="2" placeholder="续期说明（可选）"></textarea>
        </div>
      </form>`,
    footer: `<button class="btn" data-act="cancel" type="button">取消</button>
             <button class="btn primary" data-act="ok" type="button">签发续期授权</button>`,
  });
  const form = ref.root.querySelector('#renew-form');
  const alertBox = ref.root.querySelector('#renew-alert');
  const daysField = ref.root.querySelector('#renew-days');
  const dateField = ref.root.querySelector('#renew-date');
  form.querySelectorAll('input[name="mode"]').forEach((r) => {
    r.addEventListener('change', () => {
      const mode = form.querySelector('input[name="mode"]:checked').value;
      daysField.style.display = mode === 'days' ? '' : 'none';
      dateField.style.display = mode === 'date' ? '' : 'none';
    });
  });
  ref.root.querySelector('[data-act="cancel"]').addEventListener('click', ref.close);
  const okBtn = ref.root.querySelector('[data-act="ok"]');
  okBtn.addEventListener('click', async () => {
    alertBox.innerHTML = '';
    const mode = form.querySelector('input[name="mode"]:checked').value;
    const payload = {
      expiry_mode: mode,
      grace_days: Number(form.elements.grace_days.value || 0),
      machine_mode: lic.machine_code === '*' ? 'any' : 'custom',
      machine_code: lic.machine_code,
      note: form.elements.note.value.trim(),
    };
    if (mode === 'days') {
      payload.days = Number(form.elements.days.value || 0);
      if (!payload.days || payload.days < 1) {
        alertBox.innerHTML = errorBox({ message: '有效天数至少为 1 天' });
        return;
      }
    } else {
      const raw = form.elements.expire_at.value;
      const d = raw ? new Date(raw) : null;
      if (!d || Number.isNaN(d.getTime()) || d.getTime() <= Date.now()) {
        alertBox.innerHTML = errorBox({ message: '请选择晚于当前时间的到期时间' });
        return;
      }
      payload.expire_at = d.toISOString();
    }
    okBtn.disabled = true;
    okBtn.innerHTML = '<span class="spinner"></span> 正在签发…';
    try {
      const created = await api.renewLicense(lic.id, payload);
      ref.close();
      toast('续期授权已签发', 'ok');
      onDone(created);
    } catch (err) {
      alertBox.innerHTML = errorBox(err);
      okBtn.disabled = false;
      okBtn.textContent = '签发续期授权';
    }
  });
}
