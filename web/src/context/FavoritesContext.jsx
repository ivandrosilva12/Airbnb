import { createContext, useContext, useEffect, useState, useCallback } from 'react';
import { api } from '../api/client';
import { useAuth } from './AuthContext';

const FavoritesContext = createContext(null);

// Tracks the set of favorited property IDs for the signed-in user so any card
// can render and toggle its heart without refetching the whole list.
export function FavoritesProvider({ children }) {
  const { authenticated } = useAuth();
  const [ids, setIds] = useState(() => new Set());

  const refresh = useCallback(async () => {
    if (!authenticated) {
      setIds(new Set());
      return;
    }
    try {
      const res = await api.listFavorites();
      setIds(new Set((res.items || []).map((p) => p.id)));
    } catch {
      /* ignore */
    }
  }, [authenticated]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const toggle = useCallback(
    async (propertyId) => {
      const next = new Set(ids);
      if (next.has(propertyId)) {
        next.delete(propertyId);
        setIds(next);
        try { await api.removeFavorite(propertyId); } catch { refresh(); }
      } else {
        next.add(propertyId);
        setIds(next);
        try { await api.addFavorite(propertyId); } catch { refresh(); }
      }
    },
    [ids, refresh],
  );

  const value = {
    isFavorite: (id) => ids.has(id),
    toggle,
    refresh,
    count: ids.size,
  };
  return <FavoritesContext.Provider value={value}>{children}</FavoritesContext.Provider>;
}

export function useFavorites() {
  const ctx = useContext(FavoritesContext);
  if (!ctx) throw new Error('useFavorites must be used within FavoritesProvider');
  return ctx;
}
