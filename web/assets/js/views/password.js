// views/password.js — 管理员修改登录密码。

import { api, auth } from '../api.js';
import { toast, errorBox } from '../ui.js';

function strength(password) {
  let score = 0;
  if (password.length >= 10) score++;
  if (password.length >= 14) score++;
  if (/[a-z]/.test(password) && /[A-Z]/.test(password)) score++;
  if (/\d/.test(password)) score++;
  if (/[^A-Za-z0-9]/.test(password)) score++;
  return Math.min(score, 4);
}

const LABELS = ['很弱', '较弱', '一般', '较强', '很强'];

export async function render(el, ctx) {
  const mustChange = Boolean(ctx.me && ctx.me.must_change_password);

  el.innerHTML = `
    <div class="page-head">
      <div>
        <h2>修改密码</h2>
        <p>密码使用 bcrypt 加盐存储；修改后当前会话将自动续签</p>
      </div>
    </div>

    ${mustChange ? `<div class="alert warn" style="margin-bottom:16px">
      <span class="ico">!</span>
      <span><strong>首次登录必须修改初始密码。</strong>完成修改前，其他功能页面不可访问。</span>
    </div>` : ''}

    <div class="card" style="max-width:560px">
      <div class="card-body">
        <div id="pwd-alert"></div>
        <form class="form" id="pwd-form" autocomplete="off">
          <div class="field">
            <label>当前密码</label>
            <input class="input" type="password" name="old_password" autocomplete="current-password" required>
          </div>
          <div class="field">
            <label>新密码</label>
            <input class="input" type="password" name="new_password" autocomplete="new-password" minlength="10" maxlength="128" required>
            <div class="strength" id="pwd-strength" hidden>
              <div class="bar"><span></span></div>
              <div class="txt"></div>
            </div>
            <div class="hint">10~128 位，需同时包含字母与数字；建议混合大小写与符号。</div>
          </div>
          <div class="field">
            <label>确认新密码</label>
            <input class="input" type="password" name="confirm_password" autocomplete="new-password" required>
          </div>
          <div class="row gap-8">
            <button class="btn primary" type="submit" id="pwd-submit">保存新密码</button>
            ${mustChange ? '' : '<button class="btn" type="button" id="pwd-cancel">返回</button>'}
          </div>
        </form>
      </div>
    </div>`;

  const form = el.querySelector('#pwd-form');
  const alertBox = el.querySelector('#pwd-alert');
  const newInput = form.elements.new_password;
  const strengthBox = el.querySelector('#pwd-strength');
  const bar = strengthBox.querySelector('.bar span');
  const strengthText = strengthBox.querySelector('.txt');

  newInput.addEventListener('input', () => {
    if (!newInput.value) {
      strengthBox.hidden = true;
      return;
    }
    const score = strength(newInput.value);
    strengthBox.hidden = false;
    bar.style.width = ((score + 1) / 5) * 100 + '%';
    bar.className = 'level-' + score;
    strengthText.textContent = '强度：' + LABELS[score];
  });

  const cancel = el.querySelector('#pwd-cancel');
  if (cancel) cancel.addEventListener('click', () => ctx.navigate('#/dashboard'));

  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alertBox.innerHTML = '';
    const oldPassword = form.elements.old_password.value;
    const newPassword = newInput.value;
    const confirmPassword = form.elements.confirm_password.value;

    if (newPassword.length < 10 || newPassword.length > 128) {
      alertBox.innerHTML = errorBox({ message: '新密码长度需为 10~128 位' });
      return;
    }
    if (!/[A-Za-z]/.test(newPassword) || !/\d/.test(newPassword)) {
      alertBox.innerHTML = errorBox({ message: '新密码需同时包含字母与数字' });
      return;
    }
    if (newPassword !== confirmPassword) {
      alertBox.innerHTML = errorBox({ message: '两次输入的新密码不一致' });
      return;
    }
    if (newPassword === oldPassword) {
      alertBox.innerHTML = errorBox({ message: '新密码不得与当前密码相同' });
      return;
    }

    const btn = el.querySelector('#pwd-submit');
    btn.disabled = true;
    btn.innerHTML = '<span class="spinner"></span> 正在保存…';
    try {
      const res = await api.changePassword(oldPassword, newPassword);
      auth.session = { token: res.token, admin: res.admin };
      const admin = await ctx.reloadMe();
      toast('密码已更新', 'ok');
      if (admin && admin.must_change_password) {
        alertBox.innerHTML = errorBox({ message: '密码状态未正常更新，请重新登录' });
      } else {
        ctx.navigate('#/dashboard');
      }
    } catch (err) {
      alertBox.innerHTML = errorBox(err);
    } finally {
      btn.disabled = false;
      btn.textContent = '保存新密码';
    }
  });

  form.elements.old_password.focus();
}
