// How sync messages travel. CollabSession only needs to send messages and
// receive them; today they go between tabs of one browser, next week over a
// WebSocket to the gateway. Keeping that behind this interface means the
// session does not change when the transport does.

import type { SyncMessage } from "./messages";

export interface Transport {
  /** Sends a message to every other participant (never back to the sender). */
  send(message: SyncMessage): void;
  /** Registers the handler for incoming messages, raw and unvalidated. */
  onMessage(handler: (data: unknown) => void): void;
  /** Stops sending and receiving. */
  close(): void;
}

/**
 * A transport between tabs (and windows) of the same browser and origin,
 * using BroadcastChannel. Each document gets its own channel, so tabs on
 * different documents never see each other's edits.
 *
 * BroadcastChannel delivers each message to every other channel with the
 * same name, in the order it was sent, and not to the sender.
 */
export function broadcastChannelTransport(docId: string): Transport {
  const channel = new BroadcastChannel(`collab-engine:${docId}`);
  return {
    send: (message) => channel.postMessage(message),
    onMessage: (handler) => {
      channel.onmessage = (event: MessageEvent) => handler(event.data);
    },
    close: () => channel.close(),
  };
}
