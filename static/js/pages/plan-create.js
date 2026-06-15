// Plan Create Page
const PlanCreatePage = {
    silos: [],
    selectedSiloIds: new Set(),

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
        container.querySelector('#silo-grid').addEventListener('change', (e) => {
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
        });

        // Create plan
        container.querySelector('#create-plan-btn').addEventListener('click', () => this._createPlan());
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
