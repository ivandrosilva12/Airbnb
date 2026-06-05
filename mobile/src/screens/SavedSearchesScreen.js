import { useCallback, useEffect, useState } from 'react';
import { View, Text, FlatList, Pressable, StyleSheet, ActivityIndicator } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';

// SavedSearchesScreen — parity with web's saved-searches chip row on Home.jsx
// (S132). The web surface keeps the chips inline above the results; mobile
// already has chips on ExploreScreen, but the chip strip can't hold more than
// a handful of names and gives no view of the filter values. This screen is
// the dedicated manager: full list, readable filter summary per row, primary
// "Search" action that re-runs the search, secondary "Bell" toggle for new-
// listing alerts, and a confirm-before-delete row that doesn't depend on
// Alert.alert (the codebase has inline-action patterns we follow here).
//
// The route name we navigate to is 'Explore' — same name ExploreScreen is
// registered under in App.js. We pass the saved query string as a route param;
// ExploreScreen doesn't yet read it (a follow-up will pick it up), but the
// navigation jump matches the user's expectation that "tap a saved search"
// returns them to the discovery surface.

// queryToParams mirrors ExploreScreen's helper so we can render a readable
// summary of each saved search (location + dates + price range + amenities).
// Kept local so this screen doesn't reach across screens — the helper is tiny
// and the duplication is intentional until a shared utils module lands.
function queryToParams(query) {
  const sp = new URLSearchParams(query || '');
  const out = {};
  for (const k of new Set(sp.keys())) {
    const all = sp.getAll(k);
    out[k] = all.length > 1 ? all : all[0];
  }
  return out;
}

// formatCents — saved filters store min/max price in cents (matches the search
// API). We render them as plain dollars for the summary; locale-aware currency
// formatting lives in the result row, not this preview.
function formatCents(c) {
  if (c == null || c === '') return null;
  const n = Number(c);
  if (Number.isNaN(n)) return null;
  return `$${(n / 100).toFixed(0)}`;
}

// summariseFilters returns a short, human-readable line describing the saved
// search's filters. Order is location → dates → guests → price → amenities,
// the same order users typically scan. Anything missing is silently dropped so
// the row stays tight.
function summariseFilters(query) {
  const p = queryToParams(query);
  const bits = [];
  const place = p.city || p.q;
  if (place) bits.push(place);
  if (p.checkIn || p.checkOut) {
    bits.push(`${p.checkIn || '?'} → ${p.checkOut || '?'}`);
  }
  if (p.minGuests) bits.push(`${p.minGuests} guests`);
  const min = formatCents(p.minPrice);
  const max = formatCents(p.maxPrice);
  if (min && max) bits.push(`${min}–${max}`);
  else if (min) bits.push(`from ${min}`);
  else if (max) bits.push(`up to ${max}`);
  if (p.instantBook === 'true') bits.push('Instant Book');
  const amenities = Array.isArray(p.amenity) ? p.amenity : p.amenity ? [p.amenity] : [];
  if (amenities.length) bits.push(`${amenities.length} amenities`);
  return bits.join(' · ');
}

export default function SavedSearchesScreen({ navigation }) {
  const api = useApi();
  const { authenticated, login } = useAuth();
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  // pendingDelete tracks which row has its inline confirmation expanded. We
  // deliberately avoid Alert.alert (the task spec calls it out) and inline a
  // two-button "Sure?" affordance instead — it matches the gentle, undoable
  // style the rest of the app uses for low-stakes destructive actions.
  const [pendingDelete, setPendingDelete] = useState(null);
  // alertBusy stores the id currently mid-toggle so we can disable the bell
  // without flickering the whole row.
  const [alertBusy, setAlertBusy] = useState(null);

  const reload = useCallback(async () => {
    setError(null);
    try {
      const r = await api.listSavedSearches();
      setItems(r.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [api]);

  useEffect(() => {
    if (authenticated) reload();
    else setLoading(false);
  }, [authenticated, reload]);

  function applySaved(s) {
    // ExploreScreen is registered as 'Explore'. We pass the raw query string
    // so a future ExploreScreen change can read route.params.savedQuery and
    // pre-apply the filters; today the navigation alone returns users to the
    // discovery surface, where the existing chip row also reflects this saved
    // search.
    navigation.navigate('Explore', { savedQuery: s.query, savedName: s.name });
  }

  // toggleAlerts flips the bell on a saved search. The web has the same
  // affordance (PATCH /saved-searches/:id with alertsEnabled). Optimistic-
  // update locally; restore from server response on failure.
  async function toggleAlerts(s) {
    if (alertBusy) return;
    setAlertBusy(s.id);
    setError(null);
    const next = !s.alertsEnabled;
    setItems((prev) => prev.map((x) => (x.id === s.id ? { ...x, alertsEnabled: next } : x)));
    try {
      await api.setSearchAlerts(s.id, next);
    } catch (e) {
      // Restore on failure so the bell icon stays truthful.
      setItems((prev) => prev.map((x) => (x.id === s.id ? { ...x, alertsEnabled: !next } : x)));
      setError(e.message);
    } finally {
      setAlertBusy(null);
    }
  }

  // remove uses an optimistic delete: drop the row immediately, restore on
  // failure. Matches the chip-row UX on ExploreScreen and the web home page,
  // where deleting feels instant.
  async function remove(s) {
    const snapshot = items;
    setError(null);
    setPendingDelete(null);
    setItems((prev) => prev.filter((x) => x.id !== s.id));
    try {
      await api.deleteSavedSearch(s.id);
    } catch (e) {
      setItems(snapshot);
      setError(e.message);
    }
  }

  if (!authenticated) {
    return (
      <View style={styles.center}>
        {/* TODO(i18n): no key yet for the saved-searches screen — follow-up
            sweep will add saved.signInHint alongside saved.empty. */}
        <Text style={styles.meta}>Sign in to see your saved searches.</Text>
        <Pressable style={styles.btn} onPress={login}>
          <Text style={styles.btnText}>Sign in</Text>
        </Pressable>
      </View>
    );
  }

  // Lightweight skeleton: a centered spinner for the initial load. The list
  // itself is short (a user rarely has >10 saved searches), so a full row
  // skeleton would be noisier than the spinner without buying much.
  if (loading) {
    return (
      <View style={styles.center}>
        <ActivityIndicator color="#ff385c" />
      </View>
    );
  }

  return (
    <View style={styles.container}>
      {/* TODO(i18n): saved.hint */}
      <Text style={styles.hint}>
        Your saved searches. Tap one to re-run it, or toggle the bell for new-listing alerts.
      </Text>
      {error && <Text style={styles.error}>{error}</Text>}
      <FlatList
        data={items}
        keyExtractor={(it) => it.id}
        ListEmptyComponent={
          // TODO(i18n): saved.empty
          <Text style={styles.empty}>
            No saved searches yet. Save a search from the filters screen.
          </Text>
        }
        renderItem={({ item }) => {
          const summary = summariseFilters(item.query);
          const confirming = pendingDelete === item.id;
          return (
            <View style={styles.row}>
              <Pressable
                style={styles.rowPrimary}
                onPress={() => applySaved(item)}
                accessibilityRole="button"
                accessibilityLabel={`Run saved search ${item.name}`}
              >
                <Text style={styles.name} numberOfLines={1}>
                  {item.alertsEnabled ? '🔔 ' : ''}
                  {item.name}
                </Text>
                {summary ? (
                  <Text style={styles.summary} numberOfLines={2}>
                    {summary}
                  </Text>
                ) : (
                  // TODO(i18n): saved.noFilters
                  <Text style={styles.summary}>No filters</Text>
                )}
              </Pressable>
              <View style={styles.actions}>
                <Pressable
                  onPress={() => applySaved(item)}
                  accessibilityRole="button"
                  // TODO(i18n): saved.run
                  accessibilityLabel="Re-run search"
                >
                  <Text style={styles.searchAction}>Search</Text>
                </Pressable>
                <Pressable
                  onPress={() => toggleAlerts(item)}
                  disabled={alertBusy === item.id}
                  accessibilityRole="switch"
                  accessibilityState={{ checked: item.alertsEnabled }}
                  // TODO(i18n): saved.alertsOn / saved.alertsOff
                  accessibilityLabel={item.alertsEnabled ? 'Disable alerts' : 'Enable alerts'}
                >
                  <Text style={styles.bellAction}>{item.alertsEnabled ? '🔔' : '🔕'}</Text>
                </Pressable>
                {confirming ? (
                  <>
                    <Pressable
                      onPress={() => remove(item)}
                      accessibilityRole="button"
                      // TODO(i18n): saved.confirmDelete
                      accessibilityLabel="Confirm delete"
                    >
                      <Text style={styles.confirmAction}>Sure?</Text>
                    </Pressable>
                    <Pressable
                      onPress={() => setPendingDelete(null)}
                      accessibilityRole="button"
                      // TODO(i18n): common.cancel
                      accessibilityLabel="Cancel delete"
                    >
                      <Text style={styles.cancelAction}>Cancel</Text>
                    </Pressable>
                  </>
                ) : (
                  <Pressable
                    onPress={() => setPendingDelete(item.id)}
                    accessibilityRole="button"
                    // TODO(i18n): saved.delete
                    accessibilityLabel="Delete saved search"
                  >
                    <Text style={styles.deleteAction}>Delete</Text>
                  </Pressable>
                )}
              </View>
            </View>
          );
        }}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff', padding: 12 },
  center: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    gap: 12,
    backgroundColor: '#fff',
    padding: 24,
  },
  hint: { color: '#717171', marginBottom: 12 },
  error: { color: '#c0392b', marginBottom: 8 },
  empty: { color: '#717171', textAlign: 'center', marginTop: 24 },
  meta: { color: '#717171', textAlign: 'center' },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, paddingHorizontal: 20, paddingVertical: 12 },
  btnText: { color: '#fff', fontWeight: '700' },
  row: { borderBottomWidth: 1, borderColor: '#eee', paddingVertical: 12 },
  rowPrimary: { paddingVertical: 4 },
  name: { fontWeight: '700', color: '#222', fontSize: 16 },
  summary: { color: '#717171', marginTop: 4 },
  actions: { flexDirection: 'row', alignItems: 'center', gap: 16, marginTop: 10, flexWrap: 'wrap' },
  searchAction: { color: '#ff385c', fontWeight: '700' },
  bellAction: { fontSize: 18 },
  deleteAction: { color: '#c0392b', fontWeight: '600' },
  confirmAction: { color: '#c0392b', fontWeight: '700' },
  cancelAction: { color: '#717171', fontWeight: '600' },
});
