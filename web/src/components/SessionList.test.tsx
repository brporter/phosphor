import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { SessionList } from "./SessionList";
import { useSessions } from "../hooks/useSessions";

vi.mock("../hooks/useSessions");
const mockUseSessions = vi.mocked(useSessions);

function renderSessionList() {
  return render(
    <MemoryRouter>
      <SessionList />
    </MemoryRouter>
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
        { id: "s1", mode: "pty", cols: 80, rows: 24, command: "bash", viewers: 1 },
        { id: "s2", mode: "pty", cols: 120, rows: 40, command: "zsh", viewers: 3 },
      ],
      isLoading: false,
      error: null,
      refresh: vi.fn(),
    });

    renderSessionList();

    expect(screen.getByText("Active Sessions [2]")).toBeInTheDocument();
  });
});
