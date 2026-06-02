import { useCallback, useEffect, useState } from 'react';
import { View, Text, FlatList, Pressable, StyleSheet, ActivityIndicator } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';

// MyDisputesScreen is a unified list of every Resolution Center case the
// caller participates in — both as opener (filed it) and as host (someone
// opened it on one of their listings). /me/disputes already returns both
// sets server-side, so this screen is purely a flat-list + tap-to-detail.
//
// Hosts don't get here from TripsScreen (that's the guest "my trips" view),
// so this is their primary surface for case discovery on mobile.
export default function MyDisputesScreen({ navigation }) {
  const api = useApi();
  const { authenticated, login } = useAuth();
  const [items, setItems] = useState([]);
  const [me, setMe] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [list, u] = await Promise.all([
        api.listMyDisputes().catch(() => []),
        api.me().catch(() => null),
      ]);
      // Sort: overdue first, then newest openedAt.
      const sorted = (list || []).slice().sort((a, b) => {
        if (a.overdue !== b.overdue) return a.overdue ? -1 : 1;
        return new Date(b.openedAt) - new Date(a.openedAt);
      });
      setItems(sorted);
      setMe(u);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [api]);

  useEffect(() => {
    if (authenticated) load();
    else setLoading(false);
  }, [authenticated, load]);

  if (!authenticated) {
    return (
      <View style={styles.center}>
        <Text style={styles.meta}>Sign in to see your cases.</Text>
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
        keyExtractor={(d) => d.id}
        ListEmptyComponent={<Text style={styles.meta}>No cases yet. Cases you open or that are filed against your listings appear here.</Text>}
        renderItem={({ item }) => {
          const isOpener = me && item.openerId === me.id;
          const role = isOpener ? 'You opened' : 'Filed against your listing';
          const amount = item.requestedAmountCents > 0
            ? `${(item.requestedAmountCents / 100).toFixed(2)} ${item.currency}`
            : null;
          return (
            <Pressable style={styles.row} onPress={() => navigation.navigate('Dispute', { id: item.id })}>
              <View style={styles.rowTop}>
                <Text style={styles.kind}>{item.kind}</Text>
                <View style={styles.badges}>
                  <Text style={styles.statusBadge}>{item.status}</Text>
                  {item.overdue && <Text style={styles.overdueBadge}>overdue</Text>}
                </View>
              </View>
              <Text style={styles.role}>{role}</Text>
              {amount && <Text style={styles.amount}>{amount}</Text>}
              <Text style={styles.meta} numberOfLines={2}>{item.reason}</Text>
              <Text style={styles.opened}>Opened {new Date(item.openedAt).toLocaleDateString()}</Text>
            </Pressable>
          );
        }}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff', padding: 12 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 12, backgroundColor: '#fff' },
  row: { borderBottomWidth: 1, borderColor: '#eee', paddingVertical: 12 },
  rowTop: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
  kind: { fontSize: 16, fontWeight: '800', color: '#222', textTransform: 'capitalize' },
  badges: { flexDirection: 'row', gap: 6 },
  statusBadge: { backgroundColor: '#fff4d6', color: '#8a5d00', paddingHorizontal: 8, paddingVertical: 2, borderRadius: 10, fontWeight: '700', overflow: 'hidden', textTransform: 'capitalize', fontSize: 12 },
  overdueBadge: { backgroundColor: '#fde2e2', color: '#991b1b', paddingHorizontal: 8, paddingVertical: 2, borderRadius: 10, fontWeight: '700', overflow: 'hidden', fontSize: 12 },
  role: { color: '#1d4ed8', marginTop: 4, fontWeight: '600' },
  amount: { color: '#222', marginTop: 2 },
  meta: { color: '#717171', marginTop: 4 },
  opened: { color: '#999', fontSize: 12, marginTop: 6 },
  error: { color: '#c0392b', marginBottom: 8 },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, paddingHorizontal: 20, paddingVertical: 12 },
  btnText: { color: '#fff', fontWeight: '700' },
});
