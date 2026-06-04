import { useCallback, useEffect, useState } from 'react';
import { ScrollView, View, Text, StyleSheet, ActivityIndicator, RefreshControl } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';
import { useT } from '../i18n/I18nContext';

// ArrivalInfoScreen renders the practical "how do I get in?" details for a
// confirmed booking: check-in method, free-text instructions and the wifi
// credentials. The backend serves /bookings/:id/arrival only inside the
// reveal window (≤ 48 h before check-in through check-out) and returns 403
// outside it — we surface that gate as a friendly "available soon" panel.
//
// This is the mobile counterpart of the small "arrival-panel" block already
// rendered inline on the web's MyTrips page (web/src/pages/MyTrips.jsx). The
// web build keeps the panel inline because the trips list has the room; on
// mobile we promote it to its own screen so the 48-h notification deep-link
// from S102 lands on a focused surface instead of dumping the user into a
// scrolling list.
//
// Clipboard: expo-clipboard is not in the project's package.json (see
// mobile/package.json) and we're not adding native deps in this PR. The
// password is therefore rendered as plain selectable Text — long-press to
// copy is the platform-native fallback on both iOS and Android.
// TODO(i18n): no arrival.* keys cover the guest-facing copy yet (the
// existing keys are for the host editor). Strings stay in English and are
// flagged so the next translation sweep picks them up.
export default function ArrivalInfoScreen({ route }) {
  const api = useApi();
  const { authenticated, login } = useAuth();
  const { t } = useT();
  const bookingId = route?.params?.bookingId;

  const [arrival, setArrival] = useState(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState(null);
  // gated flips true when the server returns 403 — i.e. the reveal window
  // hasn't opened yet. Distinguishing this from a generic error lets us
  // show a calmer "come back later" message instead of a red banner.
  const [gated, setGated] = useState(false);

  const load = useCallback(async () => {
    if (!bookingId) {
      setError('Missing booking id.'); // TODO(i18n)
      setLoading(false);
      return;
    }
    setError(null);
    setGated(false);
    try {
      const data = await api.getBookingArrival(bookingId);
      setArrival(data);
    } catch (e) {
      if (e.status === 403) {
        setGated(true);
        setArrival(null);
      } else {
        setError(e.message);
      }
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, [api, bookingId]);

  useEffect(() => {
    if (authenticated) load();
    else setLoading(false);
  }, [authenticated, load]);

  function onRefresh() {
    setRefreshing(true);
    load();
  }

  if (!authenticated) {
    return (
      <View style={styles.center}>
        <Text style={styles.muted}>{t('detail.signInReserve')}</Text>
        <Text style={styles.link} onPress={login}>{t('nav.signIn')}</Text>
      </View>
    );
  }

  if (loading) return <ActivityIndicator style={{ flex: 1 }} color="#ff385c" />;

  return (
    <ScrollView
      style={styles.container}
      contentContainerStyle={styles.content}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor="#ff385c" />}
    >
      {/* TODO(i18n): no key for the screen subtitle yet. */}
      <Text style={styles.title}>{t('arrival.bannerTitle')}</Text>

      {error && <Text style={styles.error}>{error}</Text>}

      {gated && (
        <View style={styles.gatedPanel}>
          {/* TODO(i18n): no arrival.lockedTitle / lockedBody keys yet. */}
          <Text style={styles.gatedTitle}>Available 48 hours before check-in</Text>
          <Text style={styles.gatedBody}>
            Your host has prepared the check-in details. They’ll unlock here automatically two days
            before your stay and stay visible through check-out.
          </Text>
        </View>
      )}

      {arrival && (
        <View style={styles.sections}>
          <Section label={t('arrival.fCheckInMethod')}>
            {arrival.checkInMethod ? (
              <Text style={styles.value}>{t(`arrival.method.${arrival.checkInMethod}`)}</Text>
            ) : (
              // TODO(i18n): no key for an empty method label yet.
              <Text style={styles.muted}>Not specified</Text>
            )}
          </Section>

          {arrival.arrivalInstructions ? (
            // TODO(i18n): reuse `arrival.fInstructions` once a guest-facing
            // variant without the "(max 2000 chars)" suffix exists.
            <Section label="Instructions">
              <Text style={styles.bodyText} selectable>{arrival.arrivalInstructions}</Text>
            </Section>
          ) : null}

          {(arrival.wifiSsid || arrival.wifiPassword) ? (
            // TODO(i18n): no `arrival.wifiSection` key yet.
            <Section label="Wi-Fi">
              {arrival.wifiSsid ? (
                <View style={styles.kvRow}>
                  <Text style={styles.kvLabel}>{t('arrival.fWifiSsid')}</Text>
                  <Text style={styles.kvValue} selectable>{arrival.wifiSsid}</Text>
                </View>
              ) : null}
              {arrival.wifiPassword ? (
                <View style={styles.kvRow}>
                  <Text style={styles.kvLabel}>{t('arrival.fWifiPassword')}</Text>
                  {/* Selectable Text — long-press to copy is the native
                      fallback while expo-clipboard isn't in deps. */}
                  <Text style={styles.kvValue} selectable>{arrival.wifiPassword}</Text>
                </View>
              ) : null}
              {/* TODO(i18n): no key for this hint yet. */}
              <Text style={styles.hint}>Tap and hold the password to copy it.</Text>
            </Section>
          ) : null}

          {!arrival.checkInMethod &&
            !arrival.arrivalInstructions &&
            !arrival.wifiSsid &&
            !arrival.wifiPassword && (
              // The endpoint can succeed with an empty payload when the
              // host hasn't configured arrival info at all — surface a
              // friendly hint instead of an empty screen.
              // TODO(i18n)
              <Text style={styles.muted}>
                Your host hasn’t added arrival details yet. Message them through the conversation
                thread if you need help getting in.
              </Text>
            )}
        </View>
      )}
    </ScrollView>
  );
}

function Section({ label, children }) {
  return (
    <View style={styles.section}>
      <Text style={styles.sectionLabel}>{label}</Text>
      {children}
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff' },
  content: { padding: 16, gap: 16 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 12, padding: 24 },
  title: { fontSize: 20, fontWeight: '800', color: '#222' },
  sections: { gap: 14 },
  section: {
    borderWidth: 1,
    borderColor: '#eee',
    borderRadius: 10,
    padding: 12,
    backgroundColor: '#fafafa',
    gap: 6,
  },
  sectionLabel: { fontSize: 12, color: '#717171', fontWeight: '700', textTransform: 'uppercase' },
  value: { fontSize: 15, color: '#222', fontWeight: '600' },
  bodyText: { fontSize: 15, color: '#222', lineHeight: 22 },
  kvRow: { flexDirection: 'row', alignItems: 'flex-start', gap: 8, paddingVertical: 4 },
  kvLabel: { color: '#717171', flex: 1 },
  kvValue: { color: '#222', fontWeight: '600', flex: 2, textAlign: 'right' },
  hint: { fontSize: 12, color: '#717171', marginTop: 4 },
  muted: { color: '#717171' },
  link: { color: '#ff385c', fontWeight: '700' },
  error: { color: '#c0392b' },
  gatedPanel: {
    borderWidth: 1,
    borderColor: '#ffd9e0',
    backgroundColor: '#fff5f7',
    borderRadius: 10,
    padding: 14,
    gap: 6,
  },
  gatedTitle: { fontWeight: '800', color: '#222' },
  gatedBody: { color: '#444', lineHeight: 20 },
});
