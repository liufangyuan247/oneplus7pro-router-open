// Run against a prepared, patched upstream tree with its installed dependencies:
// PAIRDROP_RELEASE=/path/to/release node --test transport-heartbeat.test.mjs
import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import path from 'node:path';
import {createRequire} from 'node:module';
import {pathToFileURL} from 'node:url';

const release = process.env.PAIRDROP_RELEASE;

test('paused application remains present; missing protocol pongs still remove the peer',
    {skip: !release, timeout: 13000}, async () => {
        const require = createRequire(path.join(release, 'package.json'));
        const WebSocket = require('ws');
        const {default: PairDropWsServer} = await import(pathToFileURL(path.join(release, 'server/ws-server.js')));
        const httpServer = http.createServer();
        const server = new PairDropWsServer(httpServer, {
            wsFallback: true, debugMode: false, rtcConfig: {iceServers: []}, ipv6Localize: false
        });
        await new Promise(resolve => httpServer.listen(0, '127.0.0.1', resolve));
        const endpoint = `ws://127.0.0.1:${httpServer.address().port}/server?webrtc_supported=false`;
        const sockets = [];
        function connect(autoPong) {
            return new Promise((resolve, reject) => {
                const socket = new WebSocket(endpoint, {autoPong});
                sockets.push(socket);
                let protocolPings = 0, applicationPings = 0;
                socket.on('ping', () => protocolPings++);
                socket.on('error', reject);
                socket.on('message', data => {
                    const message = JSON.parse(data);
                    if (message.type === 'ping') applicationPings++;
                    // Deliberately do not answer JSON heartbeats.
                    if (message.type === 'display-name') resolve({
                        socket, id: message.peerId,
                        get protocolPings() { return protocolPings; },
                        get applicationPings() { return applicationPings; }
                    });
                });
            });
        }
        try {
            const healthy = await connect(true);
            const unresponsive = await connect(false);
            healthy.socket.send(JSON.stringify({type: 'join-ip-room'}));
            unresponsive.socket.send(JSON.stringify({type: 'join-ip-room'}));
            const started = Date.now();
            let closedAfter;
            unresponsive.socket.once('close', () => { closedAfter = Date.now() - started; });
            await new Promise(resolve => setTimeout(resolve, 7500));
            assert.equal(healthy.socket.readyState, WebSocket.OPEN);
            assert.equal(unresponsive.socket.readyState, WebSocket.CLOSED);
            assert.ok(closedAfter >= 4500 && closedAfter <= 7000, `timeout changed: ${closedAfter}ms`);
            assert.ok(healthy.protocolPings >= 6);
            assert.equal(healthy.applicationPings, 0);
            assert.ok(server._rooms['127.0.0.1'][healthy.id]);
            assert.equal(server._rooms['127.0.0.1'][unresponsive.id], undefined);
        } finally {
            for (const socket of sockets) socket.terminate();
            for (const entry of Object.values(server._keepAliveTimers)) clearTimeout(entry.timer);
            server._wss.close();
            await new Promise(resolve => httpServer.close(resolve));
        }
    });
