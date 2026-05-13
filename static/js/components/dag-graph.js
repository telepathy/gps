// DAG Graph component using SVG — layout computed from edges, no external layer info
class DagGraph {
    constructor(container) {
        this.container = container;
        this.edges = [];
        this.moduleMap = {};
        this.selectedNodeId = null;
        this.onNodeClick = null;
        this.statusMap = {};
        this.positions = {};

        // Adjacency for neighbor highlight
        this.upstreamOf = {};
        this.downstreamOf = {};

        // Layout params
        this.nodeW = 120;
        this.nodeH = 32;
        this.layerGapY = 70;
        this.nodeGapX = 16;
        this.padding = 40;

        // Pan/zoom
        this.scale = 1;
        this.translateX = 0;
        this.translateY = 0;
        this.isDragging = false;
        this.lastMouse = { x: 0, y: 0 };

        this._initSVG();
    }

    _initSVG() {
        this.container.innerHTML = `
            <svg id="dag-svg" width="100%" height="100%">
                <defs>
                    <marker id="arrowhead" viewBox="0 0 10 7" refX="10" refY="3.5"
                        markerWidth="8" markerHeight="6" orient="auto">
                        <polygon points="0 0, 10 3.5, 0 7" fill="#4a4e5c"/>
                    </marker>
                    <marker id="arrowhead-up" viewBox="0 0 10 7" refX="10" refY="3.5"
                        markerWidth="8" markerHeight="6" orient="auto">
                        <polygon points="0 0, 10 3.5, 0 7" fill="#f59e0b"/>
                    </marker>
                    <marker id="arrowhead-down" viewBox="0 0 10 7" refX="10" refY="3.5"
                        markerWidth="8" markerHeight="6" orient="auto">
                        <polygon points="0 0, 10 3.5, 0 7" fill="#06b6d4"/>
                    </marker>
                </defs>
                <g id="dag-root"></g>
            </svg>
            <div class="dag-zoom-controls">
                <button class="dag-zoom-btn" id="dag-zoom-in">+</button>
                <button class="dag-zoom-btn" id="dag-zoom-out">-</button>
                <button class="dag-zoom-btn" id="dag-zoom-reset">&#8634;</button>
            </div>
        `;

        this.svg = this.container.querySelector('#dag-svg');
        this.root = this.container.querySelector('#dag-root');

        this.container.querySelector('#dag-zoom-in').onclick = () => this._zoom(1.2);
        this.container.querySelector('#dag-zoom-out').onclick = () => this._zoom(0.8);
        this.container.querySelector('#dag-zoom-reset').onclick = () => this._resetView();

        this.svg.addEventListener('mousedown', (e) => {
            if (e.target === this.svg || e.target === this.root) {
                this.isDragging = true;
                this.lastMouse = { x: e.clientX, y: e.clientY };
                this.svg.style.cursor = 'grabbing';
            }
        });
        window.addEventListener('mousemove', (e) => {
            if (!this.isDragging) return;
            this.translateX += e.clientX - this.lastMouse.x;
            this.translateY += e.clientY - this.lastMouse.y;
            this.lastMouse = { x: e.clientX, y: e.clientY };
            this._applyTransform();
        });
        window.addEventListener('mouseup', () => {
            this.isDragging = false;
            this.svg.style.cursor = 'default';
        });
        this.svg.addEventListener('wheel', (e) => {
            e.preventDefault();
            this._zoom(e.deltaY < 0 ? 1.1 : 0.9);
        }, { passive: false });
    }

    _zoom(factor) {
        this.scale = Math.max(0.2, Math.min(3, this.scale * factor));
        this._applyTransform();
    }

    _resetView() {
        this.scale = 1;
        this.translateX = 0;
        this.translateY = 0;
        this._applyTransform();
        this._fitToView();
    }

    _applyTransform() {
        this.root.setAttribute('transform',
            `translate(${this.translateX},${this.translateY}) scale(${this.scale})`);
    }

    // --- Public API ---

    setData(modules, edges, moduleStatusMap) {
        this.moduleMap = {};
        modules.forEach(m => { this.moduleMap[m.module_id || m.id] = m; });
        this.edges = edges || [];
        this.statusMap = moduleStatusMap || {};

        // Build adjacency
        this.upstreamOf = {};
        this.downstreamOf = {};
        for (const id of Object.keys(this.moduleMap)) {
            this.upstreamOf[id] = new Set();
            this.downstreamOf[id] = new Set();
        }
        for (const e of this.edges) {
            if (this.downstreamOf[e.from]) this.downstreamOf[e.from].add(e.to);
            if (this.upstreamOf[e.to]) this.upstreamOf[e.to].add(e.from);
        }

        this._layout();
        this._render();
        setTimeout(() => this._fitToView(), 50);
    }

    updateStatus(moduleId, status) {
        this.statusMap[moduleId] = status;
        this._render();
    }

    selectNode(moduleId) {
        this.selectedNodeId = moduleId;
        this._render();
    }

    // --- Layout: compute depth from edges (longest path from roots) ---

    _computeDepths() {
        const ids = Object.keys(this.moduleMap);
        const depth = {};
        const inDeg = {};
        const adj = {};  // from -> [to]

        for (const id of ids) {
            depth[id] = 0;
            inDeg[id] = 0;
            adj[id] = [];
        }
        for (const e of this.edges) {
            if (adj[e.from] && inDeg[e.to] !== undefined) {
                adj[e.from].push(e.to);
                inDeg[e.to]++;
            }
        }

        // Kahn's algorithm with longest-path propagation
        const queue = [];
        for (const id of ids) {
            if (inDeg[id] === 0) queue.push(id);
        }

        while (queue.length > 0) {
            const node = queue.shift();
            for (const next of adj[node]) {
                depth[next] = Math.max(depth[next], depth[node] + 1);
                inDeg[next]--;
                if (inDeg[next] === 0) queue.push(next);
            }
        }

        return depth;
    }

    _layout() {
        const depth = this._computeDepths();

        // Group nodes by computed depth
        const layers = {};
        for (const [id, d] of Object.entries(depth)) {
            if (!layers[d]) layers[d] = [];
            layers[d].push(id);
        }

        const layerKeys = Object.keys(layers).map(Number).sort((a, b) => a - b);
        if (layerKeys.length === 0) return;

        this.positions = {};
        layerKeys.forEach((layerVal, li) => {
            const ids = layers[layerVal];
            const totalW = ids.length * (this.nodeW + this.nodeGapX) - this.nodeGapX;
            const startX = -totalW / 2 + this.nodeW / 2;
            ids.forEach((id, ni) => {
                this.positions[id] = {
                    x: startX + ni * (this.nodeW + this.nodeGapX),
                    y: this.padding + li * this.layerGapY,
                };
            });
        });
    }

    // --- Neighbor highlight ---

    _getNeighborhood(nodeId) {
        if (!nodeId) return { selected: null, upstream: new Set(), downstream: new Set(), edgeKeys: new Set() };
        const upstream = this.upstreamOf[nodeId] || new Set();
        const downstream = this.downstreamOf[nodeId] || new Set();
        const edgeKeys = new Set();
        for (const uid of upstream) edgeKeys.add(uid + '->' + nodeId);
        for (const did of downstream) edgeKeys.add(nodeId + '->' + did);
        return { selected: nodeId, upstream, downstream, edgeKeys };
    }

    // --- Rendering ---

    _render() {
        if (!this.root) return;
        this.root.innerHTML = '';

        const nb = this._getNeighborhood(this.selectedNodeId);
        const hasSelection = !!nb.selected;

        // Separate edges into normal and highlighted
        const hlEdges = [];
        const normalEdges = [];
        for (const e of this.edges) {
            const key = e.from + '->' + e.to;
            (nb.edgeKeys.has(key) ? hlEdges : normalEdges).push(e);
        }

        // Draw normal edges
        for (const e of normalEdges) {
            const path = this._createEdgePath(e);
            if (!path) continue;
            if (hasSelection) {
                path.setAttribute('stroke', '#252839');
                path.setAttribute('stroke-width', '0.8');
                path.setAttribute('opacity', '0.3');
            }
            path.setAttribute('marker-end', 'url(#arrowhead)');
            this.root.appendChild(path);
        }

        // Draw highlighted edges on top
        for (const e of hlEdges) {
            const path = this._createEdgePath(e);
            if (!path) continue;
            const isUpstream = nb.upstream.has(e.from) && e.to === nb.selected;
            path.setAttribute('stroke', isUpstream ? '#f59e0b' : '#06b6d4');
            path.setAttribute('stroke-width', '2.5');
            path.setAttribute('opacity', '1');
            path.setAttribute('marker-end', isUpstream ? 'url(#arrowhead-up)' : 'url(#arrowhead-down)');
            this.root.appendChild(path);
        }

        // Render nodes
        for (const [id, pos] of Object.entries(this.positions)) {
            const m = this.moduleMap[id];
            if (!m) continue;

            const isSelected = id === nb.selected;
            const isUpstream = nb.upstream.has(id);
            const isDownstream = nb.downstream.has(id);
            const isNeighbor = isSelected || isUpstream || isDownstream;

            const status = this.statusMap[id] || m.status || 'PENDING';
            const g = document.createElementNS('http://www.w3.org/2000/svg', 'g');
            g.setAttribute('transform', `translate(${pos.x},${pos.y})`);
            if (hasSelection && !isNeighbor) g.setAttribute('opacity', '0.25');

            const colors = this._statusColors(status);
            const rect = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
            rect.setAttribute('width', this.nodeW);
            rect.setAttribute('height', this.nodeH);
            rect.setAttribute('rx', '4');
            rect.setAttribute('ry', '4');
            rect.setAttribute('fill', colors.fill);
            rect.setAttribute('stroke', colors.stroke);
            rect.setAttribute('stroke-width', '1.5');

            if (isSelected) {
                rect.setAttribute('stroke', '#4f6ef7');
                rect.setAttribute('stroke-width', '3');
                rect.setAttribute('filter', 'drop-shadow(0 0 8px rgba(79,110,247,0.6))');
            } else if (isUpstream) {
                rect.setAttribute('stroke', '#f59e0b');
                rect.setAttribute('stroke-width', '2.5');
                rect.setAttribute('filter', 'drop-shadow(0 0 6px rgba(245,158,11,0.5))');
            } else if (isDownstream) {
                rect.setAttribute('stroke', '#06b6d4');
                rect.setAttribute('stroke-width', '2.5');
                rect.setAttribute('filter', 'drop-shadow(0 0 6px rgba(6,182,212,0.5))');
            }

            const label = (m.module_name || m.name || id);
            // Show last 2 segments of name for readability
            const parts = label.split('-');
            const shortName = parts.length > 2 ? parts.slice(-2).join('-') : label;
            const text = document.createElementNS('http://www.w3.org/2000/svg', 'text');
            text.setAttribute('x', this.nodeW / 2);
            text.setAttribute('y', this.nodeH / 2 + 4);
            text.setAttribute('text-anchor', 'middle');
            text.setAttribute('fill', colors.text);
            text.setAttribute('font-size', '10');
            text.textContent = shortName.substring(0, 16);

            g.appendChild(rect);
            g.appendChild(text);

            // Upstream/downstream indicator
            if (isUpstream) {
                g.appendChild(this._createTag('上游', '#f59e0b'));
            } else if (isDownstream) {
                g.appendChild(this._createTag('下游', '#06b6d4'));
            }

            g.style.cursor = 'pointer';
            g.addEventListener('click', (evt) => {
                evt.stopPropagation();
                if (this.selectedNodeId === id) {
                    this.selectedNodeId = null;
                } else {
                    this.selectedNodeId = id;
                }
                this._render();
                if (this.onNodeClick) this.onNodeClick(this.selectedNodeId, this.selectedNodeId ? m : null);
            });

            if (status === 'RELEASING' || status === 'RETRYING') {
                rect.style.animation = 'pulse 1.5s infinite';
            }

            this.root.appendChild(g);
        }

        // Click empty space to deselect
        this.svg.onclick = (evt) => {
            if (evt.target === this.svg || evt.target === this.root) {
                if (this.selectedNodeId) {
                    this.selectedNodeId = null;
                    this._render();
                    if (this.onNodeClick) this.onNodeClick(null, null);
                }
            }
        };
    }

    _createEdgePath(e) {
        const fromPos = this.positions[e.from];
        const toPos = this.positions[e.to];
        if (!fromPos || !toPos) return null;

        const x1 = fromPos.x + this.nodeW / 2;
        const y1 = fromPos.y + this.nodeH;
        const x2 = toPos.x + this.nodeW / 2;
        const y2 = toPos.y;
        const midY = (y1 + y2) / 2;

        const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
        path.setAttribute('d', `M${x1},${y1} C${x1},${midY} ${x2},${midY} ${x2},${y2}`);
        path.setAttribute('class', 'dag-edge');
        path.setAttribute('fill', 'none');
        return path;
    }

    _createTag(text, color) {
        const t = document.createElementNS('http://www.w3.org/2000/svg', 'text');
        t.setAttribute('x', this.nodeW / 2);
        t.setAttribute('y', -6);
        t.setAttribute('text-anchor', 'middle');
        t.setAttribute('fill', color);
        t.setAttribute('font-size', '9');
        t.setAttribute('font-weight', '700');
        t.textContent = text;
        return t;
    }

    _statusColors(status) {
        const map = {
            'PENDING':   { fill: '#1e2030', stroke: '#4a4e5c', text: '#8b90a0' },
            'TAGGED':    { fill: '#2a1f4e', stroke: '#7c3aed', text: '#a78bfa' },
            'RELEASING': { fill: '#0c2d48', stroke: '#0ea5e9', text: '#38bdf8' },
            'RETRYING':  { fill: '#422006', stroke: '#d97706', text: '#fbbf24' },
            'SUCCESS':   { fill: '#052e16', stroke: '#16a34a', text: '#34d399' },
            'FAILED':    { fill: '#450a0a', stroke: '#dc2626', text: '#f87171' },
            'SKIPPED':   { fill: '#1a1a2e', stroke: '#4b5563', text: '#9ca3af' },
        };
        return map[status] || map['PENDING'];
    }

    _fitToView() {
        if (!this.positions || Object.keys(this.positions).length === 0) return;
        const svgRect = this.svg.getBoundingClientRect();
        if (svgRect.width === 0 || svgRect.height === 0) return;

        let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
        for (const p of Object.values(this.positions)) {
            minX = Math.min(minX, p.x);
            minY = Math.min(minY, p.y);
            maxX = Math.max(maxX, p.x + this.nodeW);
            maxY = Math.max(maxY, p.y + this.nodeH);
        }

        const graphW = maxX - minX + this.padding * 2;
        const graphH = maxY - minY + this.padding * 2;
        this.scale = Math.min(svgRect.width / graphW, svgRect.height / graphH, 1.5);
        this.translateX = (svgRect.width - graphW * this.scale) / 2 - minX * this.scale + this.padding * this.scale;
        this.translateY = (svgRect.height - graphH * this.scale) / 2 - minY * this.scale + this.padding * this.scale;
        this._applyTransform();
    }
}
