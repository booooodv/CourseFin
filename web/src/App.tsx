import type { ReactNode } from 'react';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { getToken } from './api/client';
import Layout from './components/Layout';
import { ToastProvider } from './components/Toast';
import { UpdateProvider } from './components/UpdateContext';
import { LibraryProvider } from './contexts/LibraryContext';
import { ThemeProvider } from './contexts/ThemeContext';
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

function AuthenticatedShell() {
  return (
    <RequireAuth>
      <LibraryProvider>
        <Layout />
      </LibraryProvider>
    </RequireAuth>
  );
}

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<SetupLoginPage />} />
      <Route element={<AuthenticatedShell />}>
        <Route path="/" element={<HomePage />} />
        <Route path="/courses/:courseId" element={<CourseDetailPage />} />
        <Route path="/courses/:courseId/edit" element={<CourseEditorPage />} />
        <Route path="/storage" element={<StoragePage />} />
        <Route path="/settings" element={<SettingsPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}

export default function App() {
  return (
    <ThemeProvider>
      <ToastProvider>
        <UpdateProvider>
          <BrowserRouter basename="/web">
            <AppRoutes />
          </BrowserRouter>
        </UpdateProvider>
      </ToastProvider>
    </ThemeProvider>
  );
}
