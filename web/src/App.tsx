import type { ReactNode } from 'react';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { getToken } from './api/client';
import Layout from './components/Layout';
import HomePage from './pages/HomePage';
import SetupLoginPage from './pages/SetupLoginPage';
import {
  CourseEditPlaceholder,
  CoursePlayerPlaceholder,
  SettingsPlaceholder,
  StoragePlaceholder,
} from './pages/Placeholders';

function RequireAuth({ children }: { children: ReactNode }) {
  const token = getToken();
  if (!token) {
    return <Navigate to="/login" replace />;
  }
  return <>{children}</>;
}

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<SetupLoginPage />} />
      <Route
        path="/"
        element={
          <RequireAuth>
            <Layout>
              <HomePage />
            </Layout>
          </RequireAuth>
        }
      />
      <Route
        path="/courses/:courseId"
        element={
          <RequireAuth>
            <Layout>
              <CoursePlayerPlaceholder />
            </Layout>
          </RequireAuth>
        }
      />
      <Route
        path="/courses/:courseId/edit"
        element={
          <RequireAuth>
            <Layout>
              <CourseEditPlaceholder />
            </Layout>
          </RequireAuth>
        }
      />
      <Route
        path="/storage"
        element={
          <RequireAuth>
            <Layout>
              <StoragePlaceholder />
            </Layout>
          </RequireAuth>
        }
      />
      <Route
        path="/settings"
        element={
          <RequireAuth>
            <Layout>
              <SettingsPlaceholder />
            </Layout>
          </RequireAuth>
        }
      />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}

export default function App() {
  return (
    <BrowserRouter basename="/web">
      <AppRoutes />
    </BrowserRouter>
  );
}
