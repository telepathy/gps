// Log Panel component
class LogPanel {
    constructor(container) {
        this.container = container;
        this.logs = [];
        this.filterModuleId = null;
        this.autoScroll = true;
        this._render();
    }

    _render() {
        this.container.innerHTML = `
            <div class="log-panel">
                <div class="log-header">
                    <span id="log-title">Build Logs</span>
                    <label style="font-size:12px;color:var(--text-dim);cursor:pointer;">
                        <input type="checkbox" id="log-autoscroll" checked> Auto-scroll
                    </label>
                </div>
                <div class="log-content" id="log-content"></div>
            </div>
        `;
        this.logContent = this.container.querySelector('#log-content');
        this.logTitle = this.container.querySelector('#log-title');
        this.container.querySelector('#log-autoscroll').addEventListener('change', (e) => {
            this.autoScroll = e.target.checked;
        });
    }

    addLog(moduleId, line, timestamp, isError) {
        this.logs.push({ moduleId, line, timestamp, isError });

        // Only show if matches filter or no filter
        if (this.filterModuleId && moduleId !== this.filterModuleId) return;

        const time = new Date(timestamp).toLocaleTimeString('zh-CN', { hour12: false });
        const div = document.createElement('div');
        div.className = `log-line ${isError ? 'log-error' : ''}`;
        div.innerHTML = `<span class="log-time">${time}</span><span class="log-module">[${moduleId}]</span>${Utils.escapeHtml(line)}`;
        this.logContent.appendChild(div);

        if (this.autoScroll) {
            this.logContent.scrollTop = this.logContent.scrollHeight;
        }
    }

    filterByModule(moduleId) {
        this.filterModuleId = moduleId;
        this.logTitle.textContent = moduleId ? `Logs: ${moduleId}` : 'Build Logs (All)';
        this._rerenderLogs();
    }

    clearFilter() {
        this.filterModuleId = null;
        this.logTitle.textContent = 'Build Logs (All)';
        this._rerenderLogs();
    }

    _rerenderLogs() {
        this.logContent.innerHTML = '';
        this.logs.forEach(log => {
            if (this.filterModuleId && log.moduleId !== this.filterModuleId) return;
            const time = new Date(log.timestamp).toLocaleTimeString('zh-CN', { hour12: false });
            const div = document.createElement('div');
            div.className = `log-line ${log.isError ? 'log-error' : ''}`;
            div.innerHTML = `<span class="log-time">${time}</span><span class="log-module">[${log.moduleId}]</span>${Utils.escapeHtml(log.line)}`;
            this.logContent.appendChild(div);
        });
        if (this.autoScroll) {
            this.logContent.scrollTop = this.logContent.scrollHeight;
        }
    }

    clear() {
        this.logs = [];
        this.logContent.innerHTML = '';
    }
}
