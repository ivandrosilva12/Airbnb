import { createContext, useContext, useEffect, useState, useCallback, useRef } from 'react';
import { api } from '../api/client';
import { useAuth } from './AuthContext';

const MessagesContext = createContext(null);

// Tracks the signed-in user's total unread message count for the navbar badge,
// polling periodically. Exposes markRead so the inbox can clear a thread.
export function MessagesProvider({ children }) {
  const { authenticated } = useAuth();
  const [unread, setUnread] = useState(0);
  const timer = useRef(null);

  const refresh = useCallback(async () => {
    if (!authenticated) {
      setUnread(0);
      return;
    }
    try {
      const res = await api.messagesUnreadCount();
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

  const markRead = useCallback(async (conversationId) => {
    try {
      await api.markConversationRead(conversationId);
    } finally {
      refresh();
    }
  }, [refresh]);

  const value = { unread, refresh, markRead };
  return <MessagesContext.Provider value={value}>{children}</MessagesContext.Provider>;
}

export function useMessages() {
  const ctx = useContext(MessagesContext);
  if (!ctx) throw new Error('useMessages must be used within MessagesProvider');
  return ctx;
}
