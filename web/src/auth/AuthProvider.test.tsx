import { render, screen, act, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useContext } from "react";
import { AuthProvider, AuthContext } from "./AuthProvider";

const STORAGE_KEY = "phosphor_user";
const SESSION_KEY = "phosphor_auth_session";

function makeTestJwt(payload: Record<string, unknown>): string {
  const header = btoa(JSON.stringify({ alg: "none", typ: "JWT" }));
  const body = btoa(JSON.stringify(payload));
  return `${header}.${body}.signature`;
}

function AuthConsumer() {
  const { user, isLoading, login, logout, getToken } = useContext(AuthContext);
  return (
    <div>
      <span data-testid="loading">{String(isLoading)}</span>
      <span data-testid="user">{user ? user.profile.sub : "null"}</span>
      <span data-testid="token">{getToken() ?? "null"}</span>
      <button onClick={() => void login("microsoft")}>login</button>
      <button onClick={() => void logout()}>logout</button>
    </div>
  );
}

const mockFetch = vi.fn();

beforeEach(() => {
  vi.stubGlobal("fetch", mockFetch);
  localStorage.clear();
  Object.defineProperty(window, "location", {
    value: { href: "" },
    writable: true,
  });
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("AuthProvider", () => {
  it("renders children and finishes loading when no localStorage or session exists", async () => {
    render(
      <AuthProvider>
        <AuthConsumer />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("loading")).toHaveTextContent("false");
    });

    expect(screen.getByTestId("user")).toHaveTextContent("null");
    expect(screen.getByTestId("token")).toHaveTextContent("null");
  });

  it("restores cached user from localStorage with a valid non-expired JWT", async () => {
    const futureExp = Math.floor(Date.now() / 1000) + 3600;
    const token = makeTestJwt({
      sub: "user-123",
      iss: "https://example.com",
      email: "user@example.com",
      exp: futureExp,
    });
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ id_token: token, profile: { sub: "user-123", iss: "https://example.com" } }));

    render(
      <AuthProvider>
        <AuthConsumer />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("loading")).toHaveTextContent("false");
    });

    expect(screen.getByTestId("user")).toHaveTextContent("user-123");
    expect(screen.getByTestId("token")).toHaveTextContent(token);
  });

  it("rejects an expired cached token and clears localStorage", async () => {
    const pastExp = Math.floor(Date.now() / 1000) - 3600;
    const token = makeTestJwt({
      sub: "user-expired",
      iss: "https://example.com",
      exp: pastExp,
    });
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ id_token: token, profile: { sub: "user-expired", iss: "https://example.com" } }));

    render(
      <AuthProvider>
        <AuthConsumer />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("loading")).toHaveTextContent("false");
    });

    expect(screen.getByTestId("user")).toHaveTextContent("null");
    expect(localStorage.getItem(STORAGE_KEY)).toBeNull();
  });

  it("handles invalid cached data gracefully and clears localStorage", async () => {
    localStorage.setItem(STORAGE_KEY, "this is not valid json {{{{");

    render(
      <AuthProvider>
        <AuthConsumer />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("loading")).toHaveTextContent("false");
    });

    expect(screen.getByTestId("user")).toHaveTextContent("null");
    expect(localStorage.getItem(STORAGE_KEY)).toBeNull();
  });

  it("calls fetch with POST /api/auth/login, saves session_id, and sets location.href on login", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        auth_url: "https://login.example.com/oauth",
        session_id: "test-session-abc",
      }),
    });

    render(
      <AuthProvider>
        <AuthConsumer />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("loading")).toHaveTextContent("false");
    });

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "login" }));

    await waitFor(() => {
      expect(mockFetch).toHaveBeenCalledWith("/api/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ provider: "microsoft" }),
      });
    });

    expect(localStorage.getItem(SESSION_KEY)).toBe("test-session-abc");
    expect(window.location.href).toBe("https://login.example.com/oauth");
  });

  it("clears user and localStorage when logout is called", async () => {
    const futureExp = Math.floor(Date.now() / 1000) + 3600;
    const token = makeTestJwt({
      sub: "user-to-logout",
      iss: "https://example.com",
      exp: futureExp,
    });
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ id_token: token, profile: { sub: "user-to-logout", iss: "https://example.com" } }));

    render(
      <AuthProvider>
        <AuthConsumer />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("user")).toHaveTextContent("user-to-logout");
    });

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "logout" }));

    await waitFor(() => {
      expect(screen.getByTestId("user")).toHaveTextContent("null");
    });

    expect(localStorage.getItem(STORAGE_KEY)).toBeNull();
  });

  it("returns the id_token via getToken when a user is logged in", async () => {
    const futureExp = Math.floor(Date.now() / 1000) + 3600;
    const token = makeTestJwt({
      sub: "token-user",
      iss: "https://example.com",
      exp: futureExp,
    });
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ id_token: token, profile: { sub: "token-user", iss: "https://example.com" } }));

    render(
      <AuthProvider>
        <AuthConsumer />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("loading")).toHaveTextContent("false");
    });

    expect(screen.getByTestId("token")).toHaveTextContent(token);
  });

  it("polls for a pending auth session and sets the user when the poll returns complete", async () => {
    localStorage.setItem(SESSION_KEY, "pending-session-xyz");

    const futureExp = Math.floor(Date.now() / 1000) + 3600;
    const token = makeTestJwt({
      sub: "polled-user",
      iss: "https://example.com",
      email: "polled@example.com",
      exp: futureExp,
    });

    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        status: "complete",
        id_token: token,
      }),
    });

    render(
      <AuthProvider>
        <AuthConsumer />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("loading")).toHaveTextContent("false");
    });

    expect(mockFetch).toHaveBeenCalledWith(
      expect.stringContaining("/api/auth/poll?session=pending-session-xyz"),
    );
    expect(localStorage.getItem(SESSION_KEY)).toBeNull();
    expect(screen.getByTestId("user")).toHaveTextContent("polled-user");
    expect(screen.getByTestId("token")).toHaveTextContent(token);
  });
});
