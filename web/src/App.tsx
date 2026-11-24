import type { ReactNode } from 'react';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { getToken } from './api/client';
import Layout from './components/Layout';
import { ToastProvider } from './components/Toast';
import { UpdateProvider } from './components/UpdateContext';
import CourseDetailPage from './pages/CourseDetailPage';
import CourseEditorPage from './pages/CourseEditorPage';
import HomePage from './pages/HomePage';
import SettingsPage from './pages/SettingsPage';
import SetupLoginPage from './pages/SetupLoginPage';
import StoragePage from './pages/StoragePage';

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
              <CourseDetailPage />
            </Layout>
          </RequireAuth>
        }
      />
      <Route
        path="/courses/:courseId/edit"
        element={
          <RequireAuth>
            <Layout>
              <CourseEditorPage />
            </Layout>
          </RequireAuth>
        }
      />
      <Route
        path="/storage"
        element={
          <RequireAuth>
            <Layout>
              <StoragePage />
            </Layout>
          </RequireAuth>
        }
      />
      <Route
        path="/settings"
        element={
          <RequireAuth>
            <Layout>
              <SettingsPage />
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
    <ToastProvider>
      <UpdateProvider>
        <BrowserRouter basename="/web">
          <AppRoutes />
        </BrowserRouter>
      </UpdateProvider>
    </ToastProvider>
  );
}
