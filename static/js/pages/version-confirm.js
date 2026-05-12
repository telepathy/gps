// Version Confirmation Page
const VersionConfirmPage = {
    plan: null,
    editedVersions: {},
    collapsedGroups: new Set(),

    async render(container, planId) {
        container.innerHTML = `
            <div class="fade-in">
                <div class="page-header">
                    <div>
                        <h1 class="page-title">版本确认</h1>
                        <p class="page-subtitle">Plan: ${planId}</p>
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
                    <input type="text" id="module-search" placeholder="搜索模块...">
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
            this.editedVersions = {};
            this._renderAll();
        } catch (err) {
            console.error('Failed to load plan:', err);
        }
    },

    _renderAll(filter) {
        if (!this.plan) return;

        // Stats
        const stats = document.getElementById('version-stats');
        const modules = this.plan.modules || [];
        const overridden = modules.filter(m => m.is_overridden).length;
        stats.innerHTML = `
            <div class="stat-item stat-total"><span class="stat-value">${modules.length}</span><span class="stat-label">总模块</span></div>
            <div class="stat-item" style="--stat-color:var(--warning);"><span class="stat-value" style="color:var(--warning);">${overridden}</span><span class="stat-label">已修改</span></div>
            <div class="stat-item"><span class="stat-value" style="color:var(--text-dim);">${this.plan.concurrency}</span><span class="stat-label">并发数</span></div>
            <div class="stat-item"><span class="stat-value" style="color:var(--text-dim);">${this.plan.failure_strategy}</span><span class="stat-label">失败策略</span></div>
        `;

        // Group by silo -> repo
        const grouped = {};
        modules.forEach(m => {
            const siloKey = m.silo_name || m.silo_id;
            const repoKey = m.repo_name || m.repo_id;
            const key = `${siloKey}|||${repoKey}`;
            if (!grouped[key]) grouped[key] = { silo: siloKey, repo: repoKey, modules: [] };

            const matchesFilter = !filter || m.module_name.toLowerCase().includes(filter.toLowerCase());
            if (matchesFilter) {
                grouped[key].modules.push(m);
            }
        });

        const groupsContainer = document.getElementById('version-groups');
        let html = '';

        Object.entries(grouped).forEach(([key, group]) => {
            if (group.modules.length === 0) return;
            const isCollapsed = this.collapsedGroups.has(key);

            html += `
                <div class="card" style="padding:0;overflow:hidden;">
                    <div class="group-header" data-group="${key}">
                        <span class="group-toggle ${isCollapsed ? '' : 'open'}">&#9654;</span>
                        <span class="group-name">${group.silo} / ${group.repo}</span>
                        <span class="group-meta">${group.modules.length} 个模块</span>
                    </div>
                    <div class="group-body ${isCollapsed ? 'collapsed' : ''}" data-group-body="${key}" ${isCollapsed ? 'style="max-height:0;"' : 'style="max-height:2000px;"'}>
                        <table>
                            <thead><tr>
                                <th style="width:40%;">模块名称</th>
                                <th>当前版本</th>
                                <th></th>
                                <th>目标版本</th>
                                <th>状态</th>
                            </tr></thead>
                            <tbody>
            `;

            group.modules.forEach(m => {
                const edited = this.editedVersions[m.module_id];
                const targetVersion = edited !== undefined ? edited : m.target_version;
                const isOverridden = m.is_overridden || edited !== undefined;

                html += `
                    <tr>
                        <td style="font-family:var(--mono);font-size:12px;">${m.module_name}</td>
                        <td><code style="color:var(--text-dim);">${m.prev_version}</code></td>
                        <td class="version-arrow">&rarr;</td>
                        <td>
                            <input class="version-input ${isOverridden ? 'version-overridden' : ''}"
                                data-module-id="${m.module_id}"
                                value="${targetVersion}">
                        </td>
                        <td>${isOverridden ? '<span style="color:var(--warning);font-size:11px;">已修改</span>' : '<span style="color:var(--text-dim);font-size:11px;">自动</span>'}</td>
                    </tr>
                `;
            });

            html += '</tbody></table></div></div>';
        });

        groupsContainer.innerHTML = html || '<div class="empty-state"><p class="empty-state-text">没有匹配的模块</p></div>';
    },

    _bindEvents(container, planId) {
        // Search
        container.querySelector('#module-search').addEventListener('input', Utils.debounce((e) => {
            this._renderAll(e.target.value.trim());
        }, 200));

        // Collapse/expand groups
        container.addEventListener('click', (e) => {
            const header = e.target.closest('.group-header');
            if (!header) return;
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

        // Version editing
        container.addEventListener('change', (e) => {
            if (!e.target.classList.contains('version-input')) return;
            const moduleId = e.target.dataset.moduleId;
            const newVersion = e.target.value.trim();
            if (newVersion) {
                this.editedVersions[moduleId] = newVersion;
                e.target.classList.add('version-overridden');
            }
        });

        // Confirm
        container.querySelector('#confirm-btn').addEventListener('click', async () => {
            const btn = container.querySelector('#confirm-btn');
            btn.disabled = true;
            btn.innerHTML = '<span class="spinner"></span> 确认中...';

            try {
                // Save any edited versions
                if (Object.keys(this.editedVersions).length > 0) {
                    await API.updateVersions(planId, this.editedVersions);
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
