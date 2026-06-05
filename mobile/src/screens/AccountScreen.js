import { useEffect, useMemo, useState } from 'react';
import { View, Text, Pressable, StyleSheet, Alert, ScrollView, Share } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';
import { useT } from '../i18n/I18nContext';

// AccountScreen — entry point for everything that lives behind sign-in. The
// link list, the host-onboarding affordance, and the new locale picker (S91)
// all hang off this screen so the user only has one place to look.
//
// LINKS used to be a top-level const carrying English labels inline. Now the
// labels come from t() at render time so they switch languages alongside the
// rest of the app. The icons stay in code — they are purely decorative and
// don't need translation. Screens added after the S91 base (MyExperienceBookings,
// Splits, MyDisputes, SavedReplies) still use English labels with a TODO(i18n)
// marker until they get their own translation keys.
export default function AccountScreen({ navigation }) {
  const { authenticated, login, logout, setupTwoFactor, canSetupTwoFactor } = useAuth();
  const api = useApi();
  const { t, lang, setLang, locales } = useT();
  const [isHost, setIsHost] = useState(false);
  const [busy, setBusy] = useState(false);

  // Memoise the list so we don't reallocate the array on every render. The
  // dependency is t (a useCallback under the hood), so the array refreshes
  // exactly when the language changes.
  const links = useMemo(
    () => [
      { screen: 'Trips', label: t('nav.trips'), icon: '🧳' },
      // TODO(i18n): no key in translations.js yet for the experience-booking namespace
      { screen: 'MyExperienceBookings', label: 'My experience bookings', icon: '🎉' },
      { screen: 'Saved', label: t('fav.title'), icon: '♥' },
      { screen: 'Messages', label: t('nav.messages'), icon: '💬' },
      { screen: 'Notifications', label: t('nav.notifications'), icon: '🔔' },
      // TODO(i18n): no key for nav.savedSearches yet — S137 parity with web's
      // saved-searches chip row. Sits next to Notifications because alerts on
      // saved searches feed the notifications panel.
      { screen: 'SavedSearches', label: 'Saved searches', icon: '🔎' },
      // TODO(i18n): split-payment / dispute / saved-replies translation keys
      // were added after the S91 base — wire them up in the next i18n sweep.
      { screen: 'Splits', label: 'Split payments', icon: '🤝' },
      { screen: 'MyDisputes', label: 'Resolution cases', icon: '⚖️' },
      { screen: 'SavedReplies', label: 'Saved replies', icon: '📝' },
      { screen: 'Verification', label: t('verify.title'), icon: '🪪' },
    ],
    [t],
  );

  useEffect(() => {
    if (!authenticated) return;
    api.me().then((u) => setIsHost(u.role === 'host' || u.role === 'admin')).catch(() => {});
  }, [authenticated]);

  async function becomeHost() {
    setBusy(true);
    try {
      await api.becomeHost();
      setIsHost(true);
      navigation.navigate('HostListings');
    } catch (e) {
      Alert.alert(t('host.becomeBtn'), e.message);
    } finally {
      setBusy(false);
    }
  }

  // exportData mirrors the web's GDPR right-of-access flow (S109): a
  // POST-but-actually-GET (the backend uses GET /me/export) that returns
  // the user's full data bundle as JSON. Web triggers a file download via
  // a hidden <a>; on mobile we hand the serialized JSON to React Native's
  // built-in Share API so the user can save it to Files / email it /
  // forward it to a regulator. The built-in Share is enough for textual
  // content; we deliberately avoid pulling in expo-file-system or
  // expo-sharing for this PR to stay inside the existing dep manifest.
  // TODO(i18n): no toast / success-string key on mobile yet — using the
  // privacy.export label for both button and feedback to avoid adding new
  // keys mid-PR; a follow-up i18n sweep will add privacy.exportSuccess.
  async function exportData() {
    if (busy) return;
    setBusy(true);
    try {
      const data = await api.exportMyData();
      const json = JSON.stringify(data, null, 2);
      await Share.share({
        message: json,
        title: t('privacy.export'),
      });
    } catch (e) {
      Alert.alert(t('privacy.export'), e.message || String(e));
    } finally {
      setBusy(false);
    }
  }

  function confirmDelete() {
    Alert.alert(
      t('privacy.delete'),
      t('privacy.deleteConfirm'),
      [
        { text: t('common.cancel'), style: 'cancel' },
        {
          text: t('privacy.delete'),
          style: 'destructive',
          onPress: async () => {
            try {
              await api.deleteAccount();
              await logout();
            } catch (e) {
              Alert.alert(t('privacy.delete'), e.message);
            }
          },
        },
      ],
    );
  }

  // LanguagePicker — rendered in both authed and signed-out states so a user
  // can pick their language before signing in. Mirrors the persistent
  // language switcher in the web's top navigation. Chips wrap to multiple
  // lines on narrow devices instead of forcing a horizontal scroll.
  function LanguagePicker() {
    return (
      <View style={styles.langPicker}>
        <Text style={styles.langLabel}>{t('a11y.language')}</Text>
        <View style={styles.langRow}>
          {locales.map((loc) => {
            const active = loc.code === lang;
            return (
              <Pressable
                key={loc.code}
                style={[styles.langChip, active && styles.langChipActive]}
                onPress={() => setLang(loc.code)}
                accessibilityRole="button"
                accessibilityState={{ selected: active }}
                accessibilityLabel={loc.label}
              >
                <Text style={[styles.langChipText, active && styles.langChipTextActive]}>
                  {loc.label}
                </Text>
              </Pressable>
            );
          })}
        </View>
      </View>
    );
  }

  if (!authenticated) {
    return (
      <ScrollView contentContainerStyle={styles.center}>
        <LanguagePicker />
        <Text style={styles.meta}>{t('detail.signInReserve')}</Text>
        <Pressable style={styles.btn} onPress={login}>
          <Text style={styles.btnText}>{t('nav.signIn')}</Text>
        </Pressable>
      </ScrollView>
    );
  }

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: 24 }}>
      {links.map((l) => (
        <Pressable key={l.screen} style={styles.row} onPress={() => navigation.navigate(l.screen)}>
          <Text style={styles.icon}>{l.icon}</Text>
          <Text style={styles.label}>{l.label}</Text>
          <Text style={styles.chevron}>›</Text>
        </Pressable>
      ))}
      {isHost ? (
        <>
          <Pressable style={styles.row} onPress={() => navigation.navigate('HostListings')}>
            <Text style={styles.icon}>🏠</Text>
            <Text style={styles.label}>{t('nav.host')}</Text>
            <Text style={styles.chevron}>›</Text>
          </Pressable>
          <Pressable style={styles.row} onPress={() => navigation.navigate('HostEarnings')}>
            <Text style={styles.icon}>💸</Text>
            <Text style={styles.label}>{t('earnings.title')}</Text>
            <Text style={styles.chevron}>›</Text>
          </Pressable>
        </>
      ) : (
        <Pressable style={styles.row} onPress={becomeHost} disabled={busy}>
          <Text style={styles.icon}>🏠</Text>
          <Text style={styles.label}>{t('host.becomeBtn')}</Text>
          <Text style={styles.chevron}>›</Text>
        </Pressable>
      )}
      <Pressable
        style={styles.row}
        onPress={() => setupTwoFactor()}
        disabled={!canSetupTwoFactor}
      >
        <Text style={styles.icon}>🔐</Text>
        <Text style={styles.label}>{t('security.title')}</Text>
        <Text style={styles.chevron}>›</Text>
      </Pressable>
      <Pressable style={styles.row} onPress={exportData} disabled={busy}>
        <Text style={styles.icon}>📤</Text>
        <Text style={styles.label}>{t('privacy.export')}</Text>
        <Text style={styles.chevron}>›</Text>
      </Pressable>
      <Pressable style={styles.row} onPress={confirmDelete}>
        <Text style={styles.icon}>🗑️</Text>
        <Text style={[styles.label, { color: '#c0392b' }]}>{t('privacy.delete')}</Text>
        <Text style={styles.chevron}>›</Text>
      </Pressable>

      <LanguagePicker />

      <Pressable style={styles.signOut} onPress={logout}>
        <Text style={styles.signOutText}>{t('nav.signOut')}</Text>
      </Pressable>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff', padding: 12 },
  center: { flexGrow: 1, alignItems: 'center', justifyContent: 'center', gap: 16, backgroundColor: '#fff', padding: 24 },
  row: { flexDirection: 'row', alignItems: 'center', paddingVertical: 16, borderBottomWidth: 1, borderColor: '#eee' },
  icon: { fontSize: 18, width: 32 },
  label: { flex: 1, fontSize: 16, fontWeight: '600' },
  chevron: { fontSize: 22, color: '#bbb' },
  meta: { color: '#717171' },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, paddingHorizontal: 20, paddingVertical: 12 },
  btnText: { color: '#fff', fontWeight: '700' },
  signOut: { marginTop: 24, alignItems: 'center', paddingVertical: 12 },
  signOutText: { color: '#c0392b', fontWeight: '700' },
  // Language picker — chips wrap so we don't need a horizontal scroll on
  // narrow devices, and the active chip uses the brand pink to make the
  // current selection unambiguous.
  langPicker: { marginTop: 24, paddingTop: 16, borderTopWidth: 1, borderColor: '#eee' },
  langLabel: { fontSize: 13, color: '#717171', marginBottom: 8, textTransform: 'uppercase', letterSpacing: 0.5 },
  langRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  langChip: { borderWidth: 1, borderColor: '#ddd', borderRadius: 20, paddingHorizontal: 14, paddingVertical: 8, backgroundColor: '#fff' },
  langChipActive: { borderColor: '#ff385c', backgroundColor: '#ff385c' },
  langChipText: { color: '#222', fontWeight: '600' },
  langChipTextActive: { color: '#fff' },
});
