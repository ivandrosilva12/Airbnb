import { useEffect, useState, useLayoutEffect } from 'react';
import { View, Text, TextInput, Pressable, Image, ScrollView, StyleSheet, ActivityIndicator, Alert } from 'react-native';
import * as ImagePicker from 'expo-image-picker';
import { useApi } from '../api/useApi';

// TODO(i18n): translate — wrap hardcoded labels via useT() from ../i18n/I18nContext.
// Key namespace mirrors the web (create.*, edit.*, photos.*).

const TYPES = ['apartment', 'house', 'room', 'villa', 'cabin'];
const POLICIES = ['flexible', 'moderate', 'strict'];

const emptyForm = {
  title: '', description: '', type: 'apartment', addressLine1: '', city: '', country: '',
  postalCode: '', latitude: '', longitude: '', price: '', cleaningFee: '', currency: 'EUR',
  maxGuests: '2', bedrooms: '1', beds: '1', bathrooms: '1', cancellationPolicy: 'moderate',
  instantBook: false,
  minNights: '1', maxNights: '', guestsIncluded: '', extraGuestFee: '', securityDeposit: '',
  weekendPrice: '',
};

// HostListingFormScreen creates a new listing or edits an existing one, then
// manages its photos and publishes it. Editing only exposes the fields the
// backend's PATCH accepts (type/address/rooms are fixed once created).
export default function HostListingFormScreen({ route, navigation }) {
  const editingId = route.params?.id || null;
  const api = useApi();
  const [form, setForm] = useState(emptyForm);
  const [propId, setPropId] = useState(editingId);
  const [photos, setPhotos] = useState([]);
  const [status, setStatus] = useState('draft');
  const [loading, setLoading] = useState(!!editingId);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  const [message, setMessage] = useState(null);

  useLayoutEffect(() => {
    navigation.setOptions({ title: editingId ? 'Edit listing' : 'New listing' });
  }, [navigation, editingId]);

  useEffect(() => {
    if (!editingId) return;
    api.getProperty(editingId)
      .then((p) => {
        setForm({
          ...emptyForm,
          title: p.title || '', description: p.description || '', type: p.type,
          city: p.address.city, country: p.address.country,
          price: (p.pricePerNight.amountCents / 100).toString(),
          cleaningFee: (p.cleaningFee.amountCents / 100).toString(),
          currency: p.pricePerNight.currency, maxGuests: String(p.maxGuests),
          cancellationPolicy: p.cancellationPolicy, instantBook: p.instantBook,
          minNights: String(p.minNights || 1),
          maxNights: p.maxNights ? String(p.maxNights) : '',
          guestsIncluded: p.guestsIncluded ? String(p.guestsIncluded) : '',
          extraGuestFee: ((p.extraGuestFee?.amountCents || 0) / 100).toString(),
          securityDeposit: ((p.securityDeposit?.amountCents || 0) / 100).toString(),
          weekendPrice: ((p.weekendPriceCents || 0) / 100).toString(),
        });
        setPhotos(p.photos || []);
        setStatus(p.status);
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, [editingId]);

  function set(k, v) {
    setForm((f) => ({ ...f, [k]: v }));
  }

  async function save() {
    setError(null);
    setMessage(null);
    if (!form.title.trim() || !form.price) {
      setError('Title and price are required.');
      return;
    }
    setSaving(true);
    try {
      const priceCents = Math.round(Number(form.price) * 100);
      const cleaningFeeCents = Math.round(Number(form.cleaningFee || 0) * 100);
      const weekendPriceCents = Math.round(Number(form.weekendPrice || 0) * 100);
      const stayRules = {
        minNights: Number(form.minNights) || 1,
        maxNights: Number(form.maxNights) || 0,
        guestsIncluded: Number(form.guestsIncluded) || 0,
        extraGuestFeeCents: Math.round(Number(form.extraGuestFee || 0) * 100),
        securityDepositCents: Math.round(Number(form.securityDeposit || 0) * 100),
      };
      if (propId) {
        const p = await api.updateProperty(propId, {
          title: form.title.trim(), description: form.description, priceCents,
          cleaningFeeCents, currency: form.currency.toUpperCase(), maxGuests: Number(form.maxGuests) || 1,
          cancellationPolicy: form.cancellationPolicy, weeklyDiscountPct: 0, monthlyDiscountPct: 0,
          taxRatePct: 0, weekendPriceCents, instantBook: form.instantBook, ...stayRules,
        });
        setStatus(p.status);
        setMessage('Listing updated.');
      } else {
        const p = await api.createProperty({
          title: form.title.trim(), description: form.description, type: form.type,
          addressLine1: form.addressLine1, city: form.city.trim(), country: form.country.trim(),
          postalCode: form.postalCode, latitude: Number(form.latitude) || 0, longitude: Number(form.longitude) || 0,
          priceCents, cleaningFeeCents, currency: form.currency.toUpperCase(),
          maxGuests: Number(form.maxGuests) || 1, bedrooms: Number(form.bedrooms) || 0,
          beds: Number(form.beds) || 0, bathrooms: Number(form.bathrooms) || 0, amenities: [],
          cancellationPolicy: form.cancellationPolicy, weeklyDiscountPct: 0, monthlyDiscountPct: 0,
          taxRatePct: 0, weekendPriceCents, instantBook: form.instantBook, ...stayRules,
        });
        setPropId(p.id);
        setStatus(p.status);
        setMessage('Listing created — add a photo, then publish.');
      }
    } catch (e) {
      setError(e.message);
    } finally {
      setSaving(false);
    }
  }

  async function addPhoto() {
    setError(null);
    const perm = await ImagePicker.requestMediaLibraryPermissionsAsync();
    if (!perm.granted) {
      setError('Photo library permission is required.');
      return;
    }
    const res = await ImagePicker.launchImageLibraryAsync({ mediaTypes: ImagePicker.MediaTypeOptions.Images, quality: 0.7 });
    if (res.canceled || !res.assets?.length) return;
    const asset = res.assets[0];
    setSaving(true);
    try {
      const updated = await api.uploadPhoto(propId, {
        uri: asset.uri,
        name: asset.fileName || 'photo.jpg',
        type: asset.mimeType || 'image/jpeg',
      });
      setPhotos(updated.photos || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setSaving(false);
    }
  }

  async function removePhoto(photoId) {
    setError(null);
    try {
      const updated = await api.deletePhoto(propId, photoId);
      setPhotos(updated.photos || []);
    } catch (e) {
      setError(e.message);
    }
  }

  // movePhoto reorders a single photo by delta (-1 = up/left, +1 =
  // down/right). The first slot is the listing's cover, so moving a
  // photo into index 0 promotes it. Mirrors the web's HostPhotos page
  // (S60), minus the drag-and-drop affordance — RN doesn't have a
  // built-in DnD and we're not adding a new dep for this PR.
  // S95 mobile parity: PATCH /properties/:id/photos/order with the new
  // id list; the backend returns the full property so the photo grid
  // refresh is one round-trip.
  async function movePhoto(index, delta) {
    const target = index + delta;
    if (target < 0 || target >= photos.length) return;
    const ids = photos.map((p) => p.id);
    [ids[index], ids[target]] = [ids[target], ids[index]];
    setError(null);
    try {
      const updated = await api.reorderPhotos(propId, ids);
      setPhotos(updated.photos || []);
    } catch (e) {
      setError(e.message);
    }
  }

  // makeCover lifts a photo into the first slot in one call. The user
  // could chain "up" arrow taps, but a single-tap shortcut matches the
  // web ("Make cover" button) and avoids N reorder round-trips.
  async function makeCover(photoId) {
    const ids = photos.map((p) => p.id);
    const reordered = [photoId, ...ids.filter((x) => x !== photoId)];
    setError(null);
    try {
      const updated = await api.reorderPhotos(propId, reordered);
      setPhotos(updated.photos || []);
    } catch (e) {
      setError(e.message);
    }
  }

  async function publish() {
    setError(null);
    setMessage(null);
    try {
      await api.publishProperty(propId);
      setStatus('published');
      setMessage('Listing published.');
    } catch (e) {
      setError(e.message);
    }
  }

  function confirmDelete() {
    Alert.alert('Delete listing', 'This permanently removes the listing.', [
      { text: 'Cancel', style: 'cancel' },
      {
        text: 'Delete', style: 'destructive',
        onPress: async () => {
          try {
            await api.deleteProperty(propId);
            navigation.goBack();
          } catch (e) {
            setError(e.message);
          }
        },
      },
    ]);
  }

  if (loading) return <ActivityIndicator style={{ flex: 1 }} color="#ff385c" />;

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ padding: 16 }}>
      {error && <Text style={styles.error}>{error}</Text>}
      {message && <Text style={styles.success}>{message}</Text>}

      <Field label="Title" value={form.title} onChangeText={(v) => set('title', v)} />
      <Field label="Description" value={form.description} onChangeText={(v) => set('description', v)} multiline />

      {!propId && (
        <>
          <Text style={styles.label}>Type</Text>
          <Chips options={TYPES} value={form.type} onChange={(v) => set('type', v)} />
          <Field label="City" value={form.city} onChangeText={(v) => set('city', v)} />
          <Field label="Country (2-letter)" value={form.country} onChangeText={(v) => set('country', v)} />
          <Field label="Address" value={form.addressLine1} onChangeText={(v) => set('addressLine1', v)} />
          <Row>
            <Field flex label="Bedrooms" value={form.bedrooms} onChangeText={(v) => set('bedrooms', v)} keyboardType="number-pad" />
            <Field flex label="Beds" value={form.beds} onChangeText={(v) => set('beds', v)} keyboardType="number-pad" />
            <Field flex label="Bathrooms" value={form.bathrooms} onChangeText={(v) => set('bathrooms', v)} keyboardType="number-pad" />
          </Row>
        </>
      )}

      <Row>
        <Field flex label={`Price/night (${form.currency})`} value={form.price} onChangeText={(v) => set('price', v)} keyboardType="decimal-pad" />
        <Field flex label="Cleaning fee" value={form.cleaningFee} onChangeText={(v) => set('cleaningFee', v)} keyboardType="decimal-pad" />
      </Row>
      <Row>
        <Field flex label="Currency" value={form.currency} onChangeText={(v) => set('currency', v)} />
        <Field flex label="Max guests" value={form.maxGuests} onChangeText={(v) => set('maxGuests', v)} keyboardType="number-pad" />
      </Row>

      <Text style={styles.label}>Cancellation policy</Text>
      <Chips options={POLICIES} value={form.cancellationPolicy} onChange={(v) => set('cancellationPolicy', v)} />

      <Text style={[styles.sectionTitle, { marginTop: 18 }]}>Stay rules & fees</Text>
      <Row>
        <Field flex label="Min nights" value={form.minNights} onChangeText={(v) => set('minNights', v)} keyboardType="number-pad" />
        <Field flex label="Max nights (0 = none)" value={form.maxNights} onChangeText={(v) => set('maxNights', v)} keyboardType="number-pad" />
      </Row>
      <Field label="Guests included in price" value={form.guestsIncluded} onChangeText={(v) => set('guestsIncluded', v)} keyboardType="number-pad" />
      <Row>
        <Field flex label={`Extra-guest fee/night (${form.currency})`} value={form.extraGuestFee} onChangeText={(v) => set('extraGuestFee', v)} keyboardType="decimal-pad" />
        <Field flex label={`Security deposit (${form.currency})`} value={form.securityDeposit} onChangeText={(v) => set('securityDeposit', v)} keyboardType="decimal-pad" />
      </Row>
      <Field label={`Weekend price Fri/Sat (${form.currency}) — 0 to disable`} value={form.weekendPrice} onChangeText={(v) => set('weekendPrice', v)} keyboardType="decimal-pad" />

      <Pressable style={styles.toggleRow} onPress={() => set('instantBook', !form.instantBook)}>
        <Text style={styles.toggleBox}>{form.instantBook ? '☑' : '☐'}</Text>
        <Text style={styles.toggleLabel}>Instant Book (confirm reservations automatically)</Text>
      </Pressable>

      <Pressable style={styles.btn} onPress={save} disabled={saving}>
        <Text style={styles.btnText}>{saving ? 'Saving…' : propId ? 'Save changes' : 'Create listing'}</Text>
      </Pressable>

      {propId && (
        <View style={styles.photoSection}>
          {/* House rules editor — S95 mobile parity with web S56. Lives
              under the listing form so the host has one entry point for
              everything that hangs off a listing. */}
          <Pressable
            style={styles.linkRow}
            onPress={() => navigation.navigate('HostHouseRules', { propertyId: propId })}
          >
            <Text style={styles.linkIcon}>📜</Text>
            <Text style={styles.linkLabel}>House rules</Text>
            <Text style={styles.linkChevron}>›</Text>
          </Pressable>

          {/* Co-host editor — S104 mobile parity with web's CohostsPanel.
              Sits next to House rules so all per-listing sub-editors live
              in one place. Title stays hardcoded here (the underlying
              screen translates via t('cohosts.title')) to match the
              "House rules" row's untranslated label and avoid touching
              the surrounding form's i18n posture (it carries a
              TODO(i18n) sweep at the file top). */}
          <Pressable
            style={styles.linkRow}
            onPress={() => navigation.navigate('HostCohosts', { propertyId: propId })}
          >
            <Text style={styles.linkIcon}>👥</Text>
            <Text style={styles.linkLabel}>Co-hosts</Text>
            <Text style={styles.linkChevron}>›</Text>
          </Pressable>

          <Text style={styles.sectionTitle}>Photos</Text>
          {photos.length > 1 && (
            <Text style={styles.meta}>The first photo is the cover. Use ← → to reorder.</Text>
          )}
          <View style={styles.photoGrid}>
            {photos.map((ph, i) => (
              <View key={ph.id} style={styles.photoItem}>
                <Image source={{ uri: ph.url }} style={styles.photo} />
                {i === 0 && (
                  <View style={styles.coverBadge}>
                    <Text style={styles.coverBadgeText}>Cover</Text>
                  </View>
                )}
                <Pressable style={styles.photoDelete} onPress={() => removePhoto(ph.id)}>
                  <Text style={styles.photoDeleteText}>✕</Text>
                </Pressable>
                {photos.length > 1 && (
                  <View style={styles.photoMoveRow}>
                    <Pressable
                      style={[styles.moveBtn, i === 0 && styles.moveBtnDisabled]}
                      disabled={i === 0 || saving}
                      onPress={() => movePhoto(i, -1)}
                      accessibilityLabel="Move photo left"
                    >
                      <Text style={styles.moveBtnText}>←</Text>
                    </Pressable>
                    {i !== 0 && (
                      <Pressable
                        style={styles.coverBtn}
                        disabled={saving}
                        onPress={() => makeCover(ph.id)}
                        accessibilityLabel="Make cover"
                      >
                        <Text style={styles.coverBtnText}>★</Text>
                      </Pressable>
                    )}
                    <Pressable
                      style={[styles.moveBtn, i === photos.length - 1 && styles.moveBtnDisabled]}
                      disabled={i === photos.length - 1 || saving}
                      onPress={() => movePhoto(i, 1)}
                      accessibilityLabel="Move photo right"
                    >
                      <Text style={styles.moveBtnText}>→</Text>
                    </Pressable>
                  </View>
                )}
              </View>
            ))}
          </View>
          <Pressable style={styles.btnGhost} onPress={addPhoto} disabled={saving}>
            <Text style={styles.btnGhostText}>+ Add photo</Text>
          </Pressable>

          <View style={styles.statusRow}>
            <Text style={styles.meta}>Status: {status}</Text>
            {status !== 'published' && (
              <Pressable style={styles.btn} onPress={publish}>
                <Text style={styles.btnText}>Publish</Text>
              </Pressable>
            )}
          </View>

          <Pressable onPress={confirmDelete}>
            <Text style={styles.deleteLink}>Delete listing</Text>
          </Pressable>
        </View>
      )}
    </ScrollView>
  );
}

function Field({ label, flex, style, ...props }) {
  return (
    <View style={[flex && { flex: 1 }]}>
      <Text style={styles.label}>{label}</Text>
      <TextInput style={[styles.input, props.multiline && styles.inputMultiline, style]} placeholderTextColor="#999" {...props} />
    </View>
  );
}

function Row({ children }) {
  return <View style={styles.row}>{children}</View>;
}

function Chips({ options, value, onChange }) {
  return (
    <View style={styles.chips}>
      {options.map((o) => (
        <Pressable key={o} style={[styles.chip, value === o && styles.chipOn]} onPress={() => onChange(o)}>
          <Text style={value === o ? styles.chipTextOn : styles.chipText}>{o}</Text>
        </Pressable>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff' },
  label: { fontSize: 13, color: '#717171', marginBottom: 4, marginTop: 8 },
  input: { borderWidth: 1, borderColor: '#ddd', borderRadius: 8, padding: 10 },
  inputMultiline: { height: 80, textAlignVertical: 'top' },
  row: { flexDirection: 'row', gap: 10 },
  chips: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  chip: { borderWidth: 1, borderColor: '#ddd', borderRadius: 999, paddingHorizontal: 12, paddingVertical: 6 },
  chipOn: { backgroundColor: '#ff385c', borderColor: '#ff385c' },
  chipText: { color: '#222', fontSize: 13 },
  chipTextOn: { color: '#fff', fontWeight: '700', fontSize: 13 },
  toggleRow: { flexDirection: 'row', alignItems: 'center', gap: 8, marginTop: 14 },
  toggleBox: { fontSize: 20 },
  toggleLabel: { flex: 1, color: '#222' },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, padding: 14, alignItems: 'center', marginTop: 16 },
  btnText: { color: '#fff', fontWeight: '700' },
  btnGhost: { borderWidth: 1, borderColor: '#ddd', borderRadius: 8, padding: 12, alignItems: 'center', marginTop: 10 },
  btnGhostText: { color: '#222', fontWeight: '700' },
  photoSection: { marginTop: 24, borderTopWidth: 1, borderColor: '#eee', paddingTop: 16 },
  sectionTitle: { fontSize: 16, fontWeight: '800', marginBottom: 8 },
  photoGrid: { flexDirection: 'row', flexWrap: 'wrap', gap: 12, marginTop: 8 },
  photoItem: { position: 'relative', width: 100 },
  photo: { width: 100, height: 100, borderRadius: 8 },
  photoDelete: { position: 'absolute', top: 4, right: 4, backgroundColor: 'rgba(0,0,0,0.6)', borderRadius: 999, width: 24, height: 24, alignItems: 'center', justifyContent: 'center' },
  photoDeleteText: { color: '#fff', fontWeight: '700' },
  // Photo reorder controls — left/right arrows live under each photo
  // tile so a host can step a tile through positions without leaving
  // the listing form. The ★ button promotes a photo to cover in one tap.
  photoMoveRow: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginTop: 4 },
  moveBtn: { backgroundColor: '#f7f7f7', borderRadius: 4, paddingHorizontal: 8, paddingVertical: 4, minWidth: 28, alignItems: 'center' },
  moveBtnDisabled: { opacity: 0.3 },
  moveBtnText: { color: '#222', fontWeight: '700', fontSize: 14 },
  coverBtn: { backgroundColor: '#fff7e6', borderRadius: 4, paddingHorizontal: 6, paddingVertical: 4, minWidth: 24, alignItems: 'center', borderWidth: 1, borderColor: '#f0c674' },
  coverBtnText: { color: '#b8860b', fontWeight: '700', fontSize: 12 },
  coverBadge: { position: 'absolute', top: 4, left: 4, backgroundColor: 'rgba(0,0,0,0.7)', borderRadius: 4, paddingHorizontal: 6, paddingVertical: 2 },
  coverBadgeText: { color: '#fff', fontSize: 10, fontWeight: '700', textTransform: 'uppercase', letterSpacing: 0.5 },
  // Inline link row — used for "House rules" nav under the photo section
  // (matches the AccountScreen row affordance for familiarity).
  linkRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 14, borderBottomWidth: 1, borderColor: '#eee', marginBottom: 12 },
  linkIcon: { fontSize: 18, width: 32 },
  linkLabel: { flex: 1, fontSize: 16, fontWeight: '600' },
  linkChevron: { fontSize: 22, color: '#bbb' },
  statusRow: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginTop: 16 },
  meta: { color: '#717171' },
  deleteLink: { color: '#c0392b', textDecorationLine: 'underline', marginTop: 20, textAlign: 'center' },
  success: { color: '#1a7f47', marginBottom: 8 },
  error: { color: '#c0392b', marginBottom: 8 },
});
