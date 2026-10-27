// views/projects.js — 项目列表与新增。

import { api } from '../api.js';
import {
  esc, fmtTime, loading, projectBadge, shortHash, toast, openModal, confirmDialog, errorBox,
} from '../ui.js';

export async function render(el, ctx) {
  el.replaceChildren(loading());
  const data = await api.projects();
  const items = data.items || [];

  const rows = items.map((p) => `
    <tr>
      <td>
        <div><a href="#/projects/${encodeURIComponent(p.id)}"><strong>${esc(p.name)}</strong></a></div>
        <div class="mono muted small">${esc(p.id)}</div>
      </td>
      <td class="mono">${esc(p.product)}</td>
      <td>${projectBadge(p.status)}</td>
      <td class="num">${p.license_count || 0}</td>
      <td class="mono muted" title="${esc(p.key_fingerprint || '')}">${esc(shortHash(p.key_fingerprint, 12, 6))}</td>
      <td class="muted nowrap">${esc(fmtTime(p.created_at))}</td>
      <td class="actions-cell">
        <a class="btn sm" href="#/projects/${encodeURIComponent(p.id)}">详情</a>
        ${p.status === 'active'
          ? `<a class="btn sm" href="#/issue?project=${encodeURIComponent(p.id)}">签发</a>`
          : ''}
        <button class="btn sm" data-toggle="${esc(p.id)}" data-status="${esc(p.status)}">${p.status === 'active' ? '停用' : '启用'}</button>
        <button class="btn sm danger" data-delete="${esc(p.id)}">删除</button>
      </td>
    </tr>`).join('');

  el.innerHTML = `
    <div class="page-head">
      <div>
        <h2>项目管理</h2>
        <p>每个项目拥有独立的 RSA-2048 密钥对，授权 product 与目标系统严格绑定</p>
      </div>
      <div class="actions">
        <button class="btn primary" id="new-project" type="button">新增项目</button>
      </div>
    </div>
    <div class="alert info" style="margin-bottom:16px">
      <span class="ico">i</span>
      <span>新增项目时自动生成密钥对并加密存储私钥；公钥与 Go / Python / Node 接入片段可在项目详情中获取。删除项目前需先清理其授权记录。</span>
    </div>
    <div class="card">
      <div class="card-head">
        <h3>项目列表</h3>
        <span class="hint">共 ${data.total || 0} 个项目</span>
      </div>
      <div class="card-body tight">
        ${items.length ? `<div class="table-wrap"><table class="table">
          <thead><tr><th>项目</th><th>product</th><th>状态</th><th>授权数</th><th>密钥指纹</th><th>创建时间</th><th style="text-align:right">操作</th></tr></thead>
          <tbody>${rows}</tbody></table></div>`
          : '<div class="empty"><div class="big">还没有登记项目</div><div>点击右上角「新增项目」开始</div></div>'}
      </div>
    </div>`;

  el.querySelector('#new-project').addEventListener('click', () => openCreate(el, ctx));

  el.querySelectorAll('[data-toggle]').forEach((btn) => {
    btn.addEventListener('click', async () => {
      const id = btn.getAttribute('data-toggle');
      const next = btn.getAttribute('data-status') === 'active' ? 'disabled' : 'active';
      const ok = await confirmDialog({
        title: next === 'disabled' ? '停用项目' : '启用项目',
        message: next === 'disabled'
          ? `停用后项目「${id}」将无法继续签发授权，已签发的授权文件不受影响。`
          : `确认重新启用项目「${id}」？`,
        confirmText: next === 'disabled' ? '停用' : '启用',
        danger: next === 'disabled',
      });
      if (!ok) return;
      try {
        await api.updateProject(id, { status: next });
        toast('项目状态已更新', 'ok');
        render(el, ctx);
      } catch (err) {
        toast(err.message, 'error');
      }
    });
  });

  el.querySelectorAll('[data-delete]').forEach((btn) => {
    btn.addEventListener('click', async () => {
      const id = btn.getAttribute('data-delete');
      const ok = await confirmDialog({
        title: '删除项目',
        message: `删除项目「${id}」将同时停用其密钥。存在授权记录的项目无法删除。`,
        confirmText: '删除项目',
        danger: true,
        requireText: id,
      });
      if (!ok) return;
      try {
        await api.deleteProject(id);
        toast('项目已删除', 'ok');
        render(el, ctx);
      } catch (err) {
        toast(err.message, 'error');
      }
    });
  });
}

function openCreate(el, ctx) {
  const ref = openModal({
    title: '新增项目',
    subtitle: '保存后立即生成独立密钥对',
    body: `
      <div id="create-alert"></div>
      <form class="form" id="create-form" autocomplete="off">
        <div class="form-row">
          <div class="field">
            <label>项目 ID<span class="required">*</span></label>
            <input class="input mono" name="id" placeholder="例如 agent-system" required>
            <span class="desc">2~64 位小写字母、数字或短横线，作为授权文件名前缀</span>
          </div>
          <div class="field">
            <label>项目名称<span class="required">*</span></label>
            <input class="input" name="name" placeholder="例如 智能体系统" required>
          </div>
        </div>
        <div class="form-row">
          <div class="field">
            <label>product 标识</label>
            <input class="input mono" name="product" placeholder="留空则与项目 ID 相同">
            <span class="desc">写入授权文件并与目标系统 product 严格比对</span>
          </div>
          <div class="field">
            <label>实例提示</label>
            <input class="input" name="instance_hint" placeholder="例如 生产环境 · 华东">
            <span class="desc">仅用于平台内识别，不写入授权</span>
          </div>
        </div>
        <div class="field">
          <label>备注</label>
          <textarea class="textarea" name="remark" rows="3" placeholder="项目负责人、部署位置等补充说明"></textarea>
        </div>
      </form>`,
    footer: `<button class="btn" data-act="cancel" type="button">取消</button>
             <button class="btn primary" data-act="ok" type="button">创建并生成密钥</button>`,
  });

  const form = ref.root.querySelector('#create-form');
  const alertBox = ref.root.querySelector('#create-alert');
  const okBtn = ref.root.querySelector('[data-act="ok"]');
  ref.root.querySelector('[data-act="cancel"]').addEventListener('click', ref.close);

  okBtn.addEventListener('click', async () => {
    alertBox.innerHTML = '';
    const payload = {
      id: form.elements.id.value.trim(),
      name: form.elements.name.value.trim(),
      product: form.elements.product.value.trim(),
      instance_hint: form.elements.instance_hint.value.trim(),
      remark: form.elements.remark.value.trim(),
    };
    if (!payload.id || !payload.name) {
      alertBox.innerHTML = errorBox({ message: '项目 ID 与项目名称为必填项' });
      return;
    }
    okBtn.disabled = true;
    okBtn.innerHTML = '<span class="spinner"></span> 正在生成密钥…';
    try {
      const p = await api.createProject(payload);
      toast('项目已创建，密钥对已生成', 'ok');
      ref.close();
      ctx.navigate('#/projects/' + encodeURIComponent(p.id));
    } catch (err) {
      alertBox.innerHTML = errorBox(err);
      okBtn.disabled = false;
      okBtn.textContent = '创建并生成密钥';
    }
  });
}
