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
  viewers: 2,
};

function renderCard(session: SessionData) {
  return render(
    <MemoryRouter>
      <SessionCard session={session} />
    </MemoryRouter>
  );
}

describe("SessionCard", () => {
  it("renders session info", () => {
    renderCard(baseSession);

    expect(screen.getByText("bash")).toBeInTheDocument();
    expect(screen.getByText("80x24")).toBeInTheDocument();
    expect(screen.getByText("2 viewers")).toBeInTheDocument();
    expect(screen.getByText("abc123")).toBeInTheDocument();
  });

  it("shows mode when no command", () => {
    renderCard({ ...baseSession, command: "" });

    // Both the command slot and mode badge show "pty" — verify there are 2 instances
    expect(screen.getAllByText("pty")).toHaveLength(2);
  });

  it("links to session URL", () => {
    renderCard(baseSession);

    const link = screen.getByRole("link");
    expect(link).toHaveAttribute("href", "/session/abc123");
  });
});
