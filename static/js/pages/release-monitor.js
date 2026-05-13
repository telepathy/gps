// Release Monitor Page
const ReleaseMonitorPage = {
    plan: null,
    dagGraph: null,
    logPanel: null,
    eventSource: null,
    pollTimer: null,
    selectedModuleId: null,
    moduleStatusCache: {}, // track latest status per module from SSE

    async render(container, planId) {
        container.innerHTML = `
            <div class="fade-in">
                <div class="page-header">
                    <div>
                        <h1 class="page-title">发版监控</h1>
                        <p class="page-subtitle">Plan: ${planId}</p>
                    </div>
                    <div style="display:flex;gap:8px;">
                        <button class="btn btn-ghost" onclick="location.hash='#/history'">返回历史</button>
                        <button class="btn btn-danger" id="abort-btn" style="display:none;">
                            中止发布
                        </button>
                    </div>
                </div>

                <div class="card" style="padding:12px 20px;">
                    <div class="phase-stepper" id="phase-stepper"></div>
                </div>

                <div class="progress-bar" id="progress-bar" style="margin-bottom:16px;"></div>

                <div class="stats-bar" id="monitor-stats"></div>

                <div class="monitor-layout">
                    <div class="monitor-left">
                        <div class="dag-container" id="dag-container"></div>
                    </div>
                    <div class="monitor-right">
                        <div id="module-detail" class="module-detail" style="display:none;"></div>
                        <div id="log-container" style="flex:1;display:flex;flex-direction:column;"></div>
                    </div>
                </div>
            </div>
        `;

        this.planId = planId;
        this.selectedModuleId = null;
        this.moduleStatusCache = {};

        // Init components
        this.dagGraph = new DagGraph(document.getElementById('dag-container'));
        this.dagGraph.onNodeClick = (id, mod) => {
            if (id) {
                this._selectModule(id);
            } else {
                // Deselected (clicked same node again or empty space)
                this.selectedModuleId = null;
                this.logPanel.clearFilter();
                const detail = document.getElementById('module-detail');
                if (detail) detail.style.display = 'none';
            }
        };

        this.logPanel = new LogPanel(document.getElementById('log-container'));

        await this._loadPlan(planId);
        this._bindEvents(container, planId);
    },

    async _loadPlan(planId) {
        try {
            this.plan = await API.getPlan(planId);

            this._updatePhase(this.plan.phase);

            // Build initial stats from plan modules
            const progress = this._computeProgress(this.plan);
            this._updateStats(progress);
            this._updateProgressBar(progress);

            // Show abort btn only if running
            const abortBtn = document.getElementById('abort-btn');
            const isRunning = this.plan.status === 'RUNNING';
            abortBtn.style.display = isRunning ? 'inline-flex' : 'none';

            // Setup DAG
            if (this.plan.dep_graph) {
                const statusMap = {};
                (this.plan.modules || []).forEach(m => {
                    statusMap[m.module_id] = m.status;
                    this.moduleStatusCache[m.module_id] = m.status;
                });

                const dagModules = (this.plan.modules || []).map(m => ({
                    module_id: m.module_id,
                    module_name: m.module_name,
                    status: m.status,
                }));

                this.dagGraph.setData(dagModules, this.plan.dep_graph.edges, statusMap);
            }

            // Only start live tracking if plan is not terminal
            if (isRunning) {
                this._startSSE(planId);
                this._startPolling(planId);
            }
        } catch (err) {
            console.error('Failed to load plan:', err);
        }
    },

    _computeProgress(plan) {
        const modules = plan.modules || [];
        const p = {
            plan_id: plan.id,
            phase: plan.phase,
            status: plan.status,
            total_modules: modules.length,
            pending: 0, tagged: 0, releasing: 0,
            succeeded: 0, failed: 0, skipped: 0,
        };
        modules.forEach(m => {
            switch (m.status) {
                case 'PENDING': p.pending++; break;
                case 'TAGGED': p.tagged++; break;
                case 'RELEASING': case 'RETRYING': p.releasing++; break;
                case 'SUCCESS': p.succeeded++; break;
                case 'FAILED': p.failed++; break;
                case 'SKIPPED': p.skipped++; break;
            }
        });
        return p;
    },

    _startSSE(planId) {
        this._stopSSE();
        this.eventSource = API.subscribeEvents(planId, (event) => {
            this._handleEvent(event);
        });
    },

    _stopSSE() {
        if (this.eventSource) {
            this.eventSource.close();
            this.eventSource = null;
        }
    },

    _startPolling(planId) {
        this._stopPolling();
        this.pollTimer = setInterval(async () => {
            try {
                const progress = await API.getProgress(planId);
                this._updateStats(progress);
                this._updateProgressBar(progress);

                // Also refresh plan data for module details
                this.plan = await API.getPlan(planId);

                if (progress.status === 'COMPLETED' || progress.status === 'ABORTED') {
                    this._updatePhase('COMPLETED');
                    this._stopPolling();
                    this._stopSSE();
                    document.getElementById('abort-btn').style.display = 'none';

                    // Final DAG update from latest plan data
                    if (this.plan.modules) {
                        this.plan.modules.forEach(m => {
                            this.dagGraph.updateStatus(m.module_id, m.status);
                        });
                    }
                }
            } catch (err) {
                // ignore
            }
        }, 2000);
    },

    _stopPolling() {
        if (this.pollTimer) {
            clearInterval(this.pollTimer);
            this.pollTimer = null;
        }
    },

    _handleEvent(event) {
        switch (event.type) {
            case 'phase_change':
                this._updatePhase(event.data.phase);
                break;

            case 'module_status': {
                const mid = event.data.module_id;
                const status = event.data.status;
                this.moduleStatusCache[mid] = status;
                this.dagGraph.updateStatus(mid, status);

                // Update plan module in memory for detail panel
                if (this.plan && this.plan.modules) {
                    const mod = this.plan.modules.find(m => m.module_id === mid);
                    if (mod) {
                        mod.status = status;
                        if (event.data.error_msg) mod.error_msg = event.data.error_msg;
                    }
                }

                if (mid === this.selectedModuleId) {
                    this._updateModuleDetail(mid);
                }

                // Add status log
                const statusMsg = event.data.error_msg
                    ? `Status: ${status} - ${event.data.error_msg}`
                    : `Status: ${status}`;
                const isErr = status === 'FAILED';
                this.logPanel.addLog(mid, statusMsg, Date.now(), isErr);
                break;
            }

            case 'module_log': {
                const isError = event.data.line.startsWith('ERROR');
                this.logPanel.addLog(event.data.module_id, event.data.line, event.data.timestamp, isError);
                break;
            }

            case 'plan_complete':
                this._updatePhase('COMPLETED');
                document.getElementById('abort-btn').style.display = 'none';
                this._stopSSE();
                this._stopPolling();
                break;
        }
    },

    _updatePhase(phase) {
        const phases = [
            { key: 'TAGGING', label: 'Tag' },
            { key: 'ANALYZING', label: '分析' },
            { key: 'RELEASING', label: '发布' },
            { key: 'COMPLETED', label: '完成' },
        ];

        const phaseIdx = phases.findIndex(p => p.key === phase);
        const stepper = document.getElementById('phase-stepper');
        if (!stepper) return;

        let html = '';
        phases.forEach((p, i) => {
            const isDone = i < phaseIdx || phase === 'COMPLETED';
            const isActive = i === phaseIdx && phase !== 'COMPLETED';

            html += `
                <div class="phase-step">
                    <div class="phase-dot ${isDone ? 'done' : ''} ${isActive ? 'active' : ''}" style="position:relative;">
                        ${isDone ? '&#10003;' : `P${i + 1}`}
                        <span class="phase-label">${p.label}</span>
                    </div>
                    ${i < phases.length - 1 ? `<div class="phase-line ${isDone ? 'done' : ''}"></div>` : ''}
                </div>
            `;
        });

        stepper.innerHTML = html;
    },

    _updateStats(progress) {
        if (!progress) return;
        const stats = document.getElementById('monitor-stats');
        if (!stats) return;
        stats.innerHTML = `
            <div class="stat-item stat-total"><span class="stat-value">${progress.total_modules}</span><span class="stat-label">总计</span></div>
            <div class="stat-item stat-success"><span class="stat-value">${progress.succeeded}</span><span class="stat-label">成功</span></div>
            <div class="stat-item stat-failed"><span class="stat-value">${progress.failed}</span><span class="stat-label">失败</span></div>
            <div class="stat-item stat-releasing"><span class="stat-value">${progress.releasing}</span><span class="stat-label">发布中</span></div>
            <div class="stat-item stat-pending"><span class="stat-value">${progress.pending + progress.tagged}</span><span class="stat-label">等待</span></div>
            <div class="stat-item stat-skipped"><span class="stat-value">${progress.skipped}</span><span class="stat-label">跳过</span></div>
        `;
    },

    _updateProgressBar(progress) {
        if (!progress || progress.total_modules === 0) return;
        const total = progress.total_modules;
        const bar = document.getElementById('progress-bar');
        if (!bar) return;
        bar.innerHTML = `
            <div class="progress-segment progress-success" style="width:${progress.succeeded / total * 100}%"></div>
            <div class="progress-segment progress-releasing" style="width:${progress.releasing / total * 100}%"></div>
            <div class="progress-segment progress-failed" style="width:${progress.failed / total * 100}%"></div>
            <div class="progress-segment progress-skipped" style="width:${progress.skipped / total * 100}%"></div>
        `;
    },

    _selectModule(moduleId) {
        this.selectedModuleId = moduleId;
        this.logPanel.filterByModule(moduleId);
        this._updateModuleDetail(moduleId);
    },

    _updateModuleDetail(moduleId) {
        const detail = document.getElementById('module-detail');
        if (!detail || !this.plan) return;

        const mod = (this.plan.modules || []).find(m => m.module_id === moduleId);
        if (!mod) {
            detail.style.display = 'none';
            return;
        }

        // Use latest status from cache
        const liveStatus = this.moduleStatusCache[moduleId] || mod.status;
        const isFailed = liveStatus === 'FAILED';

        detail.style.display = 'block';
        detail.innerHTML = `
            <div class="module-detail-header">
                <span class="module-detail-name">${mod.module_name}</span>
                ${Utils.statusBadge(liveStatus)}
            </div>
            <div class="module-detail-grid">
                <span class="module-detail-label">版本:</span>
                <span class="module-detail-value">${mod.prev_version} &rarr; ${mod.target_version}</span>
                <span class="module-detail-label">仓库:</span>
                <span class="module-detail-value">${mod.repo_name}</span>
                <span class="module-detail-label">竖井:</span>
                <span class="module-detail-value">${mod.silo_name}</span>
                <span class="module-detail-label">重试:</span>
                <span class="module-detail-value">${mod.retry_count || 0}</span>
                ${mod.error_msg ? `<span class="module-detail-label">错误:</span><span class="module-detail-value" style="color:var(--danger);">${mod.error_msg}</span>` : ''}
            </div>
            <div style="margin-top:10px;display:flex;gap:8px;">
                ${isFailed ? `<button class="btn btn-danger btn-sm" id="retry-module-btn">&#8635; 重试该模块</button>` : ''}
                <button class="btn btn-ghost btn-sm" id="clear-filter-btn">
                    查看全部日志
                </button>
            </div>
        `;

        detail.querySelector('#clear-filter-btn').onclick = () => {
            this.logPanel.clearFilter();
            this.selectedModuleId = null;
            this.dagGraph.selectNode(null);
            detail.style.display = 'none';
        };

        if (isFailed) {
            detail.querySelector('#retry-module-btn').onclick = async () => {
                const btn = detail.querySelector('#retry-module-btn');
                btn.disabled = true;
                btn.innerHTML = '<span class="spinner"></span> 重试中...';
                try {
                    await API.retryModule(this.planId, moduleId);
                    // Restart SSE + polling to track the retry
                    this._startSSE(this.planId);
                    this._startPolling(this.planId);
                    document.getElementById('abort-btn').style.display = 'inline-flex';
                } catch (err) {
                    alert('重试失败: ' + err.message);
                    btn.disabled = false;
                    btn.innerHTML = '&#8635; 重试该模块';
                }
            };
        }
    },

    _bindEvents(container, planId) {
        container.querySelector('#abort-btn').addEventListener('click', async () => {
            if (!confirm('确认要中止发布吗？')) return;
            try {
                await API.abortPlan(planId);
            } catch (err) {
                alert('中止失败: ' + err.message);
            }
        });
    },

    // Cleanup when navigating away
    destroy() {
        this._stopSSE();
        this._stopPolling();
    },
};
