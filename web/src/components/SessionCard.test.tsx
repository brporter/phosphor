import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { SessionCard } from "./SessionCard";
import type { SessionData } from "./SessionCard";

const baseSession: SessionData = {
  id: "abc123",
  mode: "pty",
  cols: 80,
  rows: 24,
  command: "bash",
  hostname: "",
  viewers: 2,
  process_exited: false,
  lazy: false,
  process_running: true,
};

function renderCard(session: SessionData) {
  return render(
    <MemoryRouter>
      <SessionCard session={session} token={null} onDestroyed={vi.fn()} />
    </MemoryRouter>
  );
}

describe("SessionCard", () => {
  it("renders session info", () => {
    renderCard(baseSession);

    expect(screen.getByText("bash")).toBeInTheDocument();
    expect(screen.getByText(/viewers: 2/)).toBeInTheDocument();
    expect(screen.getByText(/abc123/)).toBeInTheDocument();
  });

  it("shows mode when no command", () => {
    renderCard({ ...baseSession, command: "" });

    // The mode appears as command text fallback and as badge [pty]
    expect(screen.getByText("[pty]")).toBeInTheDocument();
  });

  it("links to session URL", () => {
    renderCard(baseSession);

    const link = screen.getByRole("link");
    expect(link).toHaveAttribute("href", "/session/abc123");
  });
});
