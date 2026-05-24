import { useEffect, useState, useCallback } from 'react';
import { View, Text, FlatList, Pressable, StyleSheet, ActivityIndicator } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';

export default function TripsScreen() {
  const api = useApi();
  const { authenticated, login } = useAuth();
  const [items, setItems] = useState([]);
  const [payments, setPayments] = useState({});
  const [expanded, setExpanded] = useState({});
  const [pending, setPending] = useState({});
  const [reviewed, setReviewed] = useState({});
  const [reviewing, setReviewing] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [bookingsRes, paymentsRes, pendingRes] = await Promise.all([
        api.myBookings(),
        api.listPayments(),
        api.pendingReviews(),
      ]);
      setItems(bookingsRes.items || []);
      const byBooking = {};
      for (const p of paymentsRes.items || []) byBooking[p.bookingId] = p;
      setPayments(byBooking);
      const pend = {};
      for (const p of pendingRes.items || []) pend[p.bookingId] = true;
      setPending(pend);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [api]);

  async function submitReview(bookingId, rating) {
    try {
      await api.createReview({ bookingId, rating, comment: '' });
      setReviewed((r) => ({ ...r, [bookingId]: true }));
      setReviewing(null);
    } catch (e) {
      setError(e.message);
    }
  }

  useEffect(() => {
    if (authenticated) load();
    else setLoading(false);
  }, [authenticated]);

  async function cancel(id) {
    try {
      await api.cancelBooking(id);
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  function toggle(id) {
    setExpanded((e) => ({ ...e, [id]: !e[id] }));
  }

  if (!authenticated) {
    return (
      <View style={styles.center}>
        <Text style={styles.meta}>Sign in to see your trips.</Text>
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
        ListEmptyComponent={<Text style={styles.meta}>No trips yet.</Text>}
        renderItem={({ item }) => {
          const pay = payments[item.id];
          const open = !!expanded[item.id];
          return (
            <View style={styles.row}>
              <View style={styles.rowTop}>
                <View style={{ flex: 1 }}>
                  <Text style={styles.dates}>{item.checkIn} → {item.checkOut} ({item.nights}n)</Text>
                  <Text style={styles.meta}>{item.guests} guest(s) · {item.totalPrice.display} · {item.status}</Text>
                  {pay && (
                    <Text style={styles.payLine}>
                      Payment: {pay.status}
                      {pay.refundedCents > 0 ? ` · refunded ${(pay.refundedCents / 100).toFixed(2)} ${pay.amount.currency}` : ''}
                    </Text>
                  )}
                </View>
                {(item.status === 'pending' || item.status === 'confirmed') && (
                  <Pressable onPress={() => cancel(item.id)}><Text style={styles.cancel}>Cancel</Text></Pressable>
                )}
              </View>

              <Pressable onPress={() => toggle(item.id)}>
                <Text style={styles.receiptToggle}>{open ? 'Hide receipt' : 'View receipt'}</Text>
              </Pressable>
              {open && (
                <View style={styles.receipt}>
                  <Receipt label={`${item.nights} night(s)`} value={item.subtotal.display} />
                  {item.discount?.amountCents > 0 && <Receipt label="Discount" value={`-${item.discount.display}`} />}
                  {item.cleaningFee?.amountCents > 0 && <Receipt label="Cleaning fee" value={item.cleaningFee.display} />}
                  {item.serviceFee?.amountCents > 0 && <Receipt label="Service fee" value={item.serviceFee.display} />}
                  {item.tax?.amountCents > 0 && <Receipt label="Tax" value={item.tax.display} />}
                  <Receipt label="Total" value={item.totalPrice.display} bold />
                </View>
              )}

              {reviewed[item.id] ? (
                <Text style={styles.reviewedText}>Reviewed ✓</Text>
              ) : item.status === 'completed' && pending[item.id] ? (
                reviewing === item.id ? (
                  <View style={styles.starRow}>
                    <Text style={styles.meta}>Rate your stay:</Text>
                    {[1, 2, 3, 4, 5].map((n) => (
                      <Pressable key={n} onPress={() => submitReview(item.id, n)}>
                        <Text style={styles.star}>★{n}</Text>
                      </Pressable>
                    ))}
                  </View>
                ) : (
                  <Pressable onPress={() => { setError(null); setReviewing(item.id); }}>
                    <Text style={styles.reviewCta}>Leave a review</Text>
                  </Pressable>
                )
              ) : null}
            </View>
          );
        }}
      />
    </View>
  );
}

function Receipt({ label, value, bold }) {
  return (
    <View style={styles.receiptLine}>
      <Text style={[styles.receiptLabel, bold && styles.receiptBold]}>{label}</Text>
      <Text style={[styles.receiptValue, bold && styles.receiptBold]}>{value}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff', padding: 12 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 12 },
  row: { borderBottomWidth: 1, borderColor: '#eee', paddingVertical: 12 },
  rowTop: { flexDirection: 'row', alignItems: 'center' },
  dates: { fontWeight: '600' },
  meta: { color: '#717171', marginVertical: 2 },
  payLine: { color: '#444', fontSize: 13, marginTop: 2 },
  cancel: { color: '#c0392b', fontWeight: '600' },
  receiptToggle: { color: '#ff385c', fontWeight: '600', marginTop: 8 },
  reviewCta: { color: '#ff385c', fontWeight: '700', marginTop: 8 },
  reviewedText: { color: '#1e7e44', fontWeight: '700', marginTop: 8 },
  starRow: { flexDirection: 'row', alignItems: 'center', flexWrap: 'wrap', gap: 10, marginTop: 8 },
  star: { color: '#ff385c', fontWeight: '700', fontSize: 15 },
  receipt: { marginTop: 8, backgroundColor: '#fafafa', borderRadius: 8, padding: 12 },
  receiptLine: { flexDirection: 'row', justifyContent: 'space-between', paddingVertical: 3 },
  receiptLabel: { color: '#717171' },
  receiptValue: { color: '#222' },
  receiptBold: { fontWeight: '800', color: '#222' },
  error: { color: '#c0392b' },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, paddingHorizontal: 20, paddingVertical: 12 },
  btnText: { color: '#fff', fontWeight: '700' },
});
