import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { AuthContext } from "../auth/AuthProvider";
import { SessionList } from "./SessionList";
import { useSessions } from "../hooks/useSessions";

vi.mock("../hooks/useSessions");
const mockUseSessions = vi.mocked(useSessions);

function renderSessionList() {
  const authValue = {
    user: null,
    isLoading: false,
    providers: [],
    login: vi.fn(),
    logout: vi.fn(),
    getToken: vi.fn(() => null),
  };
  return render(
    <AuthContext.Provider value={authValue}>
      <MemoryRouter>
        <SessionList />
      </MemoryRouter>
    </AuthContext.Provider>
  );
}

describe("SessionList", () => {
  it("shows loading state", () => {
    mockUseSessions.mockReturnValue({
      sessions: [],
      isLoading: true,
      error: null,
      refresh: vi.fn(),
    });

    renderSessionList();

    expect(screen.getByText("Loading sessions...")).toBeInTheDocument();
  });

  it("shows error state", () => {
    mockUseSessions.mockReturnValue({
      sessions: [],
      isLoading: false,
      error: "Failed to fetch",
      refresh: vi.fn(),
    });

    renderSessionList();

    expect(screen.getByText("Error: Failed to fetch")).toBeInTheDocument();
  });

  it("renders session cards", () => {
    mockUseSessions.mockReturnValue({
      sessions: [
        { id: "s1", mode: "pty", cols: 80, rows: 24, command: "bash", hostname: "", viewers: 1, process_exited: false, lazy: false, process_running: true },
        { id: "s2", mode: "pty", cols: 120, rows: 40, command: "zsh", hostname: "", viewers: 3, process_exited: false, lazy: false, process_running: true },
      ],
      isLoading: false,
      error: null,
      refresh: vi.fn(),
    });

    renderSessionList();

    expect(screen.getByText(/ACTIVE SESSIONS.*\[2\]/)).toBeInTheDocument();
  });
});
