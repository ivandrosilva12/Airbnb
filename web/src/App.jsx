import { Routes, Route, Navigate } from 'react-router-dom';
import { useAuth } from './context/AuthContext';
import Navbar from './components/Navbar';
import Home from './pages/Home';
import PropertyDetail from './pages/PropertyDetail';
import MyTrips from './pages/MyTrips';
import HostDashboard from './pages/HostDashboard';
import CreateListing from './pages/CreateListing';

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
  return (
    <>
      <Navbar />
      <main>
        {!ready ? (
          <div className="container">Loading…</div>
        ) : (
          <Routes>
            <Route path="/" element={<Home />} />
            <Route path="/properties/:id" element={<PropertyDetail />} />
            <Route path="/trips" element={<Protected><MyTrips /></Protected>} />
            <Route path="/host" element={<Protected><HostDashboard /></Protected>} />
            <Route path="/host/new" element={<Protected><CreateListing /></Protected>} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        )}
      </main>
    </>
  );
}
