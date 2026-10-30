import test from 'node:test';
import assert from 'node:assert/strict';
import {handleRtcFallback} from './router-compat-server.js';

function fixture() {
    const peer = id => ({id, ip: 'lan', roomSecrets: ['paired'], publicRoomId: 'public', getInfo() { return {id, rtcSupported: true}; }});
    const a = peer('a'), b = peer('b'), c = peer('c');
    const sent = [];
    const server = {_conf: {wsFallback: true}, _rooms: {lan: {a, b, c}}, _send(to, message) { sent.push({to: to.id, ...message}); }};
    const message = {to: 'b', roomType: 'ip'};
    return {a, b, c, sent, server, message};
}

test('only the requested pair is renegotiated, without changing rooms or RTC capabilities', () => {
    const {a, b, c, sent, server, message} = fixture();
    handleRtcFallback(server, a, message);
    assert.equal(sent.length, 4);
    assert.deepEqual(sent.map(m => m.type), ['peer-left', 'peer-left', 'peer-joined', 'peers']);
    assert.ok(sent.every(m => m.to !== c.id));
    assert.equal(sent[2].peer.rtcSupported, false);
    assert.equal(sent[3].peers[0].rtcSupported, false);
    assert.equal(a.getInfo().rtcSupported, true);
    assert.equal(b.getInfo().rtcSupported, true);
    assert.deepEqual(server._rooms.lan, {a, b, c});
});

test('disabled fallback, missing peer and forged sender cannot renegotiate', () => {
    const {a, sent, server, message} = fixture();
    server._conf.wsFallback = false;
    handleRtcFallback(server, a, message);
    server._conf.wsFallback = true;
    handleRtcFallback(server, {...a}, message);
    handleRtcFallback(server, a, {...message, to: 'outside'});
    handleRtcFallback(server, a, {...message, to: 'a'});
    handleRtcFallback(server, a, {...message, roomType: 'secret', roomId: 'outside'});
    assert.deepEqual(sent, []);
});

test('duplicate requests from either end are ignored, but a reconnected peer can retry', () => {
    const {a, b, sent, server, message} = fixture();
    handleRtcFallback(server, a, message);
    handleRtcFallback(server, a, message);
    handleRtcFallback(server, b, {to: 'a', roomType: 'ip'});
    assert.equal(sent.length, 4);
    server._rooms.lan.b = {...b, _relayPeers: undefined};
    handleRtcFallback(server, a, message);
    assert.equal(sent.length, 8);
});

test('all shared rooms are disconnected before either end is reintroduced', () => {
    const {a, b, sent, server, message} = fixture();
    server._rooms.paired = {a, b};
    server._rooms.public = {a, b};
    handleRtcFallback(server, a, message);
    assert.equal(sent.length, 12);
    assert.ok(sent.slice(0, 6).every(m => m.type === 'peer-left' && m.disconnect));
    assert.deepEqual(new Set(sent.map(m => m.roomType)), new Set(['ip', 'secret', 'public-id']));
});
