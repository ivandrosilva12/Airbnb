import { useEffect, useRef, useState, useCallback } from 'react';
import { View, Text, FlatList, Pressable, StyleSheet, ActivityIndicator } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';
import { useRealtime } from '../api/RealtimeContext';

// TODO(i18n): translate — wrap hardcoded labels via useT() from ../i18n/I18nContext.
// Key namespace mirrors the web (msg.*).

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
      // Pull my own threads and the team-mailbox in parallel. The mailbox is
      // empty when I'm not a co-host anywhere, so this is cheap (the request
      // still goes out but returns {items:[]}).
      const [own, mailbox] = await Promise.all([
        api.listConversations(),
        api.myCohostMailbox().catch(() => ({ items: [] })),
      ]);
      const ownItems = own.items || [];
      const teamItems = (mailbox.items || []).map((c) => ({ ...c, isTeamMailbox: true }));
      // The server already excludes threads where I'm a literal participant
      // from the mailbox, so plain concat is safe.
      const convos = [...ownItems, ...teamItems].sort((a, b) =>
        new Date(b.lastMessageAt || 0) - new Date(a.lastMessageAt || 0),
      );
      setItems(convos);
      // Enrich with property titles for friendlier rows. Same propertyId
      // shared by own + team threads gets de-duped to a single fetch.
      const uniquePropIds = Array.from(new Set(convos.map((c) => c.propertyId)));
      const entries = await Promise.all(
        uniquePropIds.map(async (pid) => {
          try {
            const p = await api.getProperty(pid);
            return [pid, p.title];
          } catch {
            return [pid, 'Listing'];
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
            onPress={() => navigation.navigate('Conversation', {
              id: item.id,
              title: titles[item.propertyId] || 'Conversation',
              isTeamMailbox: !!item.isTeamMailbox,
            })}
          >
            <View style={{ flex: 1 }}>
              <View style={styles.titleRow}>
                <Text style={styles.title} numberOfLines={1}>{titles[item.propertyId] || 'Conversation'}</Text>
                {item.isTeamMailbox && <Text style={styles.teamBadge}>team</Text>}
              </View>
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
  titleRow: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  title: { fontWeight: '600', fontSize: 16, flexShrink: 1 },
  teamBadge: { backgroundColor: '#e6f0ff', color: '#1d4ed8', paddingHorizontal: 7, paddingVertical: 2, borderRadius: 8, fontSize: 11, fontWeight: '700', overflow: 'hidden' },
  meta: { color: '#717171', marginTop: 2 },
  badge: { backgroundColor: '#ff385c', borderRadius: 999, minWidth: 22, paddingHorizontal: 6, paddingVertical: 2, alignItems: 'center' },
  badgeText: { color: '#fff', fontWeight: '700', fontSize: 12 },
  empty: { textAlign: 'center', color: '#717171', marginTop: 24 },
  error: { color: '#c0392b', marginBottom: 8 },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, paddingHorizontal: 20, paddingVertical: 12 },
  btnText: { color: '#fff', fontWeight: '700' },
});
