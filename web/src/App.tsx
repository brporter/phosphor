import { BrowserRouter, Routes, Route } from "react-router";
import { AuthProvider } from "./auth/AuthProvider";
import { Layout } from "./components/Layout";
import { SessionList } from "./components/SessionList";
import { TerminalView } from "./components/TerminalView";
import { ProtectedRoute } from "./components/ProtectedRoute";
import { SettingsPage } from "./components/SettingsPage";

export function App() {
  return (
    <AuthProvider>
      <BrowserRouter>
        <Routes>
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
            <Route
              path="/settings"
              element={
                <ProtectedRoute>
                  <SettingsPage />
                </ProtectedRoute>
              }
            />
          </Route>
        </Routes>
      </BrowserRouter>
    </AuthProvider>
  );
}
