import { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useNotifications } from '../context/NotificationsContext';

const ICONS = {
  booking_requested: '📩',
  booking_confirmed: '✅',
  booking_cancelled: '❌',
  message_received: '💬',
};

export default function Notifications() {
  const { items, unread, refresh, markRead, markAllRead } = useNotifications();
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
    <div className="container">
      <div className="row-between">
        <h1>Notifications</h1>
        {unread > 0 && <button className="btn btn-ghost" onClick={markAllRead}>Mark all read</button>}
      </div>
      {items.length === 0 ? (
        <p>No notifications yet.</p>
      ) : (
        <ul className="notif-list">
          {items.map((n) => (
            <li
              key={n.id}
              className={`notif-item${n.read ? '' : ' notif-unread'}`}
              onClick={() => onClick(n)}
            >
              <span className="notif-icon">{ICONS[n.type] || '🔔'}</span>
              <div className="notif-body">
                <div className="notif-title">{n.title}</div>
                <div className="notif-text">{n.body}</div>
                <div className="notif-time">{new Date(n.createdAt).toLocaleString()}</div>
              </div>
              {!n.read && <span className="notif-dot" />}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
