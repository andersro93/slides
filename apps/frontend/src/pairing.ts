import type { RevealApi } from "reveal.js";
import { renderSVG } from "uqr";
import {
  type Action,
  firstLine,
  type Incoming,
  remoteUrl,
  type SlideState,
} from "./lib/protocol";
import { Socket, storage } from "./lib/socket";

const KEY_R = 82;

/**
 * The deck's half of the phone remote. Nothing shows on the projector until
 * the presenter presses R: then a QR code for the phone, which closes by
 * itself once a phone has joined. The socket is opened on that first press
 * (or at load, if this tab already owns a session — a reload mid-talk keeps
 * the phone paired).
 */
export function setupPairing(
  reveal: RevealApi,
  code: string,
  deckTitle: string,
): void {
  const session = storage("session");
  const hostKeyName = `slides:host:${code}`;
  const overlay = new PairingOverlay(() => socket?.send({ type: "rotate" }));

  let socket: Socket | null = null;
  let token = "";
  // False once another tab (a duplicated one shares sessionStorage) took
  // over this session: the next R starts a session of our own.
  let resume = true;

  function connect(): void {
    if (socket) return;
    socket = new Socket(`/${code}/_ws`, {
      hello: () => ({
        type: "host",
        hostKey: resume ? (session.get(hostKeyName) ?? undefined) : undefined,
      }),
      onMessage: handle,
      onStatus: (s) => {
        if (s !== "open") overlay.setStatus("Connecting…");
      },
      onFatal: () => {
        socket = null;
        token = "";
        resume = false;
        overlay.clear(
          "Another tab took over the phone remote. Press R again to pair here.",
        );
      },
    });
  }

  function handle(msg: Incoming): void {
    switch (msg.type) {
      case "hello":
        resume = true;
        if (msg.hostKey) session.set(hostKeyName, msg.hostKey);
        if (msg.token) setToken(msg.token);
        setPeer(msg.peer);
        publish();
        break;
      case "token":
        setToken(msg.token);
        break;
      case "peer":
        setPeer(msg.connected);
        if (msg.connected) publish();
        break;
      case "cmd":
        run(msg.action);
        break;
    }
  }

  function setToken(t: string): void {
    token = t;
    overlay.setUrl(remoteUrl(location.origin, code, token));
  }

  function setPeer(connected: boolean): void {
    if (connected) {
      if (overlay.isOpen) {
        overlay.close();
        toast("Remote connected");
      }
    } else {
      overlay.setStatus("Scan with your phone's camera");
    }
  }

  function run(action: Action): void {
    switch (action) {
      case "next":
        reveal.next();
        break;
      case "prev":
        reveal.prev();
        break;
      case "first":
        reveal.slide(0, 0, 0);
        break;
      case "last":
        reveal.slide(Number.MAX_SAFE_INTEGER);
        break;
      case "pause":
        reveal.togglePause();
        break;
      case "overview":
        reveal.toggleOverview();
        break;
    }
  }

  function publish(): void {
    if (!socket?.open) return;
    const current = reveal.getCurrentSlide();
    const slides = reveal.getSlides();
    const index = reveal.getSlidePastCount();
    const next = slides[index + 1];
    const state: SlideState = {
      type: "state",
      deckTitle,
      index: index + 1,
      total: reveal.getTotalSlides(),
      title: slideTitle(current),
      notes: reveal.getSlideNotes(current),
      next: next ? slideTitle(next) : null,
      fragmentsLeft: reveal.availableFragments().next,
      paused: reveal.isPaused(),
      overview: reveal.isOverview(),
    };
    socket.send(state);
  }

  for (const event of [
    "slidechanged",
    "fragmentshown",
    "fragmenthidden",
    "paused",
    "resumed",
    "overviewshown",
    "overviewhidden",
  ]) {
    reveal.on(event, publish);
  }

  reveal.addKeyBinding(
    { keyCode: KEY_R, key: "R", description: "Control from your phone" },
    () => {
      if (overlay.isOpen) {
        overlay.close();
        return;
      }
      connect();
      if (token) overlay.setUrl(remoteUrl(location.origin, code, token));
      overlay.open();
    },
  );

  if (session.get(hostKeyName)) connect();
}

function slideTitle(slide: HTMLElement | undefined): string {
  if (!slide) return "";
  const heading = slide.querySelector("h1, h2, h3, h4, h5, h6");
  return firstLine((heading ?? slide).textContent);
}

class PairingOverlay {
  private el: HTMLDivElement;
  private qr: HTMLDivElement;
  private link: HTMLAnchorElement;
  private status: HTMLParagraphElement;
  private url = "";

  constructor(onRotate: () => void) {
    this.el = document.createElement("div");
    this.el.className = "pairing";
    this.el.hidden = true;
    this.el.setAttribute("role", "dialog");
    this.el.setAttribute("aria-modal", "true");
    this.el.setAttribute("aria-label", "Control from your phone");
    this.el.innerHTML = `
      <div class="pairing-card">
        <h2>Control from your phone</h2>
        <div class="pairing-qr" aria-hidden="true"></div>
        <p class="pairing-status" role="status">Connecting…</p>
        <a class="pairing-link" target="_blank" rel="noreferrer"></a>
        <p class="pairing-hint">The code works once. Next / previous, notes and a timer on your phone.</p>
        <div class="pairing-actions">
          <button type="button" data-action="rotate">New code</button>
          <button type="button" data-action="close">Close <kbd>Esc</kbd></button>
        </div>
      </div>`;
    this.qr = this.el.querySelector(".pairing-qr")!;
    this.link = this.el.querySelector(".pairing-link")!;
    this.status = this.el.querySelector(".pairing-status")!;
    this.el.addEventListener("click", (e) => {
      const action = (e.target as HTMLElement)
        .closest("button")
        ?.getAttribute("data-action");
      if (action === "rotate") onRotate();
      if (action === "close" || e.target === this.el) this.close();
    });
    // Capture phase: reveal must not also treat Esc as "overview".
    document.addEventListener(
      "keydown",
      (e) => {
        if (this.isOpen && e.key === "Escape") {
          e.stopImmediatePropagation();
          e.preventDefault();
          this.close();
        }
      },
      true,
    );
    document.body.append(this.el);
  }

  get isOpen(): boolean {
    return !this.el.hidden;
  }

  open(): void {
    this.el.hidden = false;
  }

  close(): void {
    this.el.hidden = true;
  }

  setUrl(url: string): void {
    if (url === this.url) return;
    this.url = url;
    this.qr.innerHTML = renderSVG(url, { border: 2 });
    this.link.href = url;
    this.link.textContent = url.replace(/#.*/, "#…");
    this.setStatus("Scan with your phone's camera");
  }

  setStatus(text: string): void {
    this.status.textContent = text;
  }

  /** Back to "no QR" — the session this dialog showed is gone. */
  clear(status: string): void {
    this.url = "";
    this.qr.replaceChildren();
    this.link.removeAttribute("href");
    this.link.textContent = "";
    this.setStatus(status);
  }
}

function toast(text: string): void {
  const el = document.createElement("div");
  el.className = "deck-toast";
  el.textContent = text;
  document.body.append(el);
  setTimeout(() => el.classList.add("out"), 1800);
  setTimeout(() => el.remove(), 2400);
}
