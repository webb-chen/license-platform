// views/issue.js — 授权签发。

import { api, downloadLicense } from '../api.js';
import {
  esc, loading, toast, openModal, errorBox, parseFeatures, toLocalInput, fmtTime,
} from '../ui.js';

export async function render(el, ctx) {
  el.replaceChildren(loading());
  const [projectsRes] = await Promise.all([api.projects()]);
  const projects = (projectsRes.items || []).filter((p) => p.status === 'active');
  const preset = ctx.query ? ctx.query.get('project') : '';

  if (!projects.length) {
    el.innerHTML = `
      <div class="page-head"><div><h2>授权签发</h2><p>创建授权文件并下发给客户</p></div></div>
      <div class="card"><div class="empty">
        <div class="big">没有可用的项目</div>
        <div>请先在「项目管理」中新增并启用项目，系统会自动生成密钥对。</div>
        <div class="mt-16"><a class="btn primary" href="#/projects">前往项目管理</a></div>
      </div></div>`;
    return;
  }

  const selected = projects.some((p) => p.id === preset) ? preset : projects[0].id;
  const minDate = toLocalInput(new Date(Date.now() + 60 * 1000));

  el.innerHTML = `
    <div class="page-head">
      <div>
        <h2>授权签发</h2>
        <p>选择项目与到期策略，签发后立即生成可下载的 <span class="mono">.lic</span> 文件</p>
      </div>
    </div>

    <div class="grid" style="grid-template-columns:minmax(0,1.7fr) minmax(0,1fr);align-items:start">
      <div class="card">
        <div class="card-head"><h3>授权信息</h3><span class="hint">带 * 为必填</span></div>
        <div class="card-body">
          <div id="issue-alert"></div>
          <form class="form" id="issue-form" autocomplete="off">
            <div class="form-row">
              <div class="field">
                <label>目标项目<span class="required">*</span></label>
                <select class="select" name="project">
                  ${projects.map((p) => `<option value="${esc(p.id)}" ${p.id === selected ? 'selected' : ''}>${esc(p.name)}（${esc(p.id)}）</option>`).join('')}
                </select>
                <span class="desc">授权中的 product 由项目配置决定</span>
              </div>
              <div class="field">
                <label>客户名称<span class="required">*</span></label>
                <input class="input" name="customer" placeholder="例如 某某科技有限公司" maxlength="100" required>
              </div>
            </div>

            <div class="form-row">
              <div class="field">
                <label>联系人 / 联系方式</label>
                <input class="input" name="customer_contact" placeholder="例如 张经理 / 138****0000" maxlength="200">
              </div>
              <div class="field">
                <label>实例 ID</label>
                <input class="input mono" name="instance_id" placeholder="可选，用于区分同客户的多个部署">
              </div>
            </div>

            <div class="field">
              <label>到期模式<span class="required">*</span></label>
              <div class="radio-cards" id="expiry-modes">
                <label class="radio-card">
                  <input type="radio" name="expiry_mode" value="date" checked>
                  <strong>指定到期时间</strong>
                  <span>精确到分钟</span>
                </label>
                <label class="radio-card">
                  <input type="radio" name="expiry_mode" value="days">
                  <strong>自定义天数</strong>
                  <span>从签发时刻起算</span>
                </label>
                <label class="radio-card">
                  <input type="radio" name="expiry_mode" value="permanent">
                  <strong>永久授权</strong>
                  <span>expire_time 为 null</span>
                </label>
              </div>
            </div>

            <div id="expiry-date-field" class="field">
              <label>到期时间<span class="required">*</span></label>
              <input class="input" type="datetime-local" name="expire_at" min="${minDate}" value="${toLocalInput(new Date(Date.now() + 365 * 86400000))}">
              <span class="desc">按浏览器本地时区填写，提交时转换为标准 RFC3339（UTC）</span>
            </div>

            <div id="expiry-days-field" class="field" style="display:none">
              <label>有效天数<span class="required">*</span></label>
              <input class="input" type="number" name="days" min="1" max="36500" value="365">
              <span class="desc">1~36500 天，从签发时刻起算</span>
            </div>

            <div class="form-row">
              <div class="field">
                <label>宽限期（天）</label>
                <input class="input" type="number" name="grace_days" min="0" max="3650" value="0">
                <span class="desc">到期后仍返回 license_grace 的天数，0 表示无宽限</span>
              </div>
              <div class="field">
                <label>机器绑定</label>
                <div class="radio-cards" style="grid-template-columns:1fr 1fr;margin-top:2px">
                  <label class="radio-card">
                    <input type="radio" name="machine_mode" value="any" checked>
                    <strong>通用授权</strong>
                    <span>machine_code = "*"</span>
                  </label>
                  <label class="radio-card">
                    <input type="radio" name="machine_mode" value="custom">
                    <strong>绑定机器</strong>
                    <span>32 位 hex 机器码</span>
                  </label>
                </div>
              </div>
            </div>

            <div class="field" id="machine-field" style="display:none">
              <label>机器码<span class="required">*</span></label>
              <input class="input mono" name="machine_code" placeholder="32 位小写十六进制，例如 3f2a…" maxlength="32">
              <span class="desc">由目标系统 license 模块计算并展示：SHA-256("ULP-LICENSE-V1:" + machine-id) 前 16 字节</span>
            </div>

            <div class="field">
              <label>功能配置（JSON 对象）</label>
              <textarea class="textarea mono" name="features" rows="4" placeholder='{"modules":["report","export"],"max_users":50}'>{}</textarea>
              <span class="desc">可选，写入 payload.features；需为 JSON 对象且小于 8KB</span>
            </div>

            <div class="field">
              <label>备注</label>
              <textarea class="textarea" name="note" rows="3" maxlength="2000" placeholder="合同编号、销售负责人等内部备注，不写入授权文件"></textarea>
            </div>

            <div class="row" style="justify-content:flex-end">
              <button class="btn primary" type="submit" id="issue-submit">签发并生成授权文件</button>
            </div>
          </form>
        </div>
      </div>

      <div>
        <div class="card">
          <div class="card-head"><h3>签发说明</h3></div>
          <div class="card-body" style="font-size:12.5px;color:var(--muted);line-height:1.75">
            <p class="mt-0">授权文件为 JSON 信封：<span class="mono">{data, sign}</span>。data 是 payload 的标准 base64，sign 是对 data 原始字节的 RSA-SHA256 签名。</p>
            <p>到期时间以 UTC 存储；离线端按本地时钟校验，并带 10 分钟回拨容差。</p>
            <p>宽限期不改变 <span class="mono">expire_time</span>，只在到期后提供缓冲并返回 <span class="mono">license_grace</span>。</p>
            <p style="margin-bottom:0">签发完成后请立即下载并交付客户；私钥不会出现在下载文件中。</p>
          </div>
        </div>
        <div class="card">
          <div class="card-head"><h3>机器码从哪来</h3></div>
          <div class="card-body" style="font-size:12.5px;color:var(--muted);line-height:1.75">
            <p class="mt-0">在目标服务器上调用 license 模块的 <span class="mono">MachineCode()</span>，或执行平台提供的验签 CLI：</p>
            <div class="code-block light"><pre>ulpd verify -machine-only</pre></div>
            <p class="mt-16" style="margin-bottom:0">绑定机器后，授权文件仅能在该机器通过校验；更换服务器需重新签发。</p>
          </div>
        </div>
      </div>
    </div>`;

  const form = el.querySelector('#issue-form');
  const alertBox = el.querySelector('#issue-alert');
  const dateField = el.querySelector('#expiry-date-field');
  const daysField = el.querySelector('#expiry-days-field');
  const machineField = el.querySelector('#machine-field');
  const submit = el.querySelector('#issue-submit');

  const syncExpiry = () => {
    const mode = form.querySelector('input[name="expiry_mode"]:checked').value;
    dateField.style.display = mode === 'date' ? '' : 'none';
    daysField.style.display = mode === 'days' ? '' : 'none';
  };
  const syncMachine = () => {
    const mode = form.querySelector('input[name="machine_mode"]:checked').value;
    machineField.style.display = mode === 'custom' ? '' : 'none';
  };
  form.querySelectorAll('input[name="expiry_mode"]').forEach((r) => r.addEventListener('change', syncExpiry));
  form.querySelectorAll('input[name="machine_mode"]').forEach((r) => r.addEventListener('change', syncMachine));

  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alertBox.innerHTML = '';
    const mode = form.querySelector('input[name="expiry_mode"]:checked').value;
    const machineMode = form.querySelector('input[name="machine_mode"]:checked').value;

    let features;
    try {
      features = parseFeatures(form.elements.features.value);
    } catch (err) {
      alertBox.innerHTML = errorBox({ message: err.message });
      return;
    }

    const payload = {
      project: form.elements.project.value,
      customer: form.elements.customer.value.trim(),
      customer_contact: form.elements.customer_contact.value.trim(),
      expiry_mode: mode,
      grace_days: Number(form.elements.grace_days.value || 0),
      machine_mode: machineMode,
      instance_id: form.elements.instance_id.value.trim(),
      features,
      note: form.elements.note.value.trim(),
    };
    if (!payload.customer) {
      alertBox.innerHTML = errorBox({ message: '请填写客户名称' });
      return;
    }
    if (mode === 'date') {
      const raw = form.elements.expire_at.value;
      if (!raw) {
        alertBox.innerHTML = errorBox({ message: '请选择到期时间' });
        return;
      }
      const d = new Date(raw);
      if (Number.isNaN(d.getTime()) || d.getTime() <= Date.now()) {
        alertBox.innerHTML = errorBox({ message: '到期时间必须晚于当前时间' });
        return;
      }
      payload.expire_at = d.toISOString();
    } else if (mode === 'days') {
      payload.days = Number(form.elements.days.value || 0);
      if (!payload.days || payload.days < 1) {
        alertBox.innerHTML = errorBox({ message: '有效天数至少为 1 天' });
        return;
      }
    }
    if (machineMode === 'custom') {
      payload.machine_code = form.elements.machine_code.value.trim().toLowerCase();
      if (!/^[0-9a-f]{32}$/.test(payload.machine_code)) {
        alertBox.innerHTML = errorBox({ message: '机器码需为 32 位小写十六进制字符串' });
        return;
      }
    }

    submit.disabled = true;
    submit.innerHTML = '<span class="spinner"></span> 正在签发…';
    try {
      const lic = await api.issueLicense(payload);
      showSuccess(el, ctx, lic);
    } catch (err) {
      alertBox.innerHTML = errorBox(err);
    } finally {
      submit.disabled = false;
      submit.textContent = '签发并生成授权文件';
    }
  });
}

function showSuccess(el, ctx, lic) {
  const ref = openModal({
    title: '授权签发成功',
    subtitle: '请立即下载授权文件并交付客户',
    body: `
      <div class="alert ok" style="margin-bottom:16px"><span class="ico">✓</span><span>授权文件已生成并加密保存到数据目录。</span></div>
      <dl class="kv">
        <dt>授权 ID</dt><dd class="mono break-all">${esc(lic.id)}</dd>
        <dt>项目</dt><dd>${esc(lic.project_name || lic.project_id)}</dd>
        <dt>客户</dt><dd>${esc(lic.customer)}</dd>
        <dt>到期时间</dt><dd>${lic.expire_time ? esc(fmtTime(lic.expire_time)) : '永久有效'}</dd>
        <dt>宽限期</dt><dd>${lic.grace_days ? lic.grace_days + ' 天' : '无'}</dd>
        <dt>机器绑定</dt><dd class="mono break-all">${esc(lic.machine_code)}</dd>
      </dl>`,
    footer: `<button class="btn" data-act="again" type="button">继续签发</button>
             <a class="btn" href="#/licenses/${encodeURIComponent(lic.id)}" data-act="detail">查看详情</a>
             <button class="btn primary" data-act="download" type="button">下载 .lic</button>`,
  });
  ref.root.querySelector('[data-act="again"]').addEventListener('click', () => {
    ref.close();
    render(el, ctx);
  });
  ref.root.querySelector('[data-act="detail"]').addEventListener('click', () => ref.close());
  ref.root.querySelector('[data-act="download"]').addEventListener('click', async (ev) => {
    const btn = ev.currentTarget;
    btn.disabled = true;
    try {
      const name = await downloadLicense(lic.id);
      toast('已下载 ' + name, 'ok');
    } catch (err) {
      toast(err.message, 'error');
    } finally {
      btn.disabled = false;
    }
  });
}
