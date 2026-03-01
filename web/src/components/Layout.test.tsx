import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { AuthContext } from "../auth/AuthProvider";
import { Layout } from "./Layout";

const testUser = {
  id_token: "test-token",
  profile: { sub: "user1", iss: "test", email: "test@test.com" },
};

function renderLayout(authOverrides = {}) {
  const defaultAuth = {
    user: null,
    isLoading: false,
    login: vi.fn(),
    logout: vi.fn(),
    getToken: vi.fn(() => null),
  };
  return render(
    <AuthContext.Provider value={{ ...defaultAuth, ...authOverrides }}>
      <MemoryRouter>
        <Layout />
      </MemoryRouter>
    </AuthContext.Provider>
  );
}

describe("Layout", () => {
  it("shows sign-in buttons when logged out", () => {
    renderLayout({ user: null });

    expect(screen.getByText("sign in with Microsoft")).toBeInTheDocument();
    expect(screen.queryByText("logout")).not.toBeInTheDocument();
  });

  it("shows user email and logout when logged in", () => {
    renderLayout({ user: testUser });

    expect(screen.getByText("test@test.com")).toBeInTheDocument();
    expect(screen.getByText("logout")).toBeInTheDocument();
    expect(screen.queryByText("sign in with Microsoft")).not.toBeInTheDocument();
  });

  it("renders footer", () => {
    renderLayout();

    expect(screen.getByText(/phosphor v0\.1\.0/)).toBeInTheDocument();
  });
});
