import { useEffect } from 'react';
import keycloak from '../keycloak';
import { useAuth } from './AuthContext';
import { useNotifications } from './NotificationsContext';
import { useMessages } from './MessagesContext';

const BASE_URL = import.meta.env.VITE_API_BASE_URL || 'http://localhost:8081/api/v1';

// RealtimeProvider opens a Server-Sent Events stream and refreshes the
// notification/message views the moment the server signals a change, giving
// near-instant badge updates. The 30s polling in those contexts remains as a
// fallback if the stream drops. The token travels as a query parameter because
// the browser EventSource API cannot set an Authorization header.
export function RealtimeProvider({ children }) {
  const { authenticated } = useAuth();
  const { refresh: refreshNotifications } = useNotifications();
  const { refresh: refreshMessages } = useMessages();

  useEffect(() => {
    if (!authenticated || !keycloak.token) return undefined;
    const url = `${BASE_URL}/realtime?access_token=${encodeURIComponent(keycloak.token)}`;
    const es = new EventSource(url);
    es.onmessage = (e) => {
      try {
        const update = JSON.parse(e.data);
        if (update.type === 'message') refreshMessages();
        else refreshNotifications();
      } catch {
        /* ignore malformed payloads */
      }
    };
    return () => es.close();
  }, [authenticated, refreshNotifications, refreshMessages]);

  return children;
}
