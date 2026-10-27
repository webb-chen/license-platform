// views/login.js — 管理员登录。

import { api, auth } from '../api.js';
import { esc, toast } from '../ui.js';

export function render(el, ctx) {
  document.title = '登录 · 统一授权平台';
  el.className = '';
  el.innerHTML = `
    <div class="login-page">
      <section class="login-brand">
        <div>
          <div class="brand-mark">
            <div class="boot-logo">ULP</div>
            <div>
              <div style="font-size:15px;font-weight:600;letter-spacing:.02em">统一授权平台</div>
              <div style="font-size:12px;color:#93c5fd">Unified License Platform</div>
            </div>
          </div>
          <h1 class="mt-24">一套密钥，<br>统一签发所有项目授权。</h1>
          <p>面向 AgentSystem、webb-api、virtual-employee、teable 等业务系统的离线授权签发与治理平台。私钥加密存储，授权文件离线可验。</p>
          <ul class="login-points">
            <li>每个项目独立 RSA-2048 密钥对，项目间完全隔离</li>
            <li>支持指定到期时间、自定义天数与永久授权</li>
            <li>完整签发记录、吊销、续期与操作审计</li>
          </ul>
        </div>
        <div class="login-foot">授权文件格式 v1 · 原始字节验签 · 私钥不离开服务端</div>
      </section>

      <section class="login-form-wrap">
        <div class="login-card">
          <h2>管理员登录</h2>
          <p class="sub">请使用平台管理员账号进入控制台</p>
          <div id="login-alert"></div>
          <form class="form" id="login-form" autocomplete="off">
            <div class="field">
              <label for="username">用户名</label>
              <input class="input" id="username" name="username" placeholder="请输入管理员用户名" autocomplete="username" required>
            </div>
            <div class="field">
              <label for="password">密码</label>
              <input class="input" id="password" name="password" type="password" placeholder="请输入密码" autocomplete="current-password" required>
            </div>
            <button class="btn primary block" type="submit" id="login-submit">登录控制台</button>
          </form>
          <div class="login-help">
            首次部署的管理员账号由 <span class="mono">ULP_ADMIN_USER</span> 与
            <span class="mono">ULP_ADMIN_PASSWORD</span> 注入，首次登录后必须修改密码。
          </div>
        </div>
      </section>
    </div>`;

  const form = el.querySelector('#login-form');
  const alertBox = el.querySelector('#login-alert');
  const submit = el.querySelector('#login-submit');
  const username = el.querySelector('#username');
  const password = el.querySelector('#password');
  username.focus();

  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    alertBox.innerHTML = '';
    submit.disabled = true;
    submit.innerHTML = '<span class="spinner"></span> 正在登录…';
    try {
      const res = await api.login(username.value.trim(), password.value);
      auth.session = { token: res.token, admin: res.admin };
      toast('登录成功', 'ok', 1800);
      await ctx.onLogin(res.admin);
    } catch (err) {
      alertBox.innerHTML = `<div class="alert danger"><span class="ico">!</span><span>${esc(err.message)}</span></div>`;
      submit.disabled = false;
      submit.textContent = '登录控制台';
      password.select();
    }
  });
}
