import { devices, expect, test } from "@playwright/test";

test("unknown code is answered on the landing page", async ({ page }) => {
  await page.goto("/");
  await page.getByLabel("Presentation code").fill("no-such-deck");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("status")).toHaveText(
    "No presentation with that code.",
  );
  await expect(page).toHaveURL("/");
});

test("a mistyped deck URL lands on the field with the error", async ({
  page,
}) => {
  const res = await page.goto("/no-such-deck/");
  expect(res?.status()).toBe(404);
  await expect(page.getByRole("status")).toHaveText(
    "No presentation with that code.",
  );
  await expect(page.getByLabel("Presentation code")).toHaveValue(
    "no-such-deck",
  );
});

test("a code opens its Markdown deck", async ({ page }) => {
  await page.goto("/");
  await page.getByLabel("Presentation code").fill("  E2E Markdown ");
  await page.keyboard.press("Enter");

  await expect(page).toHaveURL(/\/e2e-markdown\/(#\/)?$/);
  await expect(page).toHaveTitle("E2E Markdown");
  await expect(page.locator(".reveal .slides h1")).toHaveText("First slide");

  await page.keyboard.press("ArrowRight");
  await expect(page).toHaveURL(/#\/1$/);
  // Relative asset from the deck's own directory.
  const img = page.locator(".reveal .slides section.present img");
  await expect(img).toHaveJSProperty("naturalWidth", 10);
});

test("an HTML deck gets its theme and stylesheet", async ({ page }) => {
  await page.goto("/e2e-html/");
  const h1 = page.locator(".reveal h1.styled");
  await expect(h1).toHaveText("HTML one");
  await expect(h1).toHaveCSS("color", "rgb(1, 2, 3)");
});

test("the source file is never served", async ({ request }) => {
  expect((await request.get("/e2e-markdown/index.md")).status()).toBe(404);
  expect((await request.get("/e2e-markdown/pixel.svg")).status()).toBe(200);
});

test("phone remote pairs, drives the deck, and the QR is single-use", async ({
  page: deck,
  browser,
}) => {
  await deck.goto("/e2e-markdown/");
  await expect(deck.locator(".reveal .slides h1")).toHaveText("First slide");

  await deck.keyboard.press("r");
  const dialog = deck.getByRole("dialog", { name: "Control from your phone" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText("Scan with your phone's camera")).toBeVisible();
  const url = await dialog.locator(".pairing-link").getAttribute("href");
  expect(url).toMatch(/\/e2e-markdown\/_remote#.+/);

  const phoneContext = await browser.newContext({ ...devices["Pixel 7"] });
  const phone = await phoneContext.newPage();
  await phone.goto(url!);

  // Pairing closes the dialog on the projector and lands on the phone.
  await expect(dialog).toBeHidden();
  await expect(phone.locator("#title")).toHaveText("First slide");
  await expect(phone.locator("#notes")).toContainText(
    "Notes for the first slide.",
  );
  await expect(phone.locator("#position")).toHaveText("1 / 4");
  // The token left the URL; the phone now holds its own key.
  await expect(phone).toHaveURL(/_remote$/);

  await phone.getByRole("button", { name: "Next" }).click();
  await expect(deck).toHaveURL(/#\/1$/);
  await expect(phone.locator("#title")).toHaveText("Second slide");

  await phone.getByRole("button", { name: "Previous" }).click();
  await expect(deck).toHaveURL(/\/e2e-markdown\/(#\/0?)?$/);

  // A phone reload reconnects with its key, no new scan needed.
  await phone.reload();
  await expect(phone.locator("#title")).toHaveText("First slide");
  await phone.getByRole("button", { name: "Next" }).click();
  await expect(deck).toHaveURL(/#\/1$/);

  // Someone who photographed the QR gets nothing.
  const thiefContext = await browser.newContext({ ...devices["Pixel 7"] });
  const thief = await thiefContext.newPage();
  await thief.goto(url!);
  await expect(thief.locator("#message-text")).toContainText(
    "This pairing has ended",
  );

  await phoneContext.close();
  await thiefContext.close();
});

test("security headers are set", async ({ request }) => {
  const res = await request.get("/e2e-markdown/");
  const h = res.headers();
  expect(h["content-security-policy"]).toContain("default-src 'self'");
  expect(h["referrer-policy"]).toBe("no-referrer");
  expect(h["x-robots-tag"]).toContain("noindex");
});

// Anyone who knows a code can run the host side of a session and send a
// pairing link to someone else. Whatever that host sends as speaker notes
// must not run on the phone.
test("notes from a hostile host session cannot run script on the phone", async ({
  page: attacker,
  browser,
}) => {
  await attacker.goto("/e2e-markdown/_remote");
  const token = await attacker.evaluate(
    () =>
      new Promise<string>((resolve) => {
        const ws = new WebSocket(`ws://${location.host}/e2e-markdown/_ws`);
        (window as unknown as { hostile: WebSocket }).hostile = ws;
        ws.onopen = () => ws.send(JSON.stringify({ type: "host" }));
        ws.onmessage = (e) => {
          const msg = JSON.parse(e.data);
          if (msg.type === "hello") resolve(msg.token);
          if (msg.type === "peer" && msg.connected) {
            ws.send(
              JSON.stringify({
                type: "state",
                deckTitle: "x",
                index: 1,
                total: 1,
                title: "Hijacked",
                notes:
                  '<b>bold survives</b><img src="x" onerror="window.pwned=1"><script>window.pwned=2</script><a href="javascript:window.pwned=3">link</a>',
                next: null,
                fragmentsLeft: false,
                paused: false,
                overview: false,
              }),
            );
          }
        };
      }),
  );

  const victimContext = await browser.newContext({ ...devices["Pixel 7"] });
  const victim = await victimContext.newPage();
  await victim.goto(`/e2e-markdown/_remote#${token}`);
  await expect(victim.locator("#title")).toHaveText("Hijacked");
  await expect(victim.locator("#notes b")).toHaveText("bold survives");
  await victim
    .locator("#notes a")
    .click({ force: true })
    .catch(() => {});
  await victim.waitForTimeout(300);
  expect(
    await victim.evaluate(() => (window as { pwned?: number }).pwned),
  ).toBeUndefined();
  expect(
    await victim.locator("#notes img[onerror], #notes script").count(),
  ).toBe(0);
  await victimContext.close();
});

test("an old QR on a phone that is still paired keeps the pairing", async ({
  page: deck,
  browser,
}) => {
  await deck.goto("/e2e-markdown/");
  await deck.keyboard.press("r");
  const link = deck.locator(".pairing-link[href]");
  const first = await link.getAttribute("href");

  const phoneContext = await browser.newContext({ ...devices["Pixel 7"] });
  const phone = await phoneContext.newPage();
  await phone.goto(first!);
  await expect(phone.locator("#title")).toHaveText("First slide");

  // Scanning the same (now spent) QR again, e.g. from the camera history.
  await phone.goto("about:blank");
  await phone.goto(first!);
  await expect(phone.locator("#title")).toHaveText("First slide");
  await phone.getByRole("button", { name: "Next" }).click();
  await expect(deck).toHaveURL(/#\/1$/);
  await phoneContext.close();
});
