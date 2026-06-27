// Repos page: flat table of all repos with a silo-code filter.
// Columns: silo code, repo name (link to web URL), JDK, release branch, action.
// Branch and JDK are editable only for repos whose silo is within the user's scope.
const ReposPage = {
    _repos: [],

    _webURL(url) {
        if (!url) return '';
        const s = url.trim();
        if (/^https?:\/\//i.test(s)) return s.replace(/\.git$/, '');
        if (s.startsWith('ssh://git@codeup')) {
            const repo = s.split(':9022')[1].replace('.git', '');
            return 'http://codeup.devops.csdc.com/codeup' + repo;
        }
        if (s.startsWith('git@git')) {
            const repo = s.split(':')[1].replace('.git', '');
            return 'https://git.sz.chinaclear.cn/' + repo;
        }
        return '';
    },

    async render(container) {
        container.innerHTML = `
            <div class="fade-in">
                <div class="page-header">
                    <div>
                        <h1 class="page-title">仓库管理</h1>
                        <p class="page-subtitle">查看全量仓库；对有权限的竖井可配置 JDK 版本和发布分支</p>
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
                <th style="text-align:left;padding:10px 16px;width:80px;">JDK</th>
                <th style="text-align:left;padding:10px 16px;width:200px;">发布分支</th>
                <th style="text-align:left;padding:10px 16px;width:100px;">操作</th>
              </tr></thead><tbody>
        `;
        rows.forEach(r => {
            const jdk = r.jdk || '21';
            const jdkCell = r.can_edit
                ? `<select class="jdk-select" data-id="${r.id}"
                         style="box-sizing:border-box;height:32px;padding:4px 6px;border-radius:6px;
                                border:1px solid var(--border);background:var(--bg);color:var(--text);">
                    <option value="8" ${jdk === '8' ? 'selected' : ''}>8</option>
                    <option value="17" ${jdk === '17' ? 'selected' : ''}>17</option>
                    <option value="21" ${jdk === '21' ? 'selected' : ''}>21</option>
                   </select>`
                : `<span style="display:inline-flex;align-items:center;box-sizing:border-box;height:32px;
                          padding:6px 8px;border:1px solid transparent;color:var(--text);">${Utils.escapeHtml(jdk)}</span>`;
            const branchCell = r.can_edit
                ? `<input type="text" class="branch-input" data-id="${r.id}"
                         value="${Utils.escapeHtml(r.release_branch || '')}"
                         placeholder="如 release/2025Q2"
                         style="box-sizing:border-box;height:32px;padding:6px 8px;border-radius:6px;
                                border:1px solid var(--border);background:var(--bg);color:var(--text);width:180px;"/>`
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
                  <td style="padding:10px 16px;">${jdkCell}</td>
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
        const branchInput = document.querySelector(`.branch-input[data-id="${repoId}"]`);
        const jdkSelect = document.querySelector(`.jdk-select[data-id="${repoId}"]`);
        const branch = branchInput ? branchInput.value.trim() : '';
        const jdk = jdkSelect ? jdkSelect.value : '21';

        if (!branch) {
            alert('发布分支不能为空');
            return;
        }
        const original = btn.textContent;
        btn.disabled = true;
        btn.textContent = '保存中...';
        try {
            await API.updateRepoBranch(repoId, branch);
            await API.updateRepoJDK(repoId, jdk);
            const r = this._repos.find(x => x.id === repoId);
            if (r) { r.release_branch = branch; r.jdk = jdk; }
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
