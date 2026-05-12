// Plan Create Page
const PlanCreatePage = {
    silos: [],
    selectedSiloIds: new Set(),
    loadedModules: [],

    async render(container) {
        container.innerHTML = `
            <div class="fade-in">
                <div class="page-header">
                    <div>
                        <h1 class="page-title">创建发版计划</h1>
                        <p class="page-subtitle">选择竖井并配置发版参数</p>
                    </div>
                </div>

                <div class="card">
                    <div class="card-header">
                        <h3 class="card-title">选择竖井 (Silo)</h3>
                        <span id="silo-count" style="font-size:13px;color:var(--text-dim);">已选: 0</span>
                    </div>
                    <div class="search-box">
                        <span class="search-icon">&#128269;</span>
                        <input type="text" id="silo-search" placeholder="搜索竖井...">
                    </div>
                    <div class="silo-grid" id="silo-grid"></div>
                </div>

                <div class="card">
                    <div class="card-header">
                        <h3 class="card-title">发版配置</h3>
                    </div>
                    <div class="form-row">
                        <div class="form-group">
                            <label class="form-label">DMS 依赖分支</label>
                            <input class="form-input" id="dms-branch" value="release/2025Q2" placeholder="如 release/2025Q2">
                        </div>
                        <div class="form-group">
                            <label class="form-label">并发数</label>
                            <select class="form-select" id="concurrency">
                                <option value="2">2</option>
                                <option value="4" selected>4</option>
                                <option value="8">8</option>
                                <option value="16">16</option>
                            </select>
                        </div>
                        <div class="form-group">
                            <label class="form-label">失败策略</label>
                            <select class="form-select" id="failure-strategy">
                                <option value="ABORT">ABORT (中止)</option>
                                <option value="SKIP">SKIP (跳过)</option>
                                <option value="RETRY">RETRY (重试)</option>
                            </select>
                        </div>
                        <div class="form-group">
                            <label class="form-label">最大重试次数</label>
                            <select class="form-select" id="max-retries">
                                <option value="1">1</option>
                                <option value="2">2</option>
                                <option value="3" selected>3</option>
                                <option value="5">5</option>
                            </select>
                        </div>
                    </div>
                </div>

                <div class="card" id="modules-preview" style="display:none;">
                    <div class="card-header">
                        <h3 class="card-title">模块预览</h3>
                        <span id="module-count" style="font-size:13px;color:var(--text-dim);"></span>
                    </div>
                    <div id="modules-table"></div>
                </div>

                <div style="text-align:right;margin-top:16px;">
                    <button class="btn btn-primary btn-lg" id="create-plan-btn" disabled>
                        创建发版计划
                    </button>
                </div>
            </div>
        `;

        this.selectedSiloIds = new Set();
        await this._loadSilos();
        this._bindEvents(container);
    },

    async _loadSilos() {
        try {
            const data = await API.getSilos();
            this.silos = data.silos || [];
            this._renderSiloGrid();
        } catch (err) {
            console.error('Failed to load silos:', err);
        }
    },

    _renderSiloGrid(filter) {
        const grid = document.getElementById('silo-grid');
        if (!grid) return;

        const filtered = filter
            ? this.silos.filter(s => s.name.includes(filter) || s.desc.includes(filter))
            : this.silos;

        grid.innerHTML = filtered.map(s => `
            <label class="silo-item ${this.selectedSiloIds.has(s.id) ? 'selected' : ''}" data-silo-id="${s.id}">
                <input type="checkbox" ${this.selectedSiloIds.has(s.id) ? 'checked' : ''}>
                <div>
                    <div style="font-weight:600;color:var(--text-bright);">${s.name}</div>
                    <div style="font-size:11px;color:var(--text-dim);">${s.desc}</div>
                </div>
            </label>
        `).join('');
    },

    _bindEvents(container) {
        // Silo search
        const searchInput = container.querySelector('#silo-search');
        searchInput.addEventListener('input', Utils.debounce((e) => {
            this._renderSiloGrid(e.target.value.trim());
        }, 200));

        // Silo selection
        container.querySelector('#silo-grid').addEventListener('change', async (e) => {
            const item = e.target.closest('.silo-item');
            if (!item) return;
            const siloId = item.dataset.siloId;

            if (e.target.checked) {
                this.selectedSiloIds.add(siloId);
                item.classList.add('selected');
            } else {
                this.selectedSiloIds.delete(siloId);
                item.classList.remove('selected');
            }

            document.getElementById('silo-count').textContent = `已选: ${this.selectedSiloIds.size}`;
            document.getElementById('create-plan-btn').disabled = this.selectedSiloIds.size === 0;

            await this._previewModules();
        });

        // Create plan
        container.querySelector('#create-plan-btn').addEventListener('click', () => this._createPlan());
    },

    async _previewModules() {
        const preview = document.getElementById('modules-preview');
        const table = document.getElementById('modules-table');

        if (this.selectedSiloIds.size === 0) {
            preview.style.display = 'none';
            return;
        }

        // Load repos and modules for selected silos
        const allModules = [];
        const repoMap = {};

        for (const siloId of this.selectedSiloIds) {
            try {
                const repoData = await API.getReposBySilo(siloId);
                const repos = repoData.repos || [];

                for (const repo of repos) {
                    repoMap[repo.id] = repo;
                    const modData = await API.getModulesByRepo(repo.id);
                    const mods = modData.modules || [];
                    mods.forEach(m => {
                        m._repoName = repo.name;
                        m._siloName = this.silos.find(s => s.id === siloId)?.name || siloId;
                    });
                    allModules.push(...mods);
                }
            } catch (err) {
                console.error('Error loading modules for silo', siloId, err);
            }
        }

        this.loadedModules = allModules;
        preview.style.display = 'block';
        document.getElementById('module-count').textContent = `${allModules.length} 个模块`;

        // Group by silo -> repo
        const grouped = {};
        allModules.forEach(m => {
            const key = `${m._siloName} / ${m._repoName}`;
            if (!grouped[key]) grouped[key] = [];
            grouped[key].push(m);
        });

        let html = '<div class="table-wrap"><table><thead><tr>';
        html += '<th>竖井 / 仓库</th><th>模块</th><th>当前版本</th><th>目标版本</th>';
        html += '</tr></thead><tbody>';

        Object.entries(grouped).forEach(([group, mods]) => {
            mods.forEach((m, i) => {
                const target = this._bumpPatch(m.current_version);
                html += `<tr>`;
                if (i === 0) {
                    html += `<td rowspan="${mods.length}" style="font-weight:600;color:var(--text-bright);vertical-align:top;">${group}</td>`;
                }
                html += `<td>${m.name}</td>`;
                html += `<td><code style="color:var(--text-dim);">${m.current_version}</code></td>`;
                html += `<td><code style="color:var(--success);">${target}</code></td>`;
                html += `</tr>`;
            });
        });

        html += '</tbody></table></div>';
        table.innerHTML = html;
    },

    _bumpPatch(version) {
        const parts = version.split('.');
        if (parts.length !== 3) return version;
        parts[2] = parseInt(parts[2]) + 1;
        return parts.join('.');
    },

    async _createPlan() {
        const btn = document.getElementById('create-plan-btn');
        btn.disabled = true;
        btn.innerHTML = '<span class="spinner"></span> 创建中...';

        try {
            const plan = await API.createPlan({
                silo_ids: Array.from(this.selectedSiloIds),
                dms_branch: document.getElementById('dms-branch').value,
                concurrency: parseInt(document.getElementById('concurrency').value),
                failure_strategy: document.getElementById('failure-strategy').value,
                max_retries: parseInt(document.getElementById('max-retries').value),
            });

            // Navigate to version confirmation
            window.location.hash = `#/plan/${plan.id}/confirm`;
        } catch (err) {
            alert('创建失败: ' + err.message);
            btn.disabled = false;
            btn.textContent = '创建发版计划';
        }
    },
};
