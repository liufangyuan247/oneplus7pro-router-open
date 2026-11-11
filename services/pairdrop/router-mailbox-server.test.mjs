import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import crypto from 'node:crypto';
import express from 'express';
import { EventEmitter } from 'node:events';
import { Writable } from 'node:stream';
import { mountMailbox } from './router-mailbox-server.js';

test('Cleanup preserves a renamed upload until finalization commits metadata', async () => {
    const fix = await createServerFixture({ cleanupIntervalMs: 5 });
    const rename = fs.promises.rename;
    try {
        const registered = await fetch(fix.baseUrl + '/devices', {
            method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'Race test' })
        }).then(r => r.json());
        fs.promises.rename = async (from, to) => {
            await rename(from, to);
            if (String(to).endsWith('.bin')) {
                await new Promise(resolve => setTimeout(resolve, 40));
                assert.ok(fs.existsSync(to), 'cleanup removed an upload before metadata commit');
            }
        };
        const response = await fetch(fix.baseUrl + '/files?to=' + registered.device.id + '&name=race.bin&size=4', {
            method: 'POST', headers: { Authorization: 'Bearer ' + registered.device.id + ':' + registered.token }, body: 'data'
        });
        assert.equal(response.status, 201);
        const { file } = await response.json();
        assert.equal(fs.readFileSync(path.join(fix.dataDir, 'blobs', file.id + '.bin'), 'utf8'), 'data');
    } finally {
        fs.promises.rename = rename;
        await fix.cleanup();
    }
});

test('Disk write errors return 507, release quota and keep the server usable', async () => {
    const fix = await createServerFixture({ maxTotalBytes: 4 });
    const createWriteStream = fs.createWriteStream;
    try {
        const registered = await fetch(fix.baseUrl + '/devices', {
            method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'Disk test' })
        }).then(r => r.json());
        const headers = { Authorization: 'Bearer ' + registered.device.id + ':' + registered.token };
        fs.createWriteStream = () => new Writable({ write(chunk, encoding, callback) {
            callback(Object.assign(new Error('disk full'), { code: 'ENOSPC' }));
        }});
        const url = fix.baseUrl + '/files?to=' + registered.device.id + '&name=disk.bin&size=4';
        const failed = await fetch(url, { method: 'POST', headers, body: 'data' });
        assert.equal(failed.status, 507);
        assert.match((await failed.json()).error, /ENOSPC/);
        fs.createWriteStream = createWriteStream;
        const success = await fetch(url, { method: 'POST', headers, body: 'data' });
        assert.equal(success.status, 201);
    } finally {
        fs.createWriteStream = createWriteStream;
        await fix.cleanup();
    }
});

function createTempDir() {
    const dir = path.join(os.tmpdir(), `mb-test-${crypto.randomUUID()}`);
    fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
    return dir;
}

async function createServerFixture(options = {}) {
    const dataDir = options.dataDir || createTempDir();
    const mockClock = options.clock || {
        _time: 1_700_000_000_000,
        now() { return this._time; },
        advance(ms) { this._time += ms; }
    };

    const app = express();
    const mailbox = mountMailbox(app, {
        dataDir,
        clock: mockClock,
        ...options
    });

    const server = http.createServer(app);
    await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
    const port = server.address().port;
    const baseUrl = `http://127.0.0.1:${port}/mailbox/api`;

    return {
        dataDir,
        clock: mockClock,
        mailbox,
        server,
        baseUrl,
        port,
        async cleanup() {
            await mailbox.close();
            server.closeAllConnections?.();
            await new Promise((resolve) => server.close(resolve));
            try {
                fs.rmSync(dataDir, { recursive: true, force: true });
            } catch (_) {}
        }
    };
}

test('Text survives restart, is isolated to participants, and follows server expiry and cleanup', async () => {
    const fix = await createServerFixture();
    let restarted;
    const register = name => fetch(fix.baseUrl + '/devices', {
        method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({name})
    }).then(r => r.json());
    const auth = d => ({Authorization: `Bearer ${d.device.id}:${d.token}`});
    try {
        const [a, b, c] = await Promise.all(['Sender', 'Receiver', 'Other'].map(register));
        const text = '中文与 emoji 🌟\n<script>alert(1)</script>\nhttps://example.com/';
        const response = await fetch(fix.baseUrl + '/messages', {method: 'POST',
            headers: {...auth(a), 'Content-Type': 'application/json'}, body: JSON.stringify({to: b.device.id, text})});
        assert.equal(response.status, 201);
        const {file} = await response.json();
        assert.equal(file.kind, 'text');
        assert.equal(file.text, text);
        assert.equal(file.size, Buffer.byteLength(text));
        assert.equal(file.expiresAt - file.createdAt, 7200_000);
        assert.equal(fs.readFileSync(path.join(fix.dataDir, 'blobs', file.id + '.bin'), 'utf8'), text);
        const inbox = await fetch(fix.baseUrl + '/files', {headers: auth(b)}).then(r => r.json());
        assert.equal(inbox.inbox[0].text, text);
        assert.deepEqual(await fetch(fix.baseUrl + '/files', {headers: auth(c)}).then(r => r.json()), {inbox: [], outbox: []});
        assert.equal((await fetch(`${fix.baseUrl}/files/${file.id}/content`, {headers: auth(c)})).status, 403);
        assert.equal((await fetch(`${fix.baseUrl}/files/${file.id}/ack`, {method: 'POST', headers: auth(a)})).status, 403);
        await fix.mailbox.close();
        fix.server.closeAllConnections?.();
        await new Promise(resolve => fix.server.close(resolve));
        restarted = await createServerFixture({dataDir: fix.dataDir, clock: fix.clock});
        assert.equal((await fetch(restarted.baseUrl + '/files', {headers: auth(b)}).then(r => r.json())).inbox[0].text, text);
        const settings = {retentionSeconds: 60, maxFileBytes: 1024, maxTotalBytes: 4096};
        const key = fs.readFileSync(path.join(fix.dataDir, 'admin.key'), 'utf8').trim();
        assert.equal((await fetch(restarted.baseUrl + '/admin/settings', {method: 'PUT',
            headers: {'Content-Type': 'application/json', 'x-router-mailbox-admin': key}, body: JSON.stringify(settings)})).status, 200);
        fix.clock.advance(60_001);
        assert.equal((await fetch(restarted.baseUrl + '/files', {headers: auth(b)}).then(r => r.json())).inbox.length, 0);
        assert.equal(fs.existsSync(path.join(fix.dataDir, 'blobs', file.id + '.bin')), false);
        assert.equal(fs.existsSync(path.join(fix.dataDir, 'meta', file.id + '.json')), false);
    } finally {
        if (restarted) await restarted.cleanup();
        else await fix.cleanup();
    }
});

test('Text validates size, quotas, idempotent retries and storage failures', async () => {
    const fix = await createServerFixture({maxTotalBytes: 20, maxFileBytes: 16384});
    const write = fs.writeFileSync;
    try {
        const a = await fetch(fix.baseUrl + '/devices', {method: 'POST',
            headers: {'Content-Type': 'application/json'}, body: JSON.stringify({name: 'Sender'})}).then(r => r.json());
        const headers = {Authorization: `Bearer ${a.device.id}:${a.token}`, 'Content-Type': 'application/json'};
        const send = (text, extra = {}) => fetch(fix.baseUrl + '/messages', {method: 'POST',
            headers: {...headers, ...extra}, body: JSON.stringify({to: a.device.id, text})});
        assert.equal((await send('   ')).status, 400);
        assert.equal((await send(42)).status, 400);
        assert.equal((await send('🌟'.repeat(4097))).status, 413);
        assert.equal((await fetch(fix.baseUrl + '/messages', {method: 'POST', headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({to: a.device.id, text: 'hello'})})).status, 401);
        const retryHeaders = {'Idempotency-Key': crypto.randomUUID()};
        const first = await send('hello', retryHeaders).then(r => r.json());
        const second = await send('hello', retryHeaders).then(r => r.json());
        assert.equal(first.file.id, second.file.id);
        assert.equal((await send('different', retryHeaders)).status, 409);
        assert.equal((await send('x'.repeat(16))).status, 507);
        fs.writeFileSync = (...args) => {
            if (String(args[0]).includes('.bin.tmp.')) throw Object.assign(new Error('Full'), {code: 'ENOSPC'});
            return write(...args);
        };
        assert.equal((await send('fail')).status, 507);
        fs.writeFileSync = write;
        assert.equal((await fetch(fix.baseUrl + '/settings', {headers}).then(r => r.json())).storedBytes, 5);
        assert.equal((await fetch(`${fix.baseUrl}/files/${first.file.id}/ack`, {method: 'POST', headers})).status, 200);
        assert.equal((await fetch(fix.baseUrl + '/settings', {headers}).then(r => r.json())).storedBytes, 0);
        assert.equal((await send('works')).status, 201);
    } finally {
        fs.writeFileSync = write;
        await fix.cleanup();
    }
});

test('Mailbox updates notify only authenticated participant sockets without exposing text', async () => {
    const fix = await createServerFixture();
    try {
        const peers = [];
        for (const name of ['Sender', 'Receiver', 'Other']) {
            const d = await fetch(fix.baseUrl + '/devices', {method: 'POST',
                headers: {'Content-Type': 'application/json'}, body: JSON.stringify({name})}).then(r => r.json());
            const socket = new EventEmitter();
            socket.sent = [];
            socket.readyState = 1;
            socket.send = data => socket.sent.push(JSON.parse(data));
            fix.mailbox.attachSocket({id: crypto.randomUUID(), socket});
            socket.emit('message', JSON.stringify({type: 'mailbox-auth', id: d.device.id, token: d.token}));
            peers.push({...d, socket});
        }
        const [a, b, c] = peers;
        const headers = {Authorization: `Bearer ${a.device.id}:${a.token}`, 'Content-Type': 'application/json'};
        const {file} = await fetch(fix.baseUrl + '/messages', {method: 'POST', headers,
            body: JSON.stringify({to: b.device.id, text: 'Private test'})}).then(r => r.json());
        for (const p of [a, b]) assert.deepEqual(p.socket.sent.at(-1), {type: 'mailbox-updated'});
        assert.equal(c.socket.sent.some(m => m.type === 'mailbox-updated'), false);
        const notifications = b.socket.sent.filter(m => m.type === 'mailbox-updated').length;
        assert.equal((await fetch(`${fix.baseUrl}/files/${file.id}`, {method: 'DELETE', headers})).status, 200);
        assert.equal(b.socket.sent.filter(m => m.type === 'mailbox-updated').length, notifications + 1);
    } finally { await fix.cleanup(); }
});

test('Device registration, validation, auth isolation, and origin checks', async () => {
    const fix = await createServerFixture({ maxDevices: 3 });
    try {
        // 1. Invalid names
        const resBlank = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: '   ', platform: 'linux' })
        });
        assert.equal(resBlank.status, 400);
        const errBlank = await resBlank.json();
        assert.ok(errBlank.error);
        assert.equal(resBlank.headers.get('cache-control'), 'no-store');

        const longName = 'A'.repeat(129);
        const resLong = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: longName })
        });
        assert.equal(resLong.status, 400);

        // 2. Successful registration of Device A
        const resDevA = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Device Alpha', platform: 'android' })
        });
        assert.equal(resDevA.status, 201);
        const devAData = await resDevA.json();
        assert.ok(devAData.device.id);
        assert.equal(devAData.device.name, 'Device Alpha');
        assert.equal(devAData.device.platform, 'android');
        assert.equal(devAData.device.peerId, null);
        assert.equal(devAData.device.online, false);
        assert.ok(devAData.token);
        assert.equal(devAData.device.tokenHash, undefined);
        const cookieA = resDevA.headers.get('set-cookie');
        assert.ok(cookieA.includes('mailbox_auth='));
        assert.ok(cookieA.includes('HttpOnly'));
        assert.ok(cookieA.includes('SameSite=Strict'));

        // 3. Successful registration of Device B
        const resDevB = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Device Beta', platform: 'ios' })
        });
        assert.equal(resDevB.status, 201);
        const devBData = await resDevB.json();

        // 4. Successful registration of Device C (reaches maxDevices: 3)
        const resDevC = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Device Gamma', platform: 'windows' })
        });
        assert.equal(resDevC.status, 201);

        // 5. Registration of 4th device exceeds maxDevices
        const resDevD = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Device Delta' })
        });
        assert.equal(resDevD.status, 403);

        // 6. Auth verification: Bearer header checks
        const resNoAuth = await fetch(`${fix.baseUrl}/devices/me`);
        assert.equal(resNoAuth.status, 401);

        const resBadFormat = await fetch(`${fix.baseUrl}/devices/me`, {
            headers: { Authorization: 'Bearer invalidtokenformat' }
        });
        assert.equal(resBadFormat.status, 401);

        const resWrongToken = await fetch(`${fix.baseUrl}/devices/me`, {
            headers: { Authorization: `Bearer ${devAData.device.id}:wrong-token` }
        });
        assert.equal(resWrongToken.status, 401);

        const resValidAuth = await fetch(`${fix.baseUrl}/devices/me`, {
            headers: { Authorization: `Bearer ${devAData.device.id}:${devAData.token}` }
        });
        assert.equal(resValidAuth.status, 200);
        const devMe = await resValidAuth.json();
        assert.equal(devMe.device.id, devAData.device.id);

        // 7. PUT /devices/me update name
        const resRename = await fetch(`${fix.baseUrl}/devices/me`, {
            method: 'PUT',
            headers: {
                Authorization: `Bearer ${devAData.device.id}:${devAData.token}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ name: 'Renamed Alpha' })
        });
        assert.equal(resRename.status, 200);
        const renamed = await resRename.json();
        assert.equal(renamed.device.name, 'Renamed Alpha');

        // 8. Cross-origin mutation rejection
        const resEvilOrigin = await fetch(`${fix.baseUrl}/devices/me`, {
            method: 'PUT',
            headers: {
                Authorization: `Bearer ${devAData.device.id}:${devAData.token}`,
                'Content-Type': 'application/json',
                Origin: 'http://evil.com'
            },
            body: JSON.stringify({ name: 'Hacked Alpha' })
        });
        assert.equal(resEvilOrigin.status, 403);

        // Same host origin passes
        const resSameOrigin = await fetch(`${fix.baseUrl}/devices/me`, {
            method: 'PUT',
            headers: {
                Authorization: `Bearer ${devAData.device.id}:${devAData.token}`,
                'Content-Type': 'application/json',
                Origin: `http://127.0.0.1:${fix.port}`
            },
            body: JSON.stringify({ name: 'Renamed Alpha Safe' })
        });
        assert.equal(resSameOrigin.status, 200);

        // 9. POST /presence updates volatile tracking and peerId
        const resPresence = await fetch(`${fix.baseUrl}/presence`, {
            method: 'POST',
            headers: {
                Authorization: `Bearer ${devAData.device.id}:${devAData.token}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ peerId: 'peer-alpha-123' })
        });
        assert.equal(resPresence.status, 200);
        const presData = await resPresence.json();
        assert.equal(presData.device.peerId, 'peer-alpha-123');
        assert.equal(presData.device.online, true);

        // GET /devices lists all devices without credentials, with proper online statuses
        const resAllDevs = await fetch(`${fix.baseUrl}/devices`, {
            headers: { Authorization: `Bearer ${devAData.device.id}:${devAData.token}` }
        });
        assert.equal(resAllDevs.status, 200);
        const allDevs = await resAllDevs.json();
        assert.equal(allDevs.devices.length, 3);
        const foundA = allDevs.devices.find(d => d.id === devAData.device.id);
        const foundB = allDevs.devices.find(d => d.id === devBData.device.id);
        assert.equal(foundA.online, true);
        assert.equal(foundA.peerId, 'peer-alpha-123');
        assert.equal(foundB.online, false);
        assert.equal(foundB.peerId, null);
    } finally {
        await fix.cleanup();
    }
});

test('Offline delivery across backend restart, native download with cookie, and ack isolation', async () => {
    const dataDir = createTempDir();
    const mockClock = {
        _time: 1_700_000_000_000,
        now() { return this._time; },
        advance(ms) { this._time += ms; }
    };

    let devA, devB, uploadedFile;

    // Phase 1: Server 1
    {
        const app1 = express();
        const mailbox1 = mountMailbox(app1, { dataDir, clock: mockClock });
        const server1 = http.createServer(app1);
        await new Promise((r) => server1.listen(0, '127.0.0.1', r));
        const url1 = `http://127.0.0.1:${server1.address().port}/mailbox/api`;

        // Register A & B
        const rA = await fetch(`${url1}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Device A' })
        });
        devA = await rA.json();

        const rB = await fetch(`${url1}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Device B' })
        });
        devB = await rB.json();

        // B establishes presence then goes offline (advance clock by 35 seconds)
        await fetch(`${url1}/presence`, {
            method: 'POST',
            headers: {
                Authorization: `Bearer ${devB.device.id}:${devB.token}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ peerId: 'peer-b-session-1' })
        });
        mockClock.advance(35_000);

        // Upload payload from A to offline B
        const payload = Buffer.from('important-offline-file-contents-123456');
        const uploadUrl = `${url1}/files?to=${encodeURIComponent(devB.device.id)}&name=${encodeURIComponent('report 2026.pdf')}&size=${payload.length}`;
        const resUpload = await fetch(uploadUrl, {
            method: 'POST',
            headers: {
                Authorization: `Bearer ${devA.device.id}:${devA.token}`,
                'Content-Type': 'application/octet-stream',
                'Idempotency-Key': '11111111-2222-3333-4444-555555555555'
            },
            body: payload
        });
        assert.equal(resUpload.status, 201);
        const upBody = await resUpload.json();
        uploadedFile = upBody.file;
        assert.equal(uploadedFile.name, 'report 2026.pdf');
        assert.equal(uploadedFile.size, payload.length);
        assert.equal(uploadedFile.senderId, devA.device.id);
        assert.equal(uploadedFile.recipientId, devB.device.id);

        const expectedSha = crypto.createHash('sha256').update(payload).digest('hex');
        assert.equal(uploadedFile.sha256, expectedSha);

        // Check A outbox and B inbox
        const resFilesA = await fetch(`${url1}/files`, {
            headers: { Authorization: `Bearer ${devA.device.id}:${devA.token}` }
        });
        const filesA = await resFilesA.json();
        assert.equal(filesA.outbox.length, 1);
        assert.equal(filesA.inbox.length, 0);

        const resFilesB = await fetch(`${url1}/files`, {
            headers: { Authorization: `Bearer ${devB.device.id}:${devB.token}` }
        });
        const filesB = await resFilesB.json();
        assert.equal(filesB.inbox.length, 1);
        assert.equal(filesB.outbox.length, 0);

        // Close server 1 cleanly
        await mailbox1.close();
        server1.closeAllConnections?.();
        await new Promise((r) => server1.close(r));
    }

    // Phase 2: Server 2 (Simulating restart)
    {
        const app2 = express();
        const mailbox2 = mountMailbox(app2, { dataDir, clock: mockClock });
        const server2 = http.createServer(app2);
        await new Promise((r) => server2.listen(0, '127.0.0.1', r));
        const url2 = `http://127.0.0.1:${server2.address().port}/mailbox/api`;

        // Check online status immediately after restart: all devices must be offline until presence
        const resDevs = await fetch(`${url2}/devices`, {
            headers: { Authorization: `Bearer ${devB.device.id}:${devB.token}` }
        });
        const devsData = await resDevs.json();
        for (const d of devsData.devices) {
            assert.equal(d.online, false);
            assert.equal(d.peerId, null);
        }

        // Check B inbox still has pending file
        const resInbox = await fetch(`${url2}/files`, {
            headers: { Authorization: `Bearer ${devB.device.id}:${devB.token}` }
        });
        const inboxData = await resInbox.json();
        assert.equal(inboxData.inbox.length, 1);
        assert.equal(inboxData.inbox[0].id, uploadedFile.id);

        // Download via HttpOnly Cookie (Native browser anchor simulation)
        const cookieAuth = `mailbox_auth=${encodeURIComponent(`${devB.device.id}:${devB.token}`)}`;
        const resDownloadCookie = await fetch(`${url2}/files/${uploadedFile.id}/content`, {
            headers: { Cookie: cookieAuth }
        });
        assert.equal(resDownloadCookie.status, 200);
        assert.equal(resDownloadCookie.headers.get('content-type'), 'application/octet-stream');
        assert.equal(resDownloadCookie.headers.get('x-content-type-options'), 'nosniff');
        assert.equal(resDownloadCookie.headers.get('content-length'), String(uploadedFile.size));
        const downloadedBuf = Buffer.from(await resDownloadCookie.arrayBuffer());
        assert.equal(downloadedBuf.toString(), 'important-offline-file-contents-123456');

        // Verify file is NOT auto-deleted on download
        const resAfterDl = await fetch(`${url2}/files`, {
            headers: { Authorization: `Bearer ${devB.device.id}:${devB.token}` }
        });
        const filesAfterDl = await resAfterDl.json();
        assert.equal(filesAfterDl.inbox.length, 1);

        // Sender A cannot ACK file (recipient only)
        const resSenderAck = await fetch(`${url2}/files/${uploadedFile.id}/ack`, {
            method: 'POST',
            headers: { Authorization: `Bearer ${devA.device.id}:${devA.token}` }
        });
        assert.equal(resSenderAck.status, 403);

        // Recipient B ACKs file
        const resAck = await fetch(`${url2}/files/${uploadedFile.id}/ack`, {
            method: 'POST',
            headers: { Authorization: `Bearer ${devB.device.id}:${devB.token}` }
        });
        assert.equal(resAck.status, 200);
        const ackData = await resAck.json();
        assert.equal(ackData.ok, true);

        // Recipient B ACKs again: idempotent response
        const resAck2 = await fetch(`${url2}/files/${uploadedFile.id}/ack`, {
            method: 'POST',
            headers: { Authorization: `Bearer ${devB.device.id}:${devB.token}` }
        });
        assert.equal(resAck2.status, 200);
        const ackData2 = await resAck2.json();
        assert.equal(ackData2.ok, true);

        // Sender A trying to ack now still gets 403 Forbidden
        const resSenderAck2 = await fetch(`${url2}/files/${uploadedFile.id}/ack`, {
            method: 'POST',
            headers: { Authorization: `Bearer ${devA.device.id}:${devA.token}` }
        });
        assert.equal(resSenderAck2.status, 403);

        // File is gone from inbox
        const resFinalFiles = await fetch(`${url2}/files`, {
            headers: { Authorization: `Bearer ${devB.device.id}:${devB.token}` }
        });
        const finalFiles = await resFinalFiles.json();
        assert.equal(finalFiles.inbox.length, 0);

        await mailbox2.close();
        server2.closeAllConnections?.();
        await new Promise((r) => server2.close(r));
    }

    try {
        fs.rmSync(dataDir, { recursive: true, force: true });
    } catch (_) {}
});

test('Streamed size validation, client abort cleanup, and interrupted upload recovery', async () => {
    const fix = await createServerFixture();
    try {
        // Register A and B
        const rA = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Sender A' })
        });
        const devA = await rA.json();

        const rB = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Receiver B' })
        });
        const devB = await rB.json();

        // 1. Stream sent less than declared size
        const shortStreamRes = await new Promise((resolve) => {
            const req = http.request({
                hostname: '127.0.0.1',
                port: fix.port,
                path: `/mailbox/api/files?to=${devB.device.id}&name=short.bin&size=100`,
                method: 'POST',
                headers: {
                    Authorization: `Bearer ${devA.device.id}:${devA.token}`,
                    'Content-Type': 'application/octet-stream'
                }
            }, (res) => {
                let data = '';
                res.on('data', c => data += c);
                res.on('end', () => resolve({ status: res.statusCode, body: JSON.parse(data) }));
            });
            req.write(Buffer.alloc(40)); // Only 40 bytes out of 100
            req.end();
        });
        assert.equal(shortStreamRes.status, 400);

        // 2. Stream sent more than declared size
        const overStreamRes = await new Promise((resolve) => {
            const req = http.request({
                hostname: '127.0.0.1',
                port: fix.port,
                path: `/mailbox/api/files?to=${devB.device.id}&name=over.bin&size=50`,
                method: 'POST',
                headers: {
                    Authorization: `Bearer ${devA.device.id}:${devA.token}`,
                    'Content-Type': 'application/octet-stream'
                }
            }, (res) => {
                let data = '';
                res.on('data', c => data += c);
                res.on('end', () => resolve({ status: res.statusCode }));
            });
            req.on('error', () => {
                // Connection closed or reset by server upon exceeding declared size
                resolve({ status: 400 });
            });
            req.write(Buffer.alloc(60)); // 60 bytes > 50 declared
            req.end();
        });
        assert.equal(overStreamRes.status, 400);

        // 3. Client socket abruptly aborts midway
        await new Promise((resolve) => {
            const req = http.request({
                hostname: '127.0.0.1',
                port: fix.port,
                path: `/mailbox/api/files?to=${devB.device.id}&name=aborted.bin&size=1000`,
                method: 'POST',
                headers: {
                    Authorization: `Bearer ${devA.device.id}:${devA.token}`,
                    'Content-Type': 'application/octet-stream'
                }
            });
            req.on('error', () => {});
            req.write(Buffer.alloc(100));
            setTimeout(() => {
                req.destroy();
                setTimeout(resolve, 50);
            }, 20);
        });

        // Verify no .part files are leftover in blobs directory
        const blobsDir = path.join(fix.dataDir, 'blobs');
        const remainingBlobs = fs.readdirSync(blobsDir);
        const partFiles = remainingBlobs.filter(f => f.endsWith('.part'));
        assert.equal(partFiles.length, 0);

        // Check settings: storedBytes must be 0 and reservations released
        const resSettings = await fetch(`${fix.baseUrl}/settings`, {
            headers: { Authorization: `Bearer ${devA.device.id}:${devA.token}` }
        });
        const settings = await resSettings.json();
        assert.equal(settings.storedBytes, 0);

        // 4. Interrupted upload from crashed process cleaned up on restart, along with orphaned .bin files
        const strayPartPath = path.join(blobsDir, '00000000-0000-4000-8000-000000000000.part');
        const strayBinPath = path.join(blobsDir, '11111111-2222-4444-8888-000000000000.bin');
        fs.writeFileSync(strayPartPath, Buffer.from('abandoned partial upload'));
        fs.writeFileSync(strayBinPath, Buffer.from('orphan binary payload without metadata'));
        assert.ok(fs.existsSync(strayPartPath));
        assert.ok(fs.existsSync(strayBinPath));

        // Restart mailbox on same dataDir
        await fix.mailbox.close();
        fix.server.closeAllConnections?.();
        await new Promise(r => fix.server.close(r));

        const appRestart = express();
        const mailboxRestart = mountMailbox(appRestart, { dataDir: fix.dataDir, clock: fix.clock });
        assert.ok(!fs.existsSync(strayPartPath)); // Stray .part file was removed on startup
        assert.ok(!fs.existsSync(strayBinPath)); // Orphan .bin file was removed on startup
        await mailboxRestart.close();
    } finally {
        await fix.cleanup();
    }
});

test('Retention setting default, directRoomSecret, update, file expiration, and persistence', async () => {
    const dataDir = createTempDir();
    const mockClock = {
        _time: 1_700_000_000_000,
        now() { return this._time; },
        advance(ms) { this._time += ms; }
    };

    let devA, devB, fileRecord, directSecret;

    {
        const app1 = express();
        const mailbox1 = mountMailbox(app1, { dataDir, clock: mockClock });
        const server1 = http.createServer(app1);
        await new Promise(r => server1.listen(0, '127.0.0.1', r));
        const url1 = `http://127.0.0.1:${server1.address().port}/mailbox/api`;

        const rA = await fetch(`${url1}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Device A' })
        });
        devA = await rA.json();

        const rB = await fetch(`${url1}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Device B' })
        });
        devB = await rB.json();

        // 1. Verify default retention = 7200 seconds and persistent 64-hex directRoomSecret
        const resSet = await fetch(`${url1}/settings`, {
            headers: { Authorization: `Bearer ${devA.device.id}:${devA.token}` }
        });
        const set1 = await resSet.json();
        assert.equal(set1.retentionSeconds, 7200);
        assert.ok(typeof set1.directRoomSecret === 'string');
        assert.equal(set1.directRoomSecret.length, 64);
        assert.ok(/^[0-9a-f]{64}$/i.test(set1.directRoomSecret));
        directSecret = set1.directRoomSecret;

        // 2. Upload file under default retention
        const payload = Buffer.from('retention test data');
        const resUp = await fetch(`${url1}/files?to=${devB.device.id}&name=retention.txt&size=${payload.length}`, {
            method: 'POST',
            headers: {
                Authorization: `Bearer ${devA.device.id}:${devA.token}`,
                'Content-Type': 'application/octet-stream'
            },
            body: payload
        });
        assert.equal(resUp.status, 201);
        fileRecord = (await resUp.json()).file;
        assert.equal(fileRecord.expiresAt, fileRecord.createdAt + 7200 * 1000);

        // 2b. Ordinary client PUT /settings must always return 403 Forbidden
        const resOrdinaryPut = await fetch(`${url1}/settings`, {
            method: 'PUT',
            headers: {
                Authorization: `Bearer ${devA.device.id}:${devA.token}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ retentionSeconds: 120 })
        });
        assert.equal(resOrdinaryPut.status, 403);

        const adminKey = fs.readFileSync(path.join(dataDir, 'admin.key'), 'utf8').trim();
        assert.equal(adminKey.length, 64);

        // 3. Invalid retention values rejected on admin PUT (must be integer 60..604800, expects all 3 fields)
        const resInvalid1 = await fetch(`${url1}/admin/settings`, {
            method: 'PUT',
            headers: {
                'x-router-mailbox-admin': adminKey,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ retentionSeconds: 30, maxFileBytes: 512 * 1024 * 1024, maxTotalBytes: 2 * 1024 * 1024 * 1024 }) // < 60
        });
        assert.equal(resInvalid1.status, 400);

        const resInvalid2 = await fetch(`${url1}/admin/settings`, {
            method: 'PUT',
            headers: {
                'x-router-mailbox-admin': adminKey,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ retentionSeconds: 1000000, maxFileBytes: 512 * 1024 * 1024, maxTotalBytes: 2 * 1024 * 1024 * 1024 }) // > 604800
        });
        assert.equal(resInvalid2.status, 400);

        // 4. Update retention to 120 seconds (2 minutes) via admin PUT
        const resUpdate = await fetch(`${url1}/admin/settings`, {
            method: 'PUT',
            headers: {
                'x-router-mailbox-admin': adminKey,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ retentionSeconds: 120, maxFileBytes: 512 * 1024 * 1024, maxTotalBytes: 2 * 1024 * 1024 * 1024 })
        });
        assert.equal(resUpdate.status, 200);
        const updatedSettings = await resUpdate.json();
        assert.equal(updatedSettings.retentionSeconds, 120);
        assert.equal(updatedSettings.directRoomSecret, undefined); // No leak of directRoomSecret on admin endpoint

        // Check that existing pending file was updated to createdAt + 120s
        const resFiles = await fetch(`${url1}/files`, {
            headers: { Authorization: `Bearer ${devB.device.id}:${devB.token}` }
        });
        const filesData = await resFiles.json();
        assert.equal(filesData.inbox.length, 1);
        assert.equal(filesData.inbox[0].expiresAt, fileRecord.createdAt + 120 * 1000);

        // Advance clock past expiration
        mockClock.advance(125 * 1000);

        // Eager expiration: GET /files immediately omits expired files even before cleanup timer
        const resExpiredFiles = await fetch(`${url1}/files`, {
            headers: { Authorization: `Bearer ${devB.device.id}:${devB.token}` }
        });
        const expiredData = await resExpiredFiles.json();
        assert.equal(expiredData.inbox.length, 0);

        // Immediate 404 for download of expired file
        const resExpiredDownload = await fetch(`${url1}/files/${fileRecord.id}/content`, {
            headers: { Authorization: `Bearer ${devB.device.id}:${devB.token}` }
        });
        assert.equal(resExpiredDownload.status, 404);

        await mailbox1.close();
        server1.closeAllConnections?.();
        await new Promise(r => server1.close(r));
    }

    // 5. Verify retention setting and directRoomSecret persisted across restart
    {
        const app2 = express();
        const mailbox2 = mountMailbox(app2, { dataDir, clock: mockClock });
        const server2 = http.createServer(app2);
        await new Promise(r => server2.listen(0, '127.0.0.1', r));
        const url2 = `http://127.0.0.1:${server2.address().port}/mailbox/api`;

        const resSet2 = await fetch(`${url2}/settings`, {
            headers: { Authorization: `Bearer ${devA.device.id}:${devA.token}` }
        });
        const set2 = await resSet2.json();
        assert.equal(set2.retentionSeconds, 120);
        assert.equal(set2.directRoomSecret, directSecret);

        await mailbox2.close();
        server2.closeAllConnections?.();
        await new Promise(r => server2.close(r));
    }

    try {
        fs.rmSync(dataDir, { recursive: true, force: true });
    } catch (_) {}
});

test('Simultaneous quota enforcement with concurrent upload reservations', async () => {
    // maxFileBytes: 400, maxTotalBytes: 700
    const fix = await createServerFixture({
        maxFileBytes: 400,
        maxTotalBytes: 700,
        maxFiles: 5
    });

    try {
        const rA = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Sender Alpha' })
        });
        const devA = await rA.json();

        const rB = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Receiver Beta' })
        });
        const devB = await rB.json();

        // 1. Single file exceeds maxFileBytes (450 > 400)
        const resExceedMaxFile = await fetch(`${fix.baseUrl}/files?to=${devB.device.id}&name=big.bin&size=450`, {
            method: 'POST',
            headers: {
                Authorization: `Bearer ${devA.device.id}:${devA.token}`,
                'Content-Type': 'application/octet-stream'
            },
            body: Buffer.alloc(450)
        });
        assert.equal(resExceedMaxFile.status, 413);

        // 2. Start two concurrent in-flight uploads:
        // Upload 1: 350 bytes
        // Upload 2: 300 bytes
        // Total reserved = 350 + 300 = 650 <= 700
        const req1 = http.request({
            hostname: '127.0.0.1',
            port: fix.port,
            path: `/mailbox/api/files?to=${devB.device.id}&name=stream1.bin&size=350`,
            method: 'POST',
            headers: {
                Authorization: `Bearer ${devA.device.id}:${devA.token}`,
                'Content-Type': 'application/octet-stream'
            }
        });
        req1.on('error', () => {});
        req1.write(Buffer.alloc(50));

        const req2 = http.request({
            hostname: '127.0.0.1',
            port: fix.port,
            path: `/mailbox/api/files?to=${devB.device.id}&name=stream2.bin&size=300`,
            method: 'POST',
            headers: {
                Authorization: `Bearer ${devA.device.id}:${devA.token}`,
                'Content-Type': 'application/octet-stream'
            }
        });
        req2.on('error', () => {});
        req2.write(Buffer.alloc(50));

        await new Promise(r => setTimeout(r, 20));

        // 3. Attempt 3rd upload with declared size 100 bytes:
        // 650 (reserved) + 100 = 750 > 700 (maxTotalBytes)
        // MUST immediately return 507 Quota Exceeded!
        const resExceedQuota = await fetch(`${fix.baseUrl}/files?to=${devB.device.id}&name=stream3.bin&size=100`, {
            method: 'POST',
            headers: {
                Authorization: `Bearer ${devA.device.id}:${devA.token}`,
                'Content-Type': 'application/octet-stream'
            },
            body: Buffer.alloc(100)
        });
        assert.equal(resExceedQuota.status, 507);
        const err507 = await resExceedQuota.json();
        assert.ok(err507.error);

        // 4. Abort upload 2 -> reservation for 300 bytes is freed
        req2.destroy();
        await new Promise(r => setTimeout(r, 30));

        // Complete upload 1 (write remaining 300 bytes out of 350)
        const upload1Promise = new Promise((resolve) => {
            req1.on('response', (res) => {
                let data = '';
                res.on('data', c => data += c);
                res.on('end', () => resolve({ status: res.statusCode, body: JSON.parse(data) }));
            });
            req1.write(Buffer.alloc(300));
            req1.end();
        });
        const up1Res = await upload1Promise;
        assert.equal(up1Res.status, 201);
        const file1 = up1Res.body.file;

        // Currently stored: 350 bytes, reserved: 0
        // Now an upload of 300 bytes (350 + 300 = 650 <= 700) will succeed!
        const resUpSuccess = await fetch(`${fix.baseUrl}/files?to=${devB.device.id}&name=success.bin&size=300`, {
            method: 'POST',
            headers: {
                Authorization: `Bearer ${devA.device.id}:${devA.token}`,
                'Content-Type': 'application/octet-stream'
            },
            body: Buffer.alloc(300)
        });
        assert.equal(resUpSuccess.status, 201);

        // Total stored is now 350 + 300 = 650. Upload of 100 exceeds 700
        const resTooMuch = await fetch(`${fix.baseUrl}/files?to=${devB.device.id}&name=fail.bin&size=100`, {
            method: 'POST',
            headers: {
                Authorization: `Bearer ${devA.device.id}:${devA.token}`,
                'Content-Type': 'application/octet-stream'
            },
            body: Buffer.alloc(100)
        });
        assert.equal(resTooMuch.status, 507);

        // Sender deletes file 1 -> stored drops to 300
        const resDel = await fetch(`${fix.baseUrl}/files/${file1.id}`, {
            method: 'DELETE',
            headers: { Authorization: `Bearer ${devA.device.id}:${devA.token}` }
        });
        assert.equal(resDel.status, 200);

        // Now upload of 100 bytes (300 + 100 = 400 <= 700) succeeds!
        const resUpAfterDel = await fetch(`${fix.baseUrl}/files?to=${devB.device.id}&name=afterdel.bin&size=100`, {
            method: 'POST',
            headers: {
                Authorization: `Bearer ${devA.device.id}:${devA.token}`,
                'Content-Type': 'application/octet-stream'
            },
            body: Buffer.alloc(100)
        });
        assert.equal(resUpAfterDel.status, 201);
    } finally {
        await fix.cleanup();
    }
});

test('DELETE /devices/me self-revocation and conflict handling', async () => {
    const fix = await createServerFixture();
    try {
        const rA = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Sender A' })
        });
        const devA = await rA.json();

        const rB = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Recipient B' })
        });
        const devB = await rB.json();

        // 1. Upload a pending file from A to B
        const payload = Buffer.from('device me test file');
        const resUp = await fetch(`${fix.baseUrl}/files?to=${devB.device.id}&name=test.txt&size=${payload.length}`, {
            method: 'POST',
            headers: {
                Authorization: `Bearer ${devA.device.id}:${devA.token}`,
                'Content-Type': 'application/octet-stream'
            },
            body: payload
        });
        assert.equal(resUp.status, 201);
        const file = (await resUp.json()).file;

        // 2. Both A (sender) and B (recipient) cannot delete /devices/me while file is pending -> 409
        const resDelAConflict = await fetch(`${fix.baseUrl}/devices/me`, {
            method: 'DELETE',
            headers: { Authorization: `Bearer ${devA.device.id}:${devA.token}` }
        });
        assert.equal(resDelAConflict.status, 409);

        const resDelBConflict = await fetch(`${fix.baseUrl}/devices/me`, {
            method: 'DELETE',
            headers: { Authorization: `Bearer ${devB.device.id}:${devB.token}` }
        });
        assert.equal(resDelBConflict.status, 409);

        // 3. Recipient B acknowledges file receipt
        const resAck = await fetch(`${fix.baseUrl}/files/${file.id}/ack`, {
            method: 'POST',
            headers: { Authorization: `Bearer ${devB.device.id}:${devB.token}` }
        });
        assert.equal(resAck.status, 200);

        // 4. Now B can safely delete itself
        const resDelB = await fetch(`${fix.baseUrl}/devices/me`, {
            method: 'DELETE',
            headers: { Authorization: `Bearer ${devB.device.id}:${devB.token}` }
        });
        assert.equal(resDelB.status, 200);
        assert.deepEqual(await resDelB.json(), { ok: true });
        const cookieB = resDelB.headers.get('set-cookie');
        assert.ok(cookieB.includes('Max-Age=0'));

        // Subsequent access with B's credentials fails with 401
        const resGetB = await fetch(`${fix.baseUrl}/devices/me`, {
            headers: { Authorization: `Bearer ${devB.device.id}:${devB.token}` }
        });
        assert.equal(resGetB.status, 401);

        // Device A is still registered and active
        const resGetA = await fetch(`${fix.baseUrl}/devices/me`, {
            headers: { Authorization: `Bearer ${devA.device.id}:${devA.token}` }
        });
        assert.equal(resGetA.status, 200);

        // Now A can also delete itself
        const resDelA = await fetch(`${fix.baseUrl}/devices/me`, {
            method: 'DELETE',
            headers: { Authorization: `Bearer ${devA.device.id}:${devA.token}` }
        });
        assert.equal(resDelA.status, 200);
        assert.deepEqual(await resDelA.json(), { ok: true });
    } finally {
        await fix.cleanup();
    }
});

test('DELETE /files/:id allows sender or recipient to revoke/remove file', async () => {
    const fix = await createServerFixture();
    try {
        const rA = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Sender A' })
        });
        const devA = await rA.json();

        const rB = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Recipient B' })
        });
        const devB = await rB.json();

        const rC = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Outsider C' })
        });
        const devC = await rC.json();

        // Upload file
        const payload = Buffer.from('deleteme');
        const resUp = await fetch(`${fix.baseUrl}/files?to=${devB.device.id}&name=deleteme.txt&size=${payload.length}`, {
            method: 'POST',
            headers: {
                Authorization: `Bearer ${devA.device.id}:${devA.token}`,
                'Content-Type': 'application/octet-stream'
            },
            body: payload
        });
        const file = (await resUp.json()).file;

        // Outsider C cannot delete file
        const resDelOutsider = await fetch(`${fix.baseUrl}/files/${file.id}`, {
            method: 'DELETE',
            headers: { Authorization: `Bearer ${devC.device.id}:${devC.token}` }
        });
        assert.equal(resDelOutsider.status, 403);

        // Sender A revokes file
        const resDelSender = await fetch(`${fix.baseUrl}/files/${file.id}`, {
            method: 'DELETE',
            headers: { Authorization: `Bearer ${devA.device.id}:${devA.token}` }
        });
        assert.equal(resDelSender.status, 200);
        assert.deepEqual(await resDelSender.json(), { ok: true });

        // Repeated DELETE by sender or recipient returns ok:true (idempotent)
        const resDelRepeat = await fetch(`${fix.baseUrl}/files/${file.id}`, {
            method: 'DELETE',
            headers: { Authorization: `Bearer ${devB.device.id}:${devB.token}` }
        });
        assert.equal(resDelRepeat.status, 200);
        assert.deepEqual(await resDelRepeat.json(), { ok: true });

        // Outsider still cannot delete even after deletion
        const resDelOutsider2 = await fetch(`${fix.baseUrl}/files/${file.id}`, {
            method: 'DELETE',
            headers: { Authorization: `Bearer ${devC.device.id}:${devC.token}` }
        });
        assert.equal(resDelOutsider2.status, 403);
    } finally {
        await fix.cleanup();
    }
});

test('Scoped idempotency keys across recipient pairs and cleanup on file deletion', async () => {
    const fix = await createServerFixture();
    try {
        const rA = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Sender A' })
        });
        const devA = await rA.json();

        const rB = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Recipient B' })
        });
        const devB = await rB.json();

        const rC = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Recipient C' })
        });
        const devC = await rC.json();

        const sharedIdemKey = crypto.randomUUID();
        const payload = Buffer.from('idem payload');

        // 1. Upload to B with Idempotency-Key
        const res1 = await fetch(`${fix.baseUrl}/files?to=${devB.device.id}&name=idem.txt&size=${payload.length}`, {
            method: 'POST',
            headers: {
                Authorization: `Bearer ${devA.device.id}:${devA.token}`,
                'Content-Type': 'application/octet-stream',
                'Idempotency-Key': sharedIdemKey
            },
            body: payload
        });
        assert.equal(res1.status, 201);
        const f1 = (await res1.json()).file;

        // 2. Repeating upload to B with same Idempotency-Key returns existing file f1
        const resRepeat = await fetch(`${fix.baseUrl}/files?to=${devB.device.id}&name=idem.txt&size=${payload.length}`, {
            method: 'POST',
            headers: {
                Authorization: `Bearer ${devA.device.id}:${devA.token}`,
                'Content-Type': 'application/octet-stream',
                'Idempotency-Key': sharedIdemKey
            },
            body: payload
        });
        assert.equal(resRepeat.status, 200);
        const f1Repeat = (await resRepeat.json()).file;
        assert.equal(f1Repeat.id, f1.id);

        // 3. Upload to C with the SAME Idempotency-Key does NOT return f1 because it is scoped to recipient C!
        const resC = await fetch(`${fix.baseUrl}/files?to=${devC.device.id}&name=idem.txt&size=${payload.length}`, {
            method: 'POST',
            headers: {
                Authorization: `Bearer ${devA.device.id}:${devA.token}`,
                'Content-Type': 'application/octet-stream',
                'Idempotency-Key': sharedIdemKey
            },
            body: payload
        });
        assert.equal(resC.status, 201);
        const f2 = (await resC.json()).file;
        assert.notEqual(f2.id, f1.id);

        // 4. Delete file f1 by B -> key is purged from idempotency tracking
        await fetch(`${fix.baseUrl}/files/${f1.id}/ack`, {
            method: 'POST',
            headers: { Authorization: `Bearer ${devB.device.id}:${devB.token}` }
        });

        // Now uploading with sharedIdemKey to B creates a new file instead of returning stale f1
        const resAfterDelete = await fetch(`${fix.baseUrl}/files?to=${devB.device.id}&name=idem2.txt&size=${payload.length}`, {
            method: 'POST',
            headers: {
                Authorization: `Bearer ${devA.device.id}:${devA.token}`,
                'Content-Type': 'application/octet-stream',
                'Idempotency-Key': sharedIdemKey
            },
            body: payload
        });
        assert.equal(resAfterDelete.status, 201);
        const f3 = (await resAfterDelete.json()).file;
        assert.notEqual(f3.id, f1.id);
    } finally {
        await fix.cleanup();
    }
});

test('Download stream is destroyed on client abort', async () => {
    const fix = await createServerFixture();
    try {
        const rA = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Sender A' })
        });
        const devA = await rA.json();

        const rB = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: 'Recipient B' })
        });
        const devB = await rB.json();

        // Upload large file
        const payload = Buffer.alloc(100_000, 'x');
        const resUp = await fetch(`${fix.baseUrl}/files?to=${devB.device.id}&name=large.bin&size=${payload.length}`, {
            method: 'POST',
            headers: {
                Authorization: `Bearer ${devA.device.id}:${devA.token}`,
                'Content-Type': 'application/octet-stream'
            },
            body: payload
        });
        const file = (await resUp.json()).file;

        // Initiate download and abort client socket after first chunk
        await new Promise((resolve) => {
            const req = http.request({
                hostname: '127.0.0.1',
                port: fix.port,
                path: `/mailbox/api/files/${file.id}/content`,
                method: 'GET',
                headers: { Authorization: `Bearer ${devB.device.id}:${devB.token}` }
            }, (res) => {
                res.once('data', () => {
                    req.destroy();
                    setTimeout(resolve, 50);
                });
            });
            req.on('error', () => {});
            req.end();
        });

        // Ensure server remained stable and subsequent file operations work
        const resCheck = await fetch(`${fix.baseUrl}/files`, {
            headers: { Authorization: `Bearer ${devB.device.id}:${devB.token}` }
        });
        assert.equal(resCheck.status, 200);
    } finally {
        await fix.cleanup();
    }
});

test('Admin settings auth, loopback isolation, limits validation, persistence, and non-purging quota', async () => {
    const fix = await createServerFixture();
    try {
        const adminKey = fs.readFileSync(path.join(fix.dataDir, 'admin.key'), 'utf8').trim();
        assert.equal(adminKey.length, 64);
        assert.ok(/^[0-9a-f]{64}$/i.test(adminKey));

        // 1. Missing or invalid admin key returns 403
        const resNoKey = await fetch(`${fix.baseUrl}/admin/settings`);
        assert.equal(resNoKey.status, 403);

        const resBadKey = await fetch(`${fix.baseUrl}/admin/settings`, {
            headers: { 'x-router-mailbox-admin': '0000000000000000000000000000000000000000000000000000000000000000' }
        });
        assert.equal(resBadKey.status, 403);

        // 2. Cookie or Bearer token cannot bypass admin auth
        const rDev = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'Dev 1' })
        }).then(r => r.json());
        const resBypass = await fetch(`${fix.baseUrl}/admin/settings`, {
            headers: { Authorization: `Bearer ${rDev.device.id}:${rDev.token}`, Cookie: `mailbox_auth=${rDev.device.id}:${rDev.token}` }
        });
        assert.equal(resBypass.status, 403);

        // 3. GET /admin/settings returns public fields only (no directRoomSecret, no key leak)
        const resAdminGet = await fetch(`${fix.baseUrl}/admin/settings`, {
            headers: { 'x-router-mailbox-admin': adminKey }
        });
        assert.equal(resAdminGet.status, 200);
        const adminGetBody = await resAdminGet.json();
        assert.equal(adminGetBody.retentionSeconds, 7200);
        assert.equal(adminGetBody.maxFileBytes, 512 * 1024 * 1024);
        assert.equal(adminGetBody.maxTotalBytes, 2 * 1024 * 1024 * 1024);
        assert.equal(adminGetBody.maxFiles, 1000);
        assert.equal(adminGetBody.storedBytes, 0);
        assert.equal(adminGetBody.directRoomSecret, undefined);
        assert.equal(adminGetBody.key, undefined);

        // 4. PUT /admin/settings body validation
        // Reject unknown keys
        const resUnknown = await fetch(`${fix.baseUrl}/admin/settings`, {
            method: 'PUT',
            headers: { 'x-router-mailbox-admin': adminKey, 'Content-Type': 'application/json' },
            body: JSON.stringify({ retentionSeconds: 3600, maxFileBytes: 1024, maxTotalBytes: 2048, extra: 'bad' })
        });
        assert.equal(resUnknown.status, 400);

        // Reject missing key (PUT expects all 3)
        const resMissing = await fetch(`${fix.baseUrl}/admin/settings`, {
            method: 'PUT',
            headers: { 'x-router-mailbox-admin': adminKey, 'Content-Type': 'application/json' },
            body: JSON.stringify({ retentionSeconds: 3600, maxFileBytes: 1024 })
        });
        assert.equal(resMissing.status, 400);

        // Reject maxFileBytes > maxTotalBytes
        const resFileOverTotal = await fetch(`${fix.baseUrl}/admin/settings`, {
            method: 'PUT',
            headers: { 'x-router-mailbox-admin': adminKey, 'Content-Type': 'application/json' },
            body: JSON.stringify({ retentionSeconds: 3600, maxFileBytes: 5000, maxTotalBytes: 4000 })
        });
        assert.equal(resFileOverTotal.status, 400);

        // 5. Cross-origin mutation rejected when Origin is supplied
        const resOriginFail = await fetch(`${fix.baseUrl}/admin/settings`, {
            method: 'PUT',
            headers: { 'x-router-mailbox-admin': adminKey, 'Content-Type': 'application/json', Origin: 'http://malicious.local' },
            body: JSON.stringify({ retentionSeconds: 3600, maxFileBytes: 1000, maxTotalBytes: 2000 })
        });
        assert.equal(resOriginFail.status, 403);

        // 6. Valid update succeeds and persists
        const resValidPut = await fetch(`${fix.baseUrl}/admin/settings`, {
            method: 'PUT',
            headers: { 'x-router-mailbox-admin': adminKey, 'Content-Type': 'application/json' },
            body: JSON.stringify({ retentionSeconds: 3600, maxFileBytes: 100, maxTotalBytes: 200 })
        });
        assert.equal(resValidPut.status, 200);

        // Upload a file of 80 bytes
        const upRes = await fetch(`${fix.baseUrl}/files?to=${rDev.device.id}&name=test.bin&size=80`, {
            method: 'POST',
            headers: { Authorization: `Bearer ${rDev.device.id}:${rDev.token}` },
            body: Buffer.alloc(80)
        });
        assert.equal(upRes.status, 201);
        const { file } = await upRes.json();

        // Reducing capacity below current storedBytes blocks future uploads but DOES NOT delete existing file!
        const resReduceCap = await fetch(`${fix.baseUrl}/admin/settings`, {
            method: 'PUT',
            headers: { 'x-router-mailbox-admin': adminKey, 'Content-Type': 'application/json' },
            body: JSON.stringify({ retentionSeconds: 3600, maxFileBytes: 50, maxTotalBytes: 50 })
        });
        assert.equal(resReduceCap.status, 200);

        // Existing file is preserved and downloadable
        const downRes = await fetch(`${fix.baseUrl}/files/${file.id}/content`, {
            headers: { Authorization: `Bearer ${rDev.device.id}:${rDev.token}` }
        });
        assert.equal(downRes.status, 200);

        // Future upload exceeds the reduced maxTotalBytes (80 + 20 > 50) and is blocked with 507
        const blockRes = await fetch(`${fix.baseUrl}/files?to=${rDev.device.id}&name=blocked.bin&size=20`, {
            method: 'POST',
            headers: { Authorization: `Bearer ${rDev.device.id}:${rDev.token}` },
            body: Buffer.alloc(20)
        });
        assert.equal(blockRes.status, 507);
    } finally {
        await fix.cleanup();
    }
});

test('Admin limits survive reload and a failed settings write leaves the live configuration unchanged', async () => {
    const fix = await createServerFixture();
    const headers = {'Content-Type': 'application/json', 'x-router-mailbox-admin': fs.readFileSync(path.join(fix.dataDir, 'admin.key'), 'utf8').trim()};
    const values = {retentionSeconds: 3600, maxFileBytes: 4096, maxTotalBytes: 8192};
    let reloaded;
    try {
        assert.equal((await fetch(fix.baseUrl + '/admin/settings', {method: 'PUT', headers, body: JSON.stringify(values)})).status, 200);
        const copiedDir = createTempDir();
        fs.copyFileSync(path.join(fix.dataDir, 'settings.json'), path.join(copiedDir, 'settings.json'));
        reloaded = await createServerFixture({dataDir: copiedDir});
        const reloadedHeaders = {'x-router-mailbox-admin': fs.readFileSync(path.join(copiedDir, 'admin.key'), 'utf8').trim()};
        const loaded = await fetch(reloaded.baseUrl + '/admin/settings', {headers: reloadedHeaders}).then(r => r.json());
        for (const key of Object.keys(values)) assert.equal(loaded[key], values[key]);
        const originalWrite = fs.writeFileSync;
        try {
            fs.writeFileSync = function(target, ...args) {
                if (String(target).includes('.settings.json.tmp.')) throw new Error('Simulated storage failure');
                return originalWrite.call(this, target, ...args);
            };
            const failed = await fetch(fix.baseUrl + '/admin/settings', {method: 'PUT', headers,
                body: JSON.stringify({...values, retentionSeconds: 60})});
            assert.equal(failed.status, 500);
        } finally { fs.writeFileSync = originalWrite; }
        const current = await fetch(fix.baseUrl + '/admin/settings', {headers}).then(r => r.json());
        assert.equal(current.retentionSeconds, 3600);
        assert.equal(JSON.parse(fs.readFileSync(path.join(fix.dataDir, 'settings.json'), 'utf8')).retentionSeconds, 3600);
    } finally {
        if (reloaded) await reloaded.cleanup();
        await fix.cleanup();
    }
});

test('Authenticated socket presence, multitab sessions, invalid token rejection, and roster push', async () => {
    const fix = await createServerFixture();
    try {
        const rDevA = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'Device A' })
        }).then(r => r.json());

        const rDevB = await fetch(`${fix.baseUrl}/devices`, {
            method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'Device B' })
        }).then(r => r.json());

        // Create mock sockets with EventEmitter
        function createMockSocket() {
            const sock = new EventEmitter();
            sock.sent = [];
            sock.readyState = 1;
            sock.send = (msg) => sock.sent.push(JSON.parse(msg));
            sock.close = () => sock.emit('close');
            return sock;
        }

        const sock1 = createMockSocket();
        fix.mailbox.attachSocket({ id: 'peer-tab-1', socket: sock1 });

        // 1. Binary frames, large frames (>2048), and invalid auth are safely ignored
        sock1.emit('message', Buffer.alloc(10), true); // binary
        sock1.emit('message', 'A'.repeat(2500)); // > 2048 bytes
        sock1.emit('message', JSON.stringify({ type: 'mailbox-auth', id: rDevA.device.id, token: 'wrong-token' }));
        assert.equal(sock1.sent.length, 0);

        // Device A remains offline
        let devsRes = await fetch(`${fix.baseUrl}/devices`, {
            headers: { Authorization: `Bearer ${rDevA.device.id}:${rDevA.token}` }
        }).then(r => r.json());
        assert.equal(devsRes.devices.find(d => d.id === rDevA.device.id).online, false);

        // 2. Valid authentication pushes roster and sets device online
        sock1.emit('message', JSON.stringify({ type: 'mailbox-auth', id: rDevA.device.id, token: rDevA.token }));
        assert.ok(sock1.sent.length >= 1);
        const firstPush = sock1.sent[sock1.sent.length - 1];
        assert.equal(firstPush.type, 'mailbox-roster');
        assert.ok(firstPush.revision >= 2);
        assert.ok(firstPush.serverGeneration);
        const devAInRoster = firstPush.devices.find(d => d.id === rDevA.device.id);
        assert.equal(devAInRoster.online, true);
        assert.equal(devAInRoster.peerId, 'peer-tab-1');

        // GET /devices returns revision and serverGeneration
        devsRes = await fetch(`${fix.baseUrl}/devices`, {
            headers: { Authorization: `Bearer ${rDevA.device.id}:${rDevA.token}` }
        }).then(r => r.json());
        assert.equal(devsRes.revision, firstPush.revision);
        assert.equal(devsRes.serverGeneration, firstPush.serverGeneration);

        // 3. Multitab: connect second socket for Device A
        const sock2 = createMockSocket();
        fix.mailbox.attachSocket({ id: 'peer-tab-2', socket: sock2 });
        sock2.emit('message', JSON.stringify({ type: 'mailbox-auth', id: rDevA.device.id, token: rDevA.token }));

        // Both sockets receive updated roster
        assert.ok(sock1.sent.length >= 2);
        assert.ok(sock2.sent.length >= 1);

        // Close tab 1: Device A is still online with peer-tab-2
        sock1.close();
        assert.ok(sock2.sent.length >= 2);
        const pushAfterTab1Close = sock2.sent[sock2.sent.length - 1];
        const devAAfterTab1Close = pushAfterTab1Close.devices.find(d => d.id === rDevA.device.id);
        assert.equal(devAAfterTab1Close.online, true);
        assert.equal(devAAfterTab1Close.peerId, 'peer-tab-2');

        // Close tab 2 (final socket): Device A goes offline
        sock2.close();
        devsRes = await fetch(`${fix.baseUrl}/devices`, {
            headers: { Authorization: `Bearer ${rDevB.device.id}:${rDevB.token}` }
        }).then(r => r.json());
        assert.equal(devsRes.devices.find(d => d.id === rDevA.device.id).online, false);

        // 4. Realtime authority: HTTP presence cannot revive offline device
        const presRes = await fetch(`${fix.baseUrl}/presence`, {
            method: 'POST',
            headers: {
                Authorization: `Bearer ${rDevA.device.id}:${rDevA.token}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ peerId: 'peer-http-revive' })
        }).then(r => r.json());
        assert.equal(presRes.device.online, false);
        assert.equal(presRes.device.peerId, null);
    } finally {
        await fix.cleanup();
    }
});
