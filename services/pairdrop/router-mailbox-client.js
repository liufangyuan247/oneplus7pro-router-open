// Persistent device selection and a streamed, expiring router inbox.
(() => {
    const storageKey = 'op7-mailbox-device-v1';
    const selectedStorageKey = 'op7-mailbox-selected-device';

    const credentials = () => {
        try { return JSON.parse(localStorage.getItem(storageKey) || 'null'); } catch { return null; }
    };

    const bytes = value => {
        if (value === undefined || value === null || Number.isNaN(value)) return '0 B';
        if (value < 1024) return `${value} B`;
        const unit = value >= 1024 ** 3 ? 'GB' : value >= 1024 ** 2 ? 'MB' : 'KB';
        return `${(value / (1024 ** ({KB: 1, MB: 2, GB: 3}[unit]))).toFixed(1)} ${unit}`;
    };

    const browserName = () => {
        const ua = navigator.userAgent;
        const os = /Android/i.test(ua) ? 'Android' : /iPhone|iPad/i.test(ua) ? 'iPhone / iPad' :
            /Windows/i.test(ua) ? 'Windows' : /Macintosh/i.test(ua) ? 'Mac' : 'Linux';
        const browser = /Edg\//.test(ua) ? 'Edge' : /Firefox/.test(ua) ? 'Firefox' :
            /Chrome|CriOS/.test(ua) ? 'Chrome' : 'Safari';
        return {name: `${os} · ${browser}`, platform: os};
    };

    const formatTime = timestamp => {
        if (!timestamp) return '';
        const date = new Date(timestamp);
        const now = new Date();
        const isToday = date.toDateString() === now.toDateString();
        const timeStr = `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
        if (isToday) return `今天 ${timeStr}`;
        return `${date.getMonth() + 1}月${date.getDate()}日 ${timeStr}`;
    };

    const formatExpiry = expiresAt => {
        if (!expiresAt) return '';
        const diffMs = new Date(expiresAt).getTime() - Date.now();
        if (diffMs <= 0) return '已到期清理';
        const diffMinutes = Math.round(diffMs / 60000);
        if (diffMinutes < 60) return `${Math.max(1, diffMinutes)} 分钟后过期`;
        const diffHours = (diffMs / 3600000).toFixed(1).replace(/\.0$/, '');
        if (Number(diffHours) < 24) return `${diffHours} 小时后过期`;
        const diffDays = Math.round(diffMs / 86400000);
        return `${diffDays} 天后过期`;
    };

    const svgNS = 'http://www.w3.org/2000/svg';
    const createSvg = (markup, viewBox = '0 0 24 24', className = '') => {
        const el = document.createElementNS(svgNS, 'svg');
        el.setAttribute('viewBox', viewBox);
        el.setAttribute('aria-hidden', 'true');
        el.setAttribute('fill', 'none');
        el.setAttribute('stroke', 'currentColor');
        el.setAttribute('stroke-width', '2');
        el.setAttribute('stroke-linecap', 'round');
        el.setAttribute('stroke-linejoin', 'round');
        if (className) el.setAttribute('class', className);
        el.innerHTML = markup;
        return el;
    };

    const ICONS = {
        brand: '<path d="M22 2L11 13"/><path d="M22 2l-7 20-4-9-9-4 20-7z"/>',
        settings: '<line x1="4" y1="21" x2="4" y2="14"/><line x1="4" y1="10" x2="4" y2="3"/><line x1="12" y1="21" x2="12" y2="12"/><line x1="12" y1="8" x2="12" y2="3"/><line x1="20" y1="21" x2="20" y2="16"/><line x1="20" y1="12" x2="20" y2="3"/><line x1="1" y1="14" x2="7" y2="14"/><line x1="9" y1="8" x2="15" y2="8"/><line x1="17" y1="16" x2="23" y2="16"/>',
        search: '<circle cx="11" cy="11" r="8" fill="none" stroke="currentColor" stroke-width="2"/><line x1="21" y1="21" x2="16.65" y2="16.65" stroke="currentColor" stroke-width="2"/>',
        inbox: '<polyline points="22 12 16 12 14 15 10 15 8 12 2 12"/><path d="M5.45 5.11L2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/>',
        refresh: '<path d="M23 4v6h-6"/><path d="M1 20v-6h6"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/>',
        back: '<line x1="19" y1="12" x2="5" y2="12"/><polyline points="12 19 5 12 12 5"/>',
        attach: '<path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48"/>',
        save: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/>',
        ack: '<polyline points="20 6 9 17 4 12"/>',
        revoke: '<polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>',
        close: '<line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>',
        smartphone: '<rect x="5" y="2" width="14" height="20" rx="2" ry="2"/><line x1="12" y1="18" x2="12.01" y2="18"/>',
        computer: '<rect x="2" y="3" width="20" height="14" rx="2" ry="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/>',
        fileImage: '<rect x="3" y="3" width="18" height="18" rx="2" ry="2"/><circle cx="8.5" cy="8.5" r="1.5"/><polyline points="21 15 16 10 5 21"/>',
        fileDoc: '<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/>',
        fileAudio: '<path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/>',
        fileVideo: '<polygon points="23 7 16 12 23 17 23 7"/><rect x="1" y="5" width="15" height="14" rx="2" ry="2"/>',
        fileArchive: '<polyline points="21 8 21 21 3 21 3 8"/><rect x="1" y="3" width="22" height="5"/><line x1="10" y1="12" x2="14" y2="12"/>',
        fileCode: '<polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/>',
        fileGeneric: '<path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><polyline points="13 2 13 9 20 9"/>'
    };

    const getFileIconMarkup = filename => {
        const ext = (filename || '').split('.').pop().toLowerCase();
        if (/^(jpg|jpeg|png|gif|webp|svg|bmp|heic|ico)$/.test(ext)) return ICONS.fileImage;
        if (/^(pdf|doc|docx|txt|md|rtf|odt|xls|xlsx|ppt|pptx|csv)$/.test(ext)) return ICONS.fileDoc;
        if (/^(mp3|wav|ogg|flac|m4a|aac)$/.test(ext)) return ICONS.fileAudio;
        if (/^(mp4|mkv|avi|mov|webm)$/.test(ext)) return ICONS.fileVideo;
        if (/^(zip|tar|gz|7z|rar|bz2|xz)$/.test(ext)) return ICONS.fileArchive;
        if (/^(js|mjs|ts|json|html|css|py|c|cpp|sh|xml|yaml|yml)$/.test(ext)) return ICONS.fileCode;
        return ICONS.fileGeneric;
    };

    const AVATAR_PALETTE = [
        {bg: '#2563eb', color: '#ffffff'},
        {bg: '#059669', color: '#ffffff'},
        {bg: '#7c3aed', color: '#ffffff'},
        {bg: '#d97706', color: '#ffffff'},
        {bg: '#db2777', color: '#ffffff'},
        {bg: '#0891b2', color: '#ffffff'},
        {bg: '#4f46e5', color: '#ffffff'},
        {bg: '#ea580c', color: '#ffffff'}
    ];

    const getAvatarStyle = str => {
        let hash = 0;
        for (let i = 0; i < (str || '').length; i++) hash = (hash * 31 + str.charCodeAt(i)) & 0xffffffff;
        return AVATAR_PALETTE[Math.abs(hash) % AVATAR_PALETTE.length];
    };

    function hookServerConnection() {
        if (typeof ServerConnection !== 'undefined' && ServerConnection.prototype) {
            if (!ServerConnection.prototype._mbRosterPatched) {
                ServerConnection.prototype._mbRosterPatched = true;
                const originalOnMessage = ServerConnection.prototype._onMessage;
                ServerConnection.prototype._onMessage = function (raw) {
                    const str = (raw && typeof raw === 'object' && typeof raw.data === 'string')
                        ? raw.data
                        : (typeof raw === 'string' ? raw : null);
                    if (str && str.includes('"mailbox-roster"')) {
                        try {
                            const msg = JSON.parse(str);
                            if (msg && msg.type === 'mailbox-roster') {
                                if (window.routerMailbox) {
                                    window.routerMailbox.handleRosterPush(msg);
                                }
                                return;
                            }
                        } catch (_) {}
                    }
                    return originalOnMessage.call(this, raw);
                };
            }
        }
    }

    class RouterMailbox {
        constructor() {
            this.connectedPeers = new Set();
            this.creds = credentials();
            this.me = null;
            this.busy = false;
            this.inFlight = false;
            this.refreshQueued = false;
            this.authedSocket = null;
            this.serverGeneration = null;
            this.authoritativeGeneration = null;
            this.rosterRevision = 0;
            this.devices = [];
            this.files = {inbox: [], outbox: []};
            this.settings = null;
            this.selectedDeviceId = sessionStorage.getItem(selectedStorageKey) || '__inbox__';
            this.targetDeviceIdForPicker = null;
            this.searchQuery = '';
            this.deviceRows = new Map();
            this.lastRenderedConversationId = null;

            if (typeof window !== 'undefined' && window.peersManager) {
                this.manager = window.peersManager;
            }

            hookServerConnection();
            this.build();

            Events.on('display-name', () => {
                this.authSocketIfNeeded();
                this.refresh();
            });
            Events.on('ws-connected', () => {
                this.authoritativeGeneration = null;
                this.rosterRevision = 0;
                this.authSocketIfNeeded();
                this.refresh();
            });
            Events.on('ws-config', () => {
                this.authSocketIfNeeded();
                this.refresh();
            });
            Events.on('ws-disconnected', () => {
                this.authedSocket = null;
                this.authoritativeGeneration = null;
                this.directRoomSecret = null;
                this.lastPeerId = null;
                this.connectedPeers.clear();
                if (this.devices) {
                    for (const d of this.devices) {
                        d.peerId = null;
                        d.online = false;
                    }
                }
                if (this.me) {
                    this.me.online = false;
                    this.me.peerId = null;
                }
                this.renderSidebar();
                this.renderActiveConversation();
            });
            Events.on('peer-connected', event => {
                this.connectedPeers.add(event.detail.peerId);
                this.renderSidebar();
                this.renderActiveConversation();
            });
            Events.on('peer-disconnected', event => {
                this.connectedPeers.delete(event.detail);
                this.renderSidebar();
                this.renderActiveConversation();
            });
            document.addEventListener('visibilitychange', () => {
                if (!document.hidden) this.refresh();
            });

            this.refresh();
            this.timer = setInterval(() => { if (!document.hidden) this.refresh(); }, 10000);
        }

        authSocketIfNeeded() {
            if (!this.creds || !this.creds.id || !this.creds.token) return;
            const server = this.manager?._server;
            const socket = server?._socket;
            if (!socket || socket.readyState !== 1 /* WebSocket.OPEN */) return;
            if (this.authedSocket === socket) return;

            try {
                // Send raw over socket to never log credentials via ServerConnection.send
                socket.send(JSON.stringify({
                    type: 'mailbox-auth',
                    id: this.creds.id,
                    token: this.creds.token
                }));
                this.authedSocket = socket;
            } catch (_) {}
        }

        handleRosterPush(msg) {
            if (!msg || !Array.isArray(msg.devices)) return;
            const accepted = this.applyDeviceRoster(msg, true);
            if (accepted) {
                this.renderSidebar();
                this.renderActiveConversation();
            }
        }

        applyDeviceRoster(data, fromPush = false) {
            if (!data || !Array.isArray(data.devices)) return false;

            if (fromPush) {
                this.authoritativeGeneration = data.serverGeneration;
            } else if (this.authoritativeGeneration && data.serverGeneration !== this.authoritativeGeneration) {
                // HTTP from different generation ignored once current socket received push
                return false;
            }

            if (data.serverGeneration !== undefined) {
                if (this.serverGeneration !== data.serverGeneration) {
                    this.serverGeneration = data.serverGeneration;
                    this.rosterRevision = 0;
                }
            }
            if (typeof data.revision === 'number') {
                if (data.revision < this.rosterRevision) {
                    return false; // Stale roster, ignore
                }
                this.rosterRevision = data.revision;
            }
            this.devices = data.devices;
            return true;
        }

        build() {
            const css = document.createElement('link');
            css.rel = 'stylesheet';
            css.href = 'styles/router-mailbox.css';
            document.head.appendChild(css);

            this.root = document.createElement('section');
            this.root.id = 'router-mailbox';
            this.root.className = 'router-mailbox';
            this.root.dataset.ready = 'false';

            // Use robust division containers to prevent upstream header/footer rule leakage
            this.root.innerHTML = `
                <aside class="mb-sidebar" aria-label="设备列表">
                    <div class="mb-sidebar-header" role="region" aria-label="侧边栏工具栏">
                        <div class="mb-brand">
                            <span class="mb-brand-icon"></span>
                            <span>设备传输站</span>
                        </div>
                        <button type="button" class="mb-icon-btn mb-btn-settings" aria-label="暂存与设备设置" title="设置">
                        </button>
                    </div>
                    <div class="mb-search-box">
                        <input type="search" class="mb-search-input" placeholder="搜索设备..." aria-label="搜索设备">
                    </div>
                    <div class="mb-device-list" role="list">
                        <div class="mb-device-row mb-inbox-row" role="listitem" tabindex="0" data-id="__inbox__" aria-label="待收文件箱">
                            <div class="mb-avatar mb-avatar-inbox"></div>
                            <div class="mb-row-main">
                                <div class="mb-row-top">
                                    <span class="mb-row-name">待收文件箱</span>
                                    <span class="mb-badge mb-inbox-badge" style="display:none">0</span>
                                </div>
                                <div class="mb-row-sub">
                                    <span class="mb-row-preview mb-inbox-preview">集中查看待收文件</span>
                                </div>
                            </div>
                        </div>
                        <div class="mb-devices-container"></div>
                        <div class="mb-devices-empty" style="display:none;"></div>
                    </div>
                    <div class="mb-sidebar-footer" role="button" tabindex="0" aria-label="此浏览器的设备身份，点击打开设置修改名称" title="点击打开设置以修改本机名称">
                        <div class="mb-avatar mb-avatar-local"></div>
                        <div class="mb-local-info">
                            <div class="mb-local-name">本机设备</div>
                            <div class="mb-local-caption">此浏览器的设备身份</div>
                        </div>
                    </div>
                </aside>

                <main class="mb-chat-pane" aria-label="传输会话">
                    <div class="mb-chat-header" role="region" aria-label="传输会话标题栏">
                        <button type="button" class="mb-icon-btn mb-btn-back" aria-label="返回设备列表" title="返回设备列表">
                        </button>
                        <div class="mb-avatar mb-chat-avatar"></div>
                        <div class="mb-chat-info">
                            <h2 class="mb-chat-name">设备传输站</h2>
                            <div class="mb-chat-status">
                                <span class="mb-status-dot"></span>
                                <span class="mb-status-text">就绪</span>
                            </div>
                        </div>
                        <button type="button" class="mb-icon-btn mb-btn-refresh" aria-label="刷新状态与文件" title="刷新">
                        </button>
                    </div>

                    <div class="mb-notice-bar" role="status" aria-live="polite" style="display:none;">
                        <span class="mb-notice-text"></span>
                    </div>

                    <div class="mb-messages-container" tabindex="0" role="region" aria-label="文件记录">
                        <div class="mb-files-list"></div>
                        <div class="mb-empty-state">
                            <div class="mb-empty-illustration"></div>
                            <h3 class="mb-empty-title">暂无待处理文件</h3>
                            <p class="mb-empty-desc">与当前设备之间没有待领取的暂存文件。</p>
                            <div class="mb-empty-tips">
                                <div class="mb-tip-item">
                                    <span class="mb-tip-bullet">●</span>
                                    <span><strong>在线互传：</strong>在线优先互传，通道未就绪时暂存。</span>
                                </div>
                                <div class="mb-tip-item">
                                    <span class="mb-tip-bullet">●</span>
                                    <span><strong>离线暂存：</strong>对方离线时，文件暂存至路由器，接收端确认收到后清理。</span>
                                </div>
                            </div>
                        </div>
                    </div>

                    <div class="mb-composer" role="region" aria-label="文件发送区">
                        <div class="mb-composer-main">
                            <label class="mb-send-button" tabindex="0" role="button" aria-label="选择并发送文件">
                                <span class="mb-send-button-text">发送文件</span>
                            </label>
                            <div class="mb-composer-hint">
                                <span class="mb-retention-note">在线优先互传；通道未就绪时暂存（保留 2 小时，接收端确认收到后清理）。</span>
                            </div>
                        </div>
                        <div class="mb-composer-aggregate-note" style="display:none;">
                            <span>待收文件箱集中展示所有待收文件。若要发送文件，请在左侧选择目标设备。</span>
                        </div>
                    </div>
                </main>

                <dialog class="mb-settings-dialog" aria-labelledby="mb-settings-heading">
                    <div class="mb-dialog-content">
                        <div class="mb-dialog-header">
                            <h2 id="mb-settings-heading" class="mb-dialog-title">传输站设置</h2>
                            <button type="button" class="mb-icon-btn mb-btn-close-dialog" aria-label="关闭设置">
                            </button>
                        </div>
                        <div class="mb-dialog-body">
                            <section class="mb-dialog-section">
                                <label class="mb-field-label" for="mb-setting-name">本机设备名称</label>
                                <div class="mb-field-row">
                                    <input type="text" id="mb-setting-name" class="mb-input mb-input-name" maxlength="128" placeholder="例如：我的手机、客厅笔记本" autocomplete="off">
                                    <button type="button" class="mb-btn mb-btn-secondary mb-btn-save-name">保存名称</button>
                                </div>
                                <p class="mb-field-tip">在局域网中其他设备打开本页面时显示的名称。</p>
                            </section>

                            <section class="mb-dialog-section">
                                <h3 class="mb-field-label">存储容量与限额</h3>
                                <div class="mb-capacity-box">
                                    <div class="mb-capacity-stats">
                                        <span class="mb-capacity-text">已暂存 0 B / 2.0 GB</span>
                                        <span class="mb-capacity-percent">0%</span>
                                    </div>
                                    <div class="mb-capacity-bar-bg">
                                        <div class="mb-capacity-bar-fill" style="width: 0%;"></div>
                                    </div>
                                    <p class="mb-capacity-limit">单个文件最大限制：512.0 MB</p>
                                </div>
                            </section>

                            <div class="mb-dialog-status" role="status" aria-live="polite" style="display:none;"></div>
                        </div>
                    </div>
                </dialog>
            `;

            // Insert SVGs
            this.root.querySelector('.mb-brand-icon').replaceChildren(createSvg(ICONS.brand));
            this.root.querySelector('.mb-btn-settings').replaceChildren(createSvg(ICONS.settings));
            this.root.querySelector('.mb-search-box').prepend(createSvg(ICONS.search));
            this.root.querySelector('.mb-avatar-inbox').replaceChildren(createSvg(ICONS.inbox));
            this.root.querySelector('.mb-avatar-local').replaceChildren(createSvg(ICONS.computer));
            this.root.querySelector('.mb-btn-back').replaceChildren(createSvg(ICONS.back));
            this.root.querySelector('.mb-btn-refresh').replaceChildren(createSvg(ICONS.refresh));
            this.root.querySelector('.mb-btn-close-dialog').replaceChildren(createSvg(ICONS.close));

            // Empty state illustration
            const illustrationSvg = createSvg(
                `<g fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <rect x="20" y="46" width="38" height="60" rx="5" opacity="0.35"/>
                    <line x1="34" y1="94" x2="44" y2="94" opacity="0.45"/>
                    <rect x="82" y="34" width="42" height="72" rx="5" opacity="0.75"/>
                    <line x1="98" y1="94" x2="108" y2="94" opacity="0.8"/>
                    <path d="M40 46 C 46 16, 92 12, 102 34" stroke-dasharray="4 4" stroke-width="2"/>
                    <path d="M68 14 l 14 4 -6 10 -2 -6 -6 -8 z" fill="currentColor" stroke="none"/>
                </g>`,
                '0 0 140 140'
            );
            this.root.querySelector('.mb-empty-illustration').replaceChildren(illustrationSvg);

            // References
            this.noticeBar = this.root.querySelector('.mb-notice-bar');
            this.noticeText = this.root.querySelector('.mb-notice-text');
            this.devicesContainer = this.root.querySelector('.mb-devices-container');
            this.devicesEmpty = this.root.querySelector('.mb-devices-empty');
            this.inboxRow = this.root.querySelector('.mb-inbox-row');
            this.inboxBadge = this.root.querySelector('.mb-inbox-badge');
            this.inboxPreview = this.root.querySelector('.mb-inbox-preview');
            this.searchInput = this.root.querySelector('.mb-search-input');
            this.localName = this.root.querySelector('.mb-local-name');
            this.sidebarFooter = this.root.querySelector('.mb-sidebar-footer');

            this.chatAvatar = this.root.querySelector('.mb-chat-avatar');
            this.chatName = this.root.querySelector('.mb-chat-name');
            this.chatStatusDot = this.root.querySelector('.mb-status-dot');
            this.chatStatusText = this.root.querySelector('.mb-status-text');
            this.messagesContainer = this.root.querySelector('.mb-messages-container');
            this.filesList = this.root.querySelector('.mb-files-list');
            this.emptyState = this.root.querySelector('.mb-empty-state');
            this.emptyTitle = this.root.querySelector('.mb-empty-title');
            this.emptyDesc = this.root.querySelector('.mb-empty-desc');
            this.emptyTips = this.root.querySelector('.mb-empty-tips');

            this.composerMain = this.root.querySelector('.mb-composer-main');
            this.composerAggregateNote = this.root.querySelector('.mb-composer-aggregate-note');
            this.sendButtonLabel = this.root.querySelector('.mb-send-button');
            this.sendButtonText = this.root.querySelector('.mb-send-button-text');
            this.retentionNote = this.root.querySelector('.mb-retention-note');

            // Settings dialog elements
            this.dialog = this.root.querySelector('.mb-settings-dialog');
            this.nameInput = this.root.querySelector('.mb-input-name');
            this.capacityText = this.root.querySelector('.mb-capacity-text');
            this.capacityPercent = this.root.querySelector('.mb-capacity-percent');
            this.capacityBarFill = this.root.querySelector('.mb-capacity-bar-fill');
            this.capacityLimit = this.root.querySelector('.mb-capacity-limit');
            this.dialogStatus = this.root.querySelector('.mb-dialog-status');

            // CRITICAL: Stable permanent file input DOM
            this.fileInput = document.createElement('input');
            this.fileInput.type = 'file';
            this.fileInput.multiple = true;
            this.fileInput.className = 'mb-real-file-input';
            this.fileInput.setAttribute('aria-label', '选择要发送的文件');

            this.fileInput.addEventListener('click', () => {
                try { NoSleepUI.enable(); } catch {}
                if (this.canSend()) {
                    this.targetDeviceIdForPicker = this.selectedDeviceId;
                }
            });

            this.fileInput.addEventListener('cancel', () => {
                this.targetDeviceIdForPicker = null;
            });

            this.fileInput.addEventListener('change', () => {
                const targetId = this.targetDeviceIdForPicker || this.selectedDeviceId;
                this.targetDeviceIdForPicker = null; // Clear so subsequent selection cannot reuse stale recipient
                const files = Array.from(this.fileInput.files || []);
                this.fileInput.value = '';
                if (files.length && targetId && targetId !== '__inbox__') {
                    this.send(targetId, files);
                }
            });

            this.sendButtonLabel.prepend(createSvg(ICONS.attach));
            this.sendButtonLabel.append(this.fileInput);

            // Send button keyboard trigger (Enter/Space)
            this.sendButtonLabel.addEventListener('keydown', event => {
                if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault();
                    if (!this.canSend()) return;
                    this.targetDeviceIdForPicker = this.selectedDeviceId;
                    try { NoSleepUI.enable(); } catch {}
                    this.fileInput.click();
                }
            });

            // Bind Event Listeners
            this.root.querySelector('.mb-btn-settings').onclick = () => this.openSettings();
            this.sidebarFooter.onclick = () => this.openSettings(true);
            this.sidebarFooter.addEventListener('keydown', event => {
                if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault();
                    this.openSettings(true);
                }
            });

            this.root.querySelector('.mb-btn-close-dialog').onclick = () => this.closeSettings();
            this.dialog.addEventListener('click', event => {
                const rect = this.dialog.getBoundingClientRect();
                const inDialog = (
                    rect.top <= event.clientY && event.clientY <= rect.top + rect.height &&
                    rect.left <= event.clientX && event.clientX <= rect.left + rect.width
                );
                if (!inDialog) this.closeSettings();
            });
            this.dialog.addEventListener('keydown', event => {
                if (event.key === 'Escape') this.closeSettings();
            });

            this.root.querySelector('.mb-btn-back').onclick = () => {
                this.root.classList.remove('mb-mobile-view-chat');
            };
            this.root.querySelector('.mb-btn-refresh').onclick = () => this.refresh(true);

            this.searchInput.addEventListener('input', () => {
                this.searchQuery = this.searchInput.value.trim().toLowerCase();
                this.renderSidebar();
            });

            this.inboxRow.onclick = () => this.selectConversation('__inbox__');
            this.inboxRow.addEventListener('keydown', event => {
                if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault();
                    this.selectConversation('__inbox__');
                }
            });

            // Settings rename
            this.root.querySelector('.mb-btn-save-name').onclick = async () => {
                const name = this.nameInput.value.trim();
                if (!name) {
                    this.showDialogStatus('设备名称不能为空。', true);
                    return;
                }
                try {
                    const result = await this.api('/devices/me', {method: 'PUT', body: {name}});
                    this.me = result.device;
                    this.nameInput.value = result.device.name;
                    this.syncDirectName(result.device.name);
                    this.localName.textContent = result.device.name;
                    this.showDialogStatus('本机名称已保存。');
                    await this.refresh();
                } catch (error) {
                    this.showDialogStatus(error.message, true);
                }
            };

            const center = document.getElementById('center');
            center.prepend(this.root);
            document.documentElement.classList.add('router-mailbox-active');
        }

        notice(text, isError = false) {
            if (!text) {
                this.noticeBar.style.display = 'none';
                return;
            }
            this.noticeText.textContent = text;
            this.noticeBar.classList.toggle('mb-notice-error', isError);
            this.noticeBar.style.display = 'flex';
        }

        showDialogStatus(text, isError = false) {
            this.dialogStatus.textContent = text;
            this.dialogStatus.classList.toggle('mb-error', isError);
            this.dialogStatus.style.display = 'block';
        }

        openSettings(focusName = false) {
            this.dialogStatus.style.display = 'none';
            if (this.me) this.nameInput.value = this.me.name;
            this.renderCapacity();
            if (typeof this.dialog.showModal === 'function') {
                this.dialog.showModal();
            } else {
                this.dialog.setAttribute('open', '');
            }
            if (focusName) {
                setTimeout(() => { this.nameInput.focus(); this.nameInput.select(); }, 50);
            }
        }

        closeSettings() {
            if (typeof this.dialog.close === 'function') {
                this.dialog.close();
            } else {
                this.dialog.removeAttribute('open');
            }
        }

        syncDirectName(name) {
            if (this.manager && this.manager._displayName !== name) {
                Events.fire('self-display-name-changed', name);
            }
        }

        canSend() {
            const hasTargetDevice = Boolean(this.selectedDeviceId) &&
                this.selectedDeviceId !== '__inbox__' &&
                this.devices.some(d => d.id === this.selectedDeviceId);
            const hasSettings = Boolean(this.settings);
            return !this.busy && hasSettings && hasTargetDevice;
        }

        async api(path, options = {}) {
            const headers = {'Accept': 'application/json'};
            if (this.creds) headers.Authorization = `Bearer ${this.creds.id}:${this.creds.token}`;
            if (options.body !== undefined) headers['Content-Type'] = 'application/json';
            const controller = new AbortController();
            const timer = setTimeout(() => controller.abort(), 15000);
            try {
                const response = await fetch('/mailbox/api' + path, {
                    method: options.method || 'GET',
                    credentials: 'same-origin',
                    cache: 'no-store',
                    headers,
                    signal: controller.signal,
                    body: options.body === undefined ? undefined : JSON.stringify(options.body)
                });
                const value = await response.json();
                if (!response.ok) {
                    const error = Error(value.error || `设备服务请求失败（${response.status}）`);
                    error.status = response.status;
                    throw error;
                }
                return value;
            } finally {
                clearTimeout(timer);
            }
        }

        async identify() {
            if (this.creds) {
                try {
                    const res = await this.api('/devices/me');
                    this.me = res.device;
                    this.authSocketIfNeeded();
                    return res.device;
                } catch (error) {
                    if (error.status !== 401) throw error;
                    this.creds = null;
                    localStorage.removeItem(storageKey);
                }
            }
            const result = await this.api('/devices', {method: 'POST', body: browserName()});
            this.creds = {id: result.device.id, token: result.token};
            this.me = result.device;
            localStorage.setItem(storageKey, JSON.stringify(this.creds));
            this.authSocketIfNeeded();
            return result.device;
        }

        async refresh(manual = false) {
            if (this.inFlight) {
                this.refreshQueued = true;
                return;
            }
            this.inFlight = true;
            try {
                const me = await this.identify();
                this.me = me;
                this.localName.textContent = me.name;
                this.syncDirectName(me.name);

                this.authSocketIfNeeded();

                const peerId = this.manager?._server?._isConnected() ? sessionStorage.getItem('peer_id') : null;
                await this.api('/presence', {method: 'POST', body: {peerId: peerId || null}});

                const [devicesRes, filesRes, settingsRes] = await Promise.all([
                    this.api('/devices'),
                    this.api('/files'),
                    this.api('/settings')
                ]);

                this.applyDeviceRoster(devicesRes);
                this.files = filesRes || {inbox: [], outbox: []};
                this.settings = settingsRes;

                // Never overwrite settings draft inputs while dialog is open
                const isDialogOpen = this.dialog && (this.dialog.open || this.dialog.hasAttribute('open'));
                if (!isDialogOpen) {
                    if (document.activeElement !== this.nameInput && !this.nameInput.matches(':focus')) {
                        this.nameInput.value = me.name;
                    }
                }

                if (settingsRes.directRoomSecret && this.manager?._server?._isConnected()) {
                    // PairDrop migrates 64-character legacy room secrets on connect.
                    // Use a stable 256-character room identifier so reconnecting devices
                    // stay in the same room. Entropy remains the private 256-bit token.
                    const roomSecret = settingsRes.directRoomSecret.length === 64
                        ? settingsRes.directRoomSecret.repeat(4) : settingsRes.directRoomSecret;
                    if (this.directRoomSecret !== roomSecret || this.lastPeerId !== peerId) {
                        this.manager._server.send({type: 'room-secrets', roomSecrets: [roomSecret]});
                        this.directRoomSecret = roomSecret;
                        this.lastPeerId = peerId;
                    }
                }

                this.renderSidebar();
                this.renderActiveConversation();
                this.renderCapacity();

                if (manual) {
                    this.notice('设备列表与暂存文件已刷新。');
                }
                this.root.dataset.ready = 'true';
            } catch (error) {
                this.notice(error.name === 'AbortError' ? '暂时无法连接路由器，请稍后刷新。' : error.message, true);
            } finally {
                this.inFlight = false;
                if (this.refreshQueued) {
                    this.refreshQueued = false;
                    this.refresh();
                }
            }
        }

        selectConversation(deviceId) {
            this.selectedDeviceId = deviceId;
            sessionStorage.setItem(selectedStorageKey, deviceId);
            this.root.classList.add('mb-mobile-view-chat');
            this.renderSidebar();
            this.renderActiveConversation();
        }

        renderSidebar() {
            // Update Inbox row
            const inboxCount = (this.files.inbox || []).length;
            this.inboxRow.classList.toggle('mb-active', this.selectedDeviceId === '__inbox__');
            if (inboxCount > 0) {
                this.inboxBadge.textContent = String(inboxCount);
                this.inboxBadge.style.display = 'inline-block';
                this.inboxPreview.textContent = `${inboxCount} 个文件待领取`;
                this.inboxPreview.classList.add('mb-has-pending');
            } else {
                this.inboxBadge.style.display = 'none';
                this.inboxPreview.textContent = '暂无待收文件';
                this.inboxPreview.classList.remove('mb-has-pending');
            }

            const focusedId = document.activeElement?.dataset?.id;
            const hadFocusInList = this.devicesContainer.contains(document.activeElement) || this.inboxRow === document.activeElement;

            // Other devices
            const otherDevices = this.devices.filter(d => d.id !== this.creds?.id);
            const filteredDevices = otherDevices.filter(d =>
                !this.searchQuery || d.name.toLowerCase().includes(this.searchQuery)
            );

            if (!otherDevices.length) {
                this.devicesEmpty.textContent = '让另一台设备在同一网络打开此页面，即可记住它，以后离线也能发送。';
                this.devicesEmpty.style.display = 'block';
            } else if (!filteredDevices.length) {
                this.devicesEmpty.textContent = `未找到包含“${this.searchQuery}”的设备。`;
                this.devicesEmpty.style.display = 'block';
            } else {
                this.devicesEmpty.style.display = 'none';
            }

            // Keyed update for device rows to keep focus and DOM state intact
            const fragment = document.createDocumentFragment();
            for (const device of filteredDevices) {
                let row = this.deviceRows.get(device.id);
                const pendingFromDevice = (this.files.inbox || []).filter(f => f.senderId === device.id);

                if (!row) {
                    row = document.createElement('div');
                    row.className = 'mb-device-row';
                    row.dataset.id = device.id;
                    row.setAttribute('role', 'listitem');
                    row.setAttribute('tabindex', '0');

                    const avatar = document.createElement('div');
                    avatar.className = 'mb-avatar';
                    const style = getAvatarStyle(device.id || device.name);
                    avatar.style.backgroundColor = style.bg;
                    avatar.style.color = style.color;
                    const isMobile = /android|iphone|ipad/i.test(device.platform || '');
                    avatar.append(createSvg(isMobile ? ICONS.smartphone : ICONS.computer));
                    const dot = document.createElement('span');
                    dot.className = 'mb-online-dot' + (device.online ? '' : ' mb-offline');
                    avatar.append(dot);
                    row.append(avatar);

                    const main = document.createElement('div');
                    main.className = 'mb-row-main';
                    const top = document.createElement('div');
                    top.className = 'mb-row-top';
                    const nameEl = document.createElement('span');
                    nameEl.className = 'mb-row-name';
                    nameEl.textContent = device.name;
                    top.append(nameEl);
                    const badge = document.createElement('span');
                    badge.className = 'mb-badge';
                    top.append(badge);
                    main.append(top);

                    const sub = document.createElement('div');
                    sub.className = 'mb-row-sub';
                    const preview = document.createElement('span');
                    preview.className = 'mb-row-preview';
                    sub.append(preview);
                    main.append(sub);
                    row.append(main);

                    row.onclick = () => this.selectConversation(device.id);
                    row.addEventListener('keydown', event => {
                        if (event.key === 'Enter' || event.key === ' ') {
                            event.preventDefault();
                            this.selectConversation(device.id);
                        }
                    });

                    this.deviceRows.set(device.id, row);
                }

                // Update in place
                row.classList.toggle('mb-active', this.selectedDeviceId === device.id);
                row.setAttribute('aria-label', device.name);
                row.querySelector('.mb-row-name').textContent = device.name;

                const isOnline = Boolean(device.online);
                const isDirectReady = Boolean(device.peerId && this.connectedPeers.has(device.peerId));
                const dot = row.querySelector('.mb-online-dot');
                dot.className = 'mb-online-dot' + (isOnline ? '' : ' mb-offline');

                const badge = row.querySelector('.mb-badge');
                if (pendingFromDevice.length > 0) {
                    badge.textContent = String(pendingFromDevice.length);
                    badge.style.display = 'inline-block';
                } else {
                    badge.style.display = 'none';
                }

                const preview = row.querySelector('.mb-row-preview');
                if (pendingFromDevice.length > 0) {
                    preview.textContent = `[待收] ${pendingFromDevice[0].name}`;
                    preview.classList.add('mb-has-pending');
                } else {
                    if (isOnline) {
                        preview.textContent = isDirectReady ? '在线 · 直连就绪' : '在线 · 等待直连';
                    } else {
                        preview.textContent = '离线 · 暂存转交';
                    }
                    preview.classList.remove('mb-has-pending');
                }

                fragment.append(row);
            }

            // Remove deleted devices from map
            for (const [id, row] of this.deviceRows) {
                if (!this.devices.some(d => d.id === id)) {
                    row.remove();
                    this.deviceRows.delete(id);
                }
            }

            this.devicesContainer.replaceChildren(fragment);

            // Restore focus if list had focus
            if (hadFocusInList && focusedId) {
                if (focusedId === '__inbox__') {
                    if (document.activeElement !== this.inboxRow) this.inboxRow.focus();
                } else {
                    const rowToFocus = this.deviceRows.get(focusedId);
                    if (rowToFocus && document.activeElement !== rowToFocus) {
                        rowToFocus.focus();
                    }
                }
            }
        }

        renderActiveConversation() {
            if (this.selectedDeviceId === '__inbox__') {
                this.renderInboxConversation();
            } else {
                const device = this.devices.find(d => d.id === this.selectedDeviceId);
                this.renderDeviceConversation(device);
            }
            this.updateComposerHint();
            this.updateSendButtonState();
        }

        renderInboxConversation() {
            this.chatAvatar.replaceChildren(createSvg(ICONS.inbox));
            this.chatAvatar.style.backgroundColor = '#0ea5e9';
            this.chatAvatar.style.color = '#ffffff';

            this.chatName.textContent = '待收文件箱';
            const count = (this.files.inbox || []).length;
            this.chatStatusText.textContent = `共 ${count} 个待收文件`;
            this.chatStatusDot.className = 'mb-status-dot mb-inbox-dot';

            this.composerMain.style.display = 'none';
            this.composerAggregateNote.style.display = 'block';

            const inboxFiles = [...(this.files.inbox || [])].sort((a, b) =>
                (new Date(a.createdAt).getTime() || 0) - (new Date(b.createdAt).getTime() || 0)
            );

            this.renderFileCards(inboxFiles, true);
        }

        renderDeviceConversation(device) {
            this.composerMain.style.display = 'flex';
            this.composerAggregateNote.style.display = 'none';

            if (!device) {
                this.chatAvatar.replaceChildren(createSvg(ICONS.computer));
                this.chatAvatar.style.backgroundColor = '#94a3b8';
                this.chatName.textContent = '未知或已移除设备';
                this.chatStatusText.textContent = '离线';
                this.chatStatusDot.className = 'mb-status-dot mb-offline';
                this.renderFileCards([], false);
                return;
            }

            const style = getAvatarStyle(device.id || device.name);
            this.chatAvatar.style.backgroundColor = style.bg;
            this.chatAvatar.style.color = style.color;
            const isMobile = /android|iphone|ipad/i.test(device.platform || '');
            this.chatAvatar.replaceChildren(createSvg(isMobile ? ICONS.smartphone : ICONS.computer));

            this.chatName.textContent = device.name;

            const isOnline = Boolean(device.online);
            const isDirectReady = Boolean(device.peerId && this.connectedPeers.has(device.peerId));
            this.chatStatusDot.className = 'mb-status-dot' + (isOnline ? '' : ' mb-offline');
            if (isOnline) {
                this.chatStatusText.textContent = isDirectReady ? '在线 · 直连就绪' : '在线 · 等待直连（可暂存）';
            } else {
                this.chatStatusText.textContent = '离线 · 暂存转交';
            }

            const inbound = (this.files.inbox || []).filter(f => f.senderId === device.id);
            const outbound = (this.files.outbox || []).filter(f => f.recipientId === device.id);

            const combined = [];
            const seen = new Set();
            for (const f of [...inbound, ...outbound]) {
                if (!seen.has(f.id)) {
                    seen.add(f.id);
                    combined.push(f);
                }
            }
            combined.sort((a, b) => (new Date(a.createdAt).getTime() || 0) - (new Date(b.createdAt).getTime() || 0));

            this.renderFileCards(combined, false, device.name);
        }

        renderFileCards(files, isInboxView = false, deviceName = '') {
            const container = this.messagesContainer;
            const isNearBottom = (container.scrollHeight - container.scrollTop - container.clientHeight) < 60;
            const prevScrollTop = container.scrollTop;
            const conversationChanged = this.lastRenderedConversationId !== this.selectedDeviceId;
            this.lastRenderedConversationId = this.selectedDeviceId;

            if (!files.length) {
                this.filesList.style.display = 'none';
                this.emptyState.style.display = 'flex';
                if (isInboxView) {
                    this.emptyTitle.textContent = '暂无待收文件';
                    this.emptyDesc.textContent = '当前没有其他设备发给本机的暂存文件。在左侧选择设备即可开始传输。';
                } else {
                    this.emptyTitle.textContent = '暂无待处理文件';
                    this.emptyDesc.textContent = deviceName
                        ? `与“${deviceName}”之间没有待领取的暂存文件。`
                        : '与当前设备之间没有待领取的暂存文件。';
                }
                return;
            }

            this.filesList.style.display = 'flex';
            this.emptyState.style.display = 'none';

            // If file IDs are unchanged and conversation didn't change, update cards in place
            const existingCards = Array.from(this.filesList.children);
            const sameFiles = existingCards.length === files.length &&
                files.every((f, i) => existingCards[i]?.dataset?.fileId === f.id);

            if (sameFiles && !conversationChanged) {
                for (let i = 0; i < files.length; i++) {
                    const card = existingCards[i];
                    const badge = card.querySelector('.mb-card-status-badge');
                    if (badge) badge.textContent = formatExpiry(files[i].expiresAt);
                }
                return;
            }

            this.filesList.replaceChildren();

            for (const file of files) {
                const isInbound = file.recipientId === this.creds?.id && file.senderId !== this.creds?.id;
                const card = document.createElement('article');
                card.className = 'mb-file-card ' + (isInbound || isInboxView ? 'mb-card-inbound' : 'mb-card-outbound');
                card.dataset.fileId = file.id;

                // Card Header
                const header = document.createElement('div');
                header.className = 'mb-card-header';

                const iconWrap = document.createElement('div');
                iconWrap.className = 'mb-file-icon-wrap';
                iconWrap.append(createSvg(getFileIconMarkup(file.name)));
                header.append(iconWrap);

                const meta = document.createElement('div');
                meta.className = 'mb-card-meta';

                const filenameEl = document.createElement('div');
                filenameEl.className = 'mb-card-filename';
                filenameEl.textContent = file.name;
                meta.append(filenameEl);

                const detailsEl = document.createElement('div');
                detailsEl.className = 'mb-card-details';
                const directionText = isInbound || isInboxView ? `来自 ${file.senderName}` : `发给 ${file.recipientName}`;
                detailsEl.textContent = `${bytes(file.size)} · ${directionText}`;
                meta.append(detailsEl);

                header.append(meta);
                card.append(header);

                // Status & Expiry
                const statusLine = document.createElement('div');
                statusLine.className = 'mb-card-status-line';

                const timeEl = document.createElement('span');
                timeEl.textContent = formatTime(file.createdAt);
                statusLine.append(timeEl);

                const badgeEl = document.createElement('span');
                badgeEl.className = 'mb-card-status-badge';
                badgeEl.textContent = formatExpiry(file.expiresAt);
                statusLine.append(badgeEl);

                card.append(statusLine);

                // Actions
                const actionsRow = document.createElement('div');
                actionsRow.className = 'mb-card-actions';

                if (isInbound || isInboxView || file.recipientId === this.creds?.id) {
                    const saveLink = document.createElement('a');
                    saveLink.className = 'mb-btn mb-btn-primary';
                    saveLink.href = `/mailbox/api/files/${encodeURIComponent(file.id)}/content`;
                    saveLink.download = file.name;
                    saveLink.append(createSvg(ICONS.save), document.createTextNode('保存文件'));
                    actionsRow.append(saveLink);

                    const ackBtn = document.createElement('button');
                    ackBtn.type = 'button';
                    ackBtn.className = 'mb-btn mb-btn-secondary';
                    ackBtn.append(createSvg(ICONS.ack), document.createTextNode('确认收到并清理'));
                    ackBtn.onclick = async () => {
                        if (!confirm(`确认已保存“${file.name}”？确认后会删除路由器上的暂存文件。`)) return;
                        try {
                            await this.api(`/files/${file.id}/ack`, {method: 'POST'});
                            this.notice('已确认收到，暂存文件已清理。');
                            await this.refresh();
                        } catch (error) {
                            this.notice(error.message, true);
                        }
                    };
                    actionsRow.append(ackBtn);
                }

                if (file.senderId === this.creds?.id) {
                    const revokeBtn = document.createElement('button');
                    revokeBtn.type = 'button';
                    revokeBtn.className = 'mb-btn mb-btn-danger';
                    revokeBtn.append(createSvg(ICONS.revoke), document.createTextNode('撤回'));
                    revokeBtn.onclick = async () => {
                        if (!confirm(`撤回“${file.name}”并删除路由器上的暂存文件？`)) return;
                        try {
                            await this.api(`/files/${file.id}`, {method: 'DELETE'});
                            this.notice('已撤回暂存文件。');
                            await this.refresh();
                        } catch (error) {
                            this.notice(error.message, true);
                        }
                    };
                    actionsRow.append(revokeBtn);
                }

                card.append(actionsRow);
                this.filesList.append(card);
            }

            // Scroll management
            if (conversationChanged) {
                container.scrollTop = container.scrollHeight;
            } else if (isNearBottom) {
                container.scrollTop = container.scrollHeight;
            } else {
                container.scrollTop = prevScrollTop;
            }
        }

        updateComposerHint() {
            const hours = this.settings ? (this.settings.retentionSeconds / 3600) : 2;
            const device = this.devices.find(d => d.id === this.selectedDeviceId);
            if (device && device.online) {
                this.retentionNote.textContent = `在线优先互传；通道未就绪时暂存（保留 ${hours} 小时，接收端确认收到后清理）。`;
            } else {
                this.retentionNote.textContent = `对方当前离线，文件暂存至路由器（保留 ${hours} 小时，接收端确认收到后清理）。`;
            }
        }

        updateSendButtonState() {
            const ready = this.canSend();
            this.fileInput.disabled = !ready;
            this.sendButtonLabel.classList.toggle('mb-disabled', !ready);
            this.sendButtonLabel.setAttribute('aria-disabled', String(!ready));
            this.sendButtonText.textContent = this.busy ? '正在发送…' : '发送文件';
        }

        renderCapacity() {
            if (!this.settings) return;
            const stored = this.settings.storedBytes || 0;
            const maxTotal = this.settings.maxTotalBytes || 1;
            const pct = Math.min(100, Math.round((stored / maxTotal) * 100));

            this.capacityText.textContent = `已暂存 ${bytes(stored)} / ${bytes(maxTotal)}`;
            this.capacityPercent.textContent = `${pct}%`;
            this.capacityBarFill.style.width = `${pct}%`;
            this.capacityLimit.textContent = `单个文件最大限制：${bytes(this.settings.maxFileBytes)}`;
        }

        async send(deviceId, files) {
            if (this.busy) {
                this.notice('已有文件正在发送，请等待完成。', true);
                return;
            }
            this.busy = true;
            this.updateSendButtonState();

            try {
                const device = this.devices.find(item => item.id === deviceId);
                if (!device) throw Error('目标设备不存在，请刷新设备列表。');

                // Determine if live WebRTC/WS peer is ready and not busy
                const peer = device.peerId && this.manager?.peers[device.peerId];
                const direct = peer && this.connectedPeers.has(device.peerId) &&
                    (peer.rtcSupported ? peer._channel?.readyState === 'open' : this.manager._server._isConnected());

                if (direct && !peer._busy) {
                    await this.manager._onFilesSelected({to: device.peerId, files});
                    this.notice(`已向 ${device.name} 发起在线传输，请在接收端确认。`);
                    return;
                }

                // Streamed upload fallback to router mailbox
                for (let index = 0; index < files.length; index++) {
                    const file = files[index];
                    if (file.size > this.settings.maxFileBytes) {
                        throw Error(`“${file.name}”超过单个文件大小上限（${bytes(this.settings.maxFileBytes)}）。`);
                    }
                    await this.upload(deviceId, file, (sent, total) => {
                        const pct = Math.round((sent / (total || 1)) * 100);
                        this.notice(`正在暂存 (${index + 1}/${files.length})：${file.name} · ${pct}%`);
                    });
                }

                const hours = this.settings.retentionSeconds / 3600;
                this.notice(`已为 ${device.name} 暂存 ${files.length} 个文件，保留 ${hours} 小时；接收端确认收到后清理。`);
                await this.refresh();
            } catch (error) {
                this.notice(error.message, true);
            } finally {
                this.busy = false;
                this.updateSendButtonState();
            }
        }

        upload(to, file, progress) {
            return new Promise((resolve, reject) => {
                const query = new URLSearchParams({to, name: file.name, size: String(file.size)});
                const request = new XMLHttpRequest();
                request.open('POST', '/mailbox/api/files?' + query);
                request.setRequestHeader('Authorization', `Bearer ${this.creds.id}:${this.creds.token}`);
                request.setRequestHeader('Content-Type', 'application/octet-stream');
                request.upload.onprogress = event => progress(event.loaded, event.total || file.size);
                request.onerror = () => reject(Error('上传中断，请确认网络后重新发送。'));
                request.onabort = () => reject(Error('上传已取消。'));
                request.onload = () => {
                    let result;
                    try { result = JSON.parse(request.responseText); } catch { result = {}; }
                    if (request.status >= 200 && request.status < 300) resolve(result);
                    else reject(Error(result.error || `暂存失败（${request.status}）`));
                };
                request.send(file);
            });
        }
    }

    let controller;
    const original = PeersManager.prototype._onWsConfig;
    PeersManager.prototype._onWsConfig = function (config) {
        if (controller) controller.manager = this;
        return original.call(this, config);
    };

    controller = new RouterMailbox();
    window.routerMailbox = controller;
    document.title = '设备传输站';
})();
