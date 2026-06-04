import { useEffect, useState, useCallback } from 'react';
import { View, Text, FlatList, Pressable, StyleSheet, ActivityIndicator, TextInput } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';
import { useT } from '../i18n/I18nContext';

// Review category keys come from the backend in this exact order; the labels
// are looked up from t() at render time so they translate alongside the rest
// of the form. The dotted-key namespace matches the web's review.cat.* set.
const REVIEW_CATEGORIES = ['cleanliness', 'accuracy', 'communication', 'location', 'checkIn', 'value'];

// ARRIVAL_LINK_WINDOW_DAYS is the client-side gate on the "Check-in details"
// CTA. The backend already enforces the strict 48-hour reveal window on
// /bookings/:id/arrival, but we don't want a CTA dangling on bookings that
// are months away — surface it only when the stay is imminent (roughly the
// next week). The ArrivalInfoScreen itself handles the server-side 403 by
// showing a "available 48 h before check-in" message, so being a few days
// early here is fine.
const ARRIVAL_LINK_WINDOW_DAYS = 7;

// daysUntilCheckIn parses the booking's check-in date (YYYY-MM-DD from the
// API) and returns the number of whole days until it. Returns a large
// positive number when the input can't be parsed so the comparison below
// fails closed (no CTA on garbage data).
function daysUntilCheckIn(checkIn) {
  if (!checkIn) return Infinity;
  const ci = new Date(`${checkIn}T00:00:00`);
  if (Number.isNaN(ci.getTime())) return Infinity;
  const now = new Date();
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  return Math.round((ci.getTime() - startOfToday.getTime()) / 86400000);
}

export default function TripsScreen({ navigation }) {
  const api = useApi();
  const { authenticated, login } = useAuth();
  const { t } = useT();
  const [items, setItems] = useState([]);
  const [payments, setPayments] = useState({});
  const [expanded, setExpanded] = useState({});
  const [pending, setPending] = useState({});
  const [reviewed, setReviewed] = useState({});
  const [reviewing, setReviewing] = useState(null);
  const [draft, setDraft] = useState({ rating: 5, cats: {} });
  const [modifying, setModifying] = useState(null);
  const [modDraft, setModDraft] = useState({ checkIn: '', checkOut: '', guests: '' });
  const [offers, setOffers] = useState([]);
  const [disputing, setDisputing] = useState(null);
  const [disputeDraft, setDisputeDraft] = useState({ kind: 'refund', reason: '', amount: '' });
  const [disputes, setDisputes] = useState({}); // bookingId -> dispute view
  const [splits, setSplits] = useState({}); // bookingId -> split view
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [bookingsRes, paymentsRes, pendingRes, offersRes, disputesRes, splitsRes] = await Promise.all([
        api.myBookings(),
        api.listPayments(),
        api.pendingReviews(),
        api.myOffers().catch(() => ({ items: [] })),
        api.listMyDisputes().catch(() => []),
        api.mySplits().catch(() => ({ items: [] })),
      ]);
      setItems(bookingsRes.items || []);
      const byBooking = {};
      for (const p of paymentsRes.items || []) byBooking[p.bookingId] = p;
      setPayments(byBooking);
      const pend = {};
      for (const p of pendingRes.items || []) pend[p.bookingId] = true;
      setPending(pend);
      setOffers((offersRes.items || []).filter((o) => o.status === 'pending'));
      const dByB = {};
      for (const d of disputesRes || []) dByB[d.bookingId] = d;
      setDisputes(dByB);
      const sByB = {};
      for (const s of (splitsRes && splitsRes.items) || []) sByB[s.bookingId] = s;
      setSplits(sByB);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [api]);

  async function acceptOffer(id) {
    setError(null);
    try {
      await api.acceptOffer(id);
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  async function declineOffer(id) {
    setError(null);
    try {
      await api.declineOffer(id);
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  function openReview(bookingId) {
    setError(null);
    setDraft({ rating: 5, cats: {} });
    setReviewing(bookingId);
  }

  function openDispute(bookingId) {
    setError(null);
    setDisputeDraft({ kind: 'refund', reason: '', amount: '' });
    setDisputing(bookingId);
  }

  async function submitDispute(booking) {
    if (!disputeDraft.reason.trim()) {
      setError('A reason is required.');
      return;
    }
    const body = {
      kind: disputeDraft.kind,
      reason: disputeDraft.reason.trim(),
    };
    if (disputeDraft.kind !== 'other') {
      body.requestedAmountCents = Math.round(Number(disputeDraft.amount || 0) * 100);
      body.currency = booking.totalPrice?.currency || 'EUR';
    }
    try {
      await api.openDispute(booking.id, body);
      setDisputing(null);
      load();
    } catch (e) {
      setError(e.message);
    }
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
        <Text style={styles.meta}>{t('detail.signInReserve')}</Text>
        <Pressable style={styles.btn} onPress={login}><Text style={styles.btnText}>{t('nav.signIn')}</Text></Pressable>
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
        ListEmptyComponent={<Text style={styles.meta}>{t('trips.none')}</Text>}
        ListHeaderComponent={offers.length > 0 ? (
          <View style={styles.offers}>
            {/* TODO(i18n): offers.* keys not in translations.js yet */}
            <Text style={styles.offersTitle}>Offers from a host</Text>
            {offers.map((o) => (
              <View key={o.id} style={styles.offerCard}>
                <Text style={styles.offerText}>
                  {o.kind === 'special_offer' ? `Special offer: ${(o.priceCents / 100).toFixed(2)} ${o.currency}/night` : 'Pre-approved to book'}
                </Text>
                <Text style={styles.offerMeta}>{o.checkIn} → {o.checkOut} · {o.guests} guest(s){o.message ? ` · “${o.message}”` : ''}</Text>
                <View style={styles.offerActions}>
                  <Pressable style={styles.btnSm} onPress={() => acceptOffer(o.id)}><Text style={styles.btnText}>Accept & book</Text></Pressable>
                  <Pressable onPress={() => declineOffer(o.id)}><Text style={styles.cancel}>Decline</Text></Pressable>
                </View>
              </View>
            ))}
          </View>
        ) : null}
        renderItem={({ item }) => {
          const pay = payments[item.id];
          const open = !!expanded[item.id];
          return (
            <View style={styles.row}>
              <View style={styles.rowTop}>
                <View style={{ flex: 1 }}>
                  <Text style={styles.dates}>{item.checkIn} → {item.checkOut} ({item.nights}n)</Text>
                  <Text style={styles.meta}>{item.guests} {t('common.guests').toLowerCase()} · {item.totalPrice.display} · {t(`status.${item.status}`)}</Text>
                  {pay && (
                    <Text style={styles.payLine}>
                      {t('trips.payment')}: {t(`pay.${pay.status}`)}
                      {pay.refundedCents > 0
                        ? ` · ${t('trips.refunded', { amount: `${(pay.refundedCents / 100).toFixed(2)} ${pay.amount.currency}` })}`
                        : ''}
                    </Text>
                  )}
                </View>
                <View style={styles.rowActions}>
                  {item.status === 'pending' && (
                    <Pressable onPress={() => (modifying === item.id ? setModifying(null) : openModify(item))}>
                      {/* TODO(i18n): no key for "Change" yet */}
                      <Text style={styles.change}>Change</Text>
                    </Pressable>
                  )}
                  {(item.status === 'pending' || item.status === 'confirmed') && (
                    <Pressable onPress={() => cancel(item.id)}><Text style={styles.cancel}>{t('common.cancel')}</Text></Pressable>
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
                <Text style={styles.receiptToggle}>{t('trips.receipt')}</Text>
              </Pressable>
              {open && (
                <View style={styles.receipt}>
                  {/* Receipt lines reuse shared dotted keys where they
                      exist on the web bundle. "Service fee" and "Tax" do
                      not yet have dedicated keys on either platform, so
                      they stay English with a TODO until the web grows
                      one (key parity first — translating in only one
                      platform would create drift). */}
                  <Receipt label={`${item.nights} ×`} value={item.subtotal.display} />
                  {item.discount?.amountCents > 0 && (
                    <Receipt
                      label={t('detail.discount', { pct: item.discount.pct ?? '' })}
                      value={`-${item.discount.display}`}
                    />
                  )}
                  {item.cleaningFee?.amountCents > 0 && <Receipt label={t('detail.cleaningFee')} value={item.cleaningFee.display} />}
                  {/* TODO(i18n): add a shared `fee.service` key on the web + here */}
                  {item.serviceFee?.amountCents > 0 && <Receipt label="Service fee" value={item.serviceFee.display} />}
                  {/* TODO(i18n): add a shared `fee.tax` key */}
                  {item.tax?.amountCents > 0 && <Receipt label="Tax" value={item.tax.display} />}
                  <Receipt label={t('trips.total')} value={item.totalPrice.display} bold />
                </View>
              )}

              {reviewed[item.id] ? (
                <Text style={styles.reviewedText}>{t('trips.reviewed')}</Text>
              ) : item.status === 'completed' && pending[item.id] ? (
                reviewing === item.id ? (
                  <View style={styles.reviewForm}>
                    <Text style={styles.reviewFormLabel}>{t('review.overall')}</Text>
                    <View style={styles.starRow}>
                      {[1, 2, 3, 4, 5].map((n) => (
                        <Pressable key={n} onPress={() => setDraft((d) => ({ ...d, rating: n }))}>
                          <Text style={n <= draft.rating ? styles.starOn : styles.starOff}>★</Text>
                        </Pressable>
                      ))}
                    </View>
                    {REVIEW_CATEGORIES.map((k) => (
                      <View key={k} style={styles.catPickRow}>
                        <Text style={styles.catPickLabel}>{t(`review.cat.${k}`)}</Text>
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
                        <Text style={styles.btnText}>{t('common.submit')}</Text>
                      </Pressable>
                      <Pressable onPress={() => setReviewing(null)}>
                        <Text style={styles.cancel}>{t('common.cancel')}</Text>
                      </Pressable>
                    </View>
                  </View>
                ) : (
                  <Pressable onPress={() => openReview(item.id)}>
                    <Text style={styles.reviewCta}>{t('trips.leaveReview')}</Text>
                  </Pressable>
                )
              ) : null}

              {/* Resolution Center: open a case on completed/cancelled stays
                  that don't already have an active dispute. */}
              {(item.status === 'completed' || item.status === 'cancelled') &&
                !disputes[item.id] &&
                (disputing === item.id ? (
                  <View style={styles.reviewForm}>
                    <Text style={styles.reviewFormLabel}>Open a case</Text>
                    <View style={styles.kindRow}>
                      {['refund', 'damage', 'other'].map((k) => (
                        <Pressable key={k} onPress={() => setDisputeDraft((d) => ({ ...d, kind: k }))}>
                          <Text style={k === disputeDraft.kind ? styles.kindPicked : styles.kindOption}>{k}</Text>
                        </Pressable>
                      ))}
                    </View>
                    {disputeDraft.kind !== 'other' && (
                      <TextInput
                        style={styles.input}
                        placeholder="Amount"
                        keyboardType="decimal-pad"
                        value={disputeDraft.amount}
                        onChangeText={(v) => setDisputeDraft((d) => ({ ...d, amount: v }))}
                      />
                    )}
                    <TextInput
                      style={[styles.input, { minHeight: 60 }]}
                      multiline
                      placeholder="What happened?"
                      value={disputeDraft.reason}
                      onChangeText={(v) => setDisputeDraft((d) => ({ ...d, reason: v }))}
                    />
                    <View style={styles.reviewFormActions}>
                      <Pressable style={styles.btnSm} onPress={() => submitDispute(item)}>
                        <Text style={styles.btnText}>Submit</Text>
                      </Pressable>
                      <Pressable onPress={() => setDisputing(null)}>
                        <Text style={styles.cancel}>Cancel</Text>
                      </Pressable>
                    </View>
                  </View>
                ) : (
                  <Pressable onPress={() => openDispute(item.id)}>
                    <Text style={styles.reviewCta}>Open a case</Text>
                  </Pressable>
                ))}
              {disputes[item.id] && (
                <Pressable onPress={() => navigation.navigate('Dispute', { id: disputes[item.id].id })}>
                  <Text style={styles.disputeBadge}>
                    Case: {disputes[item.id].status}
                    {disputes[item.id].overdue ? ' · overdue' : ''}
                    {' · view'}
                  </Text>
                </Pressable>
              )}

              {/* Check-in details link — only visible on confirmed bookings
                  whose check-in falls within the next ~week. The 48-hour
                  reveal window is enforced server-side; if the user taps
                  earlier than that, ArrivalInfoScreen renders a friendly
                  "available soon" panel instead of an error. */}
              {item.status === 'confirmed' &&
                daysUntilCheckIn(item.checkIn) <= ARRIVAL_LINK_WINDOW_DAYS &&
                daysUntilCheckIn(item.checkIn) >= 0 && (
                  <Pressable onPress={() => navigation.navigate('ArrivalInfo', { bookingId: item.id })}>
                    {/* TODO(i18n): no key for the trips-side CTA yet. */}
                    <Text style={styles.arrivalCta}>Check-in details</Text>
                  </Pressable>
                )}

              {splits[item.id] && (
                <Pressable onPress={() => navigation.navigate('Splits', { splitId: splits[item.id].id })}>
                  <Text style={styles.splitCta}>
                    Split payment: {splits[item.id].status}
                    {splits[item.id].status === 'pending' ? ' · view to authorise your share' : ''}
                  </Text>
                </Pressable>
              )}
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
  offers: { marginBottom: 16 },
  offersTitle: { fontSize: 16, fontWeight: '800', marginBottom: 8 },
  offerCard: { backgroundColor: '#fff5f7', borderRadius: 10, padding: 12, marginBottom: 10, borderWidth: 1, borderColor: '#ffd9e0' },
  offerText: { fontWeight: '700', color: '#222' },
  offerMeta: { color: '#717171', marginTop: 2, marginBottom: 8 },
  offerActions: { flexDirection: 'row', alignItems: 'center', gap: 16 },
  receipt: { marginTop: 8, backgroundColor: '#fafafa', borderRadius: 8, padding: 12 },
  receiptLine: { flexDirection: 'row', justifyContent: 'space-between', paddingVertical: 3 },
  receiptLabel: { color: '#717171' },
  receiptValue: { color: '#222' },
  receiptBold: { fontWeight: '800', color: '#222' },
  error: { color: '#c0392b' },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, paddingHorizontal: 20, paddingVertical: 12 },
  btnText: { color: '#fff', fontWeight: '700' },
  input: { borderWidth: 1, borderColor: '#ddd', borderRadius: 8, paddingHorizontal: 10, paddingVertical: 8, marginTop: 6 },
  kindRow: { flexDirection: 'row', gap: 14, marginTop: 6 },
  kindOption: { color: '#717171', textTransform: 'capitalize' },
  kindPicked: { color: '#ff385c', fontWeight: '700', textTransform: 'capitalize' },
  disputeBadge: { color: '#a05a00', fontWeight: '700', marginTop: 8 },
  splitCta: { color: '#ff385c', fontWeight: '700', marginTop: 8 },
  arrivalCta: { color: '#ff385c', fontWeight: '700', marginTop: 8 },
});
