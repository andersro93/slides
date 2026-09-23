import "./remote.css";
import { codeFromPath } from "./lib/code";
import { sanitizeNotes } from "./lib/sanitize";
import {
  type Action,
  CLOSE_REPLACED,
  type Incoming,
  type SlideState,
} from "./lib/protocol";
import { Socket, storage } from "./lib/socket";

const $ = <T extends HTMLElement>(id: string) =>
  document.getElementById(id) as T;

const code = codeFromPath(location.pathname);
const local = storage("local");
const keyName = `slides:remote:${code}`;

// A token in the URL fragment is a fresh QR scan and is tried before any key
// this phone kept from an earlier pairing. It is single-use: after the
// hello, the remote key takes over and the fragment is dropped.
let token = tokenFromHash(location.hash);
let remoteKey = token ? null : local.get(keyName);

function tokenFromHash(hash: string): string {
  try {
    return decodeURIComponent(hash.slice(1));
  } catch {
    return "";
  }
}

const els = {
  message: $("message"),
  messageText: $("message-text"),
  remote: $("remote"),
  conn: $("conn"),
  position: $("position"),
  timer: $<HTMLButtonElement>("timer"),
  timerReset: $<HTMLButtonElement>("timer-reset"),
  clock: $("clock"),
  banner: $("banner"),
  title: $("title"),
  notes: $("notes"),
  next: $("next"),
  pause: $("pause"),
  overview: $("overview"),
};

let hostConnected = false;
let socketOpen = false;

function showMessage(text: string): void {
  els.remote.hidden = true;
  els.message.hidden = false;
  els.messageText.textContent = text;
}

function showRemote(): void {
  els.message.hidden = true;
  els.remote.hidden = false;
}

function updateConnection(): void {
  const ready = socketOpen && hostConnected;
  els.conn.dataset.state = !socketOpen
    ? "offline"
    : hostConnected
      ? "ok"
      : "waiting";
  els.conn.setAttribute(
    "aria-label",
    ready ? "Connected" : socketOpen ? "Waiting for presentation" : "Offline",
  );
  els.banner.hidden = ready;
  els.banner.textContent = !socketOpen
    ? "Reconnecting…"
    : "Presentation not connected. Waiting for it…";
  for (const b of document.querySelectorAll<HTMLButtonElement>(
    "[data-action]",
  )) {
    b.disabled = !ready;
  }
}

function render(s: SlideState): void {
  document.title = `${s.index}/${s.total} · ${s.deckTitle}`;
  els.position.textContent = `${s.index} / ${s.total}`;
  els.title.textContent = s.title || "Untitled slide";
  if (s.notes) {
    // Speaker notes as rendered by reveal on the presenting computer. The
    // host side of a session can be anyone who knows the code, so this is
    // untrusted HTML: sanitized here, and the page's CSP forbids inline
    // script besides.
    els.notes.replaceChildren(sanitizeNotes(s.notes));
  } else {
    els.notes.textContent = "No notes for this slide.";
  }
  els.notes.classList.toggle("empty", !s.notes);
  els.next.textContent = s.fragmentsLeft
    ? "More on this slide"
    : s.next
      ? `Next: ${s.next}`
      : "Last slide";
  els.pause.classList.toggle("active", s.paused);
  els.overview.classList.toggle("active", s.overview);
}

function handle(msg: Incoming): void {
  switch (msg.type) {
    case "hello":
      if (msg.remoteKey) {
        remoteKey = msg.remoteKey;
        token = "";
        local.set(keyName, remoteKey);
        history.replaceState(null, "", location.pathname);
      }
      hostConnected = msg.peer;
      showRemote();
      updateConnection();
      void keepAwake();
      break;
    case "peer":
      hostConnected = msg.connected;
      updateConnection();
      break;
    case "state":
      render(msg);
      break;
  }
}

function send(action: Action): void {
  if (!socket || !hostConnected) return;
  if (socket.send({ type: "cmd", action })) {
    navigator.vibrate?.(8);
    if (action === "next" && !timer.running && timer.elapsed === 0) {
      timer.toggle();
    }
  }
}

// --- connection -----------------------------------------------------------

let socket: Socket | null = null;

function connect(): void {
  showMessage("Connecting…");
  socket = new Socket(`/${code}/_ws`, {
    hello: () =>
      remoteKey ? { type: "join", remoteKey } : { type: "join", token },
    onMessage: handle,
    onStatus: (s) => {
      socketOpen = s === "open";
      updateConnection();
    },
    onFatal: (closeCode) => {
      const stored = local.get(keyName);
      if (token && stored && stored !== remoteKey) {
        // An old or already-used QR, but this phone is still paired from
        // before: carry on with that pairing instead of giving up.
        token = "";
        remoteKey = stored;
        history.replaceState(null, "", location.pathname);
        connect();
        return;
      }
      if (remoteKey) local.remove(keyName);
      remoteKey = null;
      showMessage(
        closeCode === CLOSE_REPLACED
          ? "Another phone is now the remote. Press R on the presentation and scan again to take over."
          : "This pairing has ended. Press R on the presentation and scan the new QR code.",
      );
    },
  });
}

if (!token && !remoteKey) {
  showMessage(
    "Open the presentation on the computer, press R, and scan the QR code with this phone.",
  );
} else {
  connect();
}

// --- controls -------------------------------------------------------------

document.addEventListener("click", (e) => {
  const action = (e.target as HTMLElement)
    .closest<HTMLButtonElement>("[data-action]")
    ?.getAttribute("data-action") as Action | undefined;
  if (action) send(action);
});

// Bluetooth clickers and keyboards paired with the phone.
document.addEventListener("keydown", (e) => {
  const map: Record<string, Action> = {
    ArrowRight: "next",
    ArrowDown: "next",
    PageDown: "next",
    " ": "next",
    ArrowLeft: "prev",
    ArrowUp: "prev",
    PageUp: "prev",
    b: "pause",
    ".": "pause",
  };
  const action = map[e.key];
  if (action) {
    e.preventDefault();
    send(action);
  }
});

// Horizontal swipes anywhere: left = next, right = previous. Vertical
// movement stays scrolling of the notes.
let touchStart: { x: number; y: number } | null = null;
document.addEventListener(
  "touchstart",
  (e) => {
    const t = e.touches[0];
    touchStart =
      t && e.touches.length === 1 ? { x: t.clientX, y: t.clientY } : null;
  },
  { passive: true },
);
document.addEventListener(
  "touchend",
  (e) => {
    const t = e.changedTouches[0];
    if (!touchStart || !t) return;
    const dx = t.clientX - touchStart.x;
    const dy = t.clientY - touchStart.y;
    touchStart = null;
    if (Math.abs(dx) > 60 && Math.abs(dx) > 2 * Math.abs(dy)) {
      send(dx < 0 ? "next" : "prev");
    }
  },
  { passive: true },
);

// --- notes size -------------------------------------------------------------

const sizeKey = "slides:remote:notes-size";
let notesSize = Number(local.get(sizeKey)) || 20;
function applyNotesSize(): void {
  els.notes.style.fontSize = `${notesSize}px`;
  local.set(sizeKey, String(notesSize));
}
$("smaller").addEventListener("click", () => {
  notesSize = Math.max(14, notesSize - 2);
  applyNotesSize();
});
$("larger").addEventListener("click", () => {
  notesSize = Math.min(40, notesSize + 2);
  applyNotesSize();
});
applyNotesSize();

// --- timer and clock --------------------------------------------------------

const timer = {
  running: false,
  elapsed: 0,
  startedAt: 0,
  now(): number {
    return this.elapsed + (this.running ? Date.now() - this.startedAt : 0);
  },
  toggle(): void {
    if (this.running) {
      this.elapsed = this.now();
      this.running = false;
    } else {
      this.startedAt = Date.now();
      this.running = true;
    }
    els.timer.classList.toggle("running", this.running);
    els.timerReset.hidden = this.running || this.elapsed === 0;
  },
  reset(): void {
    this.running = false;
    this.elapsed = 0;
    els.timer.classList.remove("running");
    els.timerReset.hidden = true;
  },
};
els.timer.addEventListener("click", () => timer.toggle());
els.timerReset.addEventListener("click", () => {
  timer.reset();
  tick();
});

function mmss(ms: number): string {
  const s = Math.floor(ms / 1000);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const pad = (n: number) => String(n).padStart(2, "0");
  return h ? `${h}:${pad(m)}:${pad(s % 60)}` : `${pad(m)}:${pad(s % 60)}`;
}

function tick(): void {
  els.timer.textContent = mmss(timer.now());
  els.clock.textContent = new Date().toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
  });
}
setInterval(tick, 500);
tick();

// --- keep the screen on -----------------------------------------------------

let wakeLock: WakeLockSentinel | null = null;
async function keepAwake(): Promise<void> {
  if (wakeLock || !("wakeLock" in navigator)) return;
  try {
    wakeLock = await navigator.wakeLock.request("screen");
    wakeLock.addEventListener("release", () => {
      wakeLock = null;
    });
  } catch {
    // Denied (battery saver, iframe…): the phone may dim; nothing breaks.
  }
}
document.addEventListener("visibilitychange", () => {
  if (document.visibilityState === "visible" && !els.remote.hidden) {
    void keepAwake();
  }
});
