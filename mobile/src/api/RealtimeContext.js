import { createContext, useCallback, useContext, useEffect, useRef } from 'react';
import { useAuth } from '../auth/AuthContext';
import { API_BASE_URL } from '../config';

const RealtimeContext = createContext({ subscribe: () => () => {} });

// useRealtime lets a screen react to live server events. subscribe(cb) calls cb
// with each parsed update ({ type, conversationId? }) and returns an unsubscribe
// function — the chat thread uses it to append inbound messages instantly.
export function useRealtime() {
  return useContext(RealtimeContext);
}

// RealtimeProvider keeps a Server-Sent Events stream open while signed in.
// React Native has no native EventSource, so we read the stream with a plain
// XMLHttpRequest (supported in Expo) and parse SSE "data:" lines ourselves.
// The token is passed as a query param (the API accepts it there for this
// route); the server closes the stream when the token expires, so the onload /
// onerror reconnect refreshes the token and reconnects.
export function RealtimeProvider({ children }) {
  const { authenticated, getAccessToken } = useAuth();
  const listenersRef = useRef(new Set());
  // getAccessToken is re-created on every AuthProvider render; keep it in a ref
  // so the effect below depends only on `authenticated` and never reconnect-storms.
  const getTokenRef = useRef(getAccessToken);
  getTokenRef.current = getAccessToken;

  const subscribe = useCallback((cb) => {
    listenersRef.current.add(cb);
    return () => listenersRef.current.delete(cb);
  }, []);

  useEffect(() => {
    if (!authenticated) return undefined;
    let stopped = false;
    let xhr = null;
    let retry = null;

    function emit(payload) {
      let update;
      try {
        update = JSON.parse(payload);
      } catch {
        return;
      }
      listenersRef.current.forEach((cb) => {
        try {
          cb(update);
        } catch {
          /* a listener error must not break the stream */
        }
      });
    }

    function scheduleReconnect() {
      if (stopped || retry) return;
      retry = setTimeout(() => {
        retry = null;
        connect();
      }, 3000);
    }

    async function connect() {
      if (stopped) return;
      const token = await getTokenRef.current();
      if (stopped) return;
      if (!token) {
        scheduleReconnect();
        return;
      }

      const req = new XMLHttpRequest();
      let offset = 0;
      req.open('GET', `${API_BASE_URL}/realtime?access_token=${encodeURIComponent(token)}`);
      req.setRequestHeader('Accept', 'text/event-stream');
      req.onreadystatechange = () => {
        if (req.readyState < 3) return; // headers not in yet
        const text = req.responseText || '';
        let idx;
        // Process only complete lines; SSE frames are "data: <json>\n\n" and
        // heartbeats are ":" comment lines we ignore.
        while ((idx = text.indexOf('\n', offset)) !== -1) {
          const line = text.substring(offset, idx);
          offset = idx + 1;
          if (line.startsWith('data:')) {
            const data = line.slice(5).trim();
            if (data) emit(data);
          }
        }
        if (req.readyState === 4) scheduleReconnect();
      };
      req.onerror = scheduleReconnect;
      req.send();
      xhr = req;
    }

    connect();
    return () => {
      stopped = true;
      if (retry) clearTimeout(retry);
      if (xhr) {
        try {
          xhr.abort();
        } catch {
          /* ignore */
        }
      }
    };
  }, [authenticated]);

  return <RealtimeContext.Provider value={{ subscribe }}>{children}</RealtimeContext.Provider>;
}
