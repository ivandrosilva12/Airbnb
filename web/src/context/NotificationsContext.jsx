import { createContext, useContext, useEffect, useState, useCallback, useRef } from 'react';
import { api } from '../api/client';
import { useAuth } from './AuthContext';

const NotificationsContext = createContext(null);

// Loads notifications for the signed-in user and polls the unread count so the
// navbar badge stays roughly fresh without a websocket.
export function NotificationsProvider({ children }) {
  const { authenticated } = useAuth();
  const [items, setItems] = useState([]);
  const [unread, setUnread] = useState(0);
  const timer = useRef(null);

  const refresh = useCallback(async () => {
    if (!authenticated) {
      setItems([]);
      setUnread(0);
      return;
    }
    try {
      const res = await api.listNotifications();
      setItems(res.items || []);
      setUnread(res.unread || 0);
    } catch {
      /* ignore transient errors */
    }
  }, [authenticated]);

  useEffect(() => {
    refresh();
    if (authenticated) {
      timer.current = setInterval(refresh, 30000);
      return () => clearInterval(timer.current);
    }
  }, [authenticated, refresh]);

  const markRead = useCallback(async (id) => {
    setItems((prev) => prev.map((n) => (n.id === id ? { ...n, read: true } : n)));
    setUnread((u) => Math.max(0, u - 1));
    try { await api.markNotificationRead(id); } catch { refresh(); }
  }, [refresh]);

  const markAllRead = useCallback(async () => {
    setItems((prev) => prev.map((n) => ({ ...n, read: true })));
    setUnread(0);
    try { await api.markAllNotificationsRead(); } catch { refresh(); }
  }, [refresh]);

  const value = { items, unread, refresh, markRead, markAllRead };
  return <NotificationsContext.Provider value={value}>{children}</NotificationsContext.Provider>;
}

export function useNotifications() {
  const ctx = useContext(NotificationsContext);
  if (!ctx) throw new Error('useNotifications must be used within NotificationsProvider');
  return ctx;
}
