import { useEffect, useRef, useState, useCallback } from 'react';
import { View, Text, FlatList, Pressable, StyleSheet, ActivityIndicator } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';
import { useRealtime } from '../api/RealtimeContext';

export default function MessagesScreen({ navigation }) {
  const api = useApi();
  const { authenticated, login } = useAuth();
  const { subscribe } = useRealtime();
  const [items, setItems] = useState([]);
  const [titles, setTitles] = useState({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const load = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    try {
      const res = await api.listConversations();
      const convos = res.items || [];
      setItems(convos);
      // Enrich with property titles for friendlier rows.
      const entries = await Promise.all(
        convos.map(async (c) => {
          try {
            const p = await api.getProperty(c.propertyId);
            return [c.propertyId, p.title];
          } catch {
            return [c.propertyId, 'Listing'];
          }
        }),
      );
      setTitles(Object.fromEntries(entries));
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [api]);

  useEffect(() => {
    const unsub = navigation.addListener('focus', () => {
      if (authenticated) load();
      else setLoading(false);
    });
    return unsub;
  }, [navigation, authenticated]);

  // Refresh the conversation list in the background when a message arrives.
  const loadRef = useRef(load);
  loadRef.current = load;
  useEffect(
    () =>
      subscribe((update) => {
        if (update.type === 'message') loadRef.current(true);
      }),
    [subscribe],
  );

  if (!authenticated) {
    return (
      <View style={styles.center}>
        <Text style={styles.meta}>Sign in to see your messages.</Text>
        <Pressable style={styles.btn} onPress={login}><Text style={styles.btnText}>Sign in</Text></Pressable>
      </View>
    );
  }

  if (loading) return <ActivityIndicator style={{ flex: 1 }} color="#ff385c" />;

  return (
    <View style={styles.container}>
      {error && <Text style={styles.error}>{error}</Text>}
      <FlatList
        data={items}
        keyExtractor={(i) => i.id}
        ListEmptyComponent={<Text style={styles.empty}>No conversations yet.</Text>}
        renderItem={({ item }) => (
          <Pressable
            style={styles.row}
            onPress={() => navigation.navigate('Conversation', { id: item.id, title: titles[item.propertyId] || 'Conversation' })}
          >
            <View style={{ flex: 1 }}>
              <Text style={styles.title}>{titles[item.propertyId] || 'Conversation'}</Text>
              <Text style={styles.meta}>Last activity {new Date(item.lastMessageAt).toLocaleDateString()}</Text>
            </View>
            {item.unreadCount > 0 && (
              <View style={styles.badge}><Text style={styles.badgeText}>{item.unreadCount}</Text></View>
            )}
          </Pressable>
        )}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff', padding: 12 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 12 },
  row: { flexDirection: 'row', alignItems: 'center', paddingVertical: 14, borderBottomWidth: 1, borderColor: '#eee' },
  title: { fontWeight: '600', fontSize: 16 },
  meta: { color: '#717171', marginTop: 2 },
  badge: { backgroundColor: '#ff385c', borderRadius: 999, minWidth: 22, paddingHorizontal: 6, paddingVertical: 2, alignItems: 'center' },
  badgeText: { color: '#fff', fontWeight: '700', fontSize: 12 },
  empty: { textAlign: 'center', color: '#717171', marginTop: 24 },
  error: { color: '#c0392b', marginBottom: 8 },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, paddingHorizontal: 20, paddingVertical: 12 },
  btnText: { color: '#fff', fontWeight: '700' },
});
