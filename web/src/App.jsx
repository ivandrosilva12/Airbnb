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
import SharedWishlist from './pages/SharedWishlist';
import SplitPay from './pages/SplitPay';

function Protected({ children, requireHost, requireAdmin }) {
  const { ready, authenticated, isHost, isAdmin, login } = useAuth();
  if (!ready) return <div className="container">Loading…</div>;
  if (!authenticated) {
    login();
    return <div className="container">Redirecting to sign in…</div>;
  }
  // Role-gated areas: a signed-in user without the role is bounced home rather
  // than shown a page whose every API call would 403.
  if (requireAdmin && !isAdmin) return <Navigate to="/" replace />;
  if (requireHost && !isHost) return <Navigate to="/" replace />;
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
            <Route path="/wishlist/:token" element={<SharedWishlist />} />
            <Route path="/trips" element={<Protected><MyTrips /></Protected>} />
            <Route path="/saved" element={<Protected><Favorites /></Protected>} />
            <Route path="/messages" element={<Protected><Messages /></Protected>} />
            <Route path="/notifications" element={<Protected><Notifications /></Protected>} />
            <Route path="/settings" element={<Protected><Settings /></Protected>} />
            <Route path="/admin" element={<Protected requireAdmin><Admin /></Protected>} />
            <Route path="/host" element={<Protected requireHost><HostDashboard /></Protected>} />
            <Route path="/host/new" element={<Protected requireHost><CreateListing /></Protected>} />
            <Route path="/host/properties/:id/edit" element={<Protected requireHost><EditListing /></Protected>} />
            <Route path="/host/earnings" element={<Protected requireHost><HostEarnings /></Protected>} />
            <Route path="/host/properties/:id/bookings" element={<Protected requireHost><HostBookings /></Protected>} />
            <Route path="/host/properties/:id/photos" element={<Protected requireHost><HostPhotos /></Protected>} />
            <Route path="/splits/:id" element={<Protected><SplitPay /></Protected>} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        )}
      </main>
    </>
  );
}
