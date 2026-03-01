import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AuthContext } from "../auth/AuthProvider";
import { ProtectedRoute } from "./ProtectedRoute";

const testUser = {
  id_token: "test-token",
  profile: { sub: "user1", iss: "test", email: "test@test.com" },
};

function renderWithAuth(ui: React.ReactElement, authOverrides = {}) {
  const defaultAuth = {
    user: null,
    isLoading: false,
    login: vi.fn(),
    logout: vi.fn(),
    getToken: vi.fn(() => null),
  };
  return render(
    <AuthContext.Provider value={{ ...defaultAuth, ...authOverrides }}>
      {ui}
    </AuthContext.Provider>
  );
}

describe("ProtectedRoute", () => {
  it("shows loading state", () => {
    renderWithAuth(
      <ProtectedRoute>
        <div>Protected content</div>
      </ProtectedRoute>,
      { isLoading: true }
    );

    expect(screen.getByText("Initializing...")).toBeInTheDocument();
    expect(screen.queryByText("Protected content")).not.toBeInTheDocument();
  });

  it("shows login buttons when not authenticated", () => {
    renderWithAuth(
      <ProtectedRoute>
        <div>Protected content</div>
      </ProtectedRoute>,
      { user: null }
    );

    expect(screen.getByText("sign in with Microsoft")).toBeInTheDocument();
    expect(screen.getByText("sign in with Google")).toBeInTheDocument();
    expect(screen.getByText("sign in with Apple")).toBeInTheDocument();
    expect(screen.queryByText("Protected content")).not.toBeInTheDocument();
  });

  it("renders children when authenticated", () => {
    renderWithAuth(
      <ProtectedRoute>
        <div>Protected content</div>
      </ProtectedRoute>,
      { user: testUser }
    );

    expect(screen.getByText("Protected content")).toBeInTheDocument();
    expect(screen.queryByText("sign in with Microsoft")).not.toBeInTheDocument();
  });
});
