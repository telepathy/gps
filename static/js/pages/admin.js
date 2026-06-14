// Admin page: user role & silo-scope management
const AdminPage = {
    _roles: [],

    async render(container) {
        container.innerHTML = `
            <div class="fade-in">
                <div class="page-header">
                    <div>
                        <h1 class="page-title">权限管理</h1>
                        <p class="page-subtitle">管理用户角色与竖井访问范围</p>
                    </div>
                </div>
                <div class="card" style="margin-bottom:16px;">
                    <h3 style="margin:0 0 8px;font-size:15px;">导入用户</h3>
                    <p style="margin:0 0 10px;font-size:12px;color:var(--text-dim);">
                        身份仍由 GitLab SSO 管理。此处预注册用户名，用户首次通过 GitLab 登录时自动绑定并保留这里设置的角色。
                        每行一个用户名，可选 <code>用户名,角色,竖井</code>（如 <code>zhangsan,releaser,silo-001</code>）。
                    </p>
                    <textarea id="import-text" rows="4" placeholder="zhangsan&#10;lisi,releaser,*&#10;wangwu,viewer"
                        style="width:100%;box-sizing:border-box;padding:8px;border-radius:6px;border:1px solid var(--border);
                               background:var(--bg);color:var(--text);font-family:monospace;font-size:13px;"></textarea>
                    <div style="margin-top:10px;display:flex;align-items:center;gap:12px;">
                        <button class="btn btn-primary btn-sm" id="import-btn">导入</button>
                        <span id="import-result" style="font-size:13px;color:var(--text-dim);"></span>
                    </div>
                </div>
                <div id="users-table">
                    <div style="text-align:center;padding:40px;"><span class="spinner"></span></div>
                </div>
            </div>
        `;
        document.getElementById('import-btn').onclick = () => this._import();
        await this._reload();
    },

    async _reload() {
        try {
            const [usersResp, rolesResp] = await Promise.all([API.getUsers(), API.getRoles()]);
            this._roles = rolesResp.roles || [];
            this._renderTable(usersResp.users || []);
        } catch (e) {
            document.getElementById('users-table').innerHTML =
                `<div class="empty-state"><p class="empty-state-text">加载失败: ${Utils.escapeHtml(e.message)}</p></div>`;
        }
    },

    // Parse textarea: one user per line, comma-separated "username,role,silo".
    _parseImport(text) {
        const users = [];
        text.split('\n').forEach(line => {
            const parts = line.split(',').map(s => s.trim());
            const username = parts[0];
            if (!username) return;
            const entry = { username };
            if (parts[1]) entry.roles = [parts[1]];
            if (parts[2] !== undefined && parts[2] !== '') entry.allowed_silos = parts[2];
            users.push(entry);
        });
        return users;
    },

    async _import() {
        const text = document.getElementById('import-text').value;
        const users = this._parseImport(text);
        const resultEl = document.getElementById('import-result');
        if (!users.length) {
            resultEl.textContent = '请输入至少一个用户名';
            return;
        }
        const btn = document.getElementById('import-btn');
        btn.disabled = true;
        btn.textContent = '导入中...';
        try {
            const res = await API.importUsers(users);
            const failed = Object.keys(res.failed || {});
            let msg = `新增 ${res.created.length}，跳过 ${res.skipped.length}`;
            if (failed.length) msg += `，失败 ${failed.length} (${failed.join(', ')})`;
            resultEl.textContent = msg;
            document.getElementById('import-text').value = '';
            await this._reload();
        } catch (e) {
            resultEl.textContent = '导入失败: ' + e.message;
        } finally {
            btn.disabled = false;
            btn.textContent = '导入';
        }
    },

    _renderTable(users) {
        const roleNames = this._roles.map(r => r.name);
        let html = `
            <div class="card" style="padding:0;overflow:hidden;">
            <table class="data-table" style="width:100%;border-collapse:collapse;">
              <thead><tr>
                <th style="text-align:left;padding:12px 16px;">ID</th>
                <th style="text-align:left;padding:12px 16px;">用户名</th>
                <th style="text-align:left;padding:12px 16px;">角色</th>
                <th style="text-align:left;padding:12px 16px;">允许的竖井 (* 或逗号分隔)</th>
                <th style="text-align:left;padding:12px 16px;">操作</th>
              </tr></thead><tbody>
        `;
        users.forEach(u => {
            const checks = roleNames.map(rn => {
                const checked = (u.roles || []).includes(rn) ? 'checked' : '';
                return `<label style="margin-right:10px;font-size:13px;">
                    <input type="checkbox" data-uid="${u.id}" class="role-chk" value="${rn}" ${checked}/> ${rn}
                </label>`;
            }).join('');
            html += `
                <tr style="border-top:1px solid var(--border);">
                  <td style="padding:12px 16px;">${u.id}</td>
                  <td style="padding:12px 16px;">${Utils.escapeHtml(u.username)}</td>
                  <td style="padding:12px 16px;">${checks}</td>
                  <td style="padding:12px 16px;">
                    <input type="text" class="silo-input" data-uid="${u.id}"
                           value="${Utils.escapeHtml(u.allowed_silos || '')}"
                           placeholder="* 或 silo-001,silo-002"
                           style="padding:6px 8px;border-radius:6px;border:1px solid var(--border);
                                  background:var(--bg);color:var(--text);width:200px;"/>
                  </td>
                  <td style="padding:12px 16px;">
                    <button class="btn btn-primary btn-sm" data-uid="${u.id}" data-username="${Utils.escapeHtml(u.username)}">保存</button>
                  </td>
                </tr>
            `;
        });
        html += '</tbody></table></div>';
        document.getElementById('users-table').innerHTML = html;

        document.querySelectorAll('#users-table button[data-uid]').forEach(btn => {
            btn.onclick = () => this._save(parseInt(btn.dataset.uid, 10), btn);
        });
    },

    async _save(uid, btn) {
        const roles = Array.from(document.querySelectorAll(`.role-chk[data-uid="${uid}"]:checked`))
            .map(cb => cb.value);
        const siloInput = document.querySelector(`.silo-input[data-uid="${uid}"]`);
        const allowedSilos = siloInput ? siloInput.value.trim() : '';
        const original = btn.textContent;
        btn.disabled = true;
        btn.textContent = '保存中...';
        try {
            await API.setUserRoles(uid, roles);
            await API.setUserAccess(uid, allowedSilos);
            btn.textContent = '已保存';
            setTimeout(() => { btn.textContent = original; btn.disabled = false; }, 1200);
        } catch (e) {
            btn.textContent = '失败';
            alert('保存失败: ' + e.message);
            btn.disabled = false;
            setTimeout(() => { btn.textContent = original; }, 1200);
        }
    },

    destroy() {},
};
