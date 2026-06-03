import { useEffect, useState } from 'react';
import {
  View,
  Text,
  Image,
  Pressable,
  ScrollView,
  StyleSheet,
  ActivityIndicator,
} from 'react-native';
import { useApi } from '../api/useApi';

// Mirrors web/src/pages/ExperienceDetail.jsx. Photos render as a horizontal
// pager (active index controls the hero swap), description and metadata
// stack below, and the Book CTA is intentionally disabled — the experience
// booking flow ships in a later slice (BookingForExperience BC).
export default function ExperienceDetailScreen({ route }) {
  const api = useApi();
  const { id } = route.params || {};
  const [experience, setExperience] = useState(null);
  const [activePhoto, setActivePhoto] = useState(0);
  const [error, setError] = useState(null);

  useEffect(() => {
    setError(null);
    setExperience(null);
    setActivePhoto(0);
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
            <Text style={styles.bookingPriceStrong}>{experience.pricePerGuest.display}</Text>
            {' '}/ guest
          </Text>
          {/* Disabled stub — matches web parity. Booking flow lands in a
              future slice once per-session availability is modelled. */}
          <Pressable style={[styles.bookBtn, styles.bookBtnDisabled]} disabled>
            <Text style={styles.bookBtnText}>Booking opens soon</Text>
          </Pressable>
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
  bookBtn: { backgroundColor: '#ff385c', borderRadius: 8, padding: 14, alignItems: 'center' },
  bookBtnDisabled: { backgroundColor: '#ddd' },
  bookBtnText: { color: '#fff', fontWeight: '700' },
  error: { color: '#c0392b' },
});
