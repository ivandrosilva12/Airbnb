import { useCallback, useEffect, useLayoutEffect, useState } from 'react';
import {
  View,
  Text,
  TextInput,
  Pressable,
  StyleSheet,
  ScrollView,
  ActivityIndicator,
  KeyboardAvoidingView,
  Platform,
  Alert,
} from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';
import { useT } from '../i18n/I18nContext';

// The three permissions a primary host can grant a co-host on a single
// listing. Kept in sync with the backend's property.CohostPermission enum
// (see backend/internal/domain/property/cohost.go — PermManageCalendar,
// PermManagePricing, PermReplyMessages). The wire string is the canonical
// token; the label is i18n'd under the cohosts.perm.* namespace and shared
// with the web's CohostsPanel.
const COHOST_PERMS = ['manage_calendar', 'manage_pricing', 'reply_messages'];

// HostCohostsScreen is the mobile counterpart to web's CohostsPanel (embedded
// in web/src/pages/EditListing.jsx). The primary host lists current co-hosts
// on a single listing, invites a new co-host by email + permission set, edits
// an existing grant's permissions in-place, and revokes a grant. The list is
// reloaded after every mutation so the UI reflects the server view without
// local optimistic bookkeeping (same approach as the web).
export default function HostCohostsScreen({ route, navigation }) {
  const { t } = useT();
  const propertyId = route.params?.propertyId;
  const api = useApi();
  const { authenticated, login } = useAuth();

  const [cohosts, setCohosts] = useState([]);
  const [email, setEmail] = useState('');
  // Default the invite form to "manage_calendar" so a quick invite is one tap
  // away (mirrors the web's initial state). The host can toggle the rest.
  const [perms, setPerms] = useState(new Set(['manage_calendar']));
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);

  useLayoutEffect(() => {
    navigation.setOptions({ title: t('cohosts.title') });
  }, [navigation, t]);

  const reload = useCallback(async () => {
    if (!propertyId) {
      setError('Missing property id.');
      setLoading(false);
      return;
    }
    setError(null);
    try {
      const r = await api.listCohosts(propertyId);
      setCohosts(r.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [api, propertyId]);

  useEffect(() => {
    if (authenticated) reload();
    else setLoading(false);
  }, [authenticated, reload]);

  function toggleInvitePerm(p) {
    setPerms((prev) => {
      const next = new Set(prev);
      if (next.has(p)) next.delete(p);
      else next.add(p);
      return next;
    });
  }

  async function invite() {
    setError(null);
    if (!email.trim()) {
      // Surface the missing-email case inline; backend would also reject but
      // the UX is friendlier if we don't round-trip for an obvious blank.
      setError(t('cohosts.fEmail'));
      return;
    }
    if (perms.size === 0) {
      setError(t('cohosts.errNoPerms'));
      return;
    }
    setSaving(true);
    try {
      await api.inviteCohost(propertyId, {
        email: email.trim(),
        permissions: Array.from(perms),
      });
      setEmail('');
      setPerms(new Set(['manage_calendar']));
      await reload();
    } catch (e) {
      setError(e.message);
    } finally {
      setSaving(false);
    }
  }

  async function updatePerms(cohostId, nextSet) {
    setError(null);
    try {
      await api.updateCohostPermissions(propertyId, cohostId, Array.from(nextSet));
      await reload();
    } catch (e) {
      setError(e.message);
    }
  }

  function confirmRevoke(c) {
    const who = c.displayName || c.email || c.userId;
    // Native confirm avoids accidental revokes; matches the destructive-action
    // pattern already used elsewhere (HostListingsScreen delete, etc.).
    Alert.alert(
      t('cohosts.revoke'),
      `${t('cohosts.revoke')}: ${who}?`,
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: t('cohosts.revoke'),
          style: 'destructive',
          onPress: async () => {
            setError(null);
            try {
              await api.revokeCohost(propertyId, c.id);
              await reload();
            } catch (e) {
              setError(e.message);
            }
          },
        },
      ],
    );
  }

  if (!authenticated) {
    return (
      <View style={styles.center}>
        <Text style={styles.meta}>{t('detail.signInReserve')}</Text>
        <Pressable style={styles.btn} onPress={login}>
          <Text style={styles.btnText}>{t('nav.signIn')}</Text>
        </Pressable>
      </View>
    );
  }

  if (loading) {
    return <ActivityIndicator style={{ flex: 1 }} color="#ff385c" />;
  }

  return (
    <KeyboardAvoidingView
      behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      style={styles.container}
    >
      <ScrollView contentContainerStyle={styles.content}>
        <Text style={styles.heading}>{t('cohosts.title')}</Text>
        <Text style={styles.hint}>{t('cohosts.hint')}</Text>
        {error && <Text style={styles.error}>{error}</Text>}

        {/* Invite form ─────────────────────────────────────────────────── */}
        <View style={styles.inviteCard}>
          <Text style={styles.label}>{t('cohosts.fEmail')}</Text>
          <TextInput
            style={styles.input}
            value={email}
            onChangeText={setEmail}
            placeholder={t('cohosts.fEmailPlaceholder')}
            placeholderTextColor="#999"
            keyboardType="email-address"
            autoCapitalize="none"
            autoCorrect={false}
          />

          <Text style={[styles.label, { marginTop: 12 }]}>{t('cohosts.permissions')}</Text>
          {COHOST_PERMS.map((p) => {
            const checked = perms.has(p);
            return (
              <Pressable
                key={p}
                style={styles.permRow}
                onPress={() => toggleInvitePerm(p)}
                accessibilityRole="checkbox"
                accessibilityState={{ checked }}
              >
                <Text style={styles.permBox}>{checked ? '☑' : '☐'}</Text>
                <Text style={styles.permLabel}>{t(`cohosts.perm.${p}`)}</Text>
              </Pressable>
            );
          })}

          <Pressable
            style={[styles.btn, saving && styles.btnDisabled]}
            onPress={invite}
            disabled={saving}
          >
            <Text style={styles.btnText}>
              {saving ? t('cohosts.inviting') : t('cohosts.invite')}
            </Text>
          </Pressable>
        </View>

        {/* Existing co-hosts ──────────────────────────────────────────── */}
        {cohosts.length === 0 ? (
          <Text style={styles.empty}>{t('cohosts.empty')}</Text>
        ) : (
          cohosts.map((c) => (
            <View key={c.id} style={styles.cohostCard}>
              <Text style={styles.cohostName}>
                {c.displayName || c.email || c.userId}
              </Text>
              {c.email && c.displayName ? (
                <Text style={styles.cohostEmail}>{c.email}</Text>
              ) : null}

              <View style={styles.chipsRow}>
                {COHOST_PERMS.map((p) => {
                  const has = (c.permissions || []).includes(p);
                  return (
                    <Pressable
                      key={p}
                      style={[styles.chip, has && styles.chipOn]}
                      accessibilityRole="checkbox"
                      accessibilityState={{ checked: has }}
                      onPress={() => {
                        const next = new Set(c.permissions || []);
                        if (has) next.delete(p);
                        else next.add(p);
                        // Empty grants are rejected by the domain layer; surface
                        // the same error inline rather than round-tripping.
                        if (next.size === 0) {
                          setError(t('cohosts.errNoPerms'));
                          return;
                        }
                        updatePerms(c.id, next);
                      }}
                    >
                      <Text style={has ? styles.chipTextOn : styles.chipText}>
                        {t(`cohosts.perm.${p}`)}
                      </Text>
                    </Pressable>
                  );
                })}
              </View>

              <Pressable
                style={styles.revokeBtn}
                onPress={() => confirmRevoke(c)}
                accessibilityRole="button"
              >
                <Text style={styles.revokeText}>{t('cohosts.revoke')}</Text>
              </Pressable>
            </View>
          ))
        )}
      </ScrollView>
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff' },
  content: { padding: 16, paddingBottom: 40 },
  center: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    gap: 16,
    padding: 24,
    backgroundColor: '#fff',
  },
  heading: { fontSize: 22, fontWeight: '800', marginBottom: 4 },
  hint: { color: '#717171', marginBottom: 16, lineHeight: 20 },
  label: { fontSize: 13, color: '#717171', marginBottom: 4 },
  input: {
    borderWidth: 1,
    borderColor: '#ddd',
    borderRadius: 8,
    padding: 10,
    color: '#222',
  },
  inviteCard: {
    borderWidth: 1,
    borderColor: '#eee',
    borderRadius: 12,
    padding: 14,
    marginBottom: 20,
    backgroundColor: '#fafafa',
  },
  permRow: { flexDirection: 'row', alignItems: 'center', gap: 8, paddingVertical: 6 },
  permBox: { fontSize: 18 },
  permLabel: { flex: 1, color: '#222' },
  btn: {
    backgroundColor: '#ff385c',
    borderRadius: 8,
    padding: 14,
    alignItems: 'center',
    marginTop: 12,
  },
  btnDisabled: { opacity: 0.6 },
  btnText: { color: '#fff', fontWeight: '700' },
  empty: { color: '#717171', textAlign: 'center', marginTop: 12 },
  cohostCard: {
    borderWidth: 1,
    borderColor: '#eee',
    borderRadius: 12,
    padding: 14,
    marginBottom: 12,
  },
  cohostName: { fontSize: 16, fontWeight: '700', color: '#222' },
  cohostEmail: { fontSize: 13, color: '#717171', marginTop: 2 },
  chipsRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 8, marginTop: 10 },
  chip: {
    borderWidth: 1,
    borderColor: '#ddd',
    borderRadius: 999,
    paddingHorizontal: 12,
    paddingVertical: 6,
  },
  chipOn: { backgroundColor: '#ff385c', borderColor: '#ff385c' },
  chipText: { color: '#222', fontSize: 12 },
  chipTextOn: { color: '#fff', fontWeight: '700', fontSize: 12 },
  revokeBtn: {
    marginTop: 12,
    alignSelf: 'flex-start',
    paddingVertical: 6,
    paddingHorizontal: 10,
  },
  revokeText: { color: '#c0392b', fontWeight: '700' },
  meta: { color: '#717171', fontSize: 13 },
  error: { color: '#c0392b', marginBottom: 8 },
});
