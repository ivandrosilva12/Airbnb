import { useEffect, useState, useCallback, useLayoutEffect } from 'react';
import { View, Text, FlatList, TextInput, Pressable, StyleSheet, ActivityIndicator } from 'react-native';
import { useApi } from '../api/useApi';

export default function HostPropertyBookingsScreen({ route, navigation }) {
  const { id, title } = route.params;
  const api = useApi();
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [reviewing, setReviewing] = useState(null);
  const [draft, setDraft] = useState({ rating: 5, comment: '' });
  const [reviewed, setReviewed] = useState({});

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

  function openReview(bookingId) {
    setError(null);
    setDraft({ rating: 5, comment: '' });
    setReviewing(bookingId);
  }

  async function submitGuestReview(bookingId) {
    setError(null);
    try {
      await api.createGuestReview({ bookingId, rating: draft.rating, comment: draft.comment.trim() });
      setReviewed((r) => ({ ...r, [bookingId]: true }));
      setReviewing(null);
    } catch (e) {
      setError(e.message);
    }
  }

  if (loading) return <ActivityIndicator style={{ flex: 1 }} color="#ff385c" />;

  return (
    <View style={styles.container}>
      {error && <Text style={styles.error}>{error}</Text>}
      <Pressable style={styles.calendarLink} onPress={() => navigation.navigate('HostCalendar', { id, title })}>
        <Text style={styles.calendarLinkText}>📅 Manage calendar & blocked dates</Text>
      </Pressable>
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
              {item.status === 'completed' && !reviewed[item.id] && reviewing !== item.id && (
                <Pressable style={styles.btnGhost} onPress={() => openReview(item.id)}>
                  <Text style={styles.reviewGuest}>Review guest</Text>
                </Pressable>
              )}
              {reviewed[item.id] && <Text style={styles.reviewedText}>Guest reviewed ✓</Text>}
            </View>
            {reviewing === item.id && (
              <View style={styles.reviewForm}>
                <View style={styles.starRow}>
                  {[1, 2, 3, 4, 5].map((n) => (
                    <Pressable key={n} onPress={() => setDraft((d) => ({ ...d, rating: n }))}>
                      <Text style={n <= draft.rating ? styles.starOn : styles.starOff}>★</Text>
                    </Pressable>
                  ))}
                </View>
                <TextInput
                  style={styles.input}
                  placeholder="How was your guest? (optional)"
                  value={draft.comment}
                  onChangeText={(v) => setDraft((d) => ({ ...d, comment: v }))}
                  multiline
                />
                <View style={styles.actions}>
                  <Pressable style={styles.btn} onPress={() => submitGuestReview(item.id)}>
                    <Text style={styles.btnText}>Submit review</Text>
                  </Pressable>
                  <Pressable style={styles.btnGhost} onPress={() => setReviewing(null)}>
                    <Text style={styles.btnGhostText}>Cancel</Text>
                  </Pressable>
                </View>
              </View>
            )}
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
  reviewGuest: { color: '#ff385c', fontWeight: '700' },
  reviewedText: { color: '#1e7e44', fontWeight: '700', alignSelf: 'center' },
  reviewForm: { marginTop: 10, backgroundColor: '#fafafa', borderRadius: 8, padding: 12 },
  starRow: { flexDirection: 'row', gap: 4, marginBottom: 8 },
  starOn: { color: '#ff385c', fontSize: 26 },
  starOff: { color: '#ddd', fontSize: 26 },
  input: { borderWidth: 1, borderColor: '#ddd', borderRadius: 8, padding: 10, marginBottom: 8 },
  calendarLink: { borderWidth: 1, borderColor: '#ddd', borderRadius: 8, padding: 12, alignItems: 'center', marginBottom: 12 },
  calendarLinkText: { fontWeight: '700', color: '#222' },
  empty: { textAlign: 'center', color: '#717171', marginTop: 24 },
  error: { color: '#c0392b', marginBottom: 8 },
});
