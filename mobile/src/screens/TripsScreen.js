import { useEffect, useState, useCallback } from 'react';
import { View, Text, FlatList, Pressable, StyleSheet, ActivityIndicator, TextInput } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';

const REVIEW_CATEGORIES = ['cleanliness', 'accuracy', 'communication', 'location', 'checkIn', 'value'];
const CATEGORY_LABELS = {
  cleanliness: 'Cleanliness', accuracy: 'Accuracy', communication: 'Communication',
  location: 'Location', checkIn: 'Check-in', value: 'Value',
};

export default function TripsScreen() {
  const api = useApi();
  const { authenticated, login } = useAuth();
  const [items, setItems] = useState([]);
  const [payments, setPayments] = useState({});
  const [expanded, setExpanded] = useState({});
  const [pending, setPending] = useState({});
  const [reviewed, setReviewed] = useState({});
  const [reviewing, setReviewing] = useState(null);
  const [draft, setDraft] = useState({ rating: 5, cats: {} });
  const [modifying, setModifying] = useState(null);
  const [modDraft, setModDraft] = useState({ checkIn: '', checkOut: '', guests: '' });
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

  function openReview(bookingId) {
    setError(null);
    setDraft({ rating: 5, cats: {} });
    setReviewing(bookingId);
  }

  async function submitReview(bookingId) {
    try {
      const categories = {};
      let any = false;
      for (const k of REVIEW_CATEGORIES) {
        const v = Number(draft.cats[k] || 0);
        categories[k] = v;
        if (v > 0) any = true;
      }
      await api.createReview({ bookingId, rating: draft.rating, comment: '', categories: any ? categories : undefined });
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

  function openModify(item) {
    setError(null);
    setModDraft({ checkIn: item.checkIn, checkOut: item.checkOut, guests: String(item.guests) });
    setModifying(item.id);
  }

  async function submitModify(id) {
    try {
      await api.modifyBooking(id, {
        checkIn: modDraft.checkIn,
        checkOut: modDraft.checkOut,
        guests: Number(modDraft.guests) || 1,
      });
      setModifying(null);
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
                <View style={styles.rowActions}>
                  {item.status === 'pending' && (
                    <Pressable onPress={() => (modifying === item.id ? setModifying(null) : openModify(item))}>
                      <Text style={styles.change}>Change</Text>
                    </Pressable>
                  )}
                  {(item.status === 'pending' || item.status === 'confirmed') && (
                    <Pressable onPress={() => cancel(item.id)}><Text style={styles.cancel}>Cancel</Text></Pressable>
                  )}
                </View>
              </View>

              {modifying === item.id && (
                <View style={styles.modForm}>
                  <Text style={styles.reviewFormLabel}>Change dates & guests</Text>
                  <View style={styles.modFields}>
                    <View style={styles.modField}>
                      <Text style={styles.modLabel}>Check-in</Text>
                      <TextInput style={styles.modInput} value={modDraft.checkIn} placeholder="YYYY-MM-DD" placeholderTextColor="#999"
                        autoCapitalize="none" onChangeText={(v) => setModDraft((d) => ({ ...d, checkIn: v }))} />
                    </View>
                    <View style={styles.modField}>
                      <Text style={styles.modLabel}>Check-out</Text>
                      <TextInput style={styles.modInput} value={modDraft.checkOut} placeholder="YYYY-MM-DD" placeholderTextColor="#999"
                        autoCapitalize="none" onChangeText={(v) => setModDraft((d) => ({ ...d, checkOut: v }))} />
                    </View>
                    <View style={styles.modField}>
                      <Text style={styles.modLabel}>Guests</Text>
                      <TextInput style={styles.modInput} value={modDraft.guests} keyboardType="number-pad"
                        onChangeText={(v) => setModDraft((d) => ({ ...d, guests: v }))} />
                    </View>
                  </View>
                  <Text style={styles.modNote}>Re-prices the stay at current rates (promo codes are not carried over).</Text>
                  <View style={styles.reviewFormActions}>
                    <Pressable style={styles.btnSm} onPress={() => submitModify(item.id)}>
                      <Text style={styles.btnText}>Save changes</Text>
                    </Pressable>
                    <Pressable onPress={() => setModifying(null)}>
                      <Text style={styles.cancel}>Cancel</Text>
                    </Pressable>
                  </View>
                </View>
              )}

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
                  <View style={styles.reviewForm}>
                    <Text style={styles.reviewFormLabel}>Overall</Text>
                    <View style={styles.starRow}>
                      {[1, 2, 3, 4, 5].map((n) => (
                        <Pressable key={n} onPress={() => setDraft((d) => ({ ...d, rating: n }))}>
                          <Text style={n <= draft.rating ? styles.starOn : styles.starOff}>★</Text>
                        </Pressable>
                      ))}
                    </View>
                    {REVIEW_CATEGORIES.map((k) => (
                      <View key={k} style={styles.catPickRow}>
                        <Text style={styles.catPickLabel}>{CATEGORY_LABELS[k]}</Text>
                        <View style={styles.starRow}>
                          {[1, 2, 3, 4, 5].map((n) => (
                            <Pressable key={n} onPress={() => setDraft((d) => ({ ...d, cats: { ...d.cats, [k]: n } }))}>
                              <Text style={n <= (draft.cats[k] || 0) ? styles.starOnSm : styles.starOffSm}>★</Text>
                            </Pressable>
                          ))}
                        </View>
                      </View>
                    ))}
                    <View style={styles.reviewFormActions}>
                      <Pressable style={styles.btnSm} onPress={() => submitReview(item.id)}>
                        <Text style={styles.btnText}>Submit review</Text>
                      </Pressable>
                      <Pressable onPress={() => setReviewing(null)}>
                        <Text style={styles.cancel}>Cancel</Text>
                      </Pressable>
                    </View>
                  </View>
                ) : (
                  <Pressable onPress={() => openReview(item.id)}>
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
  change: { color: '#ff385c', fontWeight: '600' },
  rowActions: { flexDirection: 'row', alignItems: 'center', gap: 14 },
  modForm: { marginTop: 10, backgroundColor: '#fafafa', borderRadius: 8, padding: 12 },
  modFields: { flexDirection: 'row', gap: 8, marginTop: 6 },
  modField: { flex: 1 },
  modLabel: { fontSize: 12, color: '#717171', marginBottom: 3 },
  modInput: { borderWidth: 1, borderColor: '#ddd', borderRadius: 8, paddingHorizontal: 8, paddingVertical: 8 },
  modNote: { color: '#717171', fontSize: 12, marginTop: 8 },
  receiptToggle: { color: '#ff385c', fontWeight: '600', marginTop: 8 },
  reviewCta: { color: '#ff385c', fontWeight: '700', marginTop: 8 },
  reviewedText: { color: '#1e7e44', fontWeight: '700', marginTop: 8 },
  starRow: { flexDirection: 'row', alignItems: 'center', gap: 4 },
  star: { color: '#ff385c', fontWeight: '700', fontSize: 15 },
  reviewForm: { marginTop: 10, backgroundColor: '#fafafa', borderRadius: 8, padding: 12 },
  reviewFormLabel: { fontWeight: '700', color: '#222', marginBottom: 4 },
  starOn: { color: '#ff385c', fontSize: 26 },
  starOff: { color: '#ddd', fontSize: 26 },
  starOnSm: { color: '#ff385c', fontSize: 18 },
  starOffSm: { color: '#ddd', fontSize: 18 },
  catPickRow: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginTop: 6 },
  catPickLabel: { color: '#717171' },
  reviewFormActions: { flexDirection: 'row', alignItems: 'center', gap: 16, marginTop: 12 },
  btnSm: { backgroundColor: '#ff385c', borderRadius: 8, paddingHorizontal: 16, paddingVertical: 10 },
  receipt: { marginTop: 8, backgroundColor: '#fafafa', borderRadius: 8, padding: 12 },
  receiptLine: { flexDirection: 'row', justifyContent: 'space-between', paddingVertical: 3 },
  receiptLabel: { color: '#717171' },
  receiptValue: { color: '#222' },
  receiptBold: { fontWeight: '800', color: '#222' },
  error: { color: '#c0392b' },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, paddingHorizontal: 20, paddingVertical: 12 },
  btnText: { color: '#fff', fontWeight: '700' },
});
