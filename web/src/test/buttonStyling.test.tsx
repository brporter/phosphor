// Convention guard: every rendered <button> must carry one of the shared
// button classes so the :disabled styling and CRT look apply uniformly.
// A bare <button> here means a new UI path skipped the design system.

import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route } from "react-router";
import { KeysPage } from "../components/KeysPage";
import { ImportKeyModal } from "../components/ImportKeyModal";
import { ConnectView } from "../components/ConnectView";
import { MachineCard } from "../components/MachineCard";
import { SettingsPage } from "../components/SettingsPage";
import { ProviderButtons } from "../components/ProviderButtons";
import { Layout } from "../components/Layout";
import { AuthModal } from "../components/AuthModal";
import type { AuthRequest } from "../hooks/useSSH";

vi.mock("../lib/wasm", () => ({ loadSSH: vi.fn().mockResolvedValue({}) }));
vi.mock("../lib/keys", () => ({
  listKeys: vi.fn().mockResolvedValue([]),
  saveKey: vi.fn(),
  deleteKey: vi.fn(),
}));
vi.mock("../lib/machines", () => ({ deleteMachine: vi.fn() }));
vi.mock("../lib/api", () => ({ generateApiKey: vi.fn() }));
vi.mock("../auth/useAuth", () => ({
  useAuth: () => ({
    user: { profile: { sub: "u", email: "u@example.com" } },
    providers: ["microsoft", "google"],
    login: vi.fn(),
    logout: vi.fn(),
    getToken: () => "tok",
  }),
}));
vi.mock("../hooks/useSSH", () => ({
  useSSH: () => ({
    connected: false,
    connecting: false,
    error: null,
    authRequest: null,
    sendStdin: vi.fn(),
    sendResize: vi.fn(),
    disconnect: vi.fn(),
  }),
}));

const ALLOWED = ["btn-action", "btn-danger", "btn-primary"];

function expectButtonsStyled() {
  const buttons = screen.queryAllByRole("button");
  expect(buttons.length).toBeGreaterThan(0);
  for (const button of buttons) {
    const classes = [...button.classList];
    expect(
      classes.some((c) => ALLOWED.includes(c)),
      `button "${button.textContent}" has classes [${classes.join(", ")}] — expected one of ${ALLOWED.join(", ")}`,
    ).toBe(true);
  }
}

describe("button styling convention", () => {
  it("KeysPage buttons use shared classes", async () => {
    render(<KeysPage />);
    await screen.findByText("No keys yet."); // initial async key load settles
    expectButtonsStyled();
  });

  it("ImportKeyModal buttons use shared classes", () => {
    render(<ImportKeyModal onClose={() => {}} onImported={() => {}} />);
    expectButtonsStyled();
  });

  it("ConnectView form buttons use shared classes (including inline key section)", async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter initialEntries={["/machine/m-1"]}>
        <Routes>
          <Route path="/machine/:id" element={<ConnectView />} />
        </Routes>
      </MemoryRouter>,
    );
    await user.selectOptions(
      screen.getByLabelText(/key \(optional/i),
      screen.getByRole("option", { name: /use a key just for this session/i }),
    );
    expectButtonsStyled();
  });

  it("MachineCard buttons use shared classes (including the remove confirm)", async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <MachineCard
          machine={{
            id: "m-1",
            name: "host",
            hostname: "host.local",
            fingerprint: "SHA256:x",
            online: true,
            created_at: "2026-01-01T00:00:00Z",
            last_seen_at: null,
          }}
          token="tok"
          onChanged={() => {}}
        />
      </MemoryRouter>,
    );
    await user.click(screen.getByRole("button", { name: /remove/i }));
    expectButtonsStyled();
  });

  it("SettingsPage buttons use shared classes", () => {
    render(<SettingsPage />);
    expectButtonsStyled();
  });

  it("ProviderButtons use shared classes", () => {
    render(<ProviderButtons providers={["microsoft", "google", "apple"]} login={vi.fn()} />);
    expectButtonsStyled();
  });

  it("Layout buttons use shared classes", () => {
    render(
      <MemoryRouter>
        <Routes>
          <Route element={<Layout />}>
            <Route index element={<div />} />
          </Route>
        </Routes>
      </MemoryRouter>,
    );
    expectButtonsStyled();
  });

  it("AuthModal variants use shared classes", () => {
    const requests: AuthRequest[] = [
      { kind: "password", respond: vi.fn() },
      { kind: "keyboard-interactive", name: "n", instruction: "i", questions: ["q1"], respond: vi.fn() },
      { kind: "hostkey", fingerprint: "SHA256:x", keyType: "ssh-ed25519", known: false, respond: vi.fn() },
    ];
    for (const request of requests) {
      const { unmount } = render(<AuthModal request={request} />);
      expectButtonsStyled();
      unmount();
    }
  });
});
