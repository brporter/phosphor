import { BrowserRouter, Routes, Route } from "react-router";
import { AuthProvider } from "./auth/AuthProvider";
import { Layout } from "./components/Layout";
import { MachineList } from "./components/MachineList";
import { ConnectView } from "./components/ConnectView";
import { KeysPage } from "./components/KeysPage";
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
                  <MachineList />
                </ProtectedRoute>
              }
            />
            <Route
              path="/machine/:id"
              element={
                <ProtectedRoute>
                  <ConnectView />
                </ProtectedRoute>
              }
            />
            <Route
              path="/keys"
              element={
                <ProtectedRoute>
                  <KeysPage />
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
