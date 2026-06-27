// API client wrapper
const API = {
    _handle401(res) {
        if (res.status === 401) {
            window.location.href = '/auth/login';
            return true;
        }
        return false;
    },

    async get(url) {
        const res = await fetch(url, { credentials: 'same-origin' });
        if (this._handle401(res)) throw new Error('unauthorized');
        if (!res.ok) throw new Error(`GET ${url}: ${res.status}`);
        return res.json();
    },

    async post(url, body) {
        const res = await fetch(url, {
            method: 'POST',
            credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json' },
            body: body ? JSON.stringify(body) : undefined,
        });
        if (this._handle401(res)) throw new Error('unauthorized');
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `POST ${url}: ${res.status}`);
        }
        return res.json();
    },

    async put(url, body) {
        const res = await fetch(url, {
            method: 'PUT',
            credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        if (this._handle401(res)) throw new Error('unauthorized');
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `PUT ${url}: ${res.status}`);
        }
        return res.json();
    },

    // Auth & users
    getCurrentUser() { return this.get('/api/current-user'); },
    logout() { return this.post('/api/logout'); },
    getUsers() { return this.get('/api/admin/users'); },
    getRoles() { return this.get('/api/admin/roles'); },
    importUsers(users) { return this.post('/api/admin/users/import', { users }); },
    setUserRoles(uid, roles) { return this.put(`/api/admin/users/${uid}/roles`, { roles }); },
    setUserAccess(uid, allowedSilos) { return this.put(`/api/admin/users/${uid}/access`, { allowed_silos: allowedSilos }); },

    // Product tree
    getSilos() { return this.get('/api/silos'); },
    getReposBySilo(siloId) { return this.get(`/api/silos/${siloId}/repos`); },

    // Repos (full list + branch config)
    getRepos() { return this.get('/api/repos'); },
    updateRepoBranch(repoId, releaseBranch) { return this.put(`/api/repos/${repoId}/branch`, { release_branch: releaseBranch }); },
    updateRepoJDK(repoId, jdk) { return this.put(`/api/repos/${repoId}/jdk`, { jdk }); },

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
    retryModule(planId, moduleId) { return this.post(`/api/plans/${planId}/modules/${moduleId}/retry`); },

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
