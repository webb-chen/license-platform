// views/project_detail.js — 项目详情、公钥与接入片段。

import { api, downloadPublicKey } from '../api.js';
import {
  esc, fmtTime, loading, projectBadge, shortHash, toast, openModal, confirmDialog,
  errorBox, bindCopyButtons, copyText,
} from '../ui.js';

const LANG_LABEL = { go: 'Go', python: 'Python', node: 'Node.js' };

export async function render(el, ctx, projectId) {
  el.replaceChildren(loading());
  const detail = await api.project(projectId);
  const p = detail.project;
  const snippets = detail.snippets || {};
  const langs = Object.keys(LANG_LABEL).filter((k) => snippets[k]);
  let current = langs[0] || 'go';

  el.innerHTML = `
    <div class="page-head">
      <div>
        <div class="row" style="margin-bottom:6px">
          <a class="btn ghost sm" href="#/projects">← 返回项目列表</a>
        </div>
        <h2>${esc(p.name)} ${projectBadge(p.status)}</h2>
        <p>项目 ID <span class="mono">${esc(p.id)}</span> · product <span class="mono">${esc(p.product)}</span></p>
      </div>
      <div class="actions">
        <a class="btn" href="#/licenses?project=${encodeURIComponent(p.id)}">授权记录</a>
        ${p.status === 'active' ? `<a class="btn primary" href="#/issue?project=${encodeURIComponent(p.id)}">签发授权</a>` : ''}
        <button class="btn" id="edit-project" type="button">编辑</button>
        <button class="btn danger" id="delete-project" type="button">删除</button>
      </div>
    </div>

    ${p.status === 'disabled' ? `<div class="alert warn" style="margin-bottom:16px"><span class="ico">!</span><span>项目当前已停用，无法签发新授权；已签发授权文件仍可离线验证。</span></div>` : ''}

    <div class="grid cols-2">
      <div class="card">
        <div class="card-head"><h3>项目信息</h3></div>
        <div class="card-body">
          <dl class="kv">
            <dt>项目 ID</dt><dd class="mono">${esc(p.id)}</dd>
            <dt>product 标识</dt><dd class="mono">${esc(p.product)}</dd>
            <dt>状态</dt><dd>${projectBadge(p.status)}</dd>
            <dt>实例提示</dt><dd>${esc(p.instance_hint || '—')}</dd>
            <dt>备注</dt><dd>${esc(p.remark || '—')}</dd>
            <dt>授权数量</dt><dd>${p.license_count || 0}</dd>
            <dt>创建时间</dt><dd>${esc(fmtTime(p.created_at))}</dd>
            <dt>更新时间</dt><dd>${esc(fmtTime(p.updated_at))}</dd>
          </dl>
        </div>
      </div>

      <div class="card">
        <div class="card-head"><h3>当前密钥</h3><span class="hint">RSA-2048 · SHA-256</span></div>
        <div class="card-body">
          <dl class="kv">
            <dt>密钥 ID</dt><dd class="mono">${esc(p.key_id || '—')}</dd>
            <dt>公钥指纹</dt><dd class="mono break-all">${esc(p.key_fingerprint || '—')}</dd>
            <dt>生成时间</dt><dd>${esc(fmtTime(p.key_created_at))}</dd>
            <dt>私钥存储</dt><dd>${p.key_id ? 'AES-256-GCM 加密（不对外暴露）' : '未生成'}</dd>
          </dl>
          <div class="row wrap mt-16">
            <button class="btn" id="copy-pub" type="button">复制公钥</button>
            <button class="btn" id="download-pub" type="button">下载公钥 PEM</button>
            <button class="btn danger" id="rotate-key" type="button">轮换密钥</button>
          </div>
        </div>
      </div>
    </div>

    <div class="card mt-16">
      <div class="card-head">
        <h3>公钥（SPKI PEM）</h3>
        <span class="hint">与目标系统内嵌公钥保持一致</span>
      </div>
      <div class="card-body">
        <div class="code-block light">
          <button class="btn sm copy" data-copy-key type="button">复制</button>
          <pre>${esc(detail.public_key || '（暂无公钥）')}</pre>
        </div>
      </div>
    </div>

    <div class="card mt-16">
      <div class="card-head">
        <div class="row">
          <h3>接入片段</h3>
          <span class="hint">离线验签参考实现，含机器码、状态码与校验顺序</span>
        </div>
        <div class="tabs" id="snippet-tabs">
          ${langs.map((k) => `<button type="button" data-lang="${k}" class="${k === current ? 'active' : ''}">${LANG_LABEL[k]}</button>`).join('')}
        </div>
      </div>
      <div class="card-body">
        <div class="alert info" style="margin-bottom:12px">
          <span class="ico">i</span>
          <span>片段中的 product 已固定为 <span class="mono">${esc(p.product)}</span>，公钥为该项目的独立公钥。请按目标项目的语言与目录结构调整后接入。</span>
        </div>
        <div class="code-block">
          <button class="btn sm copy" id="copy-snippet" type="button">复制代码</button>
          <pre id="snippet-code">${esc(snippets[current] || '（暂无片段）')}</pre>
        </div>
      </div>
    </div>`;

  bindCopyButtons(el);
  const pubBtn = el.querySelector('#copy-pub');
  if (pubBtn) pubBtn.addEventListener('click', () => copyText(detail.public_key || '', '公钥已复制'));
  el.querySelector('[data-copy-key]').addEventListener('click', () => copyText(detail.public_key || '', '公钥已复制'));
  el.querySelector('#download-pub').addEventListener('click', async () => {
    try { await downloadPublicKey(p.id); } catch (err) { toast(err.message, 'error'); }
  });
  el.querySelector('#copy-snippet').addEventListener('click', () => copyText(snippets[current] || '', '代码已复制'));

  el.querySelectorAll('#snippet-tabs button').forEach((btn) => {
    btn.addEventListener('click', () => {
      current = btn.getAttribute('data-lang');
      el.querySelectorAll('#snippet-tabs button').forEach((b) => b.classList.toggle('active', b === btn));
      el.querySelector('#snippet-code').textContent = snippets[current] || '（暂无片段）';
    });
  });

  el.querySelector('#edit-project').addEventListener('click', () => openEdit(el, ctx, p));
  el.querySelector('#delete-project').addEventListener('click', async () => {
    const ok = await confirmDialog({
      title: '删除项目',
      message: `确认删除项目「${p.name}」？项目下的密钥将被停用，且不可恢复。存在授权记录时会被拒绝。`,
      confirmText: '删除项目',
      danger: true,
      requireText: p.id,
    });
    if (!ok) return;
    try {
      await api.deleteProject(p.id);
      toast('项目已删除', 'ok');
      ctx.navigate('#/projects');
    } catch (err) {
      toast(err.message, 'error');
    }
  });

  el.querySelector('#rotate-key').addEventListener('click', async () => {
    const ok = await confirmDialog({
      title: '轮换密钥对',
      message: '轮换后将生成新的 RSA-2048 密钥对。旧密钥会停用：使用旧公钥验证的历史授权仍可被验证，但目标系统更新为新公钥后，旧授权将无法通过验签校验。请确认已安排好重新签发。',
      confirmText: '轮换密钥',
      danger: true,
      requireText: p.id,
    });
    if (!ok) return;
    try {
      const key = await api.rotateKey(p.id);
      toast('密钥已轮换，新指纹 ' + shortHash(key.fingerprint, 12, 6), 'ok');
      render(el, ctx, p.id);
    } catch (err) {
      toast(err.message, 'error');
    }
  });
}

function openEdit(el, ctx, p) {
  const ref = openModal({
    title: '编辑项目',
    subtitle: '项目 ID 与 product 创建后不可修改',
    body: `
      <div id="edit-alert"></div>
      <form class="form" id="edit-form" autocomplete="off">
        <div class="form-row">
          <div class="field">
            <label>项目名称<span class="required">*</span></label>
            <input class="input" name="name" value="${esc(p.name)}" required>
          </div>
          <div class="field">
            <label>状态</label>
            <label class="switch" style="height:38px">
              <input type="checkbox" name="active" ${p.status === 'active' ? 'checked' : ''}>
              <span>${p.status === 'active' ? '启用中' : '已停用'}（切换后保存生效）</span>
            </label>
          </div>
        </div>
        <div class="field">
          <label>实例提示</label>
          <input class="input" name="instance_hint" value="${esc(p.instance_hint || '')}">
        </div>
        <div class="field">
          <label>备注</label>
          <textarea class="textarea" name="remark" rows="3">${esc(p.remark || '')}</textarea>
        </div>
      </form>`,
    footer: `<button class="btn" data-act="cancel" type="button">取消</button>
             <button class="btn primary" data-act="ok" type="button">保存</button>`,
  });
  const form = ref.root.querySelector('#edit-form');
  const alertBox = ref.root.querySelector('#edit-alert');
  ref.root.querySelector('[data-act="cancel"]').addEventListener('click', ref.close);
  const okBtn = ref.root.querySelector('[data-act="ok"]');
  okBtn.addEventListener('click', async () => {
    alertBox.innerHTML = '';
    try {
      await api.updateProject(p.id, {
        name: form.elements.name.value.trim(),
        status: form.elements.active.checked ? 'active' : 'disabled',
        instance_hint: form.elements.instance_hint.value.trim(),
        remark: form.elements.remark.value.trim(),
      });
      toast('项目信息已保存', 'ok');
      ref.close();
      render(el, ctx, p.id);
    } catch (err) {
      alertBox.innerHTML = errorBox(err);
    }
  });
}
