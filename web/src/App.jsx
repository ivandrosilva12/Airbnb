import { Routes, Route, Navigate } from 'react-router-dom';
import { useAuth } from './context/AuthContext';
import { useT } from './i18n/I18nContext';
import Navbar from './components/Navbar';
import Home from './pages/Home';
import PropertyDetail from './pages/PropertyDetail';
import MyTrips from './pages/MyTrips';
import Favorites from './pages/Favorites';
import Messages from './pages/Messages';
import Notifications from './pages/Notifications';
import HostDashboard from './pages/HostDashboard';
import HostBookings from './pages/HostBookings';
import CreateListing from './pages/CreateListing';
import EditListing from './pages/EditListing';
import HostPhotos from './pages/HostPhotos';
import HostEarnings from './pages/HostEarnings';
import Settings from './pages/Settings';
import Admin from './pages/Admin';

function Protected({ children }) {
  const { ready, authenticated, login } = useAuth();
  if (!ready) return <div className="container">Loading…</div>;
  if (!authenticated) {
    login();
    return <div className="container">Redirecting to sign in…</div>;
  }
  return children;
}

export default function App() {
  const { ready } = useAuth();
  const { t } = useT();
  return (
    <>
      <a href="#main" className="skip-link">{t('a11y.skip')}</a>
      <Navbar />
      <main id="main" tabIndex={-1}>
        {!ready ? (
          <div className="container">Loading…</div>
        ) : (
          <Routes>
            <Route path="/" element={<Home />} />
            <Route path="/properties/:id" element={<PropertyDetail />} />
            <Route path="/trips" element={<Protected><MyTrips /></Protected>} />
            <Route path="/saved" element={<Protected><Favorites /></Protected>} />
            <Route path="/messages" element={<Protected><Messages /></Protected>} />
            <Route path="/notifications" element={<Protected><Notifications /></Protected>} />
            <Route path="/settings" element={<Protected><Settings /></Protected>} />
            <Route path="/admin" element={<Protected><Admin /></Protected>} />
            <Route path="/host" element={<Protected><HostDashboard /></Protected>} />
            <Route path="/host/new" element={<Protected><CreateListing /></Protected>} />
            <Route path="/host/properties/:id/edit" element={<Protected><EditListing /></Protected>} />
            <Route path="/host/earnings" element={<Protected><HostEarnings /></Protected>} />
            <Route path="/host/properties/:id/bookings" element={<Protected><HostBookings /></Protected>} />
            <Route path="/host/properties/:id/photos" element={<Protected><HostPhotos /></Protected>} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        )}
      </main>
    </>
  );
}
