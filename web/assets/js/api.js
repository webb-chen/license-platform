// api.js — 后端接口封装、令牌管理与错误归一化。

const TOKEN_KEY = 'ulp.token';
const ADMIN_KEY = 'ulp.admin';

let token = localStorage.getItem(TOKEN_KEY) || '';

export class ApiError extends Error {
  constructor(status, code, message) {
    super(message || '请求失败');
    this.status = status;
    this.code = code || 'error';
  }
}

export const auth = {
  get token() { return token; },
  set session({ token: t, admin }) {
    token = t || '';
    if (token) localStorage.setItem(TOKEN_KEY, token);
    else localStorage.removeItem(TOKEN_KEY);
    if (admin) localStorage.setItem(ADMIN_KEY, JSON.stringify(admin));
  },
  get cachedAdmin() {
    try { return JSON.parse(localStorage.getItem(ADMIN_KEY) || 'null'); } catch { return null; }
  },
  clear() {
    token = '';
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(ADMIN_KEY);
  },
};

async function parseError(res) {
  let code = 'http_' + res.status;
  let message = '';
  try {
    const body = await res.json();
    code = body.code || code;
    message = body.message || '';
  } catch { /* 非 JSON 错误体 */ }
  if (!message) {
    if (res.status === 401) message = '未登录或会话已过期';
    else if (res.status === 403) message = '没有权限执行该操作';
    else if (res.status === 404) message = '资源不存在';
    else if (res.status >= 500) message = '服务器内部错误';
    else message = '请求失败（HTTP ' + res.status + '）';
  }
  return new ApiError(res.status, code, message);
}

async function request(path, { method = 'GET', body, signal } = {}) {
  const headers = { Accept: 'application/json' };
  if (token) headers.Authorization = 'Bearer ' + token;
  if (body !== undefined) headers['Content-Type'] = 'application/json';

  let res;
  try {
    res = await fetch('/api' + path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal,
    });
  } catch (err) {
    if (err && err.name === 'AbortError') throw err;
    throw new ApiError(0, 'network_error', '无法连接服务器，请检查网络或服务状态');
  }

  if (!res.ok) {
    const error = await parseError(res);
    if (error.status === 401) {
      auth.clear();
      window.dispatchEvent(new CustomEvent('ulp:unauthorized'));
    }
    if (error.code === 'password_change_required') {
      window.dispatchEvent(new CustomEvent('ulp:password-required'));
    }
    throw error;
  }
  if (res.status === 204) return null;
  const type = res.headers.get('Content-Type') || '';
  if (!type.includes('application/json')) return res.text();
  return res.json();
}

export const api = {
  health: () => request('/health'),
  login: (username, password) => request('/admin/login', { method: 'POST', body: { username, password } }),
  me: () => request('/admin/me'),
  changePassword: (oldPassword, newPassword) =>
    request('/admin/password', { method: 'POST', body: { old_password: oldPassword, new_password: newPassword } }),

  dashboard: () => request('/dashboard'),

  projects: () => request('/projects'),
  createProject: (payload) => request('/projects', { method: 'POST', body: payload }),
  project: (id) => request('/projects/' + encodeURIComponent(id)),
  updateProject: (id, payload) => request('/projects/' + encodeURIComponent(id), { method: 'PATCH', body: payload }),
  deleteProject: (id) => request('/projects/' + encodeURIComponent(id), { method: 'DELETE' }),

  licenses: (query = {}) => {
    const qs = new URLSearchParams();
    Object.entries(query).forEach(([k, v]) => {
      if (v !== undefined && v !== null && v !== '') qs.set(k, v);
    });
    const q = qs.toString();
    return request('/licenses' + (q ? '?' + q : ''));
  },
  issueLicense: (payload) => request('/licenses', { method: 'POST', body: payload }),
  license: (id) => request('/licenses/' + encodeURIComponent(id)),
  revokeLicense: (id, reason) => request('/licenses/' + encodeURIComponent(id) + '/revoke', { method: 'POST', body: { reason } }),
  renewLicense: (id, payload) => request('/licenses/' + encodeURIComponent(id) + '/renew', { method: 'POST', body: payload }),
  deleteLicense: (id) => request('/licenses/' + encodeURIComponent(id), { method: 'DELETE' }),

  keys: () => request('/keys'),
  rotateKey: (project) => request('/keys/rotate', { method: 'POST', body: { project } }),

  audits: (query = {}) => {
    const qs = new URLSearchParams();
    Object.entries(query).forEach(([k, v]) => {
      if (v !== undefined && v !== null && v !== '') qs.set(k, v);
    });
    const q = qs.toString();
    return request('/audit-logs' + (q ? '?' + q : ''));
  },
};

function filenameFromDisposition(header, fallback) {
  if (!header) return fallback;
  const star = /filename\*=UTF-8''([^;]+)/i.exec(header);
  if (star) {
    try { return decodeURIComponent(star[1].trim()); } catch { /* ignore */ }
  }
  const plain = /filename="?([^";]+)"?/i.exec(header);
  return plain ? plain[1] : fallback;
}

async function download(path, fallbackName) {
  const headers = {};
  if (token) headers.Authorization = 'Bearer ' + token;
  const res = await fetch('/api' + path, { headers });
  if (!res.ok) {
    const error = await parseError(res);
    if (error.status === 401) {
      auth.clear();
      window.dispatchEvent(new CustomEvent('ulp:unauthorized'));
    }
    throw error;
  }
  const blob = await res.blob();
  const name = filenameFromDisposition(res.headers.get('Content-Disposition'), fallbackName);
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 4000);
  return name;
}

export function downloadLicense(id) {
  return download('/licenses/' + encodeURIComponent(id) + '/download', id + '.lic');
}

export function downloadPublicKey(projectId) {
  return download('/projects/' + encodeURIComponent(projectId) + '/public-key', projectId + '-public.pem');
}
