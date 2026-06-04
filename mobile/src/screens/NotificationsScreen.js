import { useEffect, useState, useCallback } from 'react';
import { View, Text, FlatList, Pressable, StyleSheet, ActivityIndicator } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';

// TODO(i18n): translate — wrap hardcoded labels via useT() from ../i18n/I18nContext.
// Key namespace mirrors the web (notif.*).

export default function NotificationsScreen() {
  const api = useApi();
  const { authenticated, login } = useAuth();
  const [items, setItems] = useState([]);
  const [unread, setUnread] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.listNotifications();
      setItems(res.items || []);
      setUnread(res.unread || 0);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [api]);

  useEffect(() => {
    if (authenticated) load();
    else setLoading(false);
  }, [authenticated]);

  async function markRead(id) {
    setItems((prev) => prev.map((n) => (n.id === id ? { ...n, read: true } : n)));
    setUnread((u) => Math.max(0, u - 1));
    try {
      await api.markNotificationRead(id);
    } catch {
      load();
    }
  }

  async function markUnread(id) {
    setItems((prev) => prev.map((n) => (n.id === id ? { ...n, read: false } : n)));
    setUnread((u) => u + 1);
    try {
      await api.markNotificationUnread(id);
    } catch {
      load();
    }
  }

  async function markAll() {
    setItems((prev) => prev.map((n) => ({ ...n, read: true })));
    setUnread(0);
    try {
      await api.markAllNotificationsRead();
    } catch {
      load();
    }
  }

  if (!authenticated) {
    return (
      <View style={styles.center}>
        <Text style={styles.meta}>Sign in to see your notifications.</Text>
        <Pressable style={styles.btn} onPress={login}><Text style={styles.btnText}>Sign in</Text></Pressable>
      </View>
    );
  }

  if (loading) return <ActivityIndicator style={{ flex: 1 }} color="#ff385c" />;

  return (
    <View style={styles.container}>
      {error && <Text style={styles.error}>{error}</Text>}
      {unread > 0 && (
        <Pressable style={styles.markAll} onPress={markAll}>
          <Text style={styles.markAllText}>Mark all read ({unread})</Text>
        </Pressable>
      )}
      <FlatList
        data={items}
        keyExtractor={(i) => i.id}
        ListEmptyComponent={<Text style={styles.empty}>No notifications.</Text>}
        renderItem={({ item }) => (
          <Pressable style={[styles.row, !item.read && styles.unreadRow]} onPress={() => !item.read && markRead(item.id)}>
            {!item.read && <View style={styles.dot} />}
            <View style={{ flex: 1 }}>
              <Text style={styles.title}>{item.title}</Text>
              <Text style={styles.meta}>{item.body}</Text>
            </View>
            <Pressable onPress={() => (item.read ? markUnread(item.id) : markRead(item.id))} hitSlop={8}>
              <Text style={styles.toggle}>{item.read ? 'Mark unread' : 'Mark read'}</Text>
            </Pressable>
          </Pressable>
        )}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff', padding: 12 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 12 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 10, paddingVertical: 12, borderBottomWidth: 1, borderColor: '#eee' },
  unreadRow: { backgroundColor: '#fff5f7' },
  dot: { width: 8, height: 8, borderRadius: 4, backgroundColor: '#ff385c' },
  title: { fontWeight: '600' },
  meta: { color: '#717171', marginTop: 2 },
  empty: { textAlign: 'center', color: '#717171', marginTop: 24 },
  error: { color: '#c0392b', marginBottom: 8 },
  markAll: { alignSelf: 'flex-end', paddingVertical: 8 },
  markAllText: { color: '#ff385c', fontWeight: '700' },
  toggle: { color: '#717171', fontSize: 12, textDecorationLine: 'underline' },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, paddingHorizontal: 20, paddingVertical: 12 },
  btnText: { color: '#fff', fontWeight: '700' },
});
