// Repos page: flat table of all repos with a silo-code filter.
// Columns: silo code, repo name (link to web URL), release branch, action.
// Branch is editable only for repos whose silo is within the user's scope.
const ReposPage = {
    _repos: [],

    // Convert a git SSH URL to a browser-openable HTTPS URL.
    // ssh://git@host:9022/group/sub/name.git -> https://host/group/sub/name
    // git@host:group/name.git                -> https://host/group/name
    _webURL(url) {
        if (!url) return '';
        let s = url.trim();
        if (/^https?:\/\//i.test(s)) return s.replace(/\.git$/, '');
        s = s.replace(/^ssh:\/\//i, '');           // drop ssh:// scheme
        s = s.replace(/^git@/i, '');               // drop git@ user
        s = s.replace(/\.git$/i, '');              // drop .git suffix
        // scp-style "host:group/name" -> "host/group/name"
        if (!s.includes('/') && s.includes(':')) {
            s = s.replace(':', '/');
        } else {
            s = s.replace(/^([^/]+):(\d+)\//, '$1/'); // drop :port before path
            s = s.replace(/^([^/]+):/, '$1/');        // scp form with path
        }
        return 'https://' + s;
    },

    async render(container) {
        container.innerHTML = `
            <div class="fade-in">
                <div class="page-header">
                    <div>
                        <h1 class="page-title">仓库管理</h1>
                        <p class="page-subtitle">查看全量仓库；对有权限的竖井可配置发布分支</p>
                    </div>
                </div>
                <div style="margin-bottom:12px;">
                    <input type="text" id="repo-filter" placeholder="按竖井代码过滤…"
                        style="padding:8px 12px;border-radius:6px;border:1px solid var(--border);
                               background:var(--bg);color:var(--text);width:240px;"/>
                </div>
                <div id="repos-content">
                    <div style="text-align:center;padding:40px;"><span class="spinner"></span></div>
                </div>
            </div>
        `;
        const filterEl = document.getElementById('repo-filter');
        filterEl.addEventListener('input', () => this._renderTable(filterEl.value.trim()));
        try {
            const data = await API.getRepos();
            this._repos = data.repos || [];
            this._renderTable('');
        } catch (e) {
            document.getElementById('repos-content').innerHTML =
                `<div class="empty-state"><p class="empty-state-text">加载失败: ${Utils.escapeHtml(e.message)}</p></div>`;
        }
    },

    _renderTable(filter) {
        const q = filter.toLowerCase();
        const rows = this._repos.filter(r => !q || (r.silo_id || '').toLowerCase().includes(q));

        if (!rows.length) {
            document.getElementById('repos-content').innerHTML =
                `<div class="empty-state"><p class="empty-state-text">无匹配仓库</p></div>`;
            return;
        }

        let html = `
            <div class="card" style="padding:0;overflow:hidden;">
            <table class="data-table" style="width:100%;border-collapse:collapse;">
              <thead><tr>
                <th style="text-align:left;padding:10px 16px;width:120px;">竖井代码</th>
                <th style="text-align:left;padding:10px 16px;">仓库</th>
                <th style="text-align:left;padding:10px 16px;width:220px;">发布分支</th>
                <th style="text-align:left;padding:10px 16px;width:90px;">操作</th>
              </tr></thead><tbody>
        `;
        rows.forEach(r => {
            const branchCell = r.can_edit
                ? `<input type="text" class="branch-input" data-id="${r.id}"
                         value="${Utils.escapeHtml(r.release_branch || '')}"
                         placeholder="如 release/2025Q2"
                         style="box-sizing:border-box;height:32px;padding:6px 8px;border-radius:6px;
                                border:1px solid var(--border);background:var(--bg);color:var(--text);width:200px;"/>`
                : `<span style="display:inline-flex;align-items:center;box-sizing:border-box;height:32px;
                          padding:6px 8px;border:1px solid transparent;color:var(--text);">${Utils.escapeHtml(r.release_branch || '-')}</span>`;
            const actionCell = r.can_edit
                ? `<button class="btn btn-primary btn-sm" data-id="${r.id}">保存</button>`
                : `<span style="font-size:12px;color:var(--text-dim);">无权限</span>`;
            const web = this._webURL(r.url);
            const nameCell = web
                ? `<a href="${Utils.escapeHtml(web)}" target="_blank" rel="noopener noreferrer"
                      style="color:var(--primary);text-decoration:none;">${Utils.escapeHtml(r.name)} &#8599;</a>`
                : Utils.escapeHtml(r.name);
            html += `
                <tr style="border-top:1px solid var(--border);">
                  <td style="padding:10px 16px;">${Utils.escapeHtml(r.silo_id || '')}</td>
                  <td style="padding:10px 16px;">${nameCell}</td>
                  <td style="padding:10px 16px;">${branchCell}</td>
                  <td style="padding:10px 16px;">${actionCell}</td>
                </tr>
            `;
        });
        html += '</tbody></table></div>';
        document.getElementById('repos-content').innerHTML = html;

        document.querySelectorAll('#repos-content button[data-id]').forEach(btn => {
            btn.onclick = () => this._save(btn.dataset.id, btn);
        });
    },

    async _save(repoId, btn) {
        const input = document.querySelector(`.branch-input[data-id="${repoId}"]`);
        const branch = input ? input.value.trim() : '';
        if (!branch) {
            alert('发布分支不能为空');
            return;
        }
        const original = btn.textContent;
        btn.disabled = true;
        btn.textContent = '保存中...';
        try {
            await API.updateRepoBranch(repoId, branch);
            // keep local copy in sync so filtering doesn't revert the edit
            const r = this._repos.find(x => x.id === repoId);
            if (r) r.release_branch = branch;
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
