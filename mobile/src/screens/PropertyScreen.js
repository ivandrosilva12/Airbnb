import { useEffect, useState } from 'react';
import { View, Text, Image, ScrollView, TextInput, Pressable, StyleSheet, ActivityIndicator } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';

export default function PropertyScreen({ route, navigation }) {
  const { id } = route.params;
  const api = useApi();
  const { authenticated, login } = useAuth();
  const [property, setProperty] = useState(null);
  const [checkIn, setCheckIn] = useState('');
  const [checkOut, setCheckOut] = useState('');
  const [guests, setGuests] = useState('1');
  const [message, setMessage] = useState(null);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState(null);
  const [reportOpen, setReportOpen] = useState(false);
  const [reportReason, setReportReason] = useState('inappropriate');
  const [reportNote, setReportNote] = useState('');
  const [reported, setReported] = useState(false);

  useEffect(() => {
    api.getProperty(id).then(setProperty).catch((e) => setError(e.message));
  }, [id]);

  // Reflect whether this listing is already in the wishlist so the heart is
  // correct on revisit (rather than always starting empty).
  useEffect(() => {
    if (!authenticated) return;
    api
      .listFavorites()
      .then((res) => setSaved((res.items || []).some((p) => p.id === id)))
      .catch(() => {});
  }, [id, authenticated]);

  async function save() {
    if (!authenticated) {
      login();
      return;
    }
    try {
      if (saved) {
        await api.removeFavorite(id);
        setSaved(false);
      } else {
        await api.addFavorite(id);
        setSaved(true);
      }
    } catch (e) {
      setError(e.message);
    }
  }

  async function contactHost() {
    if (!authenticated) {
      login();
      return;
    }
    try {
      const conv = await api.startConversation(id);
      navigation.navigate('Conversation', { id: conv.id, title: property?.title || 'Conversation' });
    } catch (e) {
      setError(e.message);
    }
  }

  async function submitReport() {
    if (!authenticated) {
      login();
      return;
    }
    setError(null);
    try {
      await api.reportListing(id, { reason: reportReason, note: reportNote });
      setReported(true);
      setReportOpen(false);
    } catch (e) {
      setError(e.message);
    }
  }

  async function book() {
    setError(null);
    setMessage(null);
    if (!authenticated) {
      login();
      return;
    }
    try {
      const b = await api.createBooking({ propertyId: id, checkIn, checkOut, guests: Number(guests) });
      setMessage(`Booked ${b.nights} night(s) for ${b.totalPrice.display}. Status: ${b.status}.`);
    } catch (e) {
      setError(e.message);
    }
  }

  if (!property) {
    return <ActivityIndicator style={{ flex: 1 }} color="#ff385c" />;
  }

  return (
    <ScrollView style={styles.container}>
      {property.photos?.[0]?.url && <Image source={{ uri: property.photos[0].url }} style={styles.hero} />}
      <View style={styles.body}>
        <Text style={styles.title}>{property.title}</Text>
        <Text style={styles.meta}>{property.address.city}, {property.address.country} · up to {property.maxGuests} guests</Text>
        <Text style={styles.price}>{property.pricePerNight.display} / night</Text>
        {property.instantBook && <Text style={styles.instant}>⚡ Instant Book — confirmed instantly</Text>}
        <Text style={styles.desc}>{property.description || 'No description provided.'}</Text>

        <View style={styles.bookBox}>
          <Text style={styles.bookTitle}>{property.instantBook ? 'Book instantly' : 'Reserve'}</Text>
          <TextInput style={styles.input} placeholder="Check in (YYYY-MM-DD)" value={checkIn} onChangeText={setCheckIn} />
          <TextInput style={styles.input} placeholder="Check out (YYYY-MM-DD)" value={checkOut} onChangeText={setCheckOut} />
          <TextInput style={styles.input} placeholder="Guests" keyboardType="number-pad" value={guests} onChangeText={setGuests} />
          <Pressable style={styles.btn} onPress={book}>
            <Text style={styles.btnText}>{!authenticated ? 'Sign in to reserve' : property.instantBook ? '⚡ Book instantly' : 'Reserve'}</Text>
          </Pressable>
          {message && <Text style={styles.success}>{message}</Text>}
          {error && <Text style={styles.error}>{error}</Text>}
        </View>

        <View style={styles.secondaryActions}>
          <Pressable style={styles.secondaryBtn} onPress={save}>
            <Text style={styles.secondaryText}>{saved ? '♥ Saved' : '♡ Save to wishlist'}</Text>
          </Pressable>
          <Pressable style={styles.secondaryBtn} onPress={contactHost}>
            <Text style={styles.secondaryText}>Contact host</Text>
          </Pressable>
        </View>

        {reported ? (
          <Text style={styles.reportDone}>Thanks — our team will review this listing.</Text>
        ) : !reportOpen ? (
          <Pressable onPress={() => setReportOpen(true)}>
            <Text style={styles.reportLink}>⚑ Report listing</Text>
          </Pressable>
        ) : (
          <View style={styles.reportBox}>
            <Text style={styles.bookTitle}>Report this listing</Text>
            <View style={styles.reasonRow}>
              {REPORT_REASONS.map((r) => (
                <Pressable
                  key={r.value}
                  style={[styles.reasonChip, reportReason === r.value && styles.reasonChipActive]}
                  onPress={() => setReportReason(r.value)}
                >
                  <Text style={reportReason === r.value ? styles.reasonChipTextActive : styles.reasonChipText}>{r.label}</Text>
                </Pressable>
              ))}
            </View>
            <TextInput
              style={[styles.input, { marginTop: 10 }]}
              placeholder="Details (optional)"
              value={reportNote}
              onChangeText={setReportNote}
              multiline
            />
            <View style={styles.secondaryActions}>
              <Pressable style={styles.btn} onPress={submitReport}>
                <Text style={styles.btnText}>Submit report</Text>
              </Pressable>
              <Pressable style={styles.secondaryBtn} onPress={() => setReportOpen(false)}>
                <Text style={styles.secondaryText}>Cancel</Text>
              </Pressable>
            </View>
          </View>
        )}
      </View>
    </ScrollView>
  );
}

const REPORT_REASONS = [
  { value: 'spam', label: 'Spam' },
  { value: 'inappropriate', label: 'Inappropriate' },
  { value: 'scam', label: 'Scam' },
  { value: 'inaccurate', label: 'Inaccurate' },
  { value: 'other', label: 'Other' },
];

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff' },
  hero: { width: '100%', height: 240 },
  body: { padding: 16 },
  title: { fontSize: 22, fontWeight: '800' },
  meta: { color: '#717171', marginVertical: 4 },
  price: { fontSize: 16, fontWeight: '600', marginVertical: 4 },
  instant: { color: '#ff385c', fontWeight: '700', marginTop: 2 },
  desc: { marginVertical: 12, lineHeight: 20 },
  bookBox: { borderWidth: 1, borderColor: '#ddd', borderRadius: 12, padding: 16, marginTop: 8 },
  bookTitle: { fontWeight: '700', fontSize: 16, marginBottom: 10 },
  input: { borderWidth: 1, borderColor: '#ddd', borderRadius: 8, padding: 10, marginBottom: 10 },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, padding: 12, alignItems: 'center' },
  btnText: { color: '#fff', fontWeight: '700' },
  success: { color: '#1a7f47', marginTop: 10 },
  error: { color: '#c0392b', marginTop: 10 },
  secondaryActions: { flexDirection: 'row', gap: 10, marginTop: 12 },
  secondaryBtn: { flex: 1, borderWidth: 1, borderColor: '#ddd', borderRadius: 8, paddingVertical: 12, alignItems: 'center' },
  secondaryText: { fontWeight: '600', color: '#222' },
  reportLink: { color: '#c0392b', textDecorationLine: 'underline', marginTop: 16 },
  reportDone: { color: '#1a7f47', marginTop: 16 },
  reportBox: { borderWidth: 1, borderColor: '#eee', borderRadius: 12, padding: 16, marginTop: 16 },
  reasonRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  reasonChip: { borderWidth: 1, borderColor: '#ddd', borderRadius: 999, paddingHorizontal: 12, paddingVertical: 6 },
  reasonChipActive: { backgroundColor: '#ff385c', borderColor: '#ff385c' },
  reasonChipText: { color: '#222', fontSize: 13 },
  reasonChipTextActive: { color: '#fff', fontWeight: '700', fontSize: 13 },
});
