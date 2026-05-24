import { useEffect, useState, useCallback, useLayoutEffect } from 'react';
import { View, Text, FlatList, Pressable, StyleSheet, ActivityIndicator } from 'react-native';
import { useApi } from '../api/useApi';

export default function HostPropertyBookingsScreen({ route, navigation }) {
  const { id, title } = route.params;
  const api = useApi();
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useLayoutEffect(() => {
    if (title) navigation.setOptions({ title });
  }, [navigation, title]);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.propertyBookings(id);
      setItems(res.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [api, id]);

  useEffect(() => {
    load();
  }, [id]);

  async function act(fn, bookingId) {
    setError(null);
    try {
      await fn(bookingId);
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  if (loading) return <ActivityIndicator style={{ flex: 1 }} color="#ff385c" />;

  return (
    <View style={styles.container}>
      {error && <Text style={styles.error}>{error}</Text>}
      <FlatList
        data={items}
        keyExtractor={(i) => i.id}
        ListEmptyComponent={<Text style={styles.empty}>No bookings yet.</Text>}
        renderItem={({ item }) => (
          <View style={styles.row}>
            <Text style={styles.dates}>{item.checkIn} → {item.checkOut} ({item.nights}n)</Text>
            <Text style={styles.meta}>{item.guests} guest(s) · {item.totalPrice.display} · {item.status}</Text>
            <View style={styles.actions}>
              {item.status === 'pending' && (
                <Pressable style={styles.btn} onPress={() => act(api.confirmBooking, item.id)}>
                  <Text style={styles.btnText}>Confirm</Text>
                </Pressable>
              )}
              {item.status === 'confirmed' && (
                <Pressable style={styles.btn} onPress={() => act(api.completeBooking, item.id)}>
                  <Text style={styles.btnText}>Mark completed</Text>
                </Pressable>
              )}
              {(item.status === 'pending' || item.status === 'confirmed') && (
                <Pressable style={styles.btnGhost} onPress={() => act(api.cancelBooking, item.id)}>
                  <Text style={styles.btnGhostText}>Cancel</Text>
                </Pressable>
              )}
            </View>
          </View>
        )}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff', padding: 12 },
  row: { borderBottomWidth: 1, borderColor: '#eee', paddingVertical: 12 },
  dates: { fontWeight: '600' },
  meta: { color: '#717171', marginVertical: 4 },
  actions: { flexDirection: 'row', gap: 8, marginTop: 4 },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, paddingHorizontal: 14, paddingVertical: 8 },
  btnText: { color: '#fff', fontWeight: '700' },
  btnGhost: { borderWidth: 1, borderColor: '#ddd', borderRadius: 8, paddingHorizontal: 14, paddingVertical: 8 },
  btnGhostText: { color: '#c0392b', fontWeight: '700' },
  empty: { textAlign: 'center', color: '#717171', marginTop: 24 },
  error: { color: '#c0392b', marginBottom: 8 },
});
