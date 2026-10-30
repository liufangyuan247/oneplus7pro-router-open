// Per-device-pair fallback, using messages understood by existing PairDrop clients.
export function handleRtcFallback(server, sender, message) {
    if (!server._conf.wsFallback || typeof message.to !== 'string' || message.to === sender.id) return;
    const requestedRoom = message.roomType === 'ip' ? sender.ip : message.roomId;
    const room = server._rooms[requestedRoom];
    if (!room || room[sender.id] !== sender) return;
    const recipient = room[message.to];
    if (!recipient) return;
    if (sender._relayPeers?.has(recipient)) return;

    const shared = [];
    for (const [roomId, members] of Object.entries(server._rooms)) {
        if (members[sender.id] !== sender || members[recipient.id] !== recipient) continue;
        let roomType;
        if (roomId === sender.ip && roomId === recipient.ip) roomType = 'ip';
        else if (sender.roomSecrets.includes(roomId) && recipient.roomSecrets.includes(roomId)) roomType = 'secret';
        else if (sender.publicRoomId === roomId && recipient.publicRoomId === roomId) roomType = 'public-id';
        if (roomType) shared.push({roomType, roomId});
    }
    if (!shared.length) return;
    sender._relayPeers ??= new WeakSet();
    recipient._relayPeers ??= new WeakSet();
    sender._relayPeers.add(recipient);
    recipient._relayPeers.add(sender);

    // Remove every shared room from the old RTC peer before reintroducing it.
    // Other device pairs and their transfers remain untouched.
    for (const {roomType, roomId} of shared) {
        server._send(sender, {type: 'peer-left', peerId: recipient.id, roomType, roomId, disconnect: true});
        server._send(recipient, {type: 'peer-left', peerId: sender.id, roomType, roomId, disconnect: true});
    }
    for (const {roomType, roomId} of shared) {
        server._send(recipient, {
            type: 'peer-joined', peer: {...sender.getInfo(), rtcSupported: false}, roomType, roomId
        });
    }
    for (const {roomType, roomId} of shared) {
        server._send(sender, {
            type: 'peers', peers: [{...recipient.getInfo(), rtcSupported: false}], roomType, roomId
        });
    }
}
