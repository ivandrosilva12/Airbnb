import { createContext, useCallback, useContext, useEffect, useRef } from 'react';
import keycloak from '../keycloak';
import { useAuth } from './AuthContext';
import { useNotifications } from './NotificationsContext';
import { useMessages } from './MessagesContext';

const BASE_URL = import.meta.env.VITE_API_BASE_URL || 'http://localhost:8081/api/v1';

const RealtimeContext = createContext({ subscribe: () => () => {} });

// useRealtime lets a component react to live server events. subscribe(cb) calls
// cb with each parsed update ({ type, conversationId? }) and returns an
// unsubscribe function — used by the chat thread to append inbound messages the
// instant they arrive instead of polling.
export function useRealtime() {
  return useContext(RealtimeContext);
}

// RealtimeProvider opens a Server-Sent Events stream and refreshes the
// notification/message views the moment the server signals a change, giving
// near-instant badge updates and live chat. The polling in those contexts
// remains as a fallback if the stream drops. The token travels as a query
// parameter because the browser EventSource API cannot set an Authorization
// header.
export function RealtimeProvider({ children }) {
  const { authenticated } = useAuth();
  const { refresh: refreshNotifications } = useNotifications();
  const { refresh: refreshMessages } = useMessages();
  const listenersRef = useRef(new Set());

  const subscribe = useCallback((cb) => {
    listenersRef.current.add(cb);
    return () => listenersRef.current.delete(cb);
  }, []);

  useEffect(() => {
    if (!authenticated) return undefined;

    let es;
    let retry;
    let stopped = false;

    // (re)connect with a freshly-refreshed token. The server closes the stream
    // when the access token expires (~minutes); onerror then reconnects with a
    // new token, so realtime updates keep flowing across token rotation.
    async function connect() {
      if (stopped) return;
      try {
        await keycloak.updateToken(30);
      } catch {
        /* token refresh failed; retry below */
      }
      if (stopped || !keycloak.token) {
        if (!stopped) retry = setTimeout(connect, 5000);
        return;
      }
      es = new EventSource(`${BASE_URL}/realtime?access_token=${encodeURIComponent(keycloak.token)}`);
      es.onmessage = (e) => {
        try {
          const update = JSON.parse(e.data);
          if (update.type === 'message') refreshMessages();
          else refreshNotifications();
          // Fan the raw update out to subscribers (e.g. an open chat thread).
          listenersRef.current.forEach((cb) => {
            try {
              cb(update);
            } catch {
              /* a listener error must not break the stream */
            }
          });
        } catch {
          /* ignore malformed payloads */
        }
      };
      es.onerror = () => {
        es.close();
        if (stopped) return;
        clearTimeout(retry);
        retry = setTimeout(connect, 5000); // reconnect with a fresh token
      };
    }

    connect();
    return () => {
      stopped = true;
      clearTimeout(retry);
      if (es) es.close();
    };
  }, [authenticated, refreshNotifications, refreshMessages]);

  return <RealtimeContext.Provider value={{ subscribe }}>{children}</RealtimeContext.Provider>;
}
