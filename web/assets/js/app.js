// app.js — 应用入口、会话管理、导航外壳与 hash 路由。

import { api, auth } from './api.js';
import { esc, toast, loading, errorBox, confirmDialog } from './ui.js';
import * as loginView from './views/login.js';
import * as dashboardView from './views/dashboard.js';
import * as projectsView from './views/projects.js';
import * as projectDetailView from './views/project_detail.js';
import * as issueView from './views/issue.js';
import * as licensesView from './views/licenses.js';
import * as licenseDetailView from './views/license_detail.js';
import * as keysView from './views/keys.js';
import * as auditsView from './views/audits.js';
import * as passwordView from './views/password.js';

const appEl = document.getElementById('app');

const state = {
  me: null,
  viewEl: null,
  titleEl: null,
  crumbEl: null,
  userEl: null,
  initial: '',
};

const ICONS = {
  dashboard: '<svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="9" rx="1.5"/><rect x="14" y="3" width="7" height="5" rx="1.5"/><rect x="14" y="12" width="7" height="9" rx="1.5"/><rect x="3" y="16" width="7" height="5" rx="1.5"/></svg>',
  issue: '<svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 8v8M8 12h8"/></svg>',
  licenses: '<svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M7 3h7l5 5v13a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1Z"/><path d="M14 3v5h5M9 13h7M9 17h5"/></svg>',
  projects: '<svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m12 3 8 4.5v9L12 21l-8-4.5v-9L12 3Z"/><path d="m4.5 7.7 7.5 4.3 7.5-4.3M12 12v9"/></svg>',
  keys: '<svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="8" r="4"/><path d="m11 11 9 9M17 17l2-2M14 14l2-2"/></svg>',
  audits: '<svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M5 4h14a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1Z"/><path d="M8 9h8M8 13h8M8 17h5"/></svg>',
  password: '<svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="10" width="16" height="11" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3M12 15v2"/></svg>',
  menu: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round"><path d="M4 7h16M4 12h16M4 17h16"/></svg>',
};

const NAV = [
  {
    label: '运营',
    items: [
      { hash: '#/dashboard', label: '概览看板', icon: 'dashboard', key: 'dashboard' },
      { hash: '#/issue', label: '授权签发', icon: 'issue', key: 'issue' },
      { hash: '#/licenses', label: '授权记录', icon: 'licenses', key: 'licenses' },
    ],
  },
  {
    label: '配置',
    items: [
      { hash: '#/projects', label: '项目管理', icon: 'projects', key: 'projects' },
      { hash: '#/keys', label: '密钥管理', icon: 'keys', key: 'keys' },
    ],
  },
  {
    label: '安全',
    items: [
      { hash: '#/audits', label: '审计日志', icon: 'audits', key: 'audits' },
      { hash: '#/password', label: '修改密码', icon: 'password', key: 'password' },
    ],
  },
];

const ROUTES = [
  { re: /^\/dashboard$/, title: '概览看板', key: 'dashboard', mod: dashboardView },
  { re: /^\/projects$/, title: '项目管理', key: 'projects', mod: projectsView },
  { re: /^\/projects\/([^/]+)$/, title: '项目详情', key: 'projects', mod: projectDetailView },
  { re: /^\/issue$/, title: '授权签发', key: 'issue', mod: issueView },
  { re: /^\/licenses$/, title: '授权记录', key: 'licenses', mod: licensesView },
  { re: /^\/licenses\/([^/]+)$/, title: '授权详情', key: 'licenses', mod: licenseDetailView },
  { re: /^\/keys$/, title: '密钥管理', key: 'keys', mod: keysView },
  { re: /^\/audits$/, title: '审计日志', key: 'audits', mod: auditsView },
  { re: /^\/password$/, title: '修改密码', key: 'password', mod: passwordView },
];

function navigate(hash) {
  if (location.hash === hash) renderRoute();
  else location.hash = hash;
}

function logout(reason) {
  auth.clear();
  state.me = null;
  state.viewEl = null;
  if (reason) toast(reason, 'error');
  showLogin();
}

function renderShell() {
  const admin = state.me || {};
  const initial = (admin.username || 'A').slice(0, 1).toUpperCase();
  appEl.className = '';
  appEl.innerHTML = `
    <div class="shell" id="shell">
      <aside class="sidebar">
        <div class="brand">
          <div class="mark">ULP</div>
          <div class="txt">
            <strong>统一授权平台</strong>
            <span>License Console</span>
          </div>
        </div>
        <nav id="nav">
          ${NAV.map((group) => `
            <div class="nav-group">
              <div class="nav-label">${esc(group.label)}</div>
              ${group.items.map((item) => `
                <a class="nav-item" href="${item.hash}" data-key="${item.key}">
                  <span class="ico">${ICONS[item.icon] || ''}</span>
                  <span>${esc(item.label)}</span>
                </a>`).join('')}
            </div>`).join('')}
        </nav>
        <div class="sidebar-foot">
          <div class="who">
            <div class="avatar">${esc(initial)}</div>
            <div style="min-width:0">
              <div class="name truncate">${esc(admin.username || '')}</div>
              <div class="role">平台管理员</div>
            </div>
          </div>
          <button class="btn ghost sm logout" id="logout-btn" type="button">退出登录</button>
        </div>
      </aside>

      <div class="main">
        <header class="topbar">
          <div class="row" style="min-width:0">
            <button class="icon-btn menu-toggle" id="menu-toggle" type="button" aria-label="菜单">${ICONS.menu}</button>
            <div style="min-width:0">
              <h1 id="page-title">概览看板</h1>
              <div class="crumb" id="page-crumb">统一授权平台 / 概览看板</div>
            </div>
          </div>
          <div class="right">
            <span class="badge neutral nowrap">v1.0.0</span>
            <button class="btn sm" id="top-password" type="button">修改密码</button>
          </div>
        </header>
        <main class="view" id="view"></main>
      </div>
    </div>`;

  state.viewEl = document.getElementById('view');
  state.titleEl = document.getElementById('page-title');
  state.crumbEl = document.getElementById('page-crumb');
  state.userEl = document.querySelector('.sidebar-foot');

  document.getElementById('logout-btn').addEventListener('click', async () => {
    const ok = await confirmDialog({ title: '退出登录', message: '确认退出当前管理员会话？', confirmText: '退出登录' });
    if (ok) logout();
  });
  document.getElementById('top-password').addEventListener('click', () => navigate('#/password'));
  document.getElementById('menu-toggle').addEventListener('click', () => {
    document.getElementById('shell').classList.toggle('nav-open');
  });
  document.getElementById('nav').addEventListener('click', () => {
    document.getElementById('shell').classList.remove('nav-open');
  });
}

function updateNav(key) {
  document.querySelectorAll('.nav-item').forEach((item) => {
    item.classList.toggle('active', item.getAttribute('data-key') === key);
  });
}

function parseHash() {
  const raw = location.hash || '#/dashboard';
  const [pathPart, queryPart = ''] = raw.slice(1).split('?');
  return { path: pathPart || '/dashboard', query: new URLSearchParams(queryPart) };
}

async function renderRoute() {
  if (!state.me) return;
  const { path, query } = parseHash();
  if (state.me.must_change_password && path !== '/password') {
    location.hash = '#/password';
    return;
  }
  let route = ROUTES.find((r) => r.re.test(path));
  if (!route) {
    location.hash = '#/dashboard';
    return;
  }
  const match = route.re.exec(path);
  const params = match ? match.slice(1).map(decodeURIComponent) : [];

  updateNav(route.key);
  state.titleEl.textContent = route.title;
  state.crumbEl.textContent = '统一授权平台 / ' + route.title;
  document.title = route.title + ' · 统一授权平台';

  const view = state.viewEl;
  view.replaceChildren(loading());
  const ctx = {
    me: state.me,
    query,
    navigate,
    reloadMe: async () => {
      const res = await api.me();
      state.me = res.admin;
      return state.me;
    },
  };
  try {
    await route.mod.render(view, ctx, ...params);
  } catch (err) {
    if (err && err.status === 401) return;
    view.innerHTML = `<div class="card"><div class="card-body">
      ${errorBox(err)}
      <div class="mt-16"><button class="btn" id="retry">重新加载</button></div>
    </div></div>`;
    const retry = view.querySelector('#retry');
    if (retry) retry.addEventListener('click', renderRoute);
  }
}

async function handleLoggedIn(admin) {
  state.me = admin;
  renderShell();
  if (admin && admin.must_change_password) {
    location.hash = '#/password';
    return;
  }
  if (!location.hash || location.hash === '#/') location.hash = '#/dashboard';
  renderRoute();
}

function showLogin() {
  document.title = '登录 · 统一授权平台';
  appEl.className = '';
  loginView.render(appEl, { onLogin: handleLoggedIn });
}

async function boot() {
  window.addEventListener('hashchange', () => {
    if (state.me) renderRoute();
  });
  window.addEventListener('ulp:unauthorized', () => {
    if (state.me) logout('会话已过期，请重新登录');
    else showLogin();
  });
  window.addEventListener('ulp:password-required', () => {
    if (state.me && !state.me.must_change_password) {
      state.me.must_change_password = true;
      toast('请先修改初始密码', 'error');
      navigate('#/password');
    }
  });

  if (!auth.token) {
    showLogin();
    return;
  }
  try {
    const res = await api.me();
    state.me = res.admin;
    renderShell();
    if (!location.hash || location.hash === '#/') location.hash = '#/dashboard';
    renderRoute();
  } catch {
    auth.clear();
    showLogin();
  }
}

boot();
