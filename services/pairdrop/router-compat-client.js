// Prefer direct WebRTC, with a bounded fallback for VPN/browser restrictions.
(() => {
    const managers = new Set();
    const timeoutMs = 8000;

    function cancel(manager, peerId) {
        clearTimeout(manager._relayTimers?.get(peerId));
        manager._relayTimers?.delete(peerId);
    }

    function requestRelay(manager, peerId, rooms) {
        if (!manager._wsConfig?.wsFallback || !manager._server._isConnected()) return false;
        const peer = manager.peers[peerId];
        if (peer && (!peer.rtcSupported || peer._busy || peer._channel?.readyState === 'open')) return false;
        const entry = Object.entries(peer?._roomIds || rooms || {})[0];
        if (!entry) return false;
        cancel(manager, peerId);
        if (!manager._relayRequested) manager._relayRequested = new Set();
        if (manager._relayRequested.has(peerId)) return true;
        manager._relayRequested.add(peerId);
        const [roomType, roomId] = entry;
        console.log('RTC unavailable; requesting router relay for', peerId);
        manager._server.send({type: 'rtc-fallback', to: peerId, roomType, roomId});
        return true;
    }

    function requestForPeer(peer) {
        for (const manager of managers) {
            if (manager.peers[peer._peerId] === peer) return requestRelay(manager, peer._peerId);
        }
        return false;
    }

    const create = PeersManager.prototype._createOrRefreshPeer;
    PeersManager.prototype._createOrRefreshPeer = function (isCaller, peerId, roomType, roomId, rtcSupported) {
        managers.add(this);
        try {
            create.call(this, isCaller, peerId, roomType, roomId, rtcSupported);
        } catch (error) {
            if (window.isRtcSupported && rtcSupported && requestRelay(this, peerId, {[roomType]: roomId})) return;
            throw error;
        }
        cancel(this, peerId);
        const peer = this.peers[peerId];
        if (!this._wsConfig?.wsFallback || !peer?.rtcSupported || peer._channel?.readyState === 'open') return;
        if (!this._relayTimers) this._relayTimers = new Map();
        this._relayTimers.set(peerId, setTimeout(() => requestRelay(this, peerId), timeoutMs));
    };

    const receive = PeersManager.prototype._onMessage;
    PeersManager.prototype._onMessage = function (message) {
        const peer = this.peers[message.sender.id];
        if (!peer) return;
        // An old ICE/SDP message can arrive just after the switch to WS.
        if (!peer.rtcSupported && (message.sdp || message.ice)) return;
        try {
            return receive.call(this, message);
        } catch (error) {
            if (peer.rtcSupported && requestRelay(this, message.sender.id)) return;
            throw error;
        }
    };

    const disconnect = PeersManager.prototype._onPeerDisconnected;
    PeersManager.prototype._onPeerDisconnected = function (peerId) {
        cancel(this, peerId);
        this._relayRequested?.delete(peerId);
        return disconnect.call(this, peerId);
    };

    const wsDisconnect = PeersManager.prototype._onWsDisconnected;
    PeersManager.prototype._onWsDisconnected = function () {
        for (const peerId of this._relayTimers?.keys() || []) cancel(this, peerId);
        this._relayRequested?.clear();
        return wsDisconnect.call(this);
    };

    for (const [method, state] of [['_onConnectionStateChange', 'connectionState'], ['_onIceConnectionStateChange', 'iceConnectionState']]) {
        const original = RTCPeer.prototype[method];
        RTCPeer.prototype[method] = function (...args) {
            if (this._conn?.[state] === 'failed' && requestForPeer(this)) return;
            return original.apply(this, args);
        };
    }

    Events.on('peer-connected', event => {
        for (const manager of managers) cancel(manager, event.detail.peerId);
    });

    // A transport renegotiation can reuse the same device ID in the UI.
    const join = PeersUI.prototype._joinPeer;
    PeersUI.prototype._joinPeer = function (peer, roomType, roomId) {
        if (this.peers[peer.id]) this.peers[peer.id].rtcSupported = peer.rtcSupported;
        return join.call(this, peer, roomType, roomId);
    };
})();
