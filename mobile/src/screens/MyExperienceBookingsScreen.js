import { useEffect, useState, useCallback } from 'react';
import {
  View,
  Text,
  FlatList,
  Pressable,
  StyleSheet,
  ActivityIndicator,
  Alert,
  RefreshControl,
} from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';

// MyExperienceBookingsScreen lists the signed-in guest's experience
// bookings (S84 mobile parity with backend S80). Mirrors the spacing /
// status badge conventions of TripsScreen but stays much lighter since the
// ExperienceBooking BC currently has no receipts, disputes, or reviews
// attached to it.
export default function MyExperienceBookingsScreen({ navigation }) {
  const api = useApi();
  const { authenticated, login } = useAuth();
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState(null);

  const load = useCallback(async () => {
    setError(null);
    try {
      const res = await api.myExperienceBookings();
      setItems(res.items || []);
    } catch (e) {
      setError(e.message);
    }
  }, [api]);

  useEffect(() => {
    if (!authenticated) {
      setLoading(false);
      return;
    }
    setLoading(true);
    load().finally(() => setLoading(false));
  }, [authenticated, load]);

  async function onRefresh() {
    setRefreshing(true);
    await load();
    setRefreshing(false);
  }

  function confirmCancel(item) {
    // Only offer cancel for bookings that can still meaningfully be
    // cancelled — pending/confirmed and starting in the future. The server
    // is the final authority but a client gate keeps the UX clean.
    const start = new Date(item.startAt);
    const cancellable =
      (item.status === 'pending' || item.status === 'confirmed') && start.getTime() > Date.now();
    if (!cancellable) return;
    Alert.alert(
      'Cancel booking?',
      `${start.toLocaleString()} · ${item.guests} guest(s)`,
      [
        { text: 'Keep', style: 'cancel' },
        {
          text: 'Cancel booking',
          style: 'destructive',
          onPress: async () => {
            try {
              await api.cancelExperienceBooking(item.id);
              await load();
            } catch (e) {
              Alert.alert('Could not cancel', e.message);
            }
          },
        },
      ],
    );
  }

  if (!authenticated) {
    return (
      <View style={styles.center}>
        <Text style={styles.meta}>Sign in to see your experience bookings.</Text>
        <Pressable style={styles.btn} onPress={login}>
          <Text style={styles.btnText}>Sign in</Text>
        </Pressable>
      </View>
    );
  }

  if (loading) {
    return <ActivityIndicator style={{ flex: 1 }} color="#ff385c" />;
  }

  return (
    <View style={styles.container}>
      {error && <Text style={styles.error}>{error}</Text>}
      <FlatList
        data={items}
        keyExtractor={(item) => item.id}
        refreshControl={
          <RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor="#ff385c" />
        }
        ListEmptyComponent={<Text style={styles.empty}>No experience bookings yet.</Text>}
        renderItem={({ item }) => {
          const start = new Date(item.startAt);
          const total = item.pricing?.total?.display || '';
          return (
            <Pressable
              style={styles.card}
              onPress={() => navigation.navigate('ExperienceDetail', { id: item.experienceId })}
              onLongPress={() => confirmCancel(item)}
              accessibilityRole="button"
              accessibilityLabel={`Experience booking for ${start.toLocaleString()}, ${item.guests} guests, status ${item.status}`}
              accessibilityHint="Tap to view the experience, long-press to cancel"
            >
              <View style={styles.cardHead}>
                <Text style={styles.expId}>Experience {item.experienceId.slice(0, 8)}…</Text>
                <StatusBadge status={item.status} />
              </View>
              <Text style={styles.line}>{start.toLocaleString()}</Text>
              <Text style={styles.meta}>{item.guests} guest(s)</Text>
              {total ? <Text style={styles.total}>Total: {total}</Text> : null}
            </Pressable>
          );
        }}
      />
    </View>
  );
}

function StatusBadge({ status }) {
  const style = STATUS_STYLES[status] || STATUS_STYLES.default;
  return (
    <View style={[styles.badge, { backgroundColor: style.bg }]}>
      <Text style={[styles.badgeText, { color: style.fg }]}>{status}</Text>
    </View>
  );
}

const STATUS_STYLES = {
  pending: { bg: '#fff3cd', fg: '#856404' },
  confirmed: { bg: '#d1ecf1', fg: '#0c5460' },
  completed: { bg: '#d4edda', fg: '#155724' },
  cancelled: { bg: '#f8d7da', fg: '#721c24' },
  default: { bg: '#eee', fg: '#444' },
};

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff', padding: 12 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 12, backgroundColor: '#fff', padding: 24 },
  empty: { textAlign: 'center', color: '#717171', marginTop: 24 },
  meta: { color: '#717171', marginVertical: 2 },
  card: { backgroundColor: '#fff', borderWidth: 1, borderColor: '#eee', borderRadius: 12, padding: 14, marginBottom: 12 },
  cardHead: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginBottom: 6 },
  expId: { fontWeight: '700', color: '#222' },
  badge: { borderRadius: 999, paddingHorizontal: 10, paddingVertical: 3 },
  badgeText: { fontSize: 12, fontWeight: '700', textTransform: 'capitalize' },
  line: { fontWeight: '600', color: '#222' },
  total: { marginTop: 4, color: '#222', fontWeight: '600' },
  error: { color: '#c0392b', marginBottom: 8 },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, paddingHorizontal: 20, paddingVertical: 12 },
  btnText: { color: '#fff', fontWeight: '700' },
});
