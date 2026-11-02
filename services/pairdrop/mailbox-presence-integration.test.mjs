// Uses a prepared upstream release to verify the HTTP / WebSocket integration.
// PAIRDROP_RELEASE=/path/to/release node --test mailbox-presence-integration.test.mjs
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {createRequire} from 'node:module';
import {pathToFileURL} from 'node:url';

const release = process.env.PAIRDROP_RELEASE;

test('authenticated native sockets push presence, rename and final-tab disconnect without HTTP polling',
    {skip: !release, timeout: 12000}, async () => {
        const require = createRequire(path.join(release, 'package.json'));
        const WebSocket = require('ws');
        const {default: HttpServer} = await import(pathToFileURL(path.join(release, 'server/server.js')));
        const {default: WsServer} = await import(pathToFileURL(path.join(release, 'server/ws-server.js')));
        const dataDir = fs.mkdtempSync(path.join(os.tmpdir(), 'mailbox-presence-'));
        const oldDir = process.env.PAIRDROP_MAILBOX_DIR;
        process.env.PAIRDROP_MAILBOX_DIR = dataDir;
        const conf = {port: 0, localhostOnly: true, rateLimit: false, wsFallback: true,
            debugMode: false, rtcConfig: {iceServers: []}, ipv6Localize: false};
        const http = new HttpServer(conf).server;
        const ws = new WsServer(http, conf);
        const sockets = [];
        if (!http.listening) await new Promise(resolve => http.once('listening', resolve));
        const base = `http://127.0.0.1:${http.address().port}/mailbox/api`;
        const endpoint = `ws://127.0.0.1:${http.address().port}/server?webrtc_supported=false`;
        async function register(name) {
            const response = await fetch(base + '/devices', {method: 'POST',
                headers: {'Content-Type': 'application/json'}, body: JSON.stringify({name})});
            assert.equal(response.status, 201);
            return response.json();
        }
        async function connect() {
            const socket = new WebSocket(endpoint);
            sockets.push(socket);
            const rosters = [];
            const result = {socket, rosters};
            await new Promise((resolve, reject) => {
                socket.on('error', reject);
                socket.on('message', data => {
                    const frame = JSON.parse(data);
                    if (frame.type === 'mailbox-roster') rosters.push(frame);
                    if (frame.type === 'display-name') { result.peerId = frame.peerId; resolve(); }
                });
            });
            return result;
        }
        async function waitFor(predicate, message) {
            const deadline = Date.now() + 1800;
            while (!predicate()) {
                if (Date.now() >= deadline) assert.fail(message);
                await new Promise(resolve => setTimeout(resolve, 10));
            }
        }
        function auth(connection, identity) {
            connection.socket.send(JSON.stringify({type: 'mailbox-auth',
                id: identity.device.id, token: identity.token, peerId: 'untrusted-client-peer'}));
        }
        function latest(connection, id) {
            return connection.rosters.at(-1)?.devices.find(device => device.id === id);
        }
        try {
            assert.ok(http.routerMailbox?.attachSocket, 'HTTP server did not expose mailbox controller');
            const a = await register('Presence observer');
            const b = await register('Two tabs');
            const observer = await connect();
            auth(observer, a);
            await waitFor(() => latest(observer, a.device.id)?.online, 'observer never authenticated');
            assert.equal(latest(observer, b.device.id).online, false);
            const tab1 = await connect();
            auth(tab1, b);
            await waitFor(() => latest(observer, b.device.id)?.online, 'online push waited for polling');
            assert.equal(latest(observer, b.device.id).peerId, tab1.peerId);
            const tab2 = await connect();
            auth(tab2, b);
            await waitFor(() => tab2.rosters.length > 0, 'second tab never authenticated');
            const rename = await fetch(base + '/devices/me', {method: 'PUT',
                headers: {'Content-Type': 'application/json', Authorization: `Bearer ${b.device.id}:${b.token}`},
                body: JSON.stringify({name: 'Renamed device'})});
            assert.equal(rename.status, 200);
            await waitFor(() => latest(observer, b.device.id)?.name === 'Renamed device', 'rename was not pushed');
            tab2.socket.close();
            await new Promise(resolve => tab2.socket.once('close', resolve));
            await waitFor(() => latest(observer, b.device.id)?.peerId === tab1.peerId,
                'remaining tab did not remain reachable');
            assert.equal(latest(observer, b.device.id).online, true);
            tab1.socket.close();
            await waitFor(() => latest(observer, b.device.id)?.online === false, 'final close was not pushed');
            const legacy = await fetch(base + '/presence', {method: 'POST',
                headers: {'Content-Type': 'application/json', Authorization: `Bearer ${b.device.id}:${b.token}`},
                body: JSON.stringify({peerId: tab1.peerId})});
            assert.equal(legacy.status, 200);
            const roster = await fetch(base + '/devices', {headers: {Authorization: `Bearer ${a.device.id}:${a.token}`}})
                .then(response => response.json());
            assert.equal(roster.devices.find(device => device.id === b.device.id).online, false,
                'legacy presence revived a disconnected authenticated socket');
            assert.ok(Number.isSafeInteger(roster.revision));
            for (let i = 1; i < observer.rosters.length; i++) {
                assert.ok(observer.rosters[i].revision > observer.rosters[i - 1].revision);
            }
        } finally {
            for (const socket of sockets) socket.terminate();
            http.routerMailbox?.close();
            for (const entry of Object.values(ws._keepAliveTimers)) clearTimeout(entry.timer);
            ws._wss.close();
            http.closeAllConnections();
            await new Promise(resolve => http.close(resolve));
            if (oldDir === undefined) delete process.env.PAIRDROP_MAILBOX_DIR;
            else process.env.PAIRDROP_MAILBOX_DIR = oldDir;
            fs.rmSync(dataDir, {recursive: true, force: true});
        }
    });
