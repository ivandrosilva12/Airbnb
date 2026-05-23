import { Link } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';

export default function Navbar() {
  const { authenticated, profile, isHost, login, register, logout } = useAuth();
  return (
    <header className="navbar">
      <div className="container navbar-inner">
        <Link to="/" className="brand">AirHost</Link>
        <nav className="nav-links">
          <Link to="/">Explore</Link>
          {authenticated && <Link to="/trips">My trips</Link>}
          {authenticated && isHost && <Link to="/host">Host dashboard</Link>}
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
