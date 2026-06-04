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
} from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';
import { useT } from '../i18n/I18nContext';

// HostHouseRulesScreen is the mobile counterpart to web's HouseRulesPanel
// (the embedded section in web/src/pages/EditListing.jsx, S56). Hosts edit
// the bullet list, save, and the server bumps the version. Older versions
// stay queryable on the backend so historical acceptances still resolve
// to the exact text the guest acknowledged.
//
// UX choices for the small screen:
//   * One row per rule, each row is a TextInput with a small "✕" remove
//     button on the right (matches the web's button-link affordance).
//   * "+ Add rule" appends an empty row instead of opening a modal — same
//     pattern as the web — so a host can author several rules in a row
//     without round-tripping through dialogs.
//   * The save button is sticky-ish at the bottom of the scrollable form;
//     a small footer notes the current saved version so the host can tell
//     what guests currently see.
export default function HostHouseRulesScreen({ route, navigation }) {
  const { t } = useT();
  const propertyId = route.params?.propertyId;
  const api = useApi();
  const { authenticated, login } = useAuth();

  const [items, setItems] = useState(['']);
  const [version, setVersion] = useState(0);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  const [savedMsg, setSavedMsg] = useState(null);

  useLayoutEffect(() => {
    navigation.setOptions({ title: t('houseRules.title') });
  }, [navigation, t]);

  // reload pulls the current rule set and primes the editor. We re-render
  // with at least one editable row when the listing has no rules yet so
  // an empty set is authorable without an extra "+ Add" tap.
  const reload = useCallback(async () => {
    if (!propertyId) {
      setError('Missing property id.');
      setLoading(false);
      return;
    }
    setError(null);
    try {
      const r = await api.getHouseRules(propertyId);
      setVersion(r.version || 0);
      setItems(r.items && r.items.length > 0 ? r.items : ['']);
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

  function updateItem(index, value) {
    setItems((prev) => prev.map((it, i) => (i === index ? value : it)));
    setSavedMsg(null);
  }

  function addItem() {
    setItems((prev) => [...prev, '']);
    setSavedMsg(null);
  }

  function removeItem(index) {
    setItems((prev) => (prev.length > 1 ? prev.filter((_, i) => i !== index) : ['']));
    setSavedMsg(null);
  }

  async function save() {
    setSaving(true);
    setError(null);
    setSavedMsg(null);
    try {
      // Drop blanks so a host who hit "+ Add" and left it empty doesn't
      // trip the server-side validation — matches the web's pre-save
      // cleanup so behavioural parity holds.
      const cleaned = items.map((s) => s.trim()).filter(Boolean);
      const r = await api.setHouseRules(propertyId, cleaned);
      setVersion(r.version);
      setItems(r.items && r.items.length > 0 ? r.items : ['']);
      setSavedMsg(t('houseRules.saved', { version: r.version }));
    } catch (e) {
      setError(e.message);
    } finally {
      setSaving(false);
    }
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
        <Text style={styles.heading}>{t('houseRules.title')}</Text>
        <Text style={styles.hint}>{t('houseRules.hint')}</Text>
        {error && <Text style={styles.error}>{error}</Text>}

        {items.map((it, i) => (
          <View key={i} style={styles.ruleRow}>
            <TextInput
              style={styles.input}
              value={it}
              maxLength={500}
              placeholder={t('houseRules.placeholder')}
              placeholderTextColor="#999"
              accessibilityLabel={t('houseRules.itemAria', { n: i + 1 })}
              onChangeText={(v) => updateItem(i, v)}
            />
            {items.length > 1 && (
              <Pressable
                style={styles.removeBtn}
                onPress={() => removeItem(i)}
                accessibilityRole="button"
                accessibilityLabel={t('houseRules.remove')}
              >
                <Text style={styles.removeText}>✕</Text>
              </Pressable>
            )}
          </View>
        ))}

        <Pressable style={styles.btnGhost} onPress={addItem}>
          <Text style={styles.btnGhostText}>{t('houseRules.add')}</Text>
        </Pressable>

        <View style={styles.footer}>
          <Text style={styles.meta}>
            {version > 0
              ? t('houseRules.currentVersion', { version })
              : t('houseRules.notSet')}
          </Text>
          <Pressable
            style={[styles.btn, saving && styles.btnDisabled]}
            onPress={save}
            disabled={saving}
          >
            <Text style={styles.btnText}>
              {saving ? t('houseRules.saving') : t('houseRules.save')}
            </Text>
          </Pressable>
        </View>

        {savedMsg && <Text style={styles.success}>{savedMsg}</Text>}
      </ScrollView>
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff' },
  content: { padding: 16, paddingBottom: 40 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 16, padding: 24, backgroundColor: '#fff' },
  heading: { fontSize: 22, fontWeight: '800', marginBottom: 4 },
  hint: { color: '#717171', marginBottom: 16, lineHeight: 20 },
  ruleRow: { flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 10 },
  input: { flex: 1, borderWidth: 1, borderColor: '#ddd', borderRadius: 8, padding: 10, color: '#222' },
  removeBtn: { width: 36, height: 36, borderRadius: 18, alignItems: 'center', justifyContent: 'center', backgroundColor: '#f7f7f7' },
  removeText: { fontSize: 16, color: '#717171', fontWeight: '700' },
  btnGhost: { borderWidth: 1, borderColor: '#ddd', borderRadius: 8, padding: 12, alignItems: 'center', marginTop: 4 },
  btnGhostText: { color: '#222', fontWeight: '700' },
  footer: { marginTop: 24, gap: 10 },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, padding: 14, alignItems: 'center' },
  btnDisabled: { opacity: 0.6 },
  btnText: { color: '#fff', fontWeight: '700' },
  meta: { color: '#717171', fontSize: 13 },
  error: { color: '#c0392b', marginBottom: 8 },
  success: { color: '#1a7f47', marginTop: 12, textAlign: 'center', fontWeight: '600' },
});
