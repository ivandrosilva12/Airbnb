import { useState } from 'react';
import {
  ScrollView,
  View,
  Text,
  Pressable,
  StyleSheet,
  ActivityIndicator,
} from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';

// CohostInvitationScreen is the deep-link target for the `cohost.invited`
// push notification (S125). The invitee taps the system push, the tap
// handler in pushBootstrap navigates here with the invitation payload, and
// from this screen they Accept or Decline.
//
// Navigation params (all optional except invitationId — the others are
// rendered as hints, so we degrade gracefully if a payload is missing them):
//   - invitationId  string  required; used for the API calls
//   - propertyTitle string  the listing they've been invited to co-host
//   - hostName      string  the primary host who sent the invite
//   - permissions   string[] which scopes the host wants to grant
//
// Two buttons:
//   - Accept  -> POST /cohost-invitations/:id/accept, then back to Explore
//                with a small confirmation banner.
//   - Decline -> POST /cohost-invitations/:id/decline, then back.
//
// Inline loading + error states only (no Alert.alert spam, per spec).
// English-only first pass: every user-facing string is marked TODO(i18n)
// for the next translation sweep.
export default function CohostInvitationScreen({ route, navigation }) {
  const api = useApi();
  const { authenticated, login } = useAuth();

  const invitationId = route?.params?.invitationId;
  const propertyTitle = route?.params?.propertyTitle;
  const hostName = route?.params?.hostName;
  const permissions = Array.isArray(route?.params?.permissions)
    ? route.params.permissions
    : [];

  // pending tracks which button is in-flight so we can disable both at once
  // and surface a spinner against the right one. Empty string = idle.
  const [pending, setPending] = useState('');
  const [error, setError] = useState(null);
  // done is the "we're navigating away" guard that lets us render a tiny
  // confirmation banner during the brief animation back to Explore.
  const [done, setDone] = useState(null);

  async function act(kind) {
    if (!invitationId) {
      setError('Missing invitation id.'); // TODO(i18n)
      return;
    }
    setError(null);
    setPending(kind);
    try {
      if (kind === 'accept') {
        await api.acceptCohostInvitation(invitationId);
        setDone('accepted');
      } else {
        await api.declineCohostInvitation(invitationId);
        setDone('declined');
      }
      // Pop back to Explore (the app's home). We pass a tiny banner hint via
      // params on the parent screen — ExploreScreen can pick this up if it
      // wants to render a toast, but it's not required for correctness.
      // navigate() (rather than goBack) handles the cold-start case where
      // this screen is the FIRST one on the stack after the push tap.
      setTimeout(() => {
        navigation.navigate('Explore', {
          cohostInvitationResult: {
            invitationId,
            decision: kind === 'accept' ? 'accepted' : 'declined',
            propertyTitle: propertyTitle || null,
          },
        });
      }, 300);
    } catch (e) {
      setError(e.message);
      setDone(null);
    } finally {
      setPending('');
    }
  }

  if (!authenticated) {
    return (
      <View style={styles.center}>
        {/* TODO(i18n) */}
        <Text style={styles.muted}>Sign in to view this co-host invitation.</Text>
        <Text style={styles.link} onPress={login}>Sign in</Text>
      </View>
    );
  }

  if (!invitationId) {
    return (
      <View style={styles.center}>
        {/* TODO(i18n) */}
        <Text style={styles.error}>This invitation link is missing its identifier.</Text>
      </View>
    );
  }

  return (
    <ScrollView style={styles.container} contentContainerStyle={styles.content}>
      {/* TODO(i18n) */}
      <Text style={styles.title}>You've been invited to co-host</Text>

      {done && (
        <View style={[styles.banner, done === 'accepted' ? styles.bannerOk : styles.bannerInfo]}>
          {/* TODO(i18n) */}
          <Text style={styles.bannerText}>
            {done === 'accepted'
              ? 'Invitation accepted. Taking you home…'
              : 'Invitation declined. Taking you home…'}
          </Text>
        </View>
      )}

      <View style={styles.card}>
        {/* TODO(i18n) */}
        <Text style={styles.label}>Listing</Text>
        <Text style={styles.value}>{propertyTitle || 'A listing'}</Text>

        {/* TODO(i18n) */}
        <Text style={styles.label}>Invited by</Text>
        <Text style={styles.value}>{hostName || 'The primary host'}</Text>

        {/* TODO(i18n) */}
        <Text style={styles.label}>Permissions</Text>
        {permissions.length > 0 ? (
          <View style={styles.chipsRow}>
            {permissions.map((p) => (
              <View key={p} style={styles.chip}>
                {/* TODO(i18n): mirror cohosts.perm.* keys once available. */}
                <Text style={styles.chipText}>{prettyPerm(p)}</Text>
              </View>
            ))}
          </View>
        ) : (
          // TODO(i18n)
          <Text style={styles.muted}>The host didn't specify which permissions to grant.</Text>
        )}
      </View>

      {error ? <Text style={styles.error}>{error}</Text> : null}

      <Pressable
        style={[styles.btnPrimary, (pending || done) && styles.btnDisabled]}
        disabled={!!pending || !!done}
        onPress={() => act('accept')}
        accessibilityRole="button"
      >
        {pending === 'accept' ? (
          <ActivityIndicator color="#fff" />
        ) : (
          // TODO(i18n)
          <Text style={styles.btnPrimaryText}>Accept invitation</Text>
        )}
      </Pressable>

      <Pressable
        style={[styles.btnSecondary, (pending || done) && styles.btnDisabled]}
        disabled={!!pending || !!done}
        onPress={() => act('decline')}
        accessibilityRole="button"
      >
        {pending === 'decline' ? (
          <ActivityIndicator color="#c0392b" />
        ) : (
          // TODO(i18n)
          <Text style={styles.btnSecondaryText}>Decline</Text>
        )}
      </Pressable>

      {/* TODO(i18n) */}
      <Text style={styles.hint}>
        Accepting gives this host the listed permissions on their property. You can step down
        anytime from the host area.
      </Text>
    </ScrollView>
  );
}

// prettyPerm turns the wire token (e.g. "manage_calendar") into a short
// human-readable label. Kept inline because the screen otherwise has no
// dependencies on the cohosts.* i18n namespace; the next i18n pass will
// switch this for t(`cohosts.perm.${p}`).
function prettyPerm(p) {
  switch (p) {
    case 'manage_calendar': return 'Manage calendar';
    case 'manage_pricing':  return 'Manage pricing';
    case 'reply_messages':  return 'Reply to messages';
    default:                return p;
  }
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff' },
  content: { padding: 16, gap: 14, paddingBottom: 40 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 12, padding: 24 },
  title: { fontSize: 22, fontWeight: '800', color: '#222' },
  card: {
    borderWidth: 1,
    borderColor: '#eee',
    borderRadius: 12,
    padding: 14,
    backgroundColor: '#fafafa',
    gap: 6,
  },
  label: { fontSize: 12, color: '#717171', fontWeight: '700', textTransform: 'uppercase', marginTop: 6 },
  value: { fontSize: 16, color: '#222', fontWeight: '600' },
  chipsRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 8, marginTop: 4 },
  chip: {
    borderWidth: 1,
    borderColor: '#ddd',
    borderRadius: 999,
    paddingHorizontal: 12,
    paddingVertical: 6,
    backgroundColor: '#fff',
  },
  chipText: { color: '#222', fontSize: 12, fontWeight: '600' },
  btnPrimary: {
    backgroundColor: '#ff385c',
    borderRadius: 10,
    paddingVertical: 14,
    alignItems: 'center',
    marginTop: 6,
  },
  btnPrimaryText: { color: '#fff', fontWeight: '700', fontSize: 16 },
  btnSecondary: {
    borderWidth: 1,
    borderColor: '#c0392b',
    borderRadius: 10,
    paddingVertical: 14,
    alignItems: 'center',
    backgroundColor: '#fff',
  },
  btnSecondaryText: { color: '#c0392b', fontWeight: '700', fontSize: 16 },
  btnDisabled: { opacity: 0.6 },
  hint: { fontSize: 12, color: '#717171', lineHeight: 18, marginTop: 4 },
  muted: { color: '#717171' },
  link: { color: '#ff385c', fontWeight: '700' },
  error: { color: '#c0392b' },
  banner: {
    borderRadius: 10,
    padding: 12,
    marginBottom: 4,
  },
  bannerOk: { backgroundColor: '#e8f7ee', borderWidth: 1, borderColor: '#bfe7cb' },
  bannerInfo: { backgroundColor: '#f3f4f6', borderWidth: 1, borderColor: '#e5e7eb' },
  bannerText: { color: '#222', fontWeight: '600' },
});
