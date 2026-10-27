// ui.js — 通用 DOM 工具、提示、弹窗与格式化。

export function esc(value) {
  if (value === null || value === undefined) return '';
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

export function fmtTime(value, fallback = '—') {
  if (!value) return fallback;
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return fallback;
  const pad = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function fmtDate(value, fallback = '—') {
  if (!value) return fallback;
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return fallback;
  const pad = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export function toLocalInput(value) {
  const d = value ? new Date(value) : new Date();
  if (Number.isNaN(d.getTime())) return '';
  const pad = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function expiryText(license) {
  if (!license) return '—';
  if (license.status === 'revoked') return '已吊销';
  if (license.status === 'superseded') return '已被续期替代';
  if (!license.expire_time) return '永久有效';
  return fmtTime(license.expire_time);
}

const STATUS_MAP = {
  active: ['有效', 'active'],
  grace: ['宽限期', 'grace'],
  expired: ['已过期', 'expired'],
  revoked: ['已吊销', 'revoked'],
  superseded: ['已替代', 'superseded'],
  disabled: ['已停用', 'disabled'],
};

export function statusBadge(status) {
  const [label, cls] = STATUS_MAP[status] || [status || '未知', 'neutral'];
  return `<span class="badge ${cls}">${esc(label)}</span>`;
}

export function projectBadge(status) {
  return status === 'active'
    ? '<span class="badge active">启用</span>'
    : '<span class="badge disabled">停用</span>';
}

export function toast(message, type = 'info', timeout = 3200) {
  const root = document.getElementById('toast-root');
  if (!root) return;
  const node = document.createElement('div');
  node.className = 'toast ' + (type === 'error' ? 'error' : type === 'ok' ? 'ok' : '');
  const icon = type === 'error' ? '!' : type === 'ok' ? '✓' : 'i';
  node.innerHTML = `<span>${icon}</span><span>${esc(message)}</span><span class="x">×</span>`;
  node.querySelector('.x').addEventListener('click', () => node.remove());
  root.appendChild(node);
  if (timeout > 0) setTimeout(() => node.remove(), timeout);
}

export function loading(message = '加载中…') {
  const el = document.createElement('div');
  el.className = 'loading-block';
  el.innerHTML = `<div class="spinner dark"></div><div>${esc(message)}</div>`;
  return el;
}

export function errorBox(err) {
  return `<div class="alert danger"><span class="ico">!</span><span>${esc(err && err.message ? err.message : '操作失败')}</span></div>`;
}

export function emptyState(title, desc = '') {
  return `<div class="empty"><div class="big">${esc(title)}</div>${desc ? `<div>${esc(desc)}</div>` : ''}</div>`;
}

export function openModal({ title, subtitle = '', body = '', footer = '', wide = false, onClose }) {
  const root = document.getElementById('modal-root');
  const backdrop = document.createElement('div');
  backdrop.className = 'modal-backdrop';
  backdrop.innerHTML = `
    <div class="modal ${wide ? 'wide' : ''}" role="dialog" aria-modal="true">
      <div class="modal-head">
        <div><h3>${esc(title)}</h3>${subtitle ? `<p>${esc(subtitle)}</p>` : ''}</div>
        <button class="close" type="button" aria-label="关闭">×</button>
      </div>
      <div class="modal-body">${body}</div>
      ${footer ? `<div class="modal-foot">${footer}</div>` : ''}
    </div>`;
  root.appendChild(backdrop);

  const close = () => {
    backdrop.remove();
    document.removeEventListener('keydown', onKey);
    if (onClose) onClose();
  };
  const onKey = (ev) => { if (ev.key === 'Escape') close(); };
  document.addEventListener('keydown', onKey);
  backdrop.querySelector('.close').addEventListener('click', close);
  backdrop.addEventListener('mousedown', (ev) => {
    if (ev.target === backdrop) close();
  });

  return {
    root: backdrop,
    body: backdrop.querySelector('.modal-body'),
    foot: backdrop.querySelector('.modal-foot'),
    close,
  };
}

export function confirmDialog({ title, message, confirmText = '确认', danger = false, requireText = '' }) {
  return new Promise((resolve) => {
    let done = false;
    const ref = openModal({
      title,
      body: `
        <div class="alert ${danger ? 'danger' : 'info'}"><span class="ico">${danger ? '!' : 'i'}</span><span>${esc(message)}</span></div>
        ${requireText ? `<div class="field mt-16"><label>请输入 <span class="mono">${esc(requireText)}</span> 以确认</label><input class="input" id="cfm-text" autocomplete="off"></div>` : ''}`,
      footer: `<button class="btn" data-act="cancel" type="button">取消</button>
               <button class="btn ${danger ? 'danger' : 'primary'}" data-act="ok" type="button">${esc(confirmText)}</button>`,
      onClose: () => { if (!done) resolve(false); },
    });
    const okBtn = ref.root.querySelector('[data-act="ok"]');
    const cancelBtn = ref.root.querySelector('[data-act="cancel"]');
    const input = ref.root.querySelector('#cfm-text');
    const finish = (value) => {
      if (done) return;
      if (value && requireText && (!input || input.value.trim() !== requireText)) {
        if (input) input.classList.add('err');
        return;
      }
      done = true;
      ref.close();
      resolve(value);
    };
    okBtn.addEventListener('click', () => finish(true));
    cancelBtn.addEventListener('click', () => finish(false));
    if (input) input.focus();
  });
}

export async function copyText(text, successMessage = '已复制到剪贴板') {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
    } else {
      const ta = document.createElement('textarea');
      ta.value = text;
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      ta.remove();
    }
    toast(successMessage, 'ok', 2000);
  } catch {
    toast('复制失败，请手动选择复制', 'error');
  }
}

export function bindCopyButtons(scope) {
  scope.querySelectorAll('[data-copy]').forEach((btn) => {
    btn.addEventListener('click', () => copyText(btn.getAttribute('data-copy'), btn.getAttribute('data-copy-msg') || '已复制'));
  });
}

export function pager(page) {
  const total = page.total || 0;
  const size = page.size || 20;
  const current = page.page || 1;
  const pages = Math.max(1, Math.ceil(total / size));
  return `
    <div class="pager">
      <div>共 <strong>${total}</strong> 条 · 第 ${current} / ${pages} 页</div>
      <div class="pages">
        <button class="btn sm" data-page="${current - 1}" ${current <= 1 ? 'disabled' : ''}>上一页</button>
        <button class="btn sm" data-page="${current + 1}" ${current >= pages ? 'disabled' : ''}>下一页</button>
      </div>
    </div>`;
}

export function bindPager(scope, handler) {
  scope.querySelectorAll('[data-page]').forEach((btn) => {
    btn.addEventListener('click', () => {
      if (btn.disabled) return;
      const n = Number(btn.getAttribute('data-page'));
      if (n >= 1) handler(n);
    });
  });
}

export function shortHash(value, head = 8, tail = 4) {
  if (!value) return '—';
  if (value.length <= head + tail + 1) return value;
  return value.slice(0, head) + '…' + value.slice(-tail);
}

export function parseFeatures(text) {
  const raw = (text || '').trim();
  if (!raw) return {};
  const value = JSON.parse(raw);
  if (typeof value !== 'object' || Array.isArray(value) || value === null) {
    throw new Error('功能配置需为 JSON 对象，例如 {"modules":["a","b"],"max_users":20}');
  }
  return value;
}
