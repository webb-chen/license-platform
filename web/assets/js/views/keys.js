// views/keys.js — 密钥管理。

import { api } from '../api.js';
import {
  esc, fmtTime, loading, shortHash, toast, openModal, confirmDialog, errorBox,
} from '../ui.js';

export async function render(el, ctx) {
  el.replaceChildren(loading());
  const [keysRes, projectsRes] = await Promise.all([api.keys(), api.projects()]);
  const keys = keysRes.items || [];
  const projects = projectsRes.items || [];

  const rows = keys.map((k) => `
    <tr>
      <td>
        <div>${esc(k.project_name || k.project_id)}</div>
        <div class="mono muted small">${esc(k.project_id)}</div>
      </td>
      <td class="mono">${esc(k.product || '—')}</td>
      <td class="mono break-all" title="${esc(k.fingerprint)}">${esc(shortHash(k.fingerprint, 16, 8))}</td>
      <td class="muted nowrap">${esc(fmtTime(k.created_at))}</td>
      <td>${k.disabled_at ? `<span class="badge disabled" title="${esc(fmtTime(k.disabled_at))}">已停用</span>` : '<span class="badge active">使用中</span>'}</td>
      <td class="mono small">${esc(k.id)}</td>
    </tr>`).join('');

  el.innerHTML = `
    <div class="page-head">
      <div>
        <h2>密钥管理</h2>
        <p>每个项目独立 RSA-2048 密钥对；私钥仅以 AES-256-GCM 密文存储</p>
      </div>
      <div class="actions">
        <button class="btn danger" id="rotate" type="button">轮换项目密钥</button>
      </div>
    </div>

    <div class="alert info" style="margin-bottom:16px">
      <span class="ico">i</span>
      <span>平台不会展示、导出或通过接口返回私钥。公钥请在「项目详情」中获取，并与目标系统内嵌公钥保持一致。轮换密钥前请规划好历史授权的重新签发。</span>
    </div>

    <div class="card">
      <div class="card-head">
        <h3>密钥列表</h3>
        <span class="hint">共 ${keys.length} 个密钥</span>
      </div>
      <div class="card-body tight">
        ${keys.length ? `<div class="table-wrap"><table class="table">
          <thead><tr><th>项目</th><th>product</th><th>公钥指纹（SHA-256）</th><th>生成时间</th><th>状态</th><th>密钥 ID</th></tr></thead>
          <tbody>${rows}</tbody></table></div>`
          : '<div class="empty"><div class="big">暂无密钥</div><div>新增项目时会自动生成密钥对</div></div>'}
      </div>
    </div>`;

  el.querySelector('#rotate').addEventListener('click', () => {
    if (!projects.length) {
      toast('请先创建项目', 'error');
      return;
    }
    const ref = openModal({
      title: '轮换项目密钥',
      subtitle: '为指定项目生成新的 RSA-2048 密钥对',
      body: `
        <div id="rotate-alert"></div>
        <div class="alert warn" style="margin-bottom:16px">
          <span class="ico">!</span>
          <span>轮换后旧密钥立即停用。目标系统更新为新公钥后，<strong>历史授权将无法通过验签</strong>，需要用新密钥重新签发。请谨慎操作。</span>
        </div>
        <div class="field">
          <label>选择项目</label>
          <select class="select" id="rotate-project">
            ${projects.map((p) => `<option value="${esc(p.id)}">${esc(p.name)}（${esc(p.id)}）</option>`).join('')}
          </select>
        </div>`,
      footer: `<button class="btn" data-act="cancel" type="button">取消</button>
               <button class="btn danger" data-act="ok" type="button">确认轮换</button>`,
    });
    const alertBox = ref.root.querySelector('#rotate-alert');
    ref.root.querySelector('[data-act="cancel"]').addEventListener('click', ref.close);
    const okBtn = ref.root.querySelector('[data-act="ok"]');
    okBtn.addEventListener('click', async () => {
      const project = ref.root.querySelector('#rotate-project').value;
      okBtn.disabled = true;
      okBtn.innerHTML = '<span class="spinner"></span> 正在生成密钥…';
      try {
        const key = await api.rotateKey(project);
        toast('密钥已轮换，新指纹 ' + shortHash(key.fingerprint, 16, 8), 'ok');
        ref.close();
        render(el, ctx);
      } catch (err) {
        alertBox.innerHTML = errorBox(err);
        okBtn.disabled = false;
        okBtn.textContent = '确认轮换';
      }
    });
  });
}
