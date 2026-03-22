// Smart Home Stack-based Navigation App

class StackNavigationApp {
    constructor() {
        this.stack = [];
        this.containers = {
            viewContainer: document.getElementById('viewContainer'),
            navTitle: document.getElementById('navTitle'),
            backBtn: document.getElementById('backBtn'),
        };
        this.devices = new Map();
        this.init();
    }

    init() {
        this.loadStackFromStorage();
        this.setupEventListeners();

        if (this.stack.length === 0) {
            this.push('main');
        } else {
            this.renderStack();
        }
    }

    setupEventListeners() {
        this.containers.backBtn.addEventListener('click', () => this.pop());
        document.getElementById('errorDismiss').addEventListener('click', () => {
            document.getElementById('errorDialog').style.display = 'none';
        });
    }

    // Stack Management
    push(view, params = {}) {
        this.stack.push({ view, params, scrollPos: 0 });
        this.saveStackToStorage();
        this.renderStack();
    }

    pop() {
        if (this.stack.length > 1) {
            const currentView = this.stack[this.stack.length - 1];
            currentView.scrollPos = this.containers.viewContainer.scrollTop;

            this.stack.pop();
            this.saveStackToStorage();
            this.renderStack();
        }
    }

    saveStackToStorage() {
        try {
            localStorage.setItem('navigationStack', JSON.stringify(this.stack));
        } catch (e) {
            console.warn('Failed to save stack:', e);
        }
    }

    loadStackFromStorage() {
        try {
            const saved = localStorage.getItem('navigationStack');
            if (saved) {
                this.stack = JSON.parse(saved);
            }
        } catch (e) {
            console.warn('Failed to load stack:', e);
            this.stack = [];
        }
    }

    // Rendering
    async renderStack() {
        const current = this.stack[this.stack.length - 1];
        const previous = this.stack.length > 1 ? this.stack[this.stack.length - 2] : null;

        // Update back button
        this.containers.backBtn.style.display = this.stack.length > 1 ? 'block' : 'none';

        // Get content for current view
        let html = '';
        try {
            switch (current.view) {
                case 'main':
                    html = await this.renderMainMenu();
                    this.containers.navTitle.textContent = 'Smart Home';
                    break;
                case 'devices':
                    html = await this.renderDeviceList(current.params.type);
                    this.containers.navTitle.textContent = this.getDeviceTypeTitle(current.params.type);
                    break;
                case 'device':
                    html = await this.renderDeviceControl(current.params.id);
                    this.containers.navTitle.textContent = 'Device';
                    break;
                case 'scenes':
                    html = await this.renderSceneList();
                    this.containers.navTitle.textContent = 'Scenes';
                    break;
                case 'radio':
                    html = await this.renderRadioMenu();
                    this.containers.navTitle.textContent = 'Radio';
                    break;
                default:
                    html = '<div class="view-content"><p>Unknown view</p></div>';
            }
        } catch (error) {
            this.showError('Failed to render view: ' + error.message);
            html = '<div class="view-content"><p>Error loading content</p></div>';
        }

        // Create and animate new view
        const newView = document.createElement('div');
        newView.className = 'view';
        newView.innerHTML = html;

        const currentViewElement = this.containers.viewContainer.querySelector('.view');
        if (currentViewElement) {
            currentViewElement.classList.add('view-exit-left');
            setTimeout(() => {
                currentViewElement.remove();
                newView.classList.add('view-enter-right');
                this.containers.viewContainer.appendChild(newView);
                this.attachEventListeners(newView);
                if (current.scrollPos) {
                    this.containers.viewContainer.scrollTop = current.scrollPos;
                }
            }, 300);
        } else {
            newView.classList.add('view-enter-right');
            this.containers.viewContainer.appendChild(newView);
            this.attachEventListeners(newView);
        }
    }

    attachEventListeners(viewElement) {
        // Device list item clicks
        viewElement.querySelectorAll('[data-action="device"]').forEach(el => {
            el.addEventListener('click', (e) => {
                e.preventDefault();
                this.push('device', { id: el.dataset.id });
            });
        });

        // Action button clicks
        viewElement.querySelectorAll('[data-action]').forEach(el => {
            if (el.dataset.action !== 'device') {
                el.addEventListener('click', async (e) => {
                    e.preventDefault();
                    await this.handleAction(el.dataset.action, el.dataset);
                });
            }
        });

        // Scene clicks
        viewElement.querySelectorAll('[data-scene]').forEach(el => {
            el.addEventListener('click', (e) => {
                e.preventDefault();
                this.executeScene(el.dataset.scene);
            });
        });

        // Navigation to device list
        viewElement.querySelectorAll('[data-view]').forEach(el => {
            el.addEventListener('click', (e) => {
                e.preventDefault();
                const params = {};
                if (el.dataset.type) params.type = el.dataset.type;
                this.push(el.dataset.view, params);
            });
        });

        // Sliders with real-time updates
        viewElement.querySelectorAll('input[type="range"]').forEach(slider => {
            const valueDisplay = slider.parentElement.querySelector('.value-display .value');
            slider.addEventListener('input', (e) => {
                if (valueDisplay) {
                    const suffix = slider.dataset.suffix || '';
                    valueDisplay.textContent = e.target.value + suffix;
                }
            });
            slider.addEventListener('change', (e) => {
                this.handleAction(slider.dataset.action, {
                    id: slider.dataset.id,
                    value: e.target.value
                });
            });
        });

        // Color preset clicks
        viewElement.querySelectorAll('.color-preset').forEach(el => {
            el.addEventListener('click', (e) => {
                e.preventDefault();
                this.handleAction('color', {
                    id: el.dataset.id,
                    hex: el.dataset.hex
                });
            });
        });

        // Rename form
        const renameForm = viewElement.querySelector('[data-action="rename"]');
        if (renameForm) {
            renameForm.addEventListener('submit', async (e) => {
                e.preventDefault();
                const name = renameForm.querySelector('input[type="text"]').value;
                await this.renameDevice(renameForm.dataset.id, name);
            });
        }

        // Refresh button
        viewElement.querySelectorAll('[data-action="refresh"]').forEach(el => {
            el.addEventListener('click', (e) => {
                e.preventDefault();
                this.fetchDevices().then(() => this.renderStack());
            });
        });
    }

    // View Renderers
    async renderMainMenu() {
        await this.fetchDevices();
        const totalDevices = this.devices.size;

        return `
            <div class="view-content">
                <div class="section">
                    <div class="device-list">
                        <button class="list-item" data-view="devices">
                            <span class="list-item-main">All Devices</span>
                            <span class="list-item-sub">${totalDevices} device${totalDevices !== 1 ? 's' : ''}</span>
                        </button>
                        <button class="list-item" data-view="scenes">
                            <span class="list-item-main">Scenes</span>
                        </button>
                        <button class="list-item" data-view="radio">
                            <span class="list-item-main">Radio</span>
                        </button>
                    </div>
                </div>
            </div>
        `;
    }

    async renderDeviceList(typeFilter = null) {
        await this.fetchDevices();

        const devices = Array.from(this.devices.values());
        let filtered = devices;

        if (typeFilter) {
            filtered = devices.filter(d => d.type === typeFilter);
        }

        filtered.sort((a, b) => a.name.localeCompare(b.name));

        if (filtered.length === 0) {
            return `
                <div class="view-content">
                    <div class="status-message info">
                        No devices found. Discovery is running in the background.
                    </div>
                    <button class="button button-primary" data-action="refresh">Refresh</button>
                </div>
            `;
        }

        let html = '<div class="view-content"><div class="device-list">';

        filtered.forEach(device => {
            const status = device.on !== null ? (device.on ? 'ON' : 'OFF') : '?';
            html += `
                <button class="list-item" data-action="device" data-id="${device.id}">
                    <span class="list-item-main">${device.name}
                        <span class="device-status">[${status}]</span>
                    </span>
                </button>
            `;
        });

        html += '</div></div>';
        return html;
    }

    async renderDeviceControl(deviceId) {
        const device = this.devices.get(deviceId);
        if (!device) {
            return '<div class="view-content"><p>Device not found</p></div>';
        }

        const status = device.on !== null ? (device.on ? 'ON' : 'OFF') : 'Unknown';
        let html = `
            <div class="view-content">
                <div class="section">
                    <div class="control-panel">
                        <div class="control-group">
                            <div class="value-display">
                                <span class="label">Status</span>
                                <span class="value">${status}</span>
                            </div>
                        </div>
                    </div>
                </div>

                <div class="section">
                    <div class="control-label">Power</div>
                    <div class="button-group">
                        <button class="button button-success" data-action="power" data-id="${deviceId}" data-state="on">ON</button>
                        <button class="button button-danger" data-action="power" data-id="${deviceId}" data-state="off">OFF</button>
                        <button class="button button-secondary" data-action="power" data-id="${deviceId}" data-state="toggle">Toggle</button>
                    </div>
                </div>
        `;

        // WLED-specific controls
        if (device.type === 'wled') {
            html += this.renderWledControls(deviceId, device);
        }

        // Tasmota-specific controls
        if (device.type === 'tasmota' && device.capabilities) {
            html += this.renderTasmotaControls(deviceId, device);
        }

        // Rename option
        html += `
                <div class="section">
                    <div class="control-label">Device Settings</div>
                    <form class="control-panel" data-action="rename" data-id="${deviceId}">
                        <div class="control-group">
                            <label class="control-label" style="text-transform: none;">Rename Device</label>
                            <input type="text" value="${device.name}" required style="margin-bottom: 12px;">
                            <button type="submit" class="button button-primary">Save</button>
                        </div>
                    </form>
                </div>
            </div>
        `;

        return html;
    }

    renderWledControls(deviceId, device) {
        let html = `
            <div class="section">
                <div class="control-label">Brightness</div>
                <div class="control-panel">
                    <div class="control-group">
                        <div class="value-display">
                            <span class="label">Brightness</span>
                            <span class="value">${device.brightness || 0}%</span>
                        </div>
                        <input type="range" min="0" max="255" value="${device.brightness || 0}"
                               data-id="${deviceId}" data-action="bright" data-suffix="">
                    </div>
                </div>
            </div>

            <div class="section">
                <div class="control-label">Colors</div>
                <div class="control-panel">
                    <div class="color-grid">
                        <button class="color-preset" style="background: #FF0000;" data-id="${deviceId}" data-hex="FF0000" title="Red"></button>
                        <button class="color-preset" style="background: #00FF00;" data-id="${deviceId}" data-hex="00FF00" title="Green"></button>
                        <button class="color-preset" style="background: #0000FF;" data-id="${deviceId}" data-hex="0000FF" title="Blue"></button>
                        <button class="color-preset" style="background: #FFFF00;" data-id="${deviceId}" data-hex="FFFF00" title="Yellow"></button>
                        <button class="color-preset" style="background: #FF00FF;" data-id="${deviceId}" data-hex="FF00FF" title="Magenta"></button>
                        <button class="color-preset" style="background: #00FFFF;" data-id="${deviceId}" data-hex="00FFFF" title="Cyan"></button>
                        <button class="color-preset" style="background: #FFA500;" data-id="${deviceId}" data-hex="FFA500" title="Orange"></button>
                        <button class="color-preset" style="background: #FFFFFF;" data-id="${deviceId}" data-hex="FFFFFF" title="White"></button>
                    </div>
                </div>
            </div>

            <div class="section">
                <div class="control-label">Effects</div>
                <button class="button button-secondary" style="width: 100%;" data-action="effects" data-id="${deviceId}">Browse Effects</button>
            </div>
        `;
        return html;
    }

    renderTasmotaControls(deviceId, device) {
        const caps = device.capabilities;
        let html = '';

        // Dimmer
        if (caps && caps.has_dimmer) {
            html += `
                <div class="section">
                    <div class="control-label">Dimmer</div>
                    <div class="control-panel">
                        <div class="control-group">
                            <div class="value-display">
                                <span class="label">Level</span>
                                <span class="value">${device.dimmer || 0}%</span>
                            </div>
                            <input type="range" min="0" max="100" value="${device.dimmer || 0}"
                                   data-id="${deviceId}" data-action="dimmer" data-suffix="%">
                        </div>
                    </div>
                </div>
            `;
        }

        // Color Temperature
        if (caps && caps.has_ct) {
            html += `
                <div class="section">
                    <div class="control-label">Color Temperature</div>
                    <div class="button-group">
                        <button class="button button-secondary" data-action="control" data-id="${deviceId}" data-action-type="ct" data-value="500">Warm</button>
                        <button class="button button-secondary" data-action="control" data-id="${deviceId}" data-action-type="ct" data-value="326">Neutral</button>
                        <button class="button button-secondary" data-action="control" data-id="${deviceId}" data-action-type="ct" data-value="153">Cool</button>
                    </div>
                </div>
            `;
        }

        // White channel
        if (caps && caps.has_white) {
            html += `
                <div class="section">
                    <div class="control-label">White Level</div>
                    <div class="control-panel">
                        <div class="control-group">
                            <div class="value-display">
                                <span class="label">Level</span>
                                <span class="value">${device.white || 0}%</span>
                            </div>
                            <input type="range" min="0" max="100" value="${device.white || 0}"
                                   data-id="${deviceId}" data-action="white" data-suffix="%">
                        </div>
                    </div>
                </div>
            `;
        }

        // Colors
        if (caps && caps.has_color) {
            html += `
                <div class="section">
                    <div class="control-label">Colors</div>
                    <div class="control-panel">
                        <div class="color-grid">
                            <button class="color-preset" style="background: #FF0000;" data-id="${deviceId}" data-hex="FF0000" title="Red"></button>
                            <button class="color-preset" style="background: #00FF00;" data-id="${deviceId}" data-hex="00FF00" title="Green"></button>
                            <button class="color-preset" style="background: #0000FF;" data-id="${deviceId}" data-hex="0000FF" title="Blue"></button>
                            <button class="color-preset" style="background: #FFFF00;" data-id="${deviceId}" data-hex="FFFF00" title="Yellow"></button>
                            <button class="color-preset" style="background: #FF00FF;" data-id="${deviceId}" data-hex="FF00FF" title="Magenta"></button>
                        </div>
                    </div>
                </div>
            `;
        }

        // Sensors
        if (device.sensors && Object.keys(device.sensors).length > 0) {
            html += '<div class="section"><div class="control-label">Sensors</div><div class="control-panel">';
            Object.entries(device.sensors).forEach(([key, value]) => {
                html += `
                    <div class="control-group" style="padding: 8px 0; border-bottom: 1px solid #E0E0E0;">
                        <span class="label">${key}</span>
                        <span class="value">${value}</span>
                    </div>
                `;
            });
            html += '</div></div>';
        }

        return html;
    }

    async renderSceneList() {
        return `
            <div class="view-content">
                <div class="section">
                    <div class="action-list">
                        <button class="list-item" data-scene="random-lights">
                            <span class="list-item-main">Random Lights</span>
                            <span class="list-item-sub">Set color-capable lights to random colors</span>
                        </button>
                    </div>
                </div>
            </div>
        `;
    }

    async renderRadioMenu() {
        return `
            <div class="view-content">
                <div class="status-message info">
                    Radio browsing coming soon
                </div>
            </div>
        `;
    }

    // API Methods
    async fetchDevices() {
        try {
            const response = await fetch('/app/api/devices');
            if (!response.ok) throw new Error(`HTTP ${response.status}`);

            const data = await response.json();
            this.devices.clear();

            data.devices?.forEach(device => {
                this.devices.set(device.id, device);
            });
        } catch (error) {
            console.error('Failed to fetch devices:', error);
            this.showError('Failed to fetch device list: ' + error.message);
        }
    }

    async handleAction(action, data) {
        this.showLoading(true);
        try {
            let endpoint = '';

            switch (action) {
                case 'power':
                    endpoint = `/device/${data.id}/${data.state}`;
                    break;
                case 'bright':
                    endpoint = `/device/${data.id}/bright?v=${Math.round(data.value)}`;
                    break;
                case 'dimmer':
                    endpoint = `/device/${data.id}/dimmer?v=${Math.round(data.value)}`;
                    break;
                case 'white':
                    endpoint = `/device/${data.id}/white?v=${Math.round(data.value)}`;
                    break;
                case 'color':
                    endpoint = `/device/${data.id}/color?hex=${data.hex}`;
                    break;
                case 'control':
                case 'effects':
                    this.showLoading(false);
                    this.push('effects', { id: data.id });
                    return;
                default:
                    throw new Error('Unknown action: ' + action);
            }

            const response = await fetch(endpoint);
            if (!response.ok) throw new Error(`HTTP ${response.status}`);

            // Refresh devices and current view
            await this.fetchDevices();
            await this.renderStack();

            this.showLoading(false);
        } catch (error) {
            this.showLoading(false);
            this.showError('Action failed: ' + error.message);
        }
    }

    async executeScene(sceneName) {
        this.showLoading(true);
        try {
            const response = await fetch(`/scene/${sceneName}`);
            if (!response.ok) throw new Error(`HTTP ${response.status}`);

            await this.fetchDevices();
            await this.renderStack();

            this.showLoading(false);
            this.showMessage(`Scene executed: ${sceneName}`, 'success');
        } catch (error) {
            this.showLoading(false);
            this.showError('Scene execution failed: ' + error.message);
        }
    }

    async renameDevice(deviceId, newName) {
        if (!newName || !newName.trim()) {
            this.showError('Name cannot be empty');
            return;
        }

        this.showLoading(true);
        try {
            const response = await fetch(`/device/${deviceId}/rename/save?name=${encodeURIComponent(newName)}`);
            if (!response.ok) throw new Error(`HTTP ${response.status}`);

            await this.fetchDevices();
            await this.renderStack();

            this.showLoading(false);
            this.showMessage('Device renamed', 'success');
        } catch (error) {
            this.showLoading(false);
            this.showError('Rename failed: ' + error.message);
        }
    }

    // UI Helpers
    getDeviceTypeTitle(type) {
        switch (type) {
            case 'wled': return 'WLED Lights';
            case 'tasmota': return 'Tasmota Switches';
            default: return 'All Devices';
        }
    }

    showLoading(show, status = 'Loading...') {
        const indicator = document.getElementById('loadingIndicator');
        if (show) {
            document.getElementById('loadingStatus').textContent = status;
            indicator.style.display = 'flex';
        } else {
            indicator.style.display = 'none';
        }
    }

    showError(message) {
        document.getElementById('errorMessage').textContent = message;
        document.getElementById('errorDialog').style.display = 'flex';
    }

    showMessage(message, type = 'info') {
        // Could be enhanced with toast notifications
        console.log(`[${type}] ${message}`);
    }
}

// Initialize app when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
    window.app = new StackNavigationApp();
});
