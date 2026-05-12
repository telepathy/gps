// SPA Router & App initialization
const App = {
    currentPage: null,

    init() {
        window.addEventListener('hashchange', () => this.route());
        this.route();
    },

    route() {
        // Cleanup previous page
        if (this.currentPage && this.currentPage.destroy) {
            this.currentPage.destroy();
        }

        const hash = window.location.hash || '#/';
        const app = document.getElementById('app');

        // Update active nav link
        document.querySelectorAll('.nav-link').forEach(link => {
            link.classList.remove('active');
        });

        // Route matching
        let match;

        if (hash === '#/' || hash === '#') {
            this._setActiveNav('plan-create');
            this.currentPage = PlanCreatePage;
            PlanCreatePage.render(app);

        } else if (hash === '#/plans') {
            this._setActiveNav('plans');
            this.currentPage = null;
            this._renderPlansPage(app);

        } else if ((match = hash.match(/^#\/plan\/([^/]+)\/confirm$/))) {
            this._setActiveNav('plans');
            this.currentPage = VersionConfirmPage;
            VersionConfirmPage.render(app, match[1]);

        } else if ((match = hash.match(/^#\/plan\/([^/]+)\/monitor$/))) {
            this._setActiveNav('plans');
            this.currentPage = ReleaseMonitorPage;
            ReleaseMonitorPage.render(app, match[1]);

        } else if (hash === '#/history') {
            this._setActiveNav('history');
            this.currentPage = ReleaseHistoryPage;
            ReleaseHistoryPage.render(app);

        } else {
            app.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state-icon">&#128269;</div>
                    <p class="empty-state-text">页面未找到</p>
                    <a href="#/" class="btn btn-primary" style="margin-top:16px;">返回首页</a>
                </div>
            `;
        }
    },

    _setActiveNav(page) {
        document.querySelectorAll('.nav-link').forEach(link => {
            if (link.dataset.page === page) {
                link.classList.add('active');
            }
        });
    },

    async _renderPlansPage(container) {
        container.innerHTML = `
            <div class="fade-in">
                <div class="page-header">
                    <div>
                        <h1 class="page-title">发版计划</h1>
                        <p class="page-subtitle">管理所有发版计划</p>
                    </div>
                    <button class="btn btn-primary" onclick="location.hash='#/'">
                        <span class="nav-icon">+</span> 新建发版
                    </button>
                </div>
                <div id="plans-list">
                    <div style="text-align:center;padding:40px;"><span class="spinner"></span></div>
                </div>
            </div>
        `;

        try {
            const data = await API.getPlans();
            const plans = data.plans || [];
            const list = document.getElementById('plans-list');

            if (plans.length === 0) {
                list.innerHTML = `
                    <div class="empty-state">
                        <div class="empty-state-icon">&#128203;</div>
                        <p class="empty-state-text">暂无发版计划</p>
                        <a href="#/" class="btn btn-primary" style="margin-top:16px;">创建第一个计划</a>
                    </div>
                `;
                return;
            }

            let html = '<div class="card" style="padding:0;overflow:hidden;">';
            plans.sort((a, b) => new Date(b.created_at) - new Date(a.created_at));

            plans.forEach((plan, i) => {
                const statusClass = (plan.status || '').toLowerCase();
                let actionHash = '';
                let actionLabel = '';

                switch (plan.status) {
                    case 'DRAFT':
                        actionHash = `#/plan/${plan.id}/confirm`;
                        actionLabel = '确认版本';
                        break;
                    case 'CONFIRMED':
                        actionHash = `#/plan/${plan.id}/confirm`;
                        actionLabel = '开始发版';
                        break;
                    case 'RUNNING':
                        actionHash = `#/plan/${plan.id}/monitor`;
                        actionLabel = '查看监控';
                        break;
                    default:
                        actionHash = `#/plan/${plan.id}/monitor`;
                        actionLabel = '查看详情';
                }

                html += `
                    <div class="history-card" style="${i > 0 ? 'border-top:1px solid var(--border);' : ''}"
                         onclick="location.hash='${actionHash}'">
                        <div class="history-info">
                            <div class="history-silos">${plan.id}</div>
                            <div class="history-time">${Utils.formatTime(plan.created_at)} &middot; ${(plan.modules || []).length} 模块</div>
                        </div>
                        ${Utils.statusBadge(plan.status)}
                        <span style="font-size:13px;color:var(--primary);cursor:pointer;">${actionLabel}</span>
                        <span style="color:var(--text-dim);font-size:18px;">&#8250;</span>
                    </div>
                `;
            });

            html += '</div>';
            list.innerHTML = html;
        } catch (err) {
            document.getElementById('plans-list').innerHTML =
                '<div class="empty-state"><p class="empty-state-text">加载失败</p></div>';
        }
    },
};

// Start the app
document.addEventListener('DOMContentLoaded', () => App.init());
