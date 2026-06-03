import { useEffect, useState } from 'react';
import {
  View,
  Text,
  Image,
  Pressable,
  ScrollView,
  StyleSheet,
  TextInput,
  ActivityIndicator,
  Alert,
} from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';

// Mirrors web/src/pages/ExperienceDetail.jsx. Photos render as a horizontal
// pager (active index controls the hero swap), description and metadata
// stack below. S84 wires the live ExperienceBooking BC: signed-in guests can
// pick a start time + guest count and submit; the disabled stub stays for
// non-published statuses so we don't allow bookings against drafts.
export default function ExperienceDetailScreen({ route, navigation }) {
  const api = useApi();
  const { authenticated, login } = useAuth();
  const { id } = route.params || {};
  const [experience, setExperience] = useState(null);
  const [activePhoto, setActivePhoto] = useState(0);
  const [error, setError] = useState(null);
  // Booking form state. startAt is a user-typed `YYYY-MM-DD HH:mm` string —
  // we keep it as a plain TextInput rather than pulling in a DateTimePicker
  // dependency. guests is a number (clamped on stepper).
  const [startAt, setStartAt] = useState('');
  const [guests, setGuests] = useState(1);
  const [submitting, setSubmitting] = useState(false);
  const [bookingError, setBookingError] = useState(null);
  const [createdBooking, setCreatedBooking] = useState(null);

  useEffect(() => {
    setError(null);
    setExperience(null);
    setActivePhoto(0);
    setStartAt('');
    setGuests(1);
    setBookingError(null);
    setCreatedBooking(null);
    api
      .getExperience(id)
      .then(setExperience)
      .catch((e) => setError(e.message));
  }, [id]);

  if (error && !experience) {
    return (
      <View style={styles.center}>
        <Text style={styles.error}>{error}</Text>
      </View>
    );
  }
  if (!experience) {
    return (
      <View style={styles.center}>
        <ActivityIndicator color="#ff385c" />
      </View>
    );
  }

  const photos = experience.photos || [];
  const hero = photos[activePhoto];
  const isPublished = experience.status === 'published';
  const maxGuests = Math.max(1, experience.maxGuests || 1);
  const pricePerGuest = experience.pricePerGuest || { amountCents: 0, currency: '', display: '' };
  const livePreviewAmount = ((pricePerGuest.amountCents || 0) * guests / 100).toFixed(2);

  // parseStartAt accepts `YYYY-MM-DD HH:mm` (with optional seconds) and
  // returns an ISO-8601 string in the device's local zone, or null when
  // unparseable. Surfaces a clear inline error before we even attempt the
  // POST so the user doesn't get a generic 422 from the server.
  function parseStartAt(raw) {
    const trimmed = (raw || '').trim();
    if (!trimmed) return null;
    // Normalise to T-separated for Date parsing.
    const normalised = trimmed.replace(' ', 'T');
    const d = new Date(normalised);
    if (Number.isNaN(d.getTime())) return null;
    return d.toISOString();
  }

  async function book() {
    setBookingError(null);
    setCreatedBooking(null);
    if (!authenticated) {
      login();
      return;
    }
    const iso = parseStartAt(startAt);
    if (!iso) {
      setBookingError('Enter a valid start time as YYYY-MM-DD HH:mm.');
      return;
    }
    setSubmitting(true);
    try {
      const booking = await api.createExperienceBooking(experience.id, {
        startAt: iso,
        guests,
      });
      setCreatedBooking(booking);
    } catch (e) {
      if (e.status === 409) {
        Alert.alert('Slot unavailable', 'This time slot is taken. Try another start time.');
      } else if (e.status === 422) {
        setBookingError(e.message || 'Some details look off — please review.');
      } else {
        setBookingError(e.message || 'Could not book this experience.');
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: 32 }}>
      {hero ? (
        <Image source={{ uri: hero.url }} style={styles.hero} />
      ) : (
        <View style={[styles.hero, styles.placeholder]}>
          <Text style={{ color: '#888' }}>No photo</Text>
        </View>
      )}

      {photos.length > 1 && (
        <ScrollView
          horizontal
          showsHorizontalScrollIndicator={false}
          style={styles.thumbsRow}
          contentContainerStyle={{ gap: 8, paddingHorizontal: 12, paddingVertical: 8 }}
        >
          {photos.map((p, i) => (
            <Pressable
              key={p.id}
              onPress={() => setActivePhoto(i)}
              style={[styles.thumbBtn, i === activePhoto && styles.thumbBtnOn]}
              accessibilityLabel={`Photo ${i + 1} of ${photos.length}`}
              accessibilityState={{ selected: i === activePhoto }}
            >
              <Image source={{ uri: p.url }} style={styles.thumb} />
            </Pressable>
          ))}
        </ScrollView>
      )}

      <View style={styles.body}>
        <Text style={styles.title}>{experience.title}</Text>
        <View style={styles.metaRow}>
          <Text style={styles.metaBadge}>{experience.category}</Text>
          <Text style={styles.meta}>
            {experience.address.city}, {experience.address.country}
          </Text>
          {!isPublished && <Text style={styles.statusBadge}>{experience.status}</Text>}
        </View>
        <Text style={styles.muted}>Hosted by {experience.hostId.slice(0, 8)}…</Text>

        {experience.description ? (
          <Text style={styles.desc}>{experience.description}</Text>
        ) : (
          <Text style={styles.muted}>No description yet.</Text>
        )}

        <View style={styles.factsBox}>
          <Fact label="Duration" value={`${experience.durationMinutes} min`} />
          <Fact label="Max guests" value={String(experience.maxGuests)} />
          <Fact label="Language" value={experience.language} />
        </View>

        <View style={styles.bookingBox}>
          <Text style={styles.bookingPrice}>
            <Text style={styles.bookingPriceStrong}>{pricePerGuest.display}</Text>
            {' '}/ guest
          </Text>

          {!isPublished ? (
            // Drafts/archived listings keep the disabled stub — the BC
            // rejects them anyway, but the visual cue prevents wasted taps.
            <Pressable style={[styles.bookBtn, styles.bookBtnDisabled]} disabled>
              <Text style={styles.bookBtnText}>Booking opens soon</Text>
            </Pressable>
          ) : createdBooking ? (
            <View style={styles.successCard}>
              <Text style={styles.successTitle}>Booking {createdBooking.status}</Text>
              <Text style={styles.successLine}>
                {new Date(createdBooking.startAt).toLocaleString()} · {createdBooking.guests} guest(s)
              </Text>
              <Text style={styles.successLine}>
                Total: {createdBooking.pricing?.total?.display || ''}
              </Text>
              <Pressable
                style={styles.linkBtn}
                onPress={() => navigation.navigate('MyExperienceBookings')}
                accessibilityRole="button"
              >
                <Text style={styles.linkBtnText}>View my experiences →</Text>
              </Pressable>
            </View>
          ) : !authenticated ? (
            <Pressable style={styles.bookBtn} onPress={login} accessibilityRole="button">
              <Text style={styles.bookBtnText}>Sign in to book</Text>
            </Pressable>
          ) : (
            <View>
              <Text style={styles.label}>Start time</Text>
              <TextInput
                style={styles.input}
                placeholder="YYYY-MM-DD HH:mm"
                placeholderTextColor="#999"
                autoCapitalize="none"
                autoCorrect={false}
                value={startAt}
                onChangeText={setStartAt}
                accessibilityLabel="Booking start time"
                accessibilityHint="Format: year-month-day space hour:minute, e.g. 2026-07-04 14:30"
              />

              <Text style={styles.label}>Guests</Text>
              <View style={styles.stepperRow}>
                <Pressable
                  style={[styles.stepperBtn, guests <= 1 && styles.stepperBtnDisabled]}
                  onPress={() => setGuests((g) => Math.max(1, g - 1))}
                  disabled={guests <= 1}
                  accessibilityRole="button"
                  accessibilityLabel="Decrease guests"
                >
                  <Text style={styles.stepperText}>−</Text>
                </Pressable>
                <Text style={styles.stepperValue}>{guests}</Text>
                <Pressable
                  style={[styles.stepperBtn, guests >= maxGuests && styles.stepperBtnDisabled]}
                  onPress={() => setGuests((g) => Math.min(maxGuests, g + 1))}
                  disabled={guests >= maxGuests}
                  accessibilityRole="button"
                  accessibilityLabel="Increase guests"
                >
                  <Text style={styles.stepperText}>+</Text>
                </Pressable>
                <Text style={styles.stepperMax}>max {maxGuests}</Text>
              </View>

              <View style={styles.previewBox}>
                <Text style={styles.previewLine}>
                  {pricePerGuest.display} × {guests} ={' '}
                  <Text style={styles.previewStrong}>
                    {livePreviewAmount} {pricePerGuest.currency}
                  </Text>
                </Text>
                <Text style={styles.previewHint}>+ service fee</Text>
              </View>

              <Pressable
                style={[styles.bookBtn, submitting && styles.bookBtnDisabled]}
                onPress={book}
                disabled={submitting}
                accessibilityRole="button"
                accessibilityLabel="Book this experience"
              >
                <Text style={styles.bookBtnText}>{submitting ? 'Booking…' : 'Book'}</Text>
              </Pressable>

              {bookingError && (
                <Text style={styles.error} accessibilityRole="alert">{bookingError}</Text>
              )}
            </View>
          )}
        </View>
      </View>
    </ScrollView>
  );
}

function Fact({ label, value }) {
  return (
    <View style={styles.factRow}>
      <Text style={styles.factLabel}>{label}</Text>
      <Text style={styles.factValue}>{value}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fafafa' },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: 24 },
  hero: { width: '100%', height: 260 },
  placeholder: { alignItems: 'center', justifyContent: 'center', backgroundColor: '#f0f0f0' },
  thumbsRow: { backgroundColor: '#fff' },
  thumbBtn: { borderRadius: 8, overflow: 'hidden', borderWidth: 2, borderColor: 'transparent' },
  thumbBtnOn: { borderColor: '#ff385c' },
  thumb: { width: 64, height: 64 },
  body: { padding: 16 },
  title: { fontSize: 22, fontWeight: '800', marginBottom: 6 },
  metaRow: { flexDirection: 'row', alignItems: 'center', flexWrap: 'wrap', gap: 8, marginBottom: 4 },
  metaBadge: { backgroundColor: '#222', color: '#fff', borderRadius: 999, paddingHorizontal: 10, paddingVertical: 2, fontSize: 12, textTransform: 'capitalize', overflow: 'hidden' },
  statusBadge: { backgroundColor: '#fff3cd', color: '#856404', borderRadius: 4, paddingHorizontal: 8, paddingVertical: 2, fontSize: 12 },
  meta: { color: '#717171' },
  muted: { color: '#717171', marginBottom: 12 },
  desc: { color: '#222', lineHeight: 22, marginBottom: 16 },
  factsBox: { backgroundColor: '#fff', borderRadius: 12, borderWidth: 1, borderColor: '#eee', padding: 12, marginBottom: 16 },
  factRow: { flexDirection: 'row', justifyContent: 'space-between', paddingVertical: 6 },
  factLabel: { color: '#717171' },
  factValue: { fontWeight: '600', color: '#222' },
  bookingBox: { backgroundColor: '#fff', borderRadius: 12, borderWidth: 1, borderColor: '#eee', padding: 16 },
  bookingPrice: { fontSize: 16, marginBottom: 12 },
  bookingPriceStrong: { fontWeight: '800', fontSize: 18 },
  label: { color: '#717171', fontSize: 12, marginBottom: 4, marginTop: 4 },
  input: { borderWidth: 1, borderColor: '#ddd', borderRadius: 8, padding: 10, marginBottom: 8, backgroundColor: '#fff' },
  stepperRow: { flexDirection: 'row', alignItems: 'center', gap: 12, marginBottom: 10 },
  stepperBtn: { width: 38, height: 38, borderRadius: 8, borderWidth: 1, borderColor: '#ddd', alignItems: 'center', justifyContent: 'center', backgroundColor: '#fff' },
  stepperBtnDisabled: { opacity: 0.4 },
  stepperText: { fontSize: 20, fontWeight: '700', color: '#222' },
  stepperValue: { fontSize: 18, fontWeight: '700', color: '#222', minWidth: 28, textAlign: 'center' },
  stepperMax: { color: '#717171', fontSize: 12, marginLeft: 4 },
  previewBox: { backgroundColor: '#fafafa', borderRadius: 8, padding: 10, marginBottom: 12 },
  previewLine: { color: '#222' },
  previewStrong: { fontWeight: '800' },
  previewHint: { color: '#717171', fontSize: 12, marginTop: 4 },
  bookBtn: { backgroundColor: '#ff385c', borderRadius: 8, padding: 14, alignItems: 'center' },
  bookBtnDisabled: { backgroundColor: '#ddd' },
  bookBtnText: { color: '#fff', fontWeight: '700' },
  successCard: { backgroundColor: '#e8f5ee', borderRadius: 8, padding: 14, borderWidth: 1, borderColor: '#b7e1c7' },
  successTitle: { fontWeight: '800', color: '#1a7f47', fontSize: 16, marginBottom: 6, textTransform: 'capitalize' },
  successLine: { color: '#1a7f47', marginVertical: 2 },
  linkBtn: { marginTop: 10 },
  linkBtnText: { color: '#ff385c', fontWeight: '700' },
  error: { color: '#c0392b', marginTop: 10 },
});
