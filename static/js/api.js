// API client wrapper
const API = {
    async get(url) {
        const res = await fetch(url);
        if (!res.ok) throw new Error(`GET ${url}: ${res.status}`);
        return res.json();
    },

    async post(url, body) {
        const res = await fetch(url, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: body ? JSON.stringify(body) : undefined,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `POST ${url}: ${res.status}`);
        }
        return res.json();
    },

    async put(url, body) {
        const res = await fetch(url, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `PUT ${url}: ${res.status}`);
        }
        return res.json();
    },

    // Product tree
    getSilos() { return this.get('/api/silos'); },
    getReposBySilo(siloId) { return this.get(`/api/silos/${siloId}/repos`); },
    getModulesByRepo(repoId) { return this.get(`/api/repos/${repoId}/modules`); },

    // Plans
    createPlan(data) { return this.post('/api/plans', data); },
    getPlans() { return this.get('/api/plans'); },
    getPlan(id) { return this.get(`/api/plans/${id}`); },
    updateVersions(planId, versions) { return this.put(`/api/plans/${planId}/versions`, { versions }); },
    confirmPlan(planId) { return this.post(`/api/plans/${planId}/confirm`); },

    // Release
    executePlan(planId) { return this.post(`/api/plans/${planId}/execute`); },
    getProgress(planId) { return this.get(`/api/plans/${planId}/progress`); },
    abortPlan(planId) { return this.post(`/api/plans/${planId}/abort`); },

    // SSE
    subscribeEvents(planId, onEvent) {
        const es = new EventSource(`/api/plans/${planId}/events`);
        es.onmessage = (e) => {
            try {
                const event = JSON.parse(e.data);
                onEvent(event);
            } catch (err) {
                console.error('SSE parse error:', err);
            }
        };
        es.onerror = () => {
            // Will auto-reconnect
        };
        return es;
    },

    // History
    getHistory() { return this.get('/api/history'); },
    getHistoryDetail(id) { return this.get(`/api/history/${id}`); },
};

// Utility functions
const Utils = {
    formatTime(ts) {
        if (!ts) return '-';
        const d = new Date(ts);
        return d.toLocaleString('zh-CN', { hour12: false });
    },

    formatDuration(start, end) {
        if (!start || !end) return '-';
        const ms = new Date(end) - new Date(start);
        const s = Math.floor(ms / 1000);
        const m = Math.floor(s / 60);
        return m > 0 ? `${m}m${s % 60}s` : `${s}s`;
    },

    statusBadge(status) {
        const s = (status || '').toLowerCase();
        return `<span class="badge badge-${s}"><span class="badge-dot"></span>${status}</span>`;
    },

    escapeHtml(str) {
        const div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    },

    debounce(fn, delay) {
        let timer;
        return function (...args) {
            clearTimeout(timer);
            timer = setTimeout(() => fn.apply(this, args), delay);
        };
    },
};
