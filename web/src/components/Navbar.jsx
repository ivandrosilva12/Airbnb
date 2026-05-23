import { Link } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { useNotifications } from '../context/NotificationsContext';
import { useMessages } from '../context/MessagesContext';

export default function Navbar() {
  const { authenticated, profile, isHost, login, register, logout } = useAuth();
  const { unread } = useNotifications();
  const { unread: msgUnread } = useMessages();
  return (
    <header className="navbar">
      <div className="container navbar-inner">
        <Link to="/" className="brand">AirHost</Link>
        <nav className="nav-links">
          <Link to="/">Explore</Link>
          {authenticated && <Link to="/trips">My trips</Link>}
          {authenticated && <Link to="/saved">Saved</Link>}
          {authenticated && (
            <Link to="/messages" className="nav-with-badge">
              Messages{msgUnread > 0 && <span className="count-badge">{msgUnread > 9 ? '9+' : msgUnread}</span>}
            </Link>
          )}
          {authenticated && isHost && <Link to="/host">Host dashboard</Link>}
          {authenticated && (
            <Link to="/notifications" className="bell" aria-label="Notifications">
              🔔{unread > 0 && <span className="bell-badge">{unread > 9 ? '9+' : unread}</span>}
            </Link>
          )}
          {authenticated ? (
            <>
              <span className="nav-user">{profile?.fullName || 'Account'}</span>
              <button className="btn btn-ghost" onClick={logout}>Sign out</button>
            </>
          ) : (
            <>
              <button className="btn btn-ghost" onClick={login}>Sign in</button>
              <button className="btn btn-primary" onClick={register}>Sign up</button>
            </>
          )}
        </nav>
      </div>
    </header>
  );
}
