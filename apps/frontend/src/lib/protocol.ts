// Messages on /<code>/_ws. The server (apps/server/internal/remote) only reads
// `type` plus the credentials in the first message; everything else passes
// through between deck and phone as-is.

export type Action = "next" | "prev" | "first" | "last" | "pause" | "overview";

/** First message on a socket. */
export type HostHello = { type: "host"; hostKey?: string };
export type RemoteHello =
  | { type: "join"; token: string }
  | { type: "join"; remoteKey: string };

/** Deck → phone: everything the remote screen shows. */
export type SlideState = {
  type: "state";
  deckTitle: string;
  index: number;
  total: number;
  title: string;
  notes: string | null;
  next: string | null;
  fragmentsLeft: boolean;
  paused: boolean;
  overview: boolean;
};

/** Phone → deck. */
export type Command = { type: "cmd"; action: Action };

/** Server → client. */
export type ServerMessage =
  | {
      type: "hello";
      role: "host" | "remote";
      token?: string;
      hostKey?: string;
      remoteKey?: string;
      peer: boolean;
    }
  | { type: "token"; token: string }
  | { type: "peer"; connected: boolean };

export type Incoming = ServerMessage | SlideState | Command;

// Close codes from internal/remote.
export const CLOSE_UNKNOWN = 4001;
export const CLOSE_REPLACED = 4002;
export const CLOSE_BUSY = 4003;

export function remoteUrl(origin: string, code: string, token: string): string {
  return `${origin}/${encodeURIComponent(code)}/_remote#${encodeURIComponent(token)}`;
}

/** First line of text from an HTML fragment, for slide titles. */
export function firstLine(text: string | null | undefined, max = 80): string {
  const line =
    (text ?? "")
      .split("\n")
      .map((l) => l.trim())
      .find(Boolean) ?? "";
  return line.length > max ? `${line.slice(0, max - 1)}…` : line;
}
