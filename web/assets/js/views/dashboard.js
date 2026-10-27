// views/dashboard.js — 概览看板。

import { api } from '../api.js';
import { esc, fmtTime, loading, statusBadge, shortHash } from '../ui.js';

export async function render(el) {
  el.replaceChildren(loading());
  const data = await api.dashboard();

  const expiring = (data.expiring_soon || []).map((l) => `
    <tr>
      <td><a class="mono" href="#/licenses/${encodeURIComponent(l.id)}">${esc(shortHash(l.id, 10, 4))}</a></td>
      <td>${esc(l.project_name || l.project_id)}</td>
      <td class="truncate" style="max-width:180px">${esc(l.customer)}</td>
      <td class="nowrap">${esc(fmtTime(l.expire_time))}</td>
    </tr>`).join('');

  const recent = (data.recent_licenses || []).map((l) => `
    <tr>
      <td><a class="mono" href="#/licenses/${encodeURIComponent(l.id)}">${esc(shortHash(l.id, 10, 4))}</a></td>
      <td>${esc(l.project_name || l.project_id)}</td>
      <td class="truncate" style="max-width:160px">${esc(l.customer)}</td>
      <td>${statusBadge(l.effective_status)}</td>
      <td class="nowrap muted">${esc(fmtTime(l.issued_at))}</td>
    </tr>`).join('');

  const audits = (data.recent_audits || []).map((a) => `
    <div class="item">
      <div class="dot"></div>
      <div>
        <div class="t"><strong>${esc(actionLabel(a.action))}</strong> <span class="muted">· ${esc(a.operator || '系统')}</span></div>
        <div class="m">${esc(a.detail || a.target || '')}</div>
        <div class="m">${esc(fmtTime(a.created_at))}</div>
      </div>
    </div>`).join('');

  el.innerHTML = `
    <div class="page-head">
      <div>
        <h2>概览看板</h2>
        <p>授权资产、到期风险与最近操作的统一视图</p>
      </div>
      <div class="actions">
        <a class="btn" href="#/projects">项目管理</a>
        <a class="btn primary" href="#/issue">签发授权</a>
      </div>
    </div>

    <div class="grid cols-5">
      <div class="stat accent">
        <div class="label">授权总数</div>
        <div class="value">${data.total}</div>
        <div class="foot">近 30 天签发 ${data.issued_last_30_days} 份</div>
      </div>
      <div class="stat ok">
        <div class="label">有效授权</div>
        <div class="value">${data.active}</div>
        <div class="foot">含永久授权</div>
      </div>
      <div class="stat warn">
        <div class="label">宽限期</div>
        <div class="value">${data.grace}</div>
        <div class="foot">已过期但仍在宽限</div>
      </div>
      <div class="stat danger">
        <div class="label">已过期</div>
        <div class="value">${data.expired}</div>
        <div class="foot">需联系客户续期</div>
      </div>
      <div class="stat">
        <div class="label">已登记项目</div>
        <div class="value">${data.projects}</div>
        <div class="foot">其中启用 ${data.active_projects} 个</div>
      </div>
    </div>

    <div class="grid cols-2 mt-16">
      <div class="card">
        <div class="card-head">
          <h3>30 天内到期</h3>
          <span class="hint">按到期时间排序，最多 8 条</span>
        </div>
        <div class="card-body tight">
          ${expiring ? `<div class="table-wrap"><table class="table">
            <thead><tr><th>授权 ID</th><th>项目</th><th>客户</th><th>到期时间</th></tr></thead>
            <tbody>${expiring}</tbody></table></div>`
            : '<div class="empty"><div class="big">近期没有即将到期的授权</div><div>有效授权均在 30 天以外到期，或为永久授权</div></div>'}
        </div>
      </div>

      <div class="card">
        <div class="card-head">
          <h3>最近签发</h3>
          <a class="hint" href="#/licenses">查看全部 →</a>
        </div>
        <div class="card-body tight">
          ${recent ? `<div class="table-wrap"><table class="table">
            <thead><tr><th>授权 ID</th><th>项目</th><th>客户</th><th>状态</th><th>签发时间</th></tr></thead>
            <tbody>${recent}</tbody></table></div>`
            : '<div class="empty"><div class="big">还没有授权记录</div><div>前往「授权签发」创建第一份授权</div></div>'}
        </div>
      </div>
    </div>

    <div class="card mt-16">
      <div class="card-head">
        <h3>最近操作</h3>
        <a class="hint" href="#/audits">审计日志 →</a>
      </div>
      <div class="card-body">
        ${audits ? `<div class="timeline">${audits}</div>` : '<div class="empty">暂无审计记录</div>'}
      </div>
    </div>`;
}

function actionLabel(action) {
  const map = {
    'admin.login': '管理员登录',
    'admin.login_failed': '登录失败',
    'admin.password_change': '修改密码',
    'project.create': '新增项目',
    'project.update': '更新项目',
    'project.delete': '删除项目',
    'key.rotate': '轮换密钥',
    'license.issue': '签发授权',
    'license.revoke': '吊销授权',
    'license.renew': '续期授权',
    'license.download': '下载授权',
    'license.delete': '删除授权',
  };
  return map[action] || action;
}
