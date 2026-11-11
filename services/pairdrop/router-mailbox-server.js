import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import crypto from 'node:crypto';
import express from 'express';

const UUID_REGEX = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function isValidUuid(val) {
    return typeof val === 'string' && UUID_REGEX.test(val);
}

function ensureDirSync(dirPath, mode = 0o700) {
    if (!fs.existsSync(dirPath)) {
        fs.mkdirSync(dirPath, { recursive: true, mode });
    }
    try {
        fs.chmodSync(dirPath, mode);
    } catch (_) {}
}

function atomicWriteFileSync(filePath, content, mode = 0o600) {
    const dir = path.dirname(filePath);
    const tmpPath = path.join(dir, `.${path.basename(filePath)}.tmp.${crypto.randomUUID()}`);
    fs.writeFileSync(tmpPath, content, { mode });
    try {
        fs.chmodSync(tmpPath, mode);
    } catch (_) {}
    fs.renameSync(tmpPath, filePath);
}

function normalizeHost(h) {
    if (!h) return '';
    let str = h.toLowerCase().trim();
    if (str.endsWith(':80')) str = str.slice(0, -3);
    else if (str.endsWith(':443')) str = str.slice(0, -4);
    return str;
}

function parseCookies(header) {
    const cookies = {};
    if (!header || typeof header !== 'string') return cookies;
    const pairs = header.split(';');
    for (const pair of pairs) {
        const idx = pair.indexOf('=');
        if (idx === -1) continue;
        const key = pair.slice(0, idx).trim();
        const val = pair.slice(idx + 1).trim();
        cookies[key] = val;
    }
    return cookies;
}

function sanitizeFilename(name) {
    if (typeof name !== 'string') return 'download.bin';
    let cleaned = name.replace(/[\0\x01-\x1F\x7F/\\]/g, '_').trim();
    cleaned = cleaned.replace(/^[\s.]+|[\s.]+$/g, '');
    if (!cleaned || cleaned === '.' || cleaned === '..') {
        return 'download.bin';
    }
    if (cleaned.length > 255) {
        const ext = path.extname(cleaned);
        const base = path.basename(cleaned, ext);
        cleaned = base.slice(0, 255 - ext.length) + ext;
    }
    return cleaned || 'download.bin';
}

function timingSafeTokenCheck(token, storedTokenHash) {
    if (!token || !storedTokenHash) return false;
    const inputHash = crypto.createHash('sha256').update(token).digest('hex');
    const storedBuf = Buffer.from(storedTokenHash, 'hex');
    const inputBuf = Buffer.from(inputHash, 'hex');
    if (storedBuf.length !== inputBuf.length) return false;
    return crypto.timingSafeEqual(storedBuf, inputBuf);
}

const LOOPBACK_IPS = new Set(['127.0.0.1', '::1', '::ffff:127.0.0.1']);

function timingSafeAdminKeyCheck(headerKey, adminKey) {
    if (typeof headerKey !== 'string' || typeof adminKey !== 'string') return false;
    const headerBuf = Buffer.from(headerKey);
    const adminBuf = Buffer.from(adminKey);
    if (headerBuf.length !== adminBuf.length) return false;
    return crypto.timingSafeEqual(headerBuf, adminBuf);
}

const asyncHandler = (fn) => (req, res, next) => {
    Promise.resolve(fn(req, res, next)).catch(next);
};

export function mountMailbox(app, options = {}) {
    const defaultDataDir = process.env.PAIRDROP_MAILBOX_DIR || path.join(os.homedir(), 'router-data', 'pairdrop', 'mailbox');
    const dataDir = options.dataDir || defaultDataDir;
    const blobsDir = path.join(dataDir, 'blobs');
    const metaDir = path.join(dataDir, 'meta');
    const devicesFile = path.join(dataDir, 'devices.json');
    const settingsFile = path.join(dataDir, 'settings.json');

    ensureDirSync(dataDir, 0o700);
    ensureDirSync(blobsDir, 0o700);
    ensureDirSync(metaDir, 0o700);

    const clock = options.clock || { now: () => Date.now() };
    const now = () => clock.now();

    let maxFileBytes = options.maxFileBytes ?? (512 * 1024 * 1024);
    let maxTotalBytes = options.maxTotalBytes ?? (2 * 1024 * 1024 * 1024);
    const maxFiles = options.maxFiles ?? 1000;
    const maxDevices = options.maxDevices ?? 64;
    const maxTextBytes = 16 * 1024;
    const cleanupIntervalMs = options.cleanupIntervalMs ?? 60_000;

    // Admin key: load or create 32-byte hex key with 0600 permissions
    const adminKeyFile = path.join(dataDir, 'admin.key');
    let adminKey = null;
    if (fs.existsSync(adminKeyFile)) {
        try {
            const rawKey = fs.readFileSync(adminKeyFile, 'utf8').trim();
            if (/^[0-9a-f]{64}$/i.test(rawKey)) {
                adminKey = rawKey.toLowerCase();
            }
        } catch (_) {}
    }
    if (!adminKey) {
        adminKey = crypto.randomBytes(32).toString('hex');
        atomicWriteFileSync(adminKeyFile, adminKey + '\n', 0o600);
    }
    try {
        fs.chmodSync(adminKeyFile, 0o600);
    } catch (_) {}

    const serverGeneration = Date.now().toString(36) + '-' + crypto.randomUUID().slice(0, 8);
    let currentRevision = 1;

    // Realtime sockets and authority tracking
    const deviceSockets = new Map(); // deviceId -> Set of { socket, peerId, onMessage, onClose, onError }
    const realtimeAuthority = new Set(); // deviceIds that have authenticated via realtime socket

    // In-memory bounded metadata
    const devices = new Map();
    const files = new Map();
    const tombstones = new Map(); // id -> { recipientId, senderId, deletedAt }
    const activeUploads = new Set(); // fileId currently in-flight
    const uploadDevices = new Map(); // fileId -> sender and recipient during streaming
    const idempotentUploads = new Map(); // compositeKey -> { file, fileId, timestamp }

    let storedBytes = 0;
    let reservedBytes = 0;
    let inFlightCount = 0;
    let retentionSeconds = 7200; // 2 hours default
    let directRoomSecret = null;

    let isClosed = false;

    // Load Settings
    if (fs.existsSync(settingsFile)) {
        try {
            const raw = fs.readFileSync(settingsFile, 'utf8');
            const parsed = JSON.parse(raw);
            if (Number.isSafeInteger(parsed.retentionSeconds) && parsed.retentionSeconds >= 60 && parsed.retentionSeconds <= 604800) {
                retentionSeconds = parsed.retentionSeconds;
            }
            if (typeof parsed.directRoomSecret === 'string' && /^[0-9a-f]{64}$/i.test(parsed.directRoomSecret)) {
                directRoomSecret = parsed.directRoomSecret.toLowerCase();
            }
            // Allow test startup overrides; otherwise load limits from settings.json
            if (options.maxFileBytes === undefined && Number.isSafeInteger(parsed.maxFileBytes) && parsed.maxFileBytes >= 1 && parsed.maxFileBytes <= 10737418240) {
                maxFileBytes = parsed.maxFileBytes;
            }
            if (options.maxTotalBytes === undefined && Number.isSafeInteger(parsed.maxTotalBytes) && parsed.maxTotalBytes >= 1 && parsed.maxTotalBytes <= 107374182400) {
                maxTotalBytes = parsed.maxTotalBytes;
            }
        } catch (_) {}
    }
    if (!directRoomSecret) {
        directRoomSecret = crypto.randomBytes(32).toString('hex');
    }

    function saveSettingsSync(values = {
            retentionSeconds,
            directRoomSecret,
            maxFileBytes,
            maxTotalBytes
        }) {
        atomicWriteFileSync(settingsFile, JSON.stringify(values, null, 2), 0o600);
    }
    saveSettingsSync();

    // Load Devices
    if (fs.existsSync(devicesFile)) {
        try {
            const raw = fs.readFileSync(devicesFile, 'utf8');
            const list = JSON.parse(raw);
            if (Array.isArray(list)) {
                for (const d of list) {
                    if (isValidUuid(d.id) && typeof d.name === 'string' && typeof d.tokenHash === 'string') {
                        devices.set(d.id, {
                            id: d.id,
                            name: d.name,
                            platform: d.platform || 'unknown',
                            tokenHash: d.tokenHash,
                            lastSeen: typeof d.lastSeen === 'number' ? d.lastSeen : now(),
                            peerId: null, // volatile, reset on restart
                            lastPresenceTime: 0, // volatile
                            persistedLastSeen: typeof d.lastSeen === 'number' ? d.lastSeen : now()
                        });
                    }
                }
            }
        } catch (_) {}
    }

    function serializeDevices() {
        const out = [];
        for (const d of devices.values()) {
            out.push({
                id: d.id,
                name: d.name,
                platform: d.platform,
                tokenHash: d.tokenHash,
                lastSeen: d.lastSeen
            });
        }
        return JSON.stringify(out, null, 2);
    }

    function saveDevicesSync() {
        atomicWriteFileSync(devicesFile, serializeDevices(), 0o600);
        for (const d of devices.values()) {
            d.persistedLastSeen = d.lastSeen;
        }
    }

    // Load Existing Files Metadata
    try {
        const metaEntries = fs.readdirSync(metaDir);
        for (const mf of metaEntries) {
            if (!mf.endsWith('.json')) continue;
            const metaPath = path.join(metaDir, mf);
            try {
                const raw = fs.readFileSync(metaPath, 'utf8');
                const fileRec = JSON.parse(raw);
                const binPath = path.join(blobsDir, `${fileRec.id}.bin`);
                if (isValidUuid(fileRec.id) && fs.existsSync(binPath)) {
                    if (fileRec.expiresAt <= now()) {
                        try { fs.unlinkSync(binPath); } catch (_) {}
                        try { fs.unlinkSync(metaPath); } catch (_) {}
                    } else {
                        files.set(fileRec.id, fileRec);
                        storedBytes += fileRec.size;
                        if (fileRec.kind === 'text' && isValidUuid(fileRec.requestId)) {
                            idempotentUploads.set(`text:${fileRec.senderId}:${fileRec.recipientId}:${fileRec.requestId}`,
                                {file: fileRec, fileId: fileRec.id, timestamp: fileRec.createdAt});
                            if (idempotentUploads.size > 200) idempotentUploads.delete(idempotentUploads.keys().next().value);
                        }
                    }
                } else {
                    try { fs.unlinkSync(metaPath); } catch (_) {}
                }
            } catch (_) {
                try { fs.unlinkSync(metaPath); } catch (_) {}
            }
        }
    } catch (_) {}

    // Startup cleanup: remove interrupted .part files and orphaned .bin payloads
    try {
        const blobEntries = fs.readdirSync(blobsDir);
        for (const bf of blobEntries) {
            if (bf.endsWith('.part')) {
                try { fs.unlinkSync(path.join(blobsDir, bf)); } catch (_) {}
            } else if (bf.endsWith('.bin')) {
                const fileId = bf.slice(0, -4);
                if (!files.has(fileId)) {
                    try { fs.unlinkSync(path.join(blobsDir, bf)); } catch (_) {}
                }
            }
        }
    } catch (_) {}

    function toPublicDevice(d) {
        let isOnline = false;
        let peerId = null;

        if (realtimeAuthority.has(d.id)) {
            const socketSet = deviceSockets.get(d.id);
            if (socketSet && socketSet.size > 0) {
                isOnline = true;
                const entries = Array.from(socketSet);
                peerId = entries[entries.length - 1].peerId || null;
            } else {
                isOnline = false;
                peerId = null;
            }
        } else {
            isOnline = Boolean(d.peerId && (now() - (d.lastPresenceTime || 0) <= 30_000));
            peerId = isOnline ? (d.peerId || null) : null;
        }

        return {
            id: d.id,
            name: d.name,
            platform: d.platform,
            peerId,
            online: isOnline,
            lastSeen: d.lastSeen
        };
    }

    function broadcastRoster() {
        currentRevision += 1;
        const msg = JSON.stringify({
            type: 'mailbox-roster',
            revision: currentRevision,
            serverGeneration,
            devices: Array.from(devices.values()).map(toPublicDevice)
        });
        for (const sockets of deviceSockets.values()) {
            for (const entry of sockets) {
                try {
                    if (entry.socket.readyState === undefined || entry.socket.readyState === 1) {
                        entry.socket.send(msg);
                    }
                } catch (_) {}
            }
        }
    }

    // Notify only the participants; payloads remain behind authenticated HTTP.
    function notifyMailbox(record) {
        const msg = JSON.stringify({ type: 'mailbox-updated' });
        for (const id of new Set([record.senderId, record.recipientId])) {
            for (const entry of deviceSockets.get(id) || []) {
                try {
                    if (entry.socket.readyState === undefined || entry.socket.readyState === 1) {
                        entry.socket.send(msg);
                    }
                } catch (_) {}
            }
        }
    }

    function attachSocket(peer) {
        if (isClosed || !peer || !peer.socket || !peer.id) return;
        const socket = peer.socket;
        const peerId = peer.id;
        let authenticatedDeviceId = null;
        let isSocketClosed = false;

        const onMessage = (data, isBinary) => {
            if (isClosed || isSocketClosed) return;
            if (authenticatedDeviceId) return;

            // Avoid parsing binary or >2048byte frames; native file transfer must remain untouched.
            if (isBinary) return;
            if (typeof data !== 'string' && !Buffer.isBuffer(data)) return;
            if (data.length > 2048) return;

            const str = typeof data === 'string' ? data : data.toString('utf8');
            if (!str.includes('mailbox-auth')) return;

            let msg;
            try {
                msg = JSON.parse(str);
            } catch (_) {
                return;
            }

            if (msg?.type !== 'mailbox-auth') return;

            const { id, token } = msg;
            if (!isValidUuid(id) || typeof token !== 'string' || !devices.has(id)) {
                return;
            }
            const device = devices.get(id);
            if (!timingSafeTokenCheck(token, device.tokenHash)) {
                return;
            }

            authenticatedDeviceId = id;
            realtimeAuthority.add(id);

            let socketSet = deviceSockets.get(id);
            if (!socketSet) {
                if (deviceSockets.size >= maxDevices) {
                    for (const [k, v] of deviceSockets.entries()) {
                        if (v.size === 0) deviceSockets.delete(k);
                    }
                }
                socketSet = new Set();
                deviceSockets.set(id, socketSet);
            }

            if (socketSet.size >= 16) {
                const oldest = socketSet.values().next().value;
                oldest.onClose();
                try {
                    oldest.socket.close?.(1008, 'Too many device tabs');
                } catch (_) {}
            }

            const socketEntry = { socket, peerId, onMessage, onClose, onError };
            socketSet.add(socketEntry);

            const nowMs = now();
            device.peerId = peerId;
            device.lastPresenceTime = nowMs;
            device.lastSeen = nowMs;

            broadcastRoster();
        };

        const onClose = () => {
            if (isSocketClosed) return;
            isSocketClosed = true;
            cleanupSocket();
        };

        const onError = () => {
            if (isSocketClosed) return;
            isSocketClosed = true;
            cleanupSocket();
        };

        function cleanupSocket() {
            try {
                if (typeof socket.removeListener === 'function') {
                    socket.removeListener('message', onMessage);
                    socket.removeListener('close', onClose);
                    socket.removeListener('error', onError);
                } else if (typeof socket.off === 'function') {
                    socket.off('message', onMessage);
                    socket.off('close', onClose);
                    socket.off('error', onError);
                }
            } catch (_) {}

            if (authenticatedDeviceId) {
                const socketSet = deviceSockets.get(authenticatedDeviceId);
                if (socketSet) {
                    for (const entry of Array.from(socketSet)) {
                        if (entry.socket === socket) {
                            socketSet.delete(entry);
                        }
                    }
                    const device = devices.get(authenticatedDeviceId);
                    if (socketSet.size === 0) {
                        deviceSockets.delete(authenticatedDeviceId);
                        if (device) {
                            device.peerId = null;
                        }
                    } else {
                        const remaining = Array.from(socketSet);
                        if (device) {
                            device.peerId = remaining[remaining.length - 1].peerId;
                        }
                    }
                }
                broadcastRoster();
                authenticatedDeviceId = null;
            }
        }

        socket.on('message', onMessage);
        socket.on('close', onClose);
        socket.on('error', onError);
    }

    function deleteFileInternal(id) {
        if (!files.has(id)) return;
        const file = files.get(id);
        files.delete(id);
        storedBytes -= file.size;
        tombstones.set(id, { recipientId: file.recipientId, senderId: file.senderId, deletedAt: now() });

        for (const [k, v] of idempotentUploads.entries()) {
            if (v.fileId === id) {
                idempotentUploads.delete(k);
            }
        }

        try { fs.unlinkSync(path.join(blobsDir, `${id}.bin`)); } catch (_) {}
        try { fs.unlinkSync(path.join(metaDir, `${id}.json`)); } catch (_) {}
        notifyMailbox(file);
    }

    function cleanup() {
        const nowMs = now();
        // 1. Expire files
        for (const [id, file] of Array.from(files.entries())) {
            if (file.expiresAt <= nowMs) {
                deleteFileInternal(id);
            }
        }
        // 2. Remove orphaned .part files and untracked .bin files
        try {
            const blobEntries = fs.readdirSync(blobsDir);
            for (const bf of blobEntries) {
                if (bf.endsWith('.part')) {
                    const fileId = bf.slice(0, -5);
                    if (!activeUploads.has(fileId)) {
                        try { fs.unlinkSync(path.join(blobsDir, bf)); } catch (_) {}
                    }
                } else if (bf.endsWith('.bin')) {
                    const fileId = bf.slice(0, -4);
                    if (!files.has(fileId) && !activeUploads.has(fileId)) {
                        try { fs.unlinkSync(path.join(blobsDir, bf)); } catch (_) {}
                    }
                }
            }
        } catch (_) {}
        // 3. Bound tombstones map
        if (tombstones.size > 1000) {
            const keysToRemove = Array.from(tombstones.keys()).slice(0, tombstones.size - 1000);
            for (const k of keysToRemove) {
                tombstones.delete(k);
            }
        }
    }

    const cleanupTimer = setInterval(() => {
        try { cleanup(); } catch (_) {}
    }, cleanupIntervalMs);
    cleanupTimer.unref();

    // Express router
    const router = express.Router();

    // Middleware: Cache-Control: no-store
    router.use((req, res, next) => {
        res.setHeader('Cache-Control', 'no-store');
        next();
    });

    // Middleware: Origin check on mutations
    router.use((req, res, next) => {
        const isMutation = ['POST', 'PUT', 'DELETE', 'PATCH'].includes(req.method);
        if (isMutation) {
            const origin = req.headers.origin;
            if (origin) {
                let originHost = '';
                try {
                    originHost = normalizeHost(new URL(origin).host);
                } catch (_) {
                    return res.status(403).json({ error: 'Invalid Origin header' });
                }
                const reqHost = normalizeHost(req.headers.host || '');
                if (!reqHost || originHost !== reqHost) {
                    return res.status(403).json({ error: 'Forbidden: cross-origin mutation rejected' });
                }
            }
            const secFetchSite = req.headers['sec-fetch-site'];
            if (secFetchSite === 'cross-site') {
                return res.status(403).json({ error: 'Forbidden: cross-site mutation rejected' });
            }
        }
        next();
    });

    // JSON body parser with 16 KiB limit (skip for binary POST /files)
    const jsonParser = express.json({ limit: '16kb' });
    const messageParser = express.json({ limit: '128kb' }); // Escaped Unicode may expand in JSON.
    router.use((req, res, next) => {
        if (req.method === 'POST' && req.path === '/files') {
            return next();
        }
        const parser = req.method === 'POST' && req.path === '/messages' ? messageParser : jsonParser;
        parser(req, res, (err) => {
            if (err) {
                const status = err.status || (err.type === 'entity.too.large' ? 413 : 400);
                return res.status(status).json({ error: err.message || 'Invalid JSON body' });
            }
            next();
        });
    });

    // Admin Middleware
    function authenticateAdmin(req, res, next) {
        const remoteAddr = req.socket?.remoteAddress;
        if (!remoteAddr || !LOOPBACK_IPS.has(remoteAddr)) {
            return res.status(403).json({ error: 'Admin API only accessible via loopback' });
        }
        const headerKey = req.headers['x-router-mailbox-admin'];
        if (!headerKey || !timingSafeAdminKeyCheck(headerKey, adminKey)) {
            return res.status(403).json({ error: 'Invalid or missing admin credentials' });
        }
        next();
    }

    // GET /admin/settings
    router.get('/admin/settings', authenticateAdmin, (req, res) => {
        res.json({
            retentionSeconds,
            maxFileBytes,
            maxTotalBytes,
            maxFiles,
            storedBytes
        });
    });

    // PUT /admin/settings
    router.put('/admin/settings', authenticateAdmin, asyncHandler(async (req, res) => {
        const body = req.body;
        if (!body || typeof body !== 'object' || Array.isArray(body)) {
            return res.status(400).json({ error: 'Invalid JSON body' });
        }
        const keys = Object.keys(body);
        if (keys.length !== 3 || !keys.includes('retentionSeconds') || !keys.includes('maxFileBytes') || !keys.includes('maxTotalBytes')) {
            return res.status(400).json({ error: 'Request body must contain exactly retentionSeconds, maxFileBytes, and maxTotalBytes' });
        }

        const { retentionSeconds: newRetention, maxFileBytes: newMaxFile, maxTotalBytes: newMaxTotal } = body;

        if (!Number.isSafeInteger(newRetention) || newRetention < 60 || newRetention > 604800) {
            return res.status(400).json({ error: 'retentionSeconds must be an integer between 60 and 604800' });
        }
        if (!Number.isSafeInteger(newMaxFile) || newMaxFile < 1 || newMaxFile > 10737418240) {
            return res.status(400).json({ error: 'maxFileBytes must be an integer between 1 and 10737418240' });
        }
        if (!Number.isSafeInteger(newMaxTotal) || newMaxTotal < 1 || newMaxTotal > 107374182400) {
            return res.status(400).json({ error: 'maxTotalBytes must be an integer between 1 and 107374182400' });
        }
        if (newMaxFile > newMaxTotal) {
            return res.status(400).json({ error: 'maxFileBytes must not exceed maxTotalBytes' });
        }

        // Do not apply a configuration in memory if persisting it fails.
        saveSettingsSync({retentionSeconds: newRetention, directRoomSecret,
            maxFileBytes: newMaxFile, maxTotalBytes: newMaxTotal});
        retentionSeconds = newRetention;
        maxFileBytes = newMaxFile;
        maxTotalBytes = newMaxTotal;

        for (const file of files.values()) {
            file.expiresAt = file.createdAt + (retentionSeconds * 1000);
            const metaPath = path.join(metaDir, `${file.id}.json`);
            try {
                atomicWriteFileSync(metaPath, JSON.stringify(file, null, 2), 0o600);
            } catch (_) {}
        }

        cleanup();

        res.json({
            retentionSeconds,
            maxFileBytes,
            maxTotalBytes,
            maxFiles,
            storedBytes
        });
    }));

    // Auth Middleware: Bearer token
    function authenticateBearer(req, res, next) {
        const auth = req.headers.authorization;
        if (!auth || !auth.startsWith('Bearer ')) {
            return res.status(401).json({ error: 'Authorization: Bearer <id>:<token> required' });
        }
        const creds = auth.slice(7).trim();
        const colonIdx = creds.indexOf(':');
        if (colonIdx === -1) {
            return res.status(401).json({ error: 'Invalid Bearer token format' });
        }
        const deviceId = creds.slice(0, colonIdx);
        const token = creds.slice(colonIdx + 1);

        if (!isValidUuid(deviceId) || !token) {
            return res.status(401).json({ error: 'Invalid credentials' });
        }
        const device = devices.get(deviceId);
        if (!device || !timingSafeTokenCheck(token, device.tokenHash)) {
            return res.status(401).json({ error: 'Invalid credentials' });
        }

        req.device = device;
        req.token = token;
        next();
    }

    // Auth Middleware: Bearer OR download cookie (for GET /files/:id/content only)
    function authenticateBearerOrCookie(req, res, next) {
        const auth = req.headers.authorization;
        let deviceId = null;
        let token = null;

        if (auth && auth.startsWith('Bearer ')) {
            const creds = auth.slice(7).trim();
            const colonIdx = creds.indexOf(':');
            if (colonIdx !== -1) {
                deviceId = creds.slice(0, colonIdx);
                token = creds.slice(colonIdx + 1);
            }
        } else if (req.headers.cookie) {
            const cookies = parseCookies(req.headers.cookie);
            if (cookies.mailbox_auth) {
                let decoded = '';
                try {
                    decoded = decodeURIComponent(cookies.mailbox_auth);
                } catch (_) {
                    decoded = cookies.mailbox_auth;
                }
                const colonIdx = decoded.indexOf(':');
                if (colonIdx !== -1) {
                    deviceId = decoded.slice(0, colonIdx);
                    token = decoded.slice(colonIdx + 1);
                }
            }
        }

        if (!deviceId || !token || !isValidUuid(deviceId)) {
            return res.status(401).json({ error: 'Authentication required' });
        }

        const device = devices.get(deviceId);
        if (!device || !timingSafeTokenCheck(token, device.tokenHash)) {
            return res.status(401).json({ error: 'Invalid credentials' });
        }

        req.device = device;
        req.token = token;
        next();
    }

    function setAuthCookie(res, deviceId, token) {
        const cookieVal = encodeURIComponent(`${deviceId}:${token}`);
        res.setHeader('Set-Cookie', `mailbox_auth=${cookieVal}; Path=/mailbox/api; HttpOnly; SameSite=Strict`);
    }

    // POST /devices (Registration)
    router.post('/devices', asyncHandler(async (req, res) => {
        if (devices.size >= maxDevices) {
            return res.status(403).json({ error: 'Maximum registered device limit reached' });
        }

        const { name, platform } = req.body || {};
        if (typeof name !== 'string' || !name.trim() || name.trim().length > 128) {
            return res.status(400).json({ error: 'Device name must not be blank or exceed 128 characters' });
        }

        let cleanPlatform = 'unknown';
        if (typeof platform === 'string' && platform.trim()) {
            cleanPlatform = platform.trim().slice(0, 64);
        }

        const id = crypto.randomUUID();
        const token = crypto.randomBytes(32).toString('base64url');
        const tokenHash = crypto.createHash('sha256').update(token).digest('hex');
        const nowMs = now();

        const deviceRecord = {
            id,
            name: name.trim(),
            platform: cleanPlatform,
            tokenHash,
            lastSeen: nowMs,
            peerId: null,
            lastPresenceTime: 0,
            persistedLastSeen: nowMs
        };

        devices.set(id, deviceRecord);
        saveDevicesSync();
        broadcastRoster();

        setAuthCookie(res, id, token);
        return res.status(201).json({
            device: toPublicDevice(deviceRecord),
            token
        });
    }));

    // GET /devices/me
    router.get('/devices/me', authenticateBearer, (req, res) => {
        res.json({ device: toPublicDevice(req.device) });
    });

    // PUT /devices/me
    router.put('/devices/me', authenticateBearer, asyncHandler(async (req, res) => {
        const { name } = req.body || {};
        if (typeof name !== 'string' || !name.trim() || name.trim().length > 128) {
            return res.status(400).json({ error: 'Device name must not be blank or exceed 128 characters' });
        }

        req.device.name = name.trim();
        saveDevicesSync();
        broadcastRoster();
        res.json({ device: toPublicDevice(req.device) });
    }));

    // DELETE /devices/me (Caller self-revocation if no pending files)
    router.delete('/devices/me', authenticateBearer, asyncHandler(async (req, res) => {
        const callerId = req.device.id;
        if (Array.from(uploadDevices.values()).some(ids => ids.includes(callerId))) {
            return res.status(409).json({ error: 'Cannot delete device during an active upload' });
        }
        for (const file of files.values()) {
            if (file.senderId === callerId || file.recipientId === callerId) {
                return res.status(409).json({ error: 'Cannot delete device with pending files in mailbox' });
            }
        }

        devices.delete(callerId);
        for (const entry of Array.from(deviceSockets.get(callerId) || [])) {
            entry.onClose();
            try { entry.socket.close?.(1008, 'Device revoked'); } catch (_) {}
        }
        realtimeAuthority.delete(callerId);
        saveDevicesSync();
        broadcastRoster();

        res.setHeader('Set-Cookie', 'mailbox_auth=; Path=/mailbox/api; HttpOnly; SameSite=Strict; Max-Age=0');
        res.json({ ok: true });
    }));

    // POST /presence
    router.post('/presence', authenticateBearer, (req, res) => {
        const { peerId } = req.body || {};
        const nowMs = now();

        let cleanPeerId = null;
        if (typeof peerId === 'string' && peerId.trim()) {
            cleanPeerId = peerId.trim().slice(0, 128);
        }

        req.device.lastSeen = nowMs;

        if (realtimeAuthority.has(req.device.id)) {
            // Realtime socket has authority; HTTP presence cannot revive after disconnect
        } else {
            req.device.peerId = cleanPeerId;
            req.device.lastPresenceTime = nowMs;
        }

        // Throttled persistence of lastSeen
        if (nowMs - (req.device.persistedLastSeen || 0) >= 300_000) {
            saveDevicesSync();
        }

        setAuthCookie(res, req.device.id, req.token);
        res.json({ device: toPublicDevice(req.device) });
    });

    // GET /devices
    router.get('/devices', authenticateBearer, (req, res) => {
        const list = Array.from(devices.values()).map(toPublicDevice);
        res.json({
            revision: currentRevision,
            serverGeneration,
            devices: list
        });
    });

    // GET /settings
    router.get('/settings', authenticateBearer, (req, res) => {
        res.json({
            directRoomSecret,
            maxTextBytes,
            retentionSeconds,
            maxFileBytes,
            maxTotalBytes,
            maxFiles,
            storedBytes
        });
    });

    // PUT /settings (always 403 for ordinary auth clients)
    router.put('/settings', authenticateBearer, (req, res) => {
        return res.status(403).json({ error: 'Forbidden: settings can only be modified via admin API' });
    });

    // Text shares file ownership, quotas, durable storage and expiry handling.
    router.post('/messages', authenticateBearer, (req, res) => {
        const { to, text } = req.body || {};
        if (!isValidUuid(to) || !devices.has(to)) {
            return res.status(404).json({ error: 'Recipient device not found' });
        }
        if (typeof text !== 'string' || !text.trim()) {
            return res.status(400).json({ error: 'Message text must not be blank' });
        }
        const payload = Buffer.from(text, 'utf8');
        if (payload.length > maxTextBytes || payload.length > maxFileBytes) {
            return res.status(413).json({ error: 'Message exceeds text or single-item size limit' });
        }
        const key = req.headers['idempotency-key'];
        if (key && !isValidUuid(key)) {
            return res.status(400).json({ error: 'Idempotency-Key must be a valid UUID' });
        }
        const compositeKey = key ? `text:${req.device.id}:${to}:${key}` : null;
        const previous = compositeKey && idempotentUploads.get(compositeKey);
        if (previous && files.has(previous.fileId) && previous.file.expiresAt > now()) {
            if (previous.file.text !== text) {
                return res.status(409).json({ error: 'Idempotency-Key already used for different text' });
            }
            return res.json({ file: previous.file });
        }
        cleanup();
        if (storedBytes + reservedBytes + payload.length > maxTotalBytes || files.size + inFlightCount >= maxFiles) {
            return res.status(507).json({ error: 'Mailbox storage or item quota exceeded' });
        }
        const id = crypto.randomUUID();
        const createdAt = now();
        const record = {
            id, kind: 'text', text, requestId: key || undefined, name: '文字消息', size: payload.length,
            senderId: req.device.id, senderName: req.device.name,
            recipientId: to, recipientName: devices.get(to).name,
            createdAt, expiresAt: createdAt + retentionSeconds * 1000,
            sha256: crypto.createHash('sha256').update(payload).digest('hex')
        };
        const binPath = path.join(blobsDir, `${id}.bin`);
        const metaPath = path.join(metaDir, `${id}.json`);
        try {
            atomicWriteFileSync(binPath, payload, 0o600);
            atomicWriteFileSync(metaPath, JSON.stringify(record), 0o600);
        } catch (error) {
            try { fs.unlinkSync(binPath); } catch (_) {}
            try { fs.unlinkSync(metaPath); } catch (_) {}
            return res.status(error.code === 'ENOSPC' ? 507 : 500).json({ error: 'Failed to store message' });
        }
        files.set(id, record);
        storedBytes += payload.length;
        if (compositeKey) {
            idempotentUploads.set(compositeKey, { file: record, fileId: id, timestamp: createdAt });
            if (idempotentUploads.size > 200) idempotentUploads.delete(idempotentUploads.keys().next().value);
        }
        notifyMailbox(record);
        res.status(201).json({ file: record });
    });

    // POST /files?to=<recipientUUID>&name=<filename>&size=<bytes>
    router.post('/files', authenticateBearer, (req, res) => {
        const toId = req.query.to;
        if (!toId || !isValidUuid(toId) || !devices.has(toId)) {
            return res.status(404).json({ error: 'Recipient device not found' });
        }
        const recipient = devices.get(toId);

        const rawSize = req.query.size;
        if (!rawSize || !/^\d+$/.test(rawSize)) {
            return res.status(400).json({ error: 'Query parameter size must be a non-negative integer' });
        }
        const declaredSize = Number.parseInt(rawSize, 10);
        if (!Number.isSafeInteger(declaredSize) || declaredSize < 0) {
            return res.status(400).json({ error: 'Invalid size parameter' });
        }

        if (declaredSize > maxFileBytes) {
            return res.status(413).json({ error: 'File size exceeds maxFileBytes' });
        }

        // Quota checks including in-flight reservations
        if (storedBytes + reservedBytes + declaredSize > maxTotalBytes) {
            return res.status(507).json({ error: 'Storage quota exceeded' });
        }
        if (files.size + inFlightCount >= maxFiles) {
            return res.status(507).json({ error: 'Maximum file count reached' });
        }

        // Scoped Idempotency-Key support: senderId:recipientId:key
        const idempotencyKey = req.headers['idempotency-key'];
        let compositeKey = null;
        if (idempotencyKey) {
            if (!isValidUuid(idempotencyKey)) {
                return res.status(400).json({ error: 'Idempotency-Key must be a valid UUID' });
            }
            compositeKey = `${req.device.id}:${recipient.id}:${idempotencyKey}`;
            if (idempotentUploads.has(compositeKey)) {
                const entry = idempotentUploads.get(compositeKey);
                if (files.has(entry.fileId) && entry.file.expiresAt > now()) {
                    return res.status(200).json({ file: files.get(entry.fileId) });
                }
                idempotentUploads.delete(compositeKey);
            }
        }

        const safeFilename = sanitizeFilename(req.query.name);
        const fileId = crypto.randomUUID();
        const partPath = path.join(blobsDir, `${fileId}.part`);
        const binPath = path.join(blobsDir, `${fileId}.bin`);
        const metaPath = path.join(metaDir, `${fileId}.json`);

        // Reserve quota
        reservedBytes += declaredSize;
        inFlightCount += 1;
        activeUploads.add(fileId);
        uploadDevices.set(fileId, [req.device.id, recipient.id]);

        let reservationReleased = false;
        function releaseReservation() {
            if (reservationReleased) return;
            reservationReleased = true;
            activeUploads.delete(fileId);
            uploadDevices.delete(fileId);
            reservedBytes -= declaredSize;
            inFlightCount -= 1;
        }

        let fileStream;
        try {
            fileStream = fs.createWriteStream(partPath, { mode: 0o600, flags: 'w' });
        } catch (err) {
            releaseReservation();
            return res.status(500).json({ error: 'Failed to initialize upload storage' });
        }

        let bytesReceived = 0;
        let isFinalizing = false;
        let isCompleted = false;
        let aborted = false;

        function cleanupPart(cb) {
            releaseReservation();
            const doUnlink = () => {
                try {
                    if (fs.existsSync(partPath)) {
                        fs.unlinkSync(partPath);
                    }
                } catch (_) {}
                if (cb) cb();
            };
            if (fileStream.closed) {
                doUnlink();
            } else {
                fileStream.once('close', doUnlink);
                try { fileStream.destroy(); } catch (_) {}
            }
        }

        fileStream.on('error', (err) => {
            if (isCompleted || aborted) return;
            aborted = true;
            // Stop backpressure from leaving the request paused after disk failure.
            req.resume();
            cleanupPart(() => {
                const status = err.code === 'ENOSPC' ? 507 : 500;
                const msg = err.code === 'ENOSPC' ? 'Storage quota exceeded (ENOSPC)' : 'Failed to write file payload';
                if (!res.headersSent) {
                    res.setHeader('Connection', 'close');
                    res.status(status).json({ error: msg });
                }
            });
        });

        const hash = crypto.createHash('sha256');

        req.on('data', (chunk) => {
            if (isCompleted || aborted || isFinalizing) return;
            bytesReceived += chunk.length;

            if (bytesReceived > declaredSize || bytesReceived > maxFileBytes) {
                aborted = true;
                req.removeAllListeners('data');
                req.on('data', () => {});
                req.resume();
                cleanupPart(() => {
                    const status = (bytesReceived > maxFileBytes) ? 413 : 400;
                    const msg = (bytesReceived > maxFileBytes) ? 'File size exceeds maxFileBytes' : 'Streamed size exceeded declared size';
                    if (!res.headersSent) {
                        res.setHeader('Connection', 'close');
                        res.status(status).json({ error: msg });
                    }
                });
                return;
            }

            hash.update(chunk);
            if (!fileStream.write(chunk)) {
                req.pause();
                fileStream.once('drain', () => req.resume());
            }
        });

        req.on('error', () => {
            if (isCompleted || aborted || isFinalizing) return;
            aborted = true;
            cleanupPart(() => {
                if (!res.headersSent) {
                    res.status(400).json({ error: 'Upload stream encountered an error' });
                }
            });
        });

        req.on('aborted', () => {
            if (!isFinalizing && !isCompleted && !aborted) {
                aborted = true;
                cleanupPart();
            }
        });

        res.on('close', () => {
            if (!isFinalizing && !isCompleted && !aborted) {
                aborted = true;
                cleanupPart();
            }
        });

        req.on('end', () => {
            if (isCompleted || aborted) return;
            if (bytesReceived !== declaredSize) {
                aborted = true;
                cleanupPart(() => {
                    if (!res.headersSent) {
                        res.setHeader('Connection', 'close');
                        res.status(400).json({ error: 'Streamed size did not match declared size' });
                    }
                });
                return;
            }

            isFinalizing = true;
            fileStream.end(async () => {
                try {
                    const sha256 = hash.digest('hex');
                    await fs.promises.rename(partPath, binPath);
                    try { fs.chmodSync(binPath, 0o600); } catch (_) {}

                    const createdAt = now();
                    const expiresAt = createdAt + (retentionSeconds * 1000);
                    const fileRecord = {
                        id: fileId,
                        name: safeFilename,
                        size: declaredSize,
                        senderId: req.device.id,
                        senderName: req.device.name,
                        recipientId: recipient.id,
                        recipientName: recipient.name,
                        createdAt,
                        expiresAt,
                        sha256
                    };

                    atomicWriteFileSync(metaPath, JSON.stringify(fileRecord, null, 2), 0o600);

                    files.set(fileId, fileRecord);
                    storedBytes += declaredSize;

                    if (compositeKey) {
                        idempotentUploads.set(compositeKey, { file: fileRecord, fileId, timestamp: createdAt });
                        if (idempotentUploads.size > 200) {
                            const oldest = idempotentUploads.keys().next().value;
                            idempotentUploads.delete(oldest);
                        }
                    }

                    isCompleted = true;
                    releaseReservation();
                    notifyMailbox(fileRecord);

                    if (!res.headersSent) {
                        res.status(201).json({ file: fileRecord });
                    }
                } catch (err) {
                    releaseReservation();
                    try { if (fs.existsSync(binPath)) fs.unlinkSync(binPath); } catch (_) {}
                    try { if (fs.existsSync(partPath)) fs.unlinkSync(partPath); } catch (_) {}
                    try { if (fs.existsSync(metaPath)) fs.unlinkSync(metaPath); } catch (_) {}
                    if (!res.headersSent) {
                        res.status(500).json({ error: 'Failed to finalize uploaded file' });
                    }
                }
            });
        });
    });

    // GET /files
    router.get('/files', authenticateBearer, (req, res) => {
        const nowMs = now();
        const inbox = [];
        const outbox = [];
        const currentDeviceId = req.device.id;
        const expiredIds = [];

        for (const [id, f] of files.entries()) {
            if (f.expiresAt <= nowMs) {
                expiredIds.push(id);
                continue;
            }
            if (f.recipientId === currentDeviceId) {
                inbox.push(f);
            }
            if (f.senderId === currentDeviceId) {
                outbox.push(f);
            }
        }

        if (expiredIds.length > 0) {
            for (const expId of expiredIds) {
                deleteFileInternal(expId);
            }
        }

        res.json({ inbox, outbox });
    });

    // GET /files/:id/content (Bearer OR HttpOnly cookie)
    router.get('/files/:id/content', authenticateBearerOrCookie, (req, res) => {
        const { id } = req.params;
        if (!isValidUuid(id) || !files.has(id)) {
            return res.status(404).json({ error: 'File not found' });
        }

        const file = files.get(id);
        if (file.expiresAt <= now()) {
            deleteFileInternal(id);
            return res.status(404).json({ error: 'File not found' });
        }

        const currentDeviceId = req.device.id;
        if (currentDeviceId !== file.recipientId && currentDeviceId !== file.senderId) {
            return res.status(403).json({ error: 'Forbidden: access denied for this device' });
        }

        const binPath = path.join(blobsDir, `${file.id}.bin`);
        if (!fs.existsSync(binPath)) {
            return res.status(404).json({ error: 'File payload not found on disk' });
        }

        const asciiFallback = file.name.replace(/[^\x20-\x7E]/g, '_').replace(/["\\]/g, '_');
        const utf8Name = encodeURIComponent(file.name);

        res.setHeader('Content-Type', 'application/octet-stream');
        res.setHeader('X-Content-Type-Options', 'nosniff');
        res.setHeader('Content-Length', String(file.size));
        res.setHeader('Content-Disposition', `attachment; filename="${asciiFallback}"; filename*=UTF-8''${utf8Name}`);

        const readStream = fs.createReadStream(binPath);
        readStream.pipe(res);
        readStream.on('error', () => {
            if (!res.headersSent) {
                res.status(500).json({ error: 'Failed to stream payload' });
            }
        });
        res.on('close', () => {
            readStream.destroy();
        });
    });

    // POST /files/:id/ack
    router.post('/files/:id/ack', authenticateBearer, asyncHandler(async (req, res) => {
        const { id } = req.params;
        if (!isValidUuid(id)) {
            return res.status(404).json({ error: 'File not found' });
        }

        if (files.has(id)) {
            const file = files.get(id);
            if (req.device.id !== file.recipientId) {
                return res.status(403).json({ error: 'Forbidden: only recipient may acknowledge receipt' });
            }

            deleteFileInternal(id);
            return res.json({ ok: true });
        }

        if (tombstones.has(id)) {
            const tombstone = tombstones.get(id);
            if (req.device.id !== tombstone.recipientId) {
                return res.status(403).json({ error: 'Forbidden: only recipient may acknowledge receipt' });
            }
            return res.json({ ok: true });
        }

        return res.status(404).json({ error: 'File not found' });
    }));

    // DELETE /files/:id
    router.delete('/files/:id', authenticateBearer, asyncHandler(async (req, res) => {
        const { id } = req.params;
        if (!isValidUuid(id)) {
            return res.status(404).json({ error: 'File not found' });
        }

        if (files.has(id)) {
            const file = files.get(id);
            if (req.device.id !== file.senderId && req.device.id !== file.recipientId) {
                return res.status(403).json({ error: 'Forbidden: only sender or recipient can delete file' });
            }

            deleteFileInternal(id);
            return res.json({ ok: true });
        }

        if (tombstones.has(id)) {
            const tombstone = tombstones.get(id);
            if (req.device.id !== tombstone.senderId && req.device.id !== tombstone.recipientId) {
                return res.status(403).json({ error: 'Forbidden: only sender or recipient can delete file' });
            }
            return res.json({ ok: true });
        }

        return res.status(404).json({ error: 'File not found' });
    }));

    // 404 handler for unknown routes in /mailbox/api
    router.all('*', (req, res) => {
        res.status(404).json({ error: 'Endpoint not found' });
    });

    // Error handling middleware for router
    router.use((err, req, res, next) => {
        const status = err.status || err.statusCode || 500;
        res.status(status).json({ error: err.message || 'Internal server error' });
    });

    app.use('/mailbox/api', router);

    return {
        attachSocket,
        close: async () => {
            isClosed = true;
            clearInterval(cleanupTimer);
            for (const sockets of deviceSockets.values()) {
                for (const entry of sockets) {
                    try {
                        if (typeof entry.socket.removeListener === 'function') {
                            entry.socket.removeListener('message', entry.onMessage);
                            entry.socket.removeListener('close', entry.onClose);
                            entry.socket.removeListener('error', entry.onError);
                        } else if (typeof entry.socket.off === 'function') {
                            entry.socket.off('message', entry.onMessage);
                            entry.socket.off('close', entry.onClose);
                            entry.socket.off('error', entry.onError);
                        }
                    } catch (_) {}
                }
            }
            deviceSockets.clear();
            realtimeAuthority.clear();
            saveDevicesSync();
        }
    };
}
