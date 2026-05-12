// Release History Page
const ReleaseHistoryPage = {
    async render(container) {
        container.innerHTML = `
            <div class="fade-in">
                <div class="page-header">
                    <div>
                        <h1 class="page-title">发布历史</h1>
                        <p class="page-subtitle">查看历次发布记录</p>
                    </div>
                    <button class="btn btn-primary" onclick="location.hash='#/'">
                        <span class="nav-icon">+</span> 新建发版
                    </button>
                </div>

                <div id="history-list">
                    <div style="text-align:center;padding:40px;">
                        <span class="spinner"></span>
                    </div>
                </div>
            </div>
        `;

        await this._loadHistory();
    },

    async _loadHistory() {
        try {
            const data = await API.getHistory();
            const history = data.history || [];
            this._renderList(history);
        } catch (err) {
            console.error('Failed to load history:', err);
            document.getElementById('history-list').innerHTML =
                '<div class="empty-state"><p class="empty-state-text">加载失败</p></div>';
        }
    },

    _renderList(history) {
        const list = document.getElementById('history-list');

        if (history.length === 0) {
            list.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state-icon">&#128230;</div>
                    <p class="empty-state-text">暂无发布记录</p>
                </div>
            `;
            return;
        }

        let html = '';

        history.forEach((entry, i) => {
            const siloNames = (entry.silo_names || []).join(', ');
            const total = entry.total_modules || 0;
            const succPct = total > 0 ? Math.round(entry.succeeded / total * 100) : 0;
            const failPct = total > 0 ? Math.round(entry.failed / total * 100) : 0;
            const skipPct = total > 0 ? Math.round(entry.skipped / total * 100) : 0;

            html += `
                <div class="card" style="margin-bottom:12px;padding:0;overflow:hidden;cursor:pointer;" data-plan-id="${entry.plan_id}">
                    <div style="padding:16px 20px;">
                        <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px;">
                            <div>
                                <div style="font-size:15px;font-weight:600;color:var(--text-bright);margin-bottom:4px;">
                                    ${siloNames || entry.plan_id}
                                </div>
                                <div style="font-size:12px;color:var(--text-dim);">
                                    ${entry.plan_id} &middot; ${Utils.formatTime(entry.created_at)} &middot; 耗时 ${entry.duration || '-'}
                                </div>
                            </div>
                            <div style="display:flex;align-items:center;gap:12px;">
                                ${Utils.statusBadge(entry.status)}
                            </div>
                        </div>
                        <div class="progress-bar" style="margin-bottom:12px;">
                            <div class="progress-segment progress-success" style="width:${succPct}%"></div>
                            <div class="progress-segment progress-failed" style="width:${failPct}%"></div>
                            <div class="progress-segment progress-skipped" style="width:${skipPct}%"></div>
                        </div>
                        <div style="display:flex;gap:24px;font-size:13px;">
                            <span style="color:var(--text-bright);">${total} 模块</span>
                            <span style="color:var(--success);">&#10003; ${entry.succeeded} 成功</span>
                            <span style="color:var(--danger);">&#10007; ${entry.failed} 失败</span>
                            <span style="color:var(--text-dim);">&#8722; ${entry.skipped} 跳过</span>
                        </div>
                    </div>
                </div>
            `;
        });

        list.innerHTML = html;

        // Click handler
        list.querySelectorAll('[data-plan-id]').forEach(card => {
            card.addEventListener('click', () => {
                const planId = card.dataset.planId;
                window.location.hash = `#/plan/${planId}/monitor`;
            });
        });
    },
};
