// Version Confirmation Page — version is repo-level (one tag per repo)
const VersionConfirmPage = {
    plan: null,
    editedRepoVersions: {}, // repo_id -> version
    collapsedGroups: new Set(),

    async render(container, planId) {
        container.innerHTML = `
            <div class="fade-in">
                <div class="page-header">
                    <div>
                        <h1 class="page-title">版本确认</h1>
                        <p class="page-subtitle">Plan: ${planId} — 版本号以仓库为粒度，同仓库内模块共享同一 Tag</p>
                    </div>
                    <div style="display:flex;gap:8px;">
                        <button class="btn btn-ghost" onclick="location.hash='#/'">返回</button>
                        <button class="btn btn-success btn-lg" id="confirm-btn">
                            确认并开始发版
                        </button>
                    </div>
                </div>

                <div class="stats-bar" id="version-stats"></div>

                <div class="search-box">
                    <span class="search-icon">&#128269;</span>
                    <input type="text" id="module-search" placeholder="搜索仓库或模块...">
                </div>

                <div id="version-groups"></div>
            </div>
        `;

        await this._loadPlan(planId);
        this._bindEvents(container, planId);
    },

    async _loadPlan(planId) {
        try {
            this.plan = await API.getPlan(planId);
            this.editedRepoVersions = {};
            this._renderAll();
        } catch (err) {
            console.error('Failed to load plan:', err);
        }
    },

    _renderAll(filter) {
        if (!this.plan) return;

        const modules = this.plan.modules || [];

        // Count unique repos and overridden repos
        const repoSet = new Set();
        const overriddenRepos = new Set();
        modules.forEach(m => {
            repoSet.add(m.repo_id);
            if (m.is_overridden || this.editedRepoVersions[m.repo_id]) {
                overriddenRepos.add(m.repo_id);
            }
        });

        const stats = document.getElementById('version-stats');
        stats.innerHTML = `
            <div class="stat-item stat-total"><span class="stat-value">${repoSet.size}</span><span class="stat-label">仓库</span></div>
            <div class="stat-item stat-total"><span class="stat-value">${modules.length}</span><span class="stat-label">模块</span></div>
            <div class="stat-item"><span class="stat-value" style="color:var(--warning);">${overriddenRepos.size}</span><span class="stat-label">已修改</span></div>
            <div class="stat-item"><span class="stat-value" style="color:var(--text-dim);">${this.plan.concurrency}</span><span class="stat-label">并发数</span></div>
            <div class="stat-item"><span class="stat-value" style="color:var(--text-dim);">${this.plan.failure_strategy}</span><span class="stat-label">失败策略</span></div>
        `;

        // Group by silo -> repo
        const grouped = {};
        modules.forEach(m => {
            const siloKey = m.silo_name || m.silo_id;
            const repoKey = m.repo_name || m.repo_id;
            const key = `${siloKey}|||${repoKey}|||${m.repo_id}`;
            if (!grouped[key]) grouped[key] = { silo: siloKey, repo: repoKey, repoId: m.repo_id, modules: [] };

            const matchesFilter = !filter ||
                m.module_name.toLowerCase().includes(filter.toLowerCase()) ||
                repoKey.toLowerCase().includes(filter.toLowerCase());
            if (matchesFilter) {
                grouped[key].modules.push(m);
            }
        });

        const groupsContainer = document.getElementById('version-groups');
        let html = '';

        Object.entries(grouped).forEach(([key, group]) => {
            if (group.modules.length === 0) return;
            const isCollapsed = this.collapsedGroups.has(key);

            // Repo-level version (all modules share the same)
            const firstMod = group.modules[0];
            const prevVersion = firstMod.prev_version;
            const editedVersion = this.editedRepoVersions[group.repoId];
            const targetVersion = editedVersion !== undefined ? editedVersion : firstMod.target_version;
            const isOverridden = firstMod.is_overridden || editedVersion !== undefined;

            html += `
                <div class="card" style="padding:0;overflow:hidden;">
                    <div class="group-header" data-group="${key}">
                        <span class="group-toggle ${isCollapsed ? '' : 'open'}">&#9654;</span>
                        <span class="group-name">${group.silo} / ${group.repo}</span>
                        <span class="group-meta" style="display:flex;align-items:center;gap:12px;">
                            <code style="color:var(--text-dim);font-size:12px;">${prevVersion}</code>
                            <span class="version-arrow">&rarr;</span>
                            <input class="version-input ${isOverridden ? 'version-overridden' : ''}"
                                data-repo-id="${group.repoId}"
                                value="${targetVersion}"
                                onclick="event.stopPropagation()"
                                title="修改此仓库的目标版本号">
                            ${isOverridden ? '<span style="color:var(--warning);font-size:11px;">已修改</span>' : ''}
                            <span style="color:var(--text-dim);font-size:12px;">${group.modules.length} 模块</span>
                        </span>
                    </div>
                    <div class="group-body ${isCollapsed ? 'collapsed' : ''}" data-group-body="${key}" ${isCollapsed ? 'style="max-height:0;"' : 'style="max-height:2000px;"'}>
                        <table>
                            <thead><tr>
                                <th style="width:50%;">模块名称</th>
                                <th>当前版本</th>
                                <th></th>
                                <th>目标版本</th>
                            </tr></thead>
                            <tbody>
            `;

            group.modules.forEach(m => {
                const modTarget = editedVersion !== undefined ? editedVersion : m.target_version;
                html += `
                    <tr>
                        <td style="font-family:var(--mono);font-size:12px;">${m.module_name}</td>
                        <td><code style="color:var(--text-dim);">${m.prev_version}</code></td>
                        <td class="version-arrow">&rarr;</td>
                        <td><code style="color:${isOverridden ? 'var(--warning)' : 'var(--success)'};">${modTarget}</code></td>
                    </tr>
                `;
            });

            html += '</tbody></table></div></div>';
        });

        groupsContainer.innerHTML = html || '<div class="empty-state"><p class="empty-state-text">没有匹配的仓库或模块</p></div>';
    },

    _bindEvents(container, planId) {
        // Search
        container.querySelector('#module-search').addEventListener('input', Utils.debounce((e) => {
            this._renderAll(e.target.value.trim());
        }, 200));

        // Collapse/expand groups
        container.addEventListener('click', (e) => {
            const header = e.target.closest('.group-header');
            if (!header || e.target.classList.contains('version-input')) return;
            const key = header.dataset.group;
            const body = container.querySelector(`[data-group-body="${key}"]`);
            const toggle = header.querySelector('.group-toggle');

            if (this.collapsedGroups.has(key)) {
                this.collapsedGroups.delete(key);
                body.style.maxHeight = '2000px';
                body.classList.remove('collapsed');
                toggle.classList.add('open');
            } else {
                this.collapsedGroups.add(key);
                body.style.maxHeight = '0';
                body.classList.add('collapsed');
                toggle.classList.remove('open');
            }
        });

        // Version editing at repo level
        container.addEventListener('change', (e) => {
            if (!e.target.classList.contains('version-input')) return;
            const repoId = e.target.dataset.repoId;
            const newVersion = e.target.value.trim();
            if (newVersion && repoId) {
                this.editedRepoVersions[repoId] = newVersion;
                // Re-render to update all module rows under this repo
                const searchVal = container.querySelector('#module-search')?.value?.trim() || '';
                this._renderAll(searchVal);
            }
        });

        // Confirm
        container.querySelector('#confirm-btn').addEventListener('click', async () => {
            const btn = container.querySelector('#confirm-btn');
            btn.disabled = true;
            btn.innerHTML = '<span class="spinner"></span> 确认中...';

            try {
                // Save any edited repo versions (keyed by repo_id)
                if (Object.keys(this.editedRepoVersions).length > 0) {
                    await API.updateVersions(planId, this.editedRepoVersions);
                }

                // Confirm plan
                await API.confirmPlan(planId);

                // Execute
                await API.executePlan(planId);

                // Navigate to monitor
                window.location.hash = `#/plan/${planId}/monitor`;
            } catch (err) {
                alert('操作失败: ' + err.message);
                btn.disabled = false;
                btn.textContent = '确认并开始发版';
            }
        });
    },
};
