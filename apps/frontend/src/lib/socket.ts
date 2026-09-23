import { CLOSE_REPLACED, CLOSE_UNKNOWN, type Incoming } from "./protocol";

export type SocketStatus = "connecting" | "open" | "closed";

type Options = {
  /** Called for every (re)connect; its result is sent as the first message. */
  hello: () => unknown;
  onMessage: (msg: Incoming) => void;
  onStatus?: (status: SocketStatus) => void;
  /**
   * The server ended the socket for good (unknown credentials, replaced by
   * another device). No reconnect follows.
   */
  onFatal?: (code: number, reason: string) => void;
};

/**
 * A WebSocket that keeps itself connected: backoff on failure, and an
 * immediate retry when the tab becomes visible again (phones kill sockets
 * the moment the screen turns off).
 */
export class Socket {
  private ws: WebSocket | null = null;
  private attempt = 0;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private stopped = false;

  constructor(
    private readonly path: string,
    private readonly opts: Options,
  ) {
    document.addEventListener("visibilitychange", this.onVisible);
    window.addEventListener("online", this.onVisible);
    this.connect();
  }

  send(msg: unknown): boolean {
    if (this.ws?.readyState !== WebSocket.OPEN) return false;
    this.ws.send(JSON.stringify(msg));
    return true;
  }

  get open(): boolean {
    return this.ws?.readyState === WebSocket.OPEN;
  }

  stop(): void {
    this.stopped = true;
    clearTimeout(this.timer);
    document.removeEventListener("visibilitychange", this.onVisible);
    window.removeEventListener("online", this.onVisible);
    this.ws?.close();
  }

  private onVisible = () => {
    if (document.visibilityState !== "visible" || this.stopped) return;
    if (!this.ws || this.ws.readyState === WebSocket.CLOSED) {
      clearTimeout(this.timer);
      this.attempt = 0;
      this.connect();
    }
  };

  private connect(): void {
    if (this.stopped) return;
    const url = new URL(this.path, location.href);
    url.protocol = location.protocol === "https:" ? "wss:" : "ws:";
    this.opts.onStatus?.("connecting");
    const ws = new WebSocket(url);
    this.ws = ws;

    ws.onopen = () => {
      this.attempt = 0;
      ws.send(JSON.stringify(this.opts.hello()));
      this.opts.onStatus?.("open");
    };
    ws.onmessage = (e) => {
      try {
        this.opts.onMessage(JSON.parse(String(e.data)) as Incoming);
      } catch (err) {
        console.warn("remote: bad message", err);
      }
    };
    ws.onclose = (e) => {
      if (this.ws !== ws) return;
      this.opts.onStatus?.("closed");
      if (e.code === CLOSE_UNKNOWN || e.code === CLOSE_REPLACED) {
        this.stopped = true;
        this.opts.onFatal?.(e.code, e.reason);
        return;
      }
      this.schedule();
    };
  }

  private schedule(): void {
    if (this.stopped) return;
    const delay = Math.min(500 * 2 ** this.attempt, 8000);
    this.attempt++;
    this.timer = setTimeout(() => this.connect(), delay);
  }
}

/** localStorage/sessionStorage that never throws (private mode, blocked). */
export function storage(kind: "local" | "session") {
  const get = (): Storage | null => {
    try {
      return kind === "local" ? localStorage : sessionStorage;
    } catch {
      return null;
    }
  };
  return {
    get(key: string): string | null {
      try {
        return get()?.getItem(key) ?? null;
      } catch {
        return null;
      }
    },
    set(key: string, value: string): void {
      try {
        get()?.setItem(key, value);
      } catch {
        // Nothing to do: the feature degrades to "scan again after reload".
      }
    },
    remove(key: string): void {
      try {
        get()?.removeItem(key);
      } catch {
        // As above.
      }
    },
  };
}
