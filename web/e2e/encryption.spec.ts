import { test, expect } from "@playwright/test";
import {
  startEncryptedSession,
  startUnencryptedSession,
  cleanup,
  devLogin,
  type SessionHandle,
} from "./helpers";

test.describe("End-to-end encryption", () => {
  let session: SessionHandle | null = null;
  let session2: SessionHandle | null = null;

  test.beforeEach(async ({ page }) => {
    // Authenticate with the dev provider so the SPA renders content.
    await devLogin(page);
  });

  test.afterEach(() => {
    if (session) {
      cleanup(session.process);
      session = null;
    }
    if (session2) {
      cleanup(session2.process);
      session2 = null;
    }
  });

  test("encrypted session shows passphrase prompt", async ({ page }) => {
    session = await startEncryptedSession("testpass123");
    await page.goto(`/session/${session.sessionId}`);

    // Passphrase overlay should be visible
    await expect(page.getByText("ENCRYPTED SESSION")).toBeVisible({
      timeout: 10_000,
    });
    await expect(page.getByPlaceholder("Passphrase")).toBeVisible();

    // Encryption badge should be shown in status bar
    await expect(page.getByText("[encrypted]")).toBeVisible();
  });

  test("correct passphrase decrypts terminal output", async ({ page }) => {
    session = await startEncryptedSession("testpass123");
    await page.goto(`/session/${session.sessionId}`);

    // Wait for passphrase overlay
    await expect(page.getByPlaceholder("Passphrase")).toBeVisible({
      timeout: 10_000,
    });

    // Enter correct passphrase
    await page.getByPlaceholder("Passphrase").fill("testpass123");
    await page.getByRole("button", { name: "[decrypt]" }).click();

    // Overlay should disappear
    await expect(page.getByText("ENCRYPTED SESSION")).not.toBeVisible({
      timeout: 5_000,
    });

    // Send a command via CLI
    await new Promise((r) => setTimeout(r, 1000));
    session.process.stdin?.write('echo "hello_e2e_test"\n');

    // Wait for output to appear in terminal
    await expect(
      page.locator(".xterm-rows").getByText("hello_e2e_test", { exact: true })
    ).toBeVisible({ timeout: 10_000 });
  });

  test("wrong passphrase shows error", async ({ page }) => {
    session = await startEncryptedSession("testpass123");
    await page.goto(`/session/${session.sessionId}`);

    // Wait for passphrase overlay
    await expect(page.getByPlaceholder("Passphrase")).toBeVisible({
      timeout: 10_000,
    });

    // Wait for some data to buffer so decryption can fail
    await new Promise((r) => setTimeout(r, 1500));
    session.process.stdin?.write('echo "trigger_output"\n');
    await new Promise((r) => setTimeout(r, 1000));

    // Enter wrong passphrase
    await page.getByPlaceholder("Passphrase").fill("wrongkey");
    await page.getByRole("button", { name: "[decrypt]" }).click();

    // Error should be shown
    await expect(page.getByText("Decryption failed")).toBeVisible({
      timeout: 5_000,
    });

    // Passphrase field should still be visible for retry
    await expect(page.getByPlaceholder("Passphrase")).toBeVisible();
  });

  test("unencrypted session works without prompt", async ({ page }) => {
    session = await startUnencryptedSession();
    await page.goto(`/session/${session.sessionId}`);

    // No passphrase prompt
    await expect(page.getByText("ENCRYPTED SESSION")).not.toBeVisible();

    // Connected status
    await expect(page.getByText("[connected]")).toBeVisible({
      timeout: 10_000,
    });

    // Unencrypted badge
    await expect(page.getByText("[unencrypted]")).toBeVisible();
  });

  test("encrypted session shows badge in session list", async ({ page }) => {
    session = await startEncryptedSession("testpass123");
    session2 = await startUnencryptedSession();

    await page.goto("/");

    // Wait for sessions to load
    await expect(page.getByText(session.sessionId)).toBeVisible({
      timeout: 10_000,
    });

    // Encrypted session should have badge
    const encryptedCard = page
      .locator(`a[href="/session/${session.sessionId}"]`)
      .locator("..");
    await expect(encryptedCard.getByText("[encrypted]")).toBeVisible();

    // Unencrypted session should NOT have encrypted badge
    const unencryptedCard = page
      .locator(`a[href="/session/${session2.sessionId}"]`)
      .locator("..");
    await expect(unencryptedCard.getByText("[encrypted]")).not.toBeVisible();
  });

  test("viewer input is encrypted and reaches CLI", async ({ page }) => {
    session = await startEncryptedSession("testpass123");
    await page.goto(`/session/${session.sessionId}`);

    // Wait for passphrase overlay
    await expect(page.getByPlaceholder("Passphrase")).toBeVisible({
      timeout: 10_000,
    });

    // Enter correct passphrase
    await page.getByPlaceholder("Passphrase").fill("testpass123");
    await page.getByRole("button", { name: "[decrypt]" }).click();

    // Wait for overlay to disappear and terminal to be ready
    await expect(page.getByText("ENCRYPTED SESSION")).not.toBeVisible({
      timeout: 5_000,
    });
    await new Promise((r) => setTimeout(r, 1000));

    // Type a command in the terminal (via xterm)
    const terminal = page.locator(".xterm-helper-textarea");
    await terminal.focus();
    await terminal.pressSequentially('echo "viewer_typed_this"', {
      delay: 50,
    });
    await terminal.press("Enter");

    // Wait for the output to appear
    await expect(
      page.locator(".xterm-rows").getByText("viewer_typed_this")
    ).toBeVisible({ timeout: 10_000 });
  });
});
