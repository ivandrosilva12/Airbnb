import { Link } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { useNotifications } from '../context/NotificationsContext';
import { useMessages } from '../context/MessagesContext';
import { useT } from '../i18n/I18nContext';

function LanguageSwitcher() {
  const { lang, setLang } = useT();
  return (
    <span className="lang-switch">
      <button className={lang === 'pt' ? 'active' : ''} onClick={() => setLang('pt')}>PT</button>
      <span>/</span>
      <button className={lang === 'en' ? 'active' : ''} onClick={() => setLang('en')}>EN</button>
    </span>
  );
}

export default function Navbar() {
  const { authenticated, profile, isHost, isAdmin, login, register, logout } = useAuth();
  const { unread } = useNotifications();
  const { unread: msgUnread } = useMessages();
  const { t } = useT();
  return (
    <header className="navbar">
      <div className="container navbar-inner">
        <Link to="/" className="brand">AirHost</Link>
        <nav className="nav-links">
          <Link to="/">{t('nav.explore')}</Link>
          {authenticated && <Link to="/trips">{t('nav.trips')}</Link>}
          {authenticated && <Link to="/saved">{t('nav.saved')}</Link>}
          {authenticated && (
            <Link to="/messages" className="nav-with-badge">
              {t('nav.messages')}{msgUnread > 0 && <span className="count-badge">{msgUnread > 9 ? '9+' : msgUnread}</span>}
            </Link>
          )}
          {authenticated && isHost && <Link to="/host">{t('nav.host')}</Link>}
          {authenticated && isAdmin && <Link to="/admin">{t('nav.admin')}</Link>}
          {authenticated && (
            <Link to="/notifications" className="bell" aria-label={t('nav.notifications')}>
              🔔{unread > 0 && <span className="bell-badge">{unread > 9 ? '9+' : unread}</span>}
            </Link>
          )}
          {authenticated ? (
            <>
              <Link to="/settings" className="nav-user">{profile?.fullName || t('nav.account')}</Link>
              <button className="btn btn-ghost" onClick={logout}>{t('nav.signOut')}</button>
            </>
          ) : (
            <>
              <button className="btn btn-ghost" onClick={login}>{t('nav.signIn')}</button>
              <button className="btn btn-primary" onClick={register}>{t('nav.signUp')}</button>
            </>
          )}
          <LanguageSwitcher />
        </nav>
      </div>
    </header>
  );
}
