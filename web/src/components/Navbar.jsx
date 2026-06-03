import { Link } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { useNotifications } from '../context/NotificationsContext';
import { useMessages } from '../context/MessagesContext';
import { useT } from '../i18n/I18nContext';

function LanguageSwitcher() {
  const { lang, setLang, locales, t } = useT();
  // Switched from a 2-button toggle to a <select> in S69 to support
  // 4+ locales without crowding the navbar. Options render with their
  // native label so a Spanish reader can find "Español" without first
  // recognising the English label.
  return (
    <label className="lang-switch" aria-label={t('a11y.language')}>
      <select
        value={lang}
        onChange={(e) => setLang(e.target.value)}
        aria-label={t('a11y.language')}
      >
        {locales.map((l) => (
          <option key={l.code} value={l.code}>{l.label}</option>
        ))}
      </select>
    </label>
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
        <nav className="nav-links" aria-label={t('a11y.primaryNav')}>
          <Link to="/">{t('nav.explore')}</Link>
          <Link to="/experiences">{t('nav.experiences')}</Link>
          {authenticated && <Link to="/trips">{t('nav.trips')}</Link>}
          {authenticated && <Link to="/experience-bookings/me">{t('nav.expBookings')}</Link>}
          {authenticated && <Link to="/saved">{t('nav.saved')}</Link>}
          {authenticated && (
            <Link
              to="/messages"
              className="nav-with-badge"
              aria-label={msgUnread > 0 ? t('a11y.messagesUnread', { count: msgUnread }) : t('nav.messages')}
            >
              {t('nav.messages')}{msgUnread > 0 && <span className="count-badge" aria-hidden="true">{msgUnread > 9 ? '9+' : msgUnread}</span>}
            </Link>
          )}
          {authenticated && isHost && <Link to="/host">{t('nav.host')}</Link>}
          {authenticated && isAdmin && <Link to="/admin">{t('nav.admin')}</Link>}
          {authenticated && (
            <Link to="/notifications" className="bell" aria-label={unread > 0 ? t('a11y.notificationsUnread', { count: unread }) : t('nav.notifications')}>
              <span aria-hidden="true">🔔{unread > 0 && <span className="bell-badge">{unread > 9 ? '9+' : unread}</span>}</span>
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
