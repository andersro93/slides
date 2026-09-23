import "./landing.css";
import { codeFromPath, isValidCode, normalizeCode } from "./lib/code";

const form = document.querySelector<HTMLFormElement>("#open")!;
const input = document.querySelector<HTMLInputElement>("#code")!;
const status = document.querySelector<HTMLParagraphElement>("#status")!;

function showError(message: string): void {
  status.textContent = message;
  input.setAttribute("aria-invalid", "true");
  form.classList.remove("shake");
  // Restart the animation on repeated errors.
  void form.offsetWidth;
  form.classList.add("shake");
  input.select();
}

function clearError(): void {
  status.textContent = "";
  input.removeAttribute("aria-invalid");
}

// The server answers an unknown /<code> with this page and a 404: say so,
// and put the typed code back so it can be corrected.
const attempted = codeFromPath(location.pathname);
if (attempted) {
  input.value = attempted;
  history.replaceState(null, "", "/");
  showError("No presentation with that code.");
}

input.addEventListener("input", clearError);
input.focus();

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (form.getAttribute("aria-busy") === "true") return;

  const code = normalizeCode(input.value);
  if (!code) {
    input.focus();
    return;
  }
  if (!isValidCode(code)) {
    showError("Codes are letters, digits and dashes.");
    return;
  }

  form.setAttribute("aria-busy", "true");
  try {
    // Check first, so a typo is answered right here instead of by a
    // page load.
    const res = await fetch(`/${code}/_deck.json`, { cache: "no-store" });
    if (res.ok) {
      location.assign(`/${code}/`);
      return;
    }
    if (res.status === 404) {
      showError("No presentation with that code.");
    } else if (res.status === 429) {
      showError("Too many attempts. Wait a few minutes and try again.");
    } else {
      showError("Something went wrong. Try again.");
    }
  } catch {
    showError("Can't reach the server. Check the connection.");
  } finally {
    form.removeAttribute("aria-busy");
  }
});
