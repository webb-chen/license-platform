// views/license_detail.js — 授权详情、内容预览与操作。

import { api, downloadLicense } from '../api.js';
import {
  esc, fmtTime, loading, statusBadge, toast, confirmDialog, bindCopyButtons, copyText,
} from '../ui.js';
import { openRenew } from './licenses.js';

export async function render(el, ctx, licenseId) {
  el.replaceChildren(loading());
  const detail = await api.license(licenseId);
  const l = detail.license;

  el.innerHTML = `
    <div class="page-head">
      <div>
        <div class="row" style="margin-bottom:6px">
          <a class="btn ghost sm" href="#/licenses">← 返回授权记录</a>
        </div>
        <h2>授权详情 ${statusBadge(l.effective_status)}</h2>
        <p class="mono break-all">${esc(l.id)}</p>
      </div>
      <div class="actions">
        ${l.status === 'revoked'
          ? '<button class="btn primary" id="dl" type="button" disabled title="已吊销，禁止下载">下载 .lic</button>'
          : '<button class="btn primary" id="dl" type="button">下载 .lic</button>'}
        ${l.status === 'revoked'
          ? '<button class="btn" id="renew" type="button" disabled title="已吊销，请重新签发">续期</button>'
          : '<button class="btn" id="renew" type="button">续期</button>'}
        ${l.status === 'active' ? `<button class="btn danger" id="revoke" type="button">吊销</button>` : ''}
        <button class="btn danger" id="delete" type="button">删除记录</button>
      </div>
    </div>

    ${l.status === 'revoked' ? `
      <div class="alert warn" style="margin-bottom:16px"><span class="ico">!</span>
        <span>该授权已在平台吊销（${esc(fmtTime(l.revoked_at))}）。<strong>离线端无法感知吊销</strong>，已安装的授权文件仍会通过本地验签。</span>
      </div>` : ''}
    ${l.status === 'superseded' ? `
      <div class="alert info" style="margin-bottom:16px"><span class="ico">i</span>
        <span>该授权已被续期替代，请使用新的授权文件。历史文件仍可离线验证。</span>
      </div>` : ''}

    <div class="grid cols-2">
      <div class="card">
        <div class="card-head"><h3>授权信息</h3></div>
        <div class="card-body">
          <dl class="kv">
            <dt>目标项目</dt><dd><a href="#/projects/${encodeURIComponent(l.project_id)}">${esc(l.project_name || l.project_id)}</a> <span class="mono muted small">(${esc(l.product || l.project_id)})</span></dd>
            <dt>客户名称</dt><dd>${esc(l.customer)}</dd>
            <dt>联系人</dt><dd>${esc(l.customer_contact || '—')}</dd>
            <dt>实例 ID</dt><dd class="mono">${esc(l.instance_id || '—')}</dd>
            <dt>签发时间</dt><dd>${esc(fmtTime(l.issued_at))}</dd>
            <dt>到期时间</dt><dd>${l.expire_time ? esc(fmtTime(l.expire_time)) : '<span class="badge blue">永久有效</span>'}</dd>
            <dt>宽限期</dt><dd>${l.grace_days ? esc(l.grace_days) + ' 天' : '无'}</dd>
            <dt>签发人</dt><dd>${esc(l.issued_by || '—')}</dd>
            <dt>下载次数</dt><dd>${l.download_count || 0}</dd>
            ${l.renew_from ? `<dt>续期自</dt><dd><a class="mono" href="#/licenses/${encodeURIComponent(l.renew_from)}">${esc(l.renew_from)}</a></dd>` : ''}
            ${l.revoked_at ? `<dt>吊销时间</dt><dd>${esc(fmtTime(l.revoked_at))}</dd>` : ''}
          </dl>
        </div>
      </div>

      <div class="card">
        <div class="card-head"><h3>校验要素</h3></div>
        <div class="card-body">
          <dl class="kv">
            <dt>机器绑定</dt><dd class="mono break-all">${l.machine_code === '*' ? '通用授权（machine_code = "*"）' : esc(l.machine_code)}</dd>
            <dt>功能配置</dt><dd><pre class="code-block light" style="margin:0;padding:0"><code style="display:block;padding:12px;font-family:var(--mono);font-size:12px;white-space:pre-wrap">${esc(prettyJSON(l.features))}</code></pre></dd>
            <dt>备注</dt><dd>${esc(l.note || '—')}</dd>
            <dt>存储状态</dt><dd>${esc(l.status)}</dd>
            <dt>展示状态</dt><dd>${esc(l.effective_status)}</dd>
          </dl>
        </div>
      </div>
    </div>

    <div class="card mt-16">
      <div class="card-head">
        <h3>授权文件内容预览</h3>
        <div class="row">
          <span class="hint">data 字段解码后的 payload</span>
          <button class="btn sm" data-copy-preview type="button">复制 JSON</button>
        </div>
      </div>
      <div class="card-body">
        <div class="code-block"><pre id="preview">${esc(detail.preview || '')}</pre></div>
      </div>
    </div>`;

  const previewText = detail.preview || '';
  bindCopyButtons(el);
  const copyBtn = el.querySelector('[data-copy-preview]');
  if (copyBtn) copyBtn.addEventListener('click', () => copyText(previewText, '授权内容已复制'));

  el.querySelector('#dl').addEventListener('click', async (ev) => {
    const btn = ev.currentTarget;
    btn.disabled = true;
    try {
      const name = await downloadLicense(l.id);
      toast('已下载 ' + name, 'ok');
      render(el, ctx, l.id);
    } catch (err) {
      toast(err.message, 'error');
    } finally {
      btn.disabled = false;
    }
  });

  el.querySelector('#renew').addEventListener('click', () => {
    openRenew(l, (created) => {
      if (created && created.id) ctx.navigate('#/licenses/' + encodeURIComponent(created.id));
      else render(el, ctx, l.id);
    });
  });

  const revokeBtn = el.querySelector('#revoke');
  if (revokeBtn) {
    revokeBtn.addEventListener('click', async () => {
      const ok = await confirmDialog({
        title: '吊销授权',
        message: `吊销后平台将阻止该授权继续下载与续期，但离线端无法感知。确认吊销「${l.customer}」的授权？`,
        confirmText: '确认吊销',
        danger: true,
      });
      if (!ok) return;
      try {
        const reason = '管理员在授权详情页吊销';
        await api.revokeLicense(l.id, reason);
        toast('授权已吊销', 'ok');
        render(el, ctx, l.id);
      } catch (err) {
        toast(err.message, 'error');
      }
    });
  }

  el.querySelector('#delete').addEventListener('click', async () => {
    const ok = await confirmDialog({
      title: '删除授权记录',
      message: '将删除平台内的授权记录与对应 .lic 文件，此操作不可恢复。建议确认客户已不再需要该文件。',
      confirmText: '永久删除',
      danger: true,
      requireText: l.id,
    });
    if (!ok) return;
    try {
      await api.deleteLicense(l.id);
      toast('授权记录已删除', 'ok');
      ctx.navigate('#/licenses');
    } catch (err) {
      toast(err.message, 'error');
    }
  });
}

function prettyJSON(text) {
  if (!text) return '{}';
  try { return JSON.stringify(JSON.parse(text), null, 2); } catch { return text; }
}
