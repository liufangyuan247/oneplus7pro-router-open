import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import {webcrypto} from 'node:crypto';

// Exercise the shipped controller with HTTP-boundary stubs, without starting PairDrop.
function fixture(text = 'hello') {
    const source = fs.readFileSync(new URL('./router-mailbox-client.js', import.meta.url), 'utf8');
    const context = vm.createContext({window: {}, document: {activeElement: null},
        PeersManager: class {}, crypto: webcrypto, TextEncoder, Uint8Array});
    vm.runInContext(source.replace('controller = new RouterMailbox();', 'window.Controller = RouterMailbox;'), context);
    const c = Object.create(context.window.Controller.prototype);
    Object.assign(c, {busy: false, settings: {maxFileBytes: 512 * 1024 * 1024, maxTextBytes: 16384},
        devices: [{id: 'a'}, {id: 'b'}], selectedDeviceId: 'a', textDrafts: new Map([['a', text]]),
        textRequests: new Map(), textInput: {value: text, dataset: {}, focus() {}},
        fileInput: {}, sendButtonLabel: {classList: {toggle() {}}, setAttribute() {}},
        sendButtonText: {}, sendTextButton: {}, notices: [], refresh: async () => {},
        notice(text, error) { this.notices.push({text, error}); }});
    return {c, context};
}

test('Text retry retains the draft and request identity until server confirmation', async () => {
    const {c} = fixture('中文 🌟\nsecond line');
    const requests = [];
    c.api = async (path, options) => {
        requests.push({path, ...options});
        if (requests.length === 1) throw Object.assign(new Error('timeout'), {name: 'AbortError'});
        return {file: {id: 'test'}};
    };
    await c.sendText();
    assert.equal(c.textInput.value, '中文 🌟\nsecond line');
    assert.equal(c.busy, false);
    assert.equal(c.notices.at(-1).error, true);
    await c.sendText();
    assert.equal(requests[0].path, '/messages');
    assert.equal(requests[0].body.to, 'a');
    assert.equal(requests[0].body.text, '中文 🌟\nsecond line');
    assert.equal(requests[0].headers['Idempotency-Key'], requests[1].headers['Idempotency-Key']);
    assert.match(requests[0].headers['Idempotency-Key'], /^[a-f0-9-]{36}$/);
    assert.equal(c.textInput.value, '');
    assert.equal(c.textDrafts.has('a'), false);
    assert.equal(c.sendTextButton.disabled, true);
});

test('Switching recipients during send preserves the new conversation draft and original target', async () => {
    const {c} = fixture('for a');
    let finish;
    c.api = (path, options) => {
        assert.equal(options.body.to, 'a');
        return new Promise(resolve => {finish = resolve;});
    };
    const sending = c.sendText();
    c.selectedDeviceId = 'b';
    c.textInput.value = 'for b';
    c.textDrafts.set('b', 'for b');
    finish({});
    await sending;
    assert.equal(c.textInput.value, 'for b');
    assert.equal(c.textDrafts.get('b'), 'for b');
    assert.equal(c.textDrafts.has('a'), false);
});

test('Empty and oversized Unicode messages never reach the network', async () => {
    const {c} = fixture('   ');
    let sent = 0;
    c.api = async () => {sent++;};
    await c.sendText();
    c.textInput.value = '🌟'.repeat(4097);
    await c.sendText();
    assert.equal(sent, 0);
    assert.equal(c.busy, false);
    assert.equal(c.notices.at(-1).error, true);
});

test('Copy on an HTTP LAN origin falls back to selection without altering the composer', async () => {
    const {c, context} = fixture('unsent draft');
    let removed = false;
    let selected = false;
    let clipboardText;
    context.document.body = {append(input) {clipboardText = input.value;}};
    context.document.createElement = () => ({style: {}, setAttribute() {}, select() {selected = true;},
        setSelectionRange() {}, remove() {removed = true;}});
    context.document.execCommand = command => command === 'copy';
    await c.copyText('copied text\n中文');
    assert.equal(clipboardText, 'copied text\n中文');
    assert.ok(selected && removed);
    assert.equal(c.textInput.value, 'unsent draft');
});
