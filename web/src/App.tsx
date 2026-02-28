import { BrowserRouter, Routes, Route } from "react-router";
import { AuthProvider } from "./auth/AuthProvider";
import { Layout } from "./components/Layout";
import { SessionList } from "./components/SessionList";
import { TerminalView } from "./components/TerminalView";
import { AuthCallback } from "./auth/AuthCallback";
import { ProtectedRoute } from "./components/ProtectedRoute";

export function App() {
  return (
    <AuthProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/auth/callback" element={<AuthCallback />} />
          <Route element={<Layout />}>
            <Route
              index
              element={
                <ProtectedRoute>
                  <SessionList />
                </ProtectedRoute>
              }
            />
            <Route
              path="/session/:id"
              element={
                <ProtectedRoute>
                  <TerminalView />
                </ProtectedRoute>
              }
            />
          </Route>
        </Routes>
      </BrowserRouter>
    </AuthProvider>
  );
}
