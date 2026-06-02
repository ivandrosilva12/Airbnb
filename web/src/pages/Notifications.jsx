import { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useNotifications } from '../context/NotificationsContext';
import { useT } from '../i18n/I18nContext';

const ICONS = {
  booking_requested: '📩',
  booking_confirmed: '✅',
  booking_cancelled: '❌',
  booking_modified: '📝',
  saved_search_alert: '🔎',
  message_received: '💬',
};

export default function Notifications() {
  const { items, unread, refresh, markRead, markUnread, markAllRead } = useNotifications();
  const { t } = useT();
  const navigate = useNavigate();

  useEffect(() => {
    refresh();
  }, []);

  function onClick(n) {
    if (!n.read) markRead(n.id);
    if (n.type === 'message_received') navigate('/messages');
    else if (n.type.startsWith('booking_')) navigate('/trips');
  }

  return (
    <main className="container" aria-label={t('notif.title')}>
      <div className="row-between">
        <h1>{t('notif.title')}</h1>
        {unread > 0 && (
          <button
            className="btn btn-ghost"
            onClick={markAllRead}
            aria-label={`${t('notif.markAll')} (${unread} unread)`}
          >{t('notif.markAll')}</button>
        )}
      </div>
      {items.length === 0 ? (
        <p>{t('notif.none')}</p>
      ) : (
        <ul className="notif-list" aria-label={t('notif.title')}>
          {items.map((n) => (
            <li
              key={n.id}
              className={`notif-item${n.read ? '' : ' notif-unread'}`}
              onClick={() => onClick(n)}
              role="button"
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  onClick(n);
                }
              }}
              aria-label={`${n.read ? '' : 'Unread, '}${n.title}: ${n.body}`}
            >
              <span className="notif-icon" aria-hidden="true">{ICONS[n.type] || '🔔'}</span>
              <div className="notif-body">
                <div className="notif-title">{n.title}</div>
                <div className="notif-text">{n.body}</div>
                <div className="notif-time">{n.createdAt ? new Date(n.createdAt).toLocaleString() : ''}</div>
              </div>
              <button
                className="notif-toggle"
                onClick={(e) => { e.stopPropagation(); (n.read ? markUnread : markRead)(n.id); }}
                aria-label={n.read ? t('notif.markUnread') : t('notif.markRead')}
                aria-pressed={!n.read}
              >
                {n.read ? t('notif.markUnread') : t('notif.markRead')}
              </button>
              {!n.read && <span className="notif-dot" aria-hidden="true" />}
            </li>
          ))}
        </ul>
      )}
    </main>
  );
}
